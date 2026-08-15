// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package remediation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/pool"
	"go.uber.org/zap"
)

// defaultExecutionPoolSize is the worker count used when no pool size is
// configured. It bounds concurrent remediation executions so a burst of
// triggers cannot spawn unbounded goroutines.
const defaultExecutionPoolSize = 4

// Manager is the remediation decision and dispatch engine. It subscribes
// to telemetry alert events on the event bus, evaluates registered Rules
// against each event, and dispatches matching rules to the registered
// Operator through a bounded worker pool.
//
// Three safety mechanisms gate dispatch (see package doc): idempotency,
// cooldown, and circuit breaker. The default deployment ships only the
// LocalOperator; callers may register additional operators via
// WithOperators / RegisterOperator.
type Manager struct {
	bus       event.Bus
	store     RuleStore
	records   RecordStore
	logger    *zap.Logger
	operators map[string]Operator

	// operatorsMu protects the operators map so concurrent
	// RegisterOperator writes and dispatch reads are safe.
	operatorsMu sync.RWMutex

	execPool  pool.Pool
	poolOwned bool

	// matchCache caches compiled condition programs keyed by
	// "<ruleID>:<expr>" so a rule's expression is compiled at most once
	// per distinct expression. A zero expression (match-all) is not cached.
	matchMu    sync.Mutex
	matchCache map[string]*vm.Program

	// inFlight tracks (ruleID:assetID) keys currently executing so a
	// flapping trigger cannot stack duplicate executions.
	inFlightMu sync.Mutex
	inFlight   map[string]struct{}

	startMu sync.Mutex
	started bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	subs    []event.Subscription
}

// Option configures a Manager.
type Option interface {
	apply(*options)
}

type options struct {
	bus       event.Bus
	store     RuleStore
	records   RecordStore
	logger    *zap.Logger
	operators []Operator
	poolSize  int
	execPool  pool.Pool
}

type funcOption func(*options)

func (f funcOption) apply(o *options) { f(o) }

// WithEventBus sets the event bus used to subscribe to alert events.
func WithEventBus(bus event.Bus) Option {
	return funcOption(func(o *options) { o.bus = bus })
}

// WithStore sets the rule store used to load and update remediation rules.
func WithStore(store RuleStore) Option {
	return funcOption(func(o *options) { o.store = store })
}

// WithRecordStore sets the store used to persist remediation dispatch
// records. When set, every dispatch lifecycle transition (triggered,
// started, completed, failed, skipped) is persisted for the records API.
func WithRecordStore(store RecordStore) Option {
	return funcOption(func(o *options) { o.records = store })
}

// WithLogger sets the structured logger.
func WithLogger(logger *zap.Logger) Option {
	return funcOption(func(o *options) { o.logger = logger })
}

// WithOperators registers operators in addition to the default LocalOperator.
// callers may use this to inject remote operators (ssh, mysql, ...).
func WithOperators(ops ...Operator) Option {
	return funcOption(func(o *options) { o.operators = append(o.operators, ops...) })
}

// WithExecutionPoolSize sets the worker pool size bounding concurrent
// remediation executions. A non-positive value defaults to 4. Ignored when
// WithPool injects an externally-owned pool.
func WithExecutionPoolSize(n int) Option {
	return funcOption(func(o *options) { o.poolSize = n })
}

// WithPool injects an externally-owned worker pool for remediation
// execution. When set, the manager does not create or shut down its own
// pool; the caller is responsible for the pool lifecycle.
func WithPool(p pool.Pool) Option {
	return funcOption(func(o *options) { o.execPool = p })
}

// New creates a new remediation Manager with the given options.
//
// The LocalOperator is registered by default; callers can override it by
// registering an operator named "local" via WithOperators. When no execution
// pool is injected, the manager creates and owns a bounded pool sized by
// WithExecutionPoolSize (default 4).
func New(opts ...Option) (*Manager, error) {
	o := &options{
		logger:   zap.NewNop(),
		poolSize: defaultExecutionPoolSize,
	}
	for _, opt := range opts {
		opt.apply(o)
	}
	if o.store == nil {
		return nil, fmt.Errorf("remediation: %w: rule store is required", errdefs.ErrInvalidArgument)
	}

	m := &Manager{
		bus:        o.bus,
		store:      o.store,
		records:    o.records,
		logger:     o.logger,
		operators:  map[string]Operator{},
		matchCache: map[string]*vm.Program{},
		inFlight:   map[string]struct{}{},
	}
	// Register the default local operator unless the caller supplied one.
	hasLocal := false
	for _, op := range o.operators {
		if op != nil {
			m.operators[op.Name()] = op
			if op.Name() == "local" {
				hasLocal = true
			}
		}
	}
	if !hasLocal {
		m.operators["local"] = NewLocalOperator(nil, WithOperatorLogger(o.logger))
	}

	if o.execPool != nil {
		m.execPool = o.execPool
		m.poolOwned = false
	} else {
		size := o.poolSize
		if size <= 0 {
			size = defaultExecutionPoolSize
		}
		p, err := pool.New(
			pool.WithWorkers(size),
			pool.WithRejectionPolicy(pool.RejectionCallerRuns),
		)
		if err != nil {
			return nil, fmt.Errorf("remediation: create execution pool: %w", err)
		}
		m.execPool = p
		m.poolOwned = true
	}
	return m, nil
}

// RegisterOperator registers (or overrides) an operator keyed by its Name.
// It must be called before Start.
func (m *Manager) RegisterOperator(op Operator) {
	if op == nil {
		return
	}
	m.operatorsMu.Lock()
	defer m.operatorsMu.Unlock()
	m.operators[op.Name()] = op
}

// Start subscribes to telemetry alert events on the event bus. It returns an
// error if the manager is already started or no event bus is configured.
func (m *Manager) Start(ctx context.Context) error {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	if m.started {
		return nil
	}
	if m.bus == nil {
		return errdefs.ErrBusNotConfigured
	}

	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.started = true

	// Metric threshold breaches -> metric trigger.
	sub, err := event.Subscribe[event.MetricExceededPayload](m.bus, event.TypeTelemetryMetricExceeded, func(_ context.Context, ev event.Event[event.MetricExceededPayload]) error {
		m.handle(runCtx, metricPayloadToContext(ev))
		return nil
	})
	if err != nil {
		m.started = false
		return fmt.Errorf("remediation: subscribe to metric exceeded events: %w", err)
	}
	m.subs = append(m.subs, sub)

	// Log keyword matches -> log trigger.
	sub, err = event.Subscribe[event.LogMatchedPayload](m.bus, event.TypeTelemetryLogMatched, func(_ context.Context, ev event.Event[event.LogMatchedPayload]) error {
		m.handle(runCtx, logPayloadToContext(ev))
		return nil
	})
	if err != nil {
		m.started = false
		return fmt.Errorf("remediation: subscribe to log matched events: %w", err)
	}
	m.subs = append(m.subs, sub)

	// Asset status transitions -> status_change trigger.
	sub, err = event.Subscribe[event.StatusChangePayload](m.bus, event.TypeAssetStatusChanged, func(_ context.Context, ev event.Event[event.StatusChangePayload]) error {
		m.handle(runCtx, statusPayloadToContext(ev))
		return nil
	})
	if err != nil {
		m.started = false
		return fmt.Errorf("remediation: subscribe to status change events: %w", err)
	}
	m.subs = append(m.subs, sub)

	m.logger.Info("prism remediation engine started",
		zap.Int("operators", len(m.operators)),
	)
	return nil
}

// Stop gracefully shuts down the manager: it cancels event subscriptions and
// waits for in-flight executions to finish. It is idempotent.
func (m *Manager) Stop(ctx context.Context) error {
	m.startMu.Lock()
	if !m.started {
		m.startMu.Unlock()
		return nil
	}
	m.started = false
	cancel := m.cancel
	m.startMu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, s := range m.subs {
		s.Cancel()
	}
	m.subs = nil
	m.wg.Wait()
	if m.poolOwned {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = m.execPool.Shutdown(shutdownCtx) // best-effort pool shutdown on stop path, error not actionable
	}
	return nil
}

// handle is the decision core. It loads matching rules from the store and
// dispatches each matching rule to its operator through the worker pool,
// gated by idempotency, cooldown, and the circuit breaker.
func (m *Manager) handle(ctx context.Context, ec EventContext) {
	rules, err := m.store.GetRules(ctx, ec.TenantID, ec.AssetID, ec.Type)
	if err != nil {
		m.logger.Warn("remediation: load rules failed",
			zap.String("trigger", ec.Type),
			zap.Int64("asset_id", ec.AssetID),
			zap.Error(err),
		)
		return
	}
	for _, r := range rules {
		r := r
		if !r.Enabled || r.Status != string(StatusActive) {
			continue
		}
		if !m.matchCondition(ctx, r, ec) {
			continue
		}
		if skip := m.checkGates(ctx, r, ec); skip {
			continue
		}
		m.dispatch(ctx, r, ec)
	}
}

// saveRecord persists a dispatch lifecycle transition. Persistence failures
// are logged but never block the engine: an unrecorded transition must not
// suppress a remediation execution.
func (m *Manager) saveRecord(rec *Record) {
	if m.records == nil || rec == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.records.UpsertRecord(ctx, rec); err != nil {
		m.logger.Warn("remediation: persist record failed",
			zap.String("run_id", rec.RunID),
			zap.String("status", rec.Status),
			zap.Error(err),
		)
	}
}

// newRecord builds the base Record for a dispatch run at its first
// (triggered) transition.
func newRecord(r *Rule, ec EventContext, runID string) *Record {
	return &Record{
		RuleID:   r.ID,
		RuleName: r.Name,
		AssetID:  ec.AssetID,
		AssetKey: ec.AssetKey,
		RunID:    runID,
		Trigger:  ec.Type,
		Status:   RecordStatusTriggered,
	}
}

// recordSkip persists a skipped dispatch with the given reason and publishes
// the corresponding RemediationSkipped event.
func (m *Manager) recordSkip(ctx context.Context, r *Rule, ec EventContext, reason string) {
	m.publish(ctx, event.TypeRemediationSkipped, skipPayload(r, ec, reason))
	rec := newRecord(r, ec, newRunID())
	rec.Status = RecordStatusSkipped
	rec.Error = reason
	m.saveRecord(rec)
}

// checkGates evaluates the idempotency, cooldown, and circuit-breaker gates
// for a rule. It returns true when the rule should be skipped (and records
// the skip reason via a RemediationSkipped event).
func (m *Manager) checkGates(ctx context.Context, r *Rule, ec EventContext) (skip bool) {
	key := inFlightKey(r.ID, ec.AssetID)

	// Idempotency: skip if an execution for this (rule, asset) is in flight.
	m.inFlightMu.Lock()
	if _, ok := m.inFlight[key]; ok {
		m.inFlightMu.Unlock()
		m.recordSkip(ctx, r, ec, "idempotency: execution in flight")
		return true
	}
	m.inFlightMu.Unlock()

	// Cooldown: skip if the last run is still within the cooldown window.
	if r.LastRunAt != nil && r.Cooldown > 0 {
		elapsed := time.Since(*r.LastRunAt)
		if elapsed < time.Duration(r.Cooldown)*time.Second {
			m.recordSkip(ctx, r, ec, "cooldown")
			return true
		}
	}

	// Circuit breaker: skip if consecutive failures have reached the
	// threshold. The rule is marked paused so it is not re-evaluated until
	// a human resumes it.
	md := parseMetadata(r.Metadata)
	if r.CircuitBreakerThreshold > 0 && md.ConsecutiveFailures >= r.CircuitBreakerThreshold {
		if r.Status != string(StatusPaused) {
			if err := m.store.UpdateRuleStatus(ctx, r.ID, string(StatusPaused), md.serialize()); err != nil {
				m.logger.Warn("remediation: pause rule failed",
					zap.Int64("rule_id", r.ID),
					zap.Error(err),
				)
			}
		}
		m.recordSkip(ctx, r, ec, "circuit breaker tripped")
		return true
	}
	return false
}

// skipPayload builds a RunPayload for a skipped remediation carrying the
// skip reason.
func skipPayload(r *Rule, ec EventContext, reason string) RunPayload {
	p := newPayload(r, ec, "")
	p.Reason = reason
	return p
}

// dispatch marks the (rule, asset) in-flight and submits the execution to the
// worker pool. On completion it updates the rule's last-run timestamp and
// circuit-breaker state.
func (m *Manager) dispatch(ctx context.Context, r *Rule, ec EventContext) {
	m.operatorsMu.RLock()
	op, ok := m.operators[r.ExecutorType]
	m.operatorsMu.RUnlock()
	if !ok {
		m.logger.Warn("remediation: operator not registered",
			zap.String("executor_type", r.ExecutorType),
			zap.Int64("rule_id", r.ID),
		)
		m.recordSkip(ctx, r, ec, "operator not registered: "+r.ExecutorType)
		return
	}
	key := inFlightKey(r.ID, ec.AssetID)
	runID := newRunID()

	m.inFlightMu.Lock()
	m.inFlight[key] = struct{}{}
	m.inFlightMu.Unlock()

	m.publish(ctx, event.TypeRemediationTriggered, newPayload(r, ec, runID))
	m.saveRecord(newRecord(r, ec, runID))

	m.wg.Add(1)
	job := pool.Lambda(func(jobCtx context.Context) error {
		defer m.wg.Done()
		defer m.releaseInFlight(key)
		m.publish(jobCtx, event.TypeRemediationStarted, newPayload(r, ec, runID))

		startedAt := time.Now()
		startedRec := newRecord(r, ec, runID)
		startedRec.Status = RecordStatusStarted
		startedRec.StartedAt = &startedAt
		m.saveRecord(startedRec)

		res, err := op.Execute(jobCtx, ExecutionRequest{
			RuleID:   r.ID,
			RuleName: r.Name,
			TenantID: ec.TenantID,
			AssetID:  ec.AssetID,
			RunID:    runID,
			Config:   r.ExecutorConfig,
		})
		now := time.Now()
		if uerr := m.store.UpdateLastRun(jobCtx, r.ID, now); uerr != nil {
			m.logger.Warn("remediation: update last run failed",
				zap.Int64("rule_id", r.ID),
				zap.Error(uerr),
			)
		}
		m.applyCircuitBreaker(jobCtx, r, res, err)

		completed := newPayload(r, ec, runID)
		completed.Success = err == nil && res != nil && res.Success
		if res != nil {
			completed.DurationMs = res.Duration.Milliseconds()
			completed.ErrorMsg = res.ErrorMsg
		}
		if err != nil {
			completed.ErrorMsg = err.Error()
		}
		m.publish(jobCtx, event.TypeRemediationCompleted, completed)

		finishedRec := newRecord(r, ec, runID)
		finishedRec.StartedAt = &startedAt
		finishedRec.FinishedAt = &now
		if completed.Success {
			finishedRec.Status = RecordStatusCompleted
		} else {
			finishedRec.Status = RecordStatusFailed
			finishedRec.Error = completed.ErrorMsg
		}
		m.saveRecord(finishedRec)
		return nil
	})
	if err := m.execPool.Submit(ctx, job); err != nil {
		m.wg.Done()
		m.releaseInFlight(key)
		m.logger.Warn("remediation: execution pool submit failed, run dropped",
			zap.Int64("rule_id", r.ID),
			zap.Error(err),
		)
		m.publish(ctx, event.TypeRemediationSkipped, skipPayload(r, ec, "execution pool full"))
		droppedRec := newRecord(r, ec, runID)
		droppedRec.Status = RecordStatusSkipped
		droppedRec.Error = "execution pool full"
		m.saveRecord(droppedRec)
	}
}

// applyCircuitBreaker updates the rule's consecutive-failure count and
// pauses the rule when the threshold is reached. Success resets the count.
func (m *Manager) applyCircuitBreaker(ctx context.Context, r *Rule, res *ExecutionResult, execErr error) {
	md := parseMetadata(r.Metadata)
	if execErr == nil && res != nil && res.Success {
		md.ConsecutiveFailures = 0
	} else {
		md.ConsecutiveFailures++
	}
	status := string(StatusActive)
	if r.CircuitBreakerThreshold > 0 && md.ConsecutiveFailures >= r.CircuitBreakerThreshold {
		status = string(StatusPaused)
		m.logger.Warn("remediation: circuit breaker tripped, rule paused",
			zap.Int64("rule_id", r.ID),
			zap.Int("consecutive_failures", md.ConsecutiveFailures),
			zap.Int("threshold", r.CircuitBreakerThreshold),
		)
	}
	if err := m.store.UpdateRuleStatus(ctx, r.ID, status, md.serialize()); err != nil {
		m.logger.Warn("remediation: update circuit breaker state failed",
			zap.Int64("rule_id", r.ID),
			zap.Error(err),
		)
	}
}

// remediationBuiltinWhitelist is the set of expr-lang builtins permitted
// inside remediation condition expressions. It mirrors the rule engine's
// builtinWhitelist so both expression surfaces share the same sandbox.
var remediationBuiltinWhitelist = []string{
	"len", "all", "any", "one", "none", "filter", "find", "count",
	"sum", "mean", "max", "min", "median",
	"keys", "values",
	"contains", "startsWith", "endsWith",
	"now", "duration",
}

// compileCondition compiles a remediation condition expression with the same
// sandbox constraints as the rule engine: a node-count cap, a hermetic
// builtin set, and the EventContext type contract. It returns a reusable
// *vm.Program safe for concurrent evaluation.
func compileCondition(expression string) (*vm.Program, error) {
	opts := make([]expr.Option, 0, len(remediationBuiltinWhitelist)+4)
	opts = append(opts,
		expr.AsBool(),
		expr.MaxNodes(1000),
		expr.DisableAllBuiltins(),
		expr.Env(EventContext{}),
	)
	for _, name := range remediationBuiltinWhitelist {
		opts = append(opts, expr.EnableBuiltin(name))
	}
	return expr.Compile(expression, opts...)
}

// matchCondition evaluates the rule's condition expression against the event
// context. An empty expression matches all events. Compilation results are
// cached per (ruleID, expression) so a rule is compiled at most once per
// distinct expression.
func (m *Manager) matchCondition(_ context.Context, r *Rule, ec EventContext) bool {
	if r.ConditionExpr == "" {
		return true
	}
	cacheKey := strconv.FormatInt(r.ID, 10) + ":" + r.ConditionExpr
	m.matchMu.Lock()
	program, ok := m.matchCache[cacheKey]
	m.matchMu.Unlock()
	if !ok {
		compiled, err := compileCondition(r.ConditionExpr)
		if err != nil {
			m.logger.Warn("remediation: compile condition failed, rule disabled",
				zap.Int64("rule_id", r.ID),
				zap.String("expr", r.ConditionExpr),
				zap.Error(err),
			)
			return false
		}
		program = compiled
		m.matchMu.Lock()
		m.matchCache[cacheKey] = program
		m.matchMu.Unlock()
	}
	output, err := expr.Run(program, ec)
	if err != nil {
		m.logger.Warn("remediation: evaluate condition failed",
			zap.Int64("rule_id", r.ID),
			zap.String("expr", r.ConditionExpr),
			zap.Error(err),
		)
		return false
	}
	result, ok := output.(bool)
	if !ok {
		m.logger.Warn("remediation: condition did not evaluate to bool",
			zap.Int64("rule_id", r.ID),
			zap.String("expr", r.ConditionExpr),
			zap.Any("result_type", output),
		)
		return false
	}
	return result
}

// releaseInFlight removes an in-flight key, tolerating concurrent release.
func (m *Manager) releaseInFlight(key string) {
	m.inFlightMu.Lock()
	delete(m.inFlight, key)
	m.inFlightMu.Unlock()
}

// publish emits a remediation lifecycle event. Publish failures are logged
// but never block the engine: an unrecorded event must not suppress a
// remediation execution.
func (m *Manager) publish(ctx context.Context, typ event.Type, payload RunPayload) {
	if m.bus == nil {
		return
	}
	if err := m.bus.Publish(ctx, typ, payload); err != nil {
		m.logger.Warn("remediation: publish event failed",
			zap.String("event_type", string(typ)),
			zap.Int64("rule_id", payload.RuleID),
			zap.Error(err),
		)
	}
}

// newPayload builds a RunPayload from a rule and event context.
func newPayload(r *Rule, ec EventContext, runID string) RunPayload {
	return RunPayload{
		RuleID:   r.ID,
		RuleName: r.Name,
		AssetID:  ec.AssetID,
		TenantID: ec.TenantID,
		RunID:    runID,
		Trigger:  ec.Type,
	}
}

// RunPayload is the event payload published for remediation lifecycle
// events (TypeRemediationTriggered/Started/Completed/Skipped).
type RunPayload struct {
	RuleID     int64  `json:"rule_id"`
	RuleName   string `json:"rule_name"`
	AssetID    int64  `json:"asset_id"`
	TenantID   int64  `json:"tenant_id"`
	RunID      string `json:"run_id"`
	Trigger    string `json:"trigger"`
	Success    bool   `json:"success"`
	Reason     string `json:"reason,omitempty"`
	ErrorMsg   string `json:"error_msg,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}

// inFlightKey builds the idempotency key for a (rule, asset) pair.
func inFlightKey(ruleID, assetID int64) string {
	return strconv.FormatInt(ruleID, 10) + ":" + strconv.FormatInt(assetID, 10)
}

// newRunID generates a unique 16-byte hex identifier for a remediation run.
func newRunID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("run-%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// ruleMetadata is the JSON structure persisted in Rule.Metadata.
type ruleMetadata struct {
	// ConsecutiveFailures is the running count of consecutive execution
	// failures consumed by the circuit breaker.
	ConsecutiveFailures int `json:"consecutive_failures"`
}

// parseMetadata decodes the rule's metadata blob, returning a zero value
// when the blob is empty or invalid.
func parseMetadata(raw string) ruleMetadata {
	if raw == "" {
		return ruleMetadata{}
	}
	var md ruleMetadata
	if err := json.Unmarshal([]byte(raw), &md); err != nil {
		return ruleMetadata{}
	}
	return md
}

// serialize encodes the metadata to its JSON blob form.
func (m ruleMetadata) serialize() string {
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// metricPayloadToContext converts a metric-exceeded event payload into an
// EventContext for rule evaluation.
func metricPayloadToContext(ev event.Event[event.MetricExceededPayload]) EventContext {
	p := ev.Payload
	assetID, _ := strconv.ParseInt(p.AssetID, 10, 64)
	tenantID, _ := strconv.ParseInt(p.TenantID, 10, 64)
	return EventContext{
		Type:        string(TriggerMetric),
		AssetID:     assetID,
		TenantID:    tenantID,
		MetricName:  p.MetricName,
		MetricValue: p.MetricValue,
		Threshold:   p.Threshold,
	}
}

// logPayloadToContext converts a log-matched event payload into an
// EventContext for rule evaluation.
func logPayloadToContext(ev event.Event[event.LogMatchedPayload]) EventContext {
	p := ev.Payload
	assetID, _ := strconv.ParseInt(p.AssetID, 10, 64)
	tenantID, _ := strconv.ParseInt(p.TenantID, 10, 64)
	return EventContext{
		Type:     string(TriggerLog),
		AssetID:  assetID,
		TenantID: tenantID,
		Level:    p.Level,
		Keyword:  p.Keyword,
		Content:  p.Content,
		SourceIP: p.SourceIP,
	}
}

// statusPayloadToContext converts an asset status-change event payload into
// an EventContext for rule evaluation.
func statusPayloadToContext(ev event.Event[event.StatusChangePayload]) EventContext {
	p := ev.Payload
	assetID, _ := strconv.ParseInt(p.AssetID, 10, 64)
	tenantID, _ := strconv.ParseInt(p.TenantID, 10, 64)
	return EventContext{
		Type:       string(TriggerStatusChange),
		AssetID:    assetID,
		AssetKey:   p.AssetKey,
		TenantID:   tenantID,
		PrevStatus: p.PrevStatus,
		CurrStatus: p.CurrStatus,
	}
}
