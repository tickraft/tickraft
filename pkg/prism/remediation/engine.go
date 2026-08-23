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

	"go.uber.org/zap"

	"github.com/bytedance/sonic"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/expr"
	"github.com/tickraft/tickraft/pkg/pool"
)

// defaultExecutionPoolSize is the worker count used when no pool size is
// configured. It bounds concurrent remediation executions so a burst of
// triggers cannot spawn unbounded goroutines.
const defaultExecutionPoolSize = 4

// conditionCacheCapacity is the LRU capacity of the compiled-condition
// program cache. It bounds memory for compiled expressions regardless of
// how many distinct rules exist (fix for D-07).
const conditionCacheCapacity = 512

// Engine is the remediation decision and dispatch engine. It subscribes
// to telemetry alert events on the event bus, evaluates registered Rules
// against each event, and dispatches matching rules to the registered
// Operator through a bounded worker pool.
//
// Three safety mechanisms gate dispatch (see package doc): idempotency,
// cooldown, and circuit breaker. The default deployment ships only the
// LocalOperator; callers may register additional operators via
// WithOperators / RegisterOperator.
type Engine struct {
	bus       event.Bus
	rules     RuleStore
	records   RecordStore
	assets    asset.Getter
	logger    *zap.Logger
	operators map[string]Operator

	// operatorsMu protects the operators map so concurrent
	// RegisterOperator writes and dispatch reads are safe.
	operatorsMu sync.RWMutex

	execPool  pool.Pool
	poolOwned bool

	// exprCache caches compiled condition programs (LRU, keyed by env
	// type + expression) so a rule's expression is compiled at most once
	// per distinct expression and retired expressions are evicted.
	exprCache *expr.ProgramCache

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

// Option configures a Engine.
type Option interface {
	apply(*engineOptions)
}

type engineOptions struct {
	bus       event.Bus
	rules     RuleStore
	records   RecordStore
	assets    asset.Getter
	logger    *zap.Logger
	operators []Operator
	poolSize  int
	execPool  pool.Pool
}

// eventBusOption sets the event bus used to subscribe to alert events.
type eventBusOption struct {
	bus event.Bus
}

func (o eventBusOption) apply(options *engineOptions) { options.bus = o.bus }

// WithEventBus sets the event bus used to subscribe to alert events.
func WithEventBus(bus event.Bus) Option {
	return eventBusOption{bus: bus}
}

// ruleStoreOption sets the rule store used to load and update remediation rules.
type ruleStoreOption struct {
	store RuleStore
}

func (o ruleStoreOption) apply(options *engineOptions) { options.rules = o.store }

// WithStore sets the rule store used to load and update remediation rules.
func WithStore(store RuleStore) Option {
	return ruleStoreOption{store: store}
}

// recordStoreOption sets the store used to persist remediation dispatch records.
type recordStoreOption struct {
	records RecordStore
}

func (o recordStoreOption) apply(options *engineOptions) { options.records = o.records }

// WithRecordStore sets the store used to persist remediation dispatch
// records. When set, every dispatch lifecycle transition (triggered,
// started, completed, failed, skipped) is persisted for the records API.
func WithRecordStore(store RecordStore) Option {
	return recordStoreOption{records: store}
}

// assetStoreOption sets the getter used to enrich the evaluation env.
type assetStoreOption struct {
	assets asset.Getter
}

func (o assetStoreOption) apply(options *engineOptions) { options.assets = o.assets }

// WithAssetStore sets the asset.Getter (satisfied by any asset.Store)
// used to enrich the RemediationEnv with asset.name/type/tags. A nil
// getter leaves the asset domain limited to the event's id/key.
func WithAssetStore(store asset.Getter) Option {
	return assetStoreOption{assets: store}
}

// loggerOption sets the structured logger.
type loggerOption struct {
	logger *zap.Logger
}

func (o loggerOption) apply(options *engineOptions) { options.logger = o.logger }

// WithLogger sets the structured logger.
func WithLogger(logger *zap.Logger) Option {
	return loggerOption{logger: logger}
}

// operatorsOption registers additional operators.
type operatorsOption struct {
	ops []Operator
}

func (o operatorsOption) apply(options *engineOptions) {
	options.operators = append(options.operators, o.ops...)
}

// WithOperators registers operators in addition to the default LocalOperator.
// callers may use this to inject remote operators (ssh, mysql, ...).
func WithOperators(ops ...Operator) Option {
	return operatorsOption{ops: ops}
}

// executionPoolSizeOption sets the worker pool size bounding concurrent
// remediation executions.
type executionPoolSizeOption int

func (o executionPoolSizeOption) apply(options *engineOptions) { options.poolSize = int(o) }

// WithExecutionPoolSize sets the worker pool size bounding concurrent
// remediation executions. A non-positive value defaults to 4. Ignored when
// WithPool injects an externally-owned pool.
func WithExecutionPoolSize(n int) Option {
	return executionPoolSizeOption(n)
}

// poolOption injects an externally-owned worker pool for remediation execution.
type poolOption struct {
	p pool.Pool
}

func (o poolOption) apply(options *engineOptions) { options.execPool = o.p }

// WithPool injects an externally-owned worker pool for remediation
// execution. When set, the engine does not create or shut down its own
// pool; the caller is responsible for the pool lifecycle.
func WithPool(p pool.Pool) Option {
	return poolOption{p: p}
}

// New creates a new remediation Engine with the given options.
//
// The LocalOperator is registered by default; callers can override it by
// registering an operator named "local" via WithOperators. When no execution
// pool is injected, the engine creates and owns a bounded pool sized by
// WithExecutionPoolSize (default 4).
func New(options ...Option) (*Engine, error) {
	opts := &engineOptions{
		logger:   zap.NewNop(),
		poolSize: defaultExecutionPoolSize,
	}
	for _, o := range options {
		o.apply(opts)
	}
	if opts.rules == nil {
		return nil, fmt.Errorf("remediation: %w: rule store is required", errdefs.ErrInvalidArgument)
	}

	m := &Engine{
		bus:       opts.bus,
		rules:     opts.rules,
		records:   opts.records,
		assets:    opts.assets,
		logger:    opts.logger,
		operators: map[string]Operator{},
		exprCache: expr.NewProgramCache(conditionCacheCapacity),
		inFlight:  map[string]struct{}{},
	}
	// Register the default local operator unless the caller supplied one.
	hasLocal := false
	for _, op := range opts.operators {
		if op != nil {
			m.operators[op.Name()] = op
			if op.Name() == localExecutorName {
				hasLocal = true
			}
		}
	}
	if !hasLocal {
		m.operators[localExecutorName] = NewLocalOperator(nil, WithOperatorLogger(opts.logger))
	}

	if opts.execPool != nil {
		m.execPool = opts.execPool
		m.poolOwned = false
	} else {
		size := opts.poolSize
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
func (m *Engine) RegisterOperator(op Operator) {
	if op == nil {
		return
	}
	m.operatorsMu.Lock()
	defer m.operatorsMu.Unlock()
	m.operators[op.Name()] = op
}

// Start subscribes to telemetry alert events on the event bus. It returns an
// error if the engine is already started or no event bus is configured.
func (m *Engine) Start(ctx context.Context) error {
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
	sub, err := event.Subscribe(m.bus, event.TypeTelemetryMetricExceeded,
		func(_ context.Context, ev event.Event[event.MetricExceededPayload]) error {
			if te, ok := metricPayloadToTrigger(ev); ok {
				m.handle(runCtx, te)
			} else {
				m.warnMalformedPayload("metric exceeded", ev.Payload.AssetID, ev.Payload.TenantID)
			}
			return nil
		})
	if err != nil {
		m.started = false
		return fmt.Errorf("remediation: subscribe to metric exceeded events: %w", err)
	}
	m.subs = append(m.subs, sub)

	// Log keyword matches -> log trigger.
	sub, err = event.Subscribe(m.bus, event.TypeTelemetryLogMatched,
		func(_ context.Context, ev event.Event[event.LogMatchedPayload]) error {
			if te, ok := logPayloadToTrigger(ev); ok {
				m.handle(runCtx, te)
			} else {
				m.warnMalformedPayload("log matched", ev.Payload.AssetID, ev.Payload.TenantID)
			}
			return nil
		})
	if err != nil {
		m.started = false
		return fmt.Errorf("remediation: subscribe to log matched events: %w", err)
	}
	m.subs = append(m.subs, sub)

	// Asset status transitions -> status_change trigger.
	sub, err = event.Subscribe(m.bus, event.TypeAssetStatusChanged,
		func(_ context.Context, ev event.Event[event.StatusChangePayload]) error {
			if te, ok := statusPayloadToTrigger(ev); ok {
				m.handle(runCtx, te)
			} else {
				m.warnMalformedPayload("status change", ev.Payload.AssetID, ev.Payload.TenantID)
			}
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

// warnMalformedPayload logs an event dropped because its identifiers could
// not be parsed. Falling back to asset id 0 is not an option: it would match
// every global rule (fix for D-10).
func (m *Engine) warnMalformedPayload(kind, assetID, tenantID string) {
	m.logger.Warn("remediation: drop event with malformed payload identifiers",
		zap.String("event_kind", kind),
		zap.String("asset_id", assetID),
		zap.String("tenant_id", tenantID),
	)
}

// Stop gracefully shuts down the engine: it cancels event subscriptions and
// waits for in-flight executions to finish. It is idempotent.
func (m *Engine) Stop(ctx context.Context) error {
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

// handle is the decision core. It loads matching rules from the store,
// evaluates each rule's trigger condition against a RemediationEnv, and
// dispatches matching rules to their operator through the worker pool,
// gated by idempotency, cooldown, and the circuit breaker.
//
// The in-flight claim happens before the remaining gates (claim-then-check)
// so no window exists between the idempotency check and the dispatch-time
// set (fix for D-09).
func (m *Engine) handle(ctx context.Context, te triggerEvent) {
	rules, err := m.rules.GetRules(ctx, te.TenantID, te.AssetID, te.Trigger)
	if err != nil {
		m.logger.Warn("remediation: load rules failed",
			zap.String("trigger", te.Trigger),
			zap.Int64("asset_id", te.AssetID),
			zap.Error(err),
		)
		return
	}
	if len(rules) == 0 {
		return
	}
	env := m.buildEnv(ctx, te)
	for _, r := range rules {
		if !r.Enabled || r.Status != string(StatusActive) {
			continue
		}
		if !m.matchCondition(r, env) {
			continue
		}
		key := inFlightKey(r.ID, te.AssetID)
		if !m.tryClaimInFlight(key) {
			m.recordSkip(ctx, r, te, "idempotency: execution in flight")
			continue
		}
		if skip := m.checkGates(ctx, r, te); skip {
			m.releaseInFlight(key)
			continue
		}
		m.dispatch(ctx, r, te, key)
	}
}

// buildEnv projects the trigger event into a RemediationEnv, enriching the
// asset domain from the asset store. A failed lookup is non-fatal: the env
// keeps the event's asset id/key with empty name/type/tags.
func (m *Engine) buildEnv(ctx context.Context, te triggerEvent) RemediationEnv {
	var res *asset.Asset
	if m.assets != nil {
		if found, err := m.assets.GetByID(ctx, te.AssetID); err == nil && found != nil {
			res = found
		}
	}
	return buildRemediationEnv(te, res)
}

// saveRecord persists a dispatch lifecycle transition. Persistence failures
// are logged but never block the engine: an unrecorded transition must not
// suppress a remediation execution.
func (m *Engine) saveRecord(rec *Record) {
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
func newRecord(r *Rule, te triggerEvent, runID string) *Record {
	return &Record{
		RuleID:   r.ID,
		RuleName: r.Name,
		AssetID:  te.AssetID,
		AssetKey: te.AssetKey,
		RunID:    runID,
		Trigger:  te.Trigger,
		Status:   RecordStatusTriggered,
	}
}

// recordSkip persists a skipped dispatch with the given reason and publishes
// the corresponding RemediationSkipped event.
func (m *Engine) recordSkip(ctx context.Context, r *Rule, te triggerEvent, reason string) {
	m.publish(ctx, event.TypeRemediationSkipped, skipPayload(r, te, reason))
	rec := newRecord(r, te, newRunID())
	rec.Status = RecordStatusSkipped
	rec.Error = reason
	m.saveRecord(rec)
}

// checkGates evaluates the cooldown and circuit-breaker gates for a rule.
// The idempotency gate lives in handle (claim-then-check). It returns true
// when the rule should be skipped (and records the skip reason via a
// RemediationSkipped event).
func (m *Engine) checkGates(ctx context.Context, r *Rule, te triggerEvent) (skip bool) {
	// Cooldown: skip if the last run is still within the cooldown window.
	if r.LastRunAt != nil && r.Cooldown > 0 {
		elapsed := time.Since(*r.LastRunAt)
		if elapsed < time.Duration(r.Cooldown)*time.Second {
			m.recordSkip(ctx, r, te, "cooldown")
			return true
		}
	}

	// Circuit breaker: skip if consecutive failures have reached the
	// threshold. The rule is normally paused atomically by the store when
	// the counter crosses the threshold; this read-side check is the
	// belt-and-braces path for rows paused out-of-band.
	if r.CircuitBreakerThreshold > 0 && r.ConsecutiveFailures >= r.CircuitBreakerThreshold {
		if r.Status != string(StatusPaused) {
			if err := m.rules.UpdateRuleStatus(ctx, r.ID, string(StatusPaused)); err != nil {
				m.logger.Warn("remediation: pause rule failed",
					zap.Int64("rule_id", r.ID),
					zap.Error(err),
				)
			}
		}
		m.recordSkip(ctx, r, te, "circuit breaker tripped")
		return true
	}
	return false
}

// skipPayload builds a RunPayload for a skipped remediation carrying the
// skip reason.
func skipPayload(r *Rule, te triggerEvent, reason string) RunPayload {
	p := newPayload(r, te, "")
	p.Reason = reason
	return p
}

// dispatch submits the execution to the worker pool. The (rule, asset)
// in-flight key must already be claimed by handle; the key is released by
// the job on completion or here when the pool rejects the submission. On
// completion the job updates the rule's last-run timestamp and circuit
// breaker state.
func (m *Engine) dispatch(ctx context.Context, r *Rule, te triggerEvent, key string) {
	m.operatorsMu.RLock()
	op, ok := m.operators[r.ExecutorType]
	m.operatorsMu.RUnlock()
	if !ok {
		m.releaseInFlight(key)
		m.logger.Warn("remediation: operator not registered",
			zap.String("executor_type", r.ExecutorType),
			zap.Int64("rule_id", r.ID),
		)
		m.recordSkip(ctx, r, te, "operator not registered: "+r.ExecutorType)
		return
	}
	runID := newRunID()

	m.publish(ctx, event.TypeRemediationTriggered, newPayload(r, te, runID))
	m.saveRecord(newRecord(r, te, runID))

	m.wg.Add(1)
	job := pool.Lambda(func(jobCtx context.Context) error {
		defer m.wg.Done()
		defer m.releaseInFlight(key)
		m.publish(jobCtx, event.TypeRemediationStarted, newPayload(r, te, runID))

		startedAt := time.Now()
		startedRec := newRecord(r, te, runID)
		startedRec.Status = RecordStatusStarted
		startedRec.StartedAt = &startedAt
		m.saveRecord(startedRec)

		res, err := op.Execute(jobCtx, ExecutionRequest{
			RuleID:   r.ID,
			RuleName: r.Name,
			TenantID: te.TenantID,
			AssetID:  te.AssetID,
			RunID:    runID,
			Config:   r.ExecutorConfig,
			Timeout:  parseExecutorTimeout(r.ExecutorConfig),
		})
		now := time.Now()
		if uerr := m.rules.UpdateLastRun(jobCtx, r.ID, now); uerr != nil {
			m.logger.Warn("remediation: update last run failed",
				zap.Int64("rule_id", r.ID),
				zap.Error(uerr),
			)
		}
		success := err == nil && res != nil && res.Success
		if oerr := m.rules.RecordExecutionOutcome(jobCtx, r.ID, success); oerr != nil {
			m.logger.Warn("remediation: record execution outcome failed",
				zap.Int64("rule_id", r.ID),
				zap.Error(oerr),
			)
		}
		if !success && r.CircuitBreakerThreshold > 0 {
			m.logger.Warn("remediation: execution failed, circuit breaker count incremented",
				zap.Int64("rule_id", r.ID),
				zap.Int("consecutive_failures", r.ConsecutiveFailures+1),
				zap.Int("threshold", r.CircuitBreakerThreshold),
			)
		}

		completed := newPayload(r, te, runID)
		completed.Success = success
		if res != nil {
			completed.DurationMs = res.Duration.Milliseconds()
			completed.ErrorMsg = res.ErrorMsg
		}
		if err != nil {
			completed.ErrorMsg = err.Error()
		}
		m.publish(jobCtx, event.TypeRemediationCompleted, completed)

		finishedRec := newRecord(r, te, runID)
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
		m.publish(ctx, event.TypeRemediationSkipped, skipPayload(r, te, "execution pool full"))
		droppedRec := newRecord(r, te, runID)
		droppedRec.Status = RecordStatusSkipped
		droppedRec.Error = "execution pool full"
		m.saveRecord(droppedRec)
	}
}

// matchCondition evaluates the rule's trigger condition expression against
// the RemediationEnv. An empty expression matches all events. Compilation
// is cached by the LRU ProgramCache; compile or evaluation failures are
// logged and treated as non-matches so sibling rules are unaffected.
func (m *Engine) matchCondition(r *Rule, env RemediationEnv) bool {
	if r.Expression == "" {
		return true
	}
	ok, err := m.exprCache.Eval(r.Expression, env)
	if err != nil {
		m.logger.Warn("remediation: evaluate condition failed",
			zap.Int64("rule_id", r.ID),
			zap.String("expr", r.Expression),
			zap.Error(err),
		)
		return false
	}
	return ok
}

// parseExecutorTimeout extracts the optional "timeout" key from the
// executor config JSON. It accepts a duration string ("90s") or a bare
// number (seconds). Zero is returned when absent or invalid, leaving the
// operator's default in effect (fix for D-15).
func parseExecutorTimeout(config string) time.Duration {
	if config == "" {
		return 0
	}
	var raw struct {
		Timeout json.RawMessage `json:"timeout"`
	}
	if err := sonic.Unmarshal([]byte(config), &raw); err != nil || len(raw.Timeout) == 0 {
		return 0
	}
	// The value is either a duration string ("90s", "1m30s") or a bare
	// number of seconds (45). sonic.Unmarshal handles the JSON quoting.
	var str string
	if err := sonic.Unmarshal(raw.Timeout, &str); err == nil {
		if d, err := time.ParseDuration(str); err == nil && d > 0 {
			return d
		}
		return 0
	}
	var secs float64
	if err := sonic.Unmarshal(raw.Timeout, &secs); err == nil && secs > 0 {
		return time.Duration(secs * float64(time.Second))
	}
	return 0
}

// tryClaimInFlight atomically claims an in-flight key: it returns false
// when the key is already claimed, otherwise it claims the key and
// returns true. Pairing claim and check in one critical section closes
// the check-then-set window (fix for D-09).
func (m *Engine) tryClaimInFlight(key string) bool {
	m.inFlightMu.Lock()
	defer m.inFlightMu.Unlock()
	if _, ok := m.inFlight[key]; ok {
		return false
	}
	m.inFlight[key] = struct{}{}
	return true
}

// releaseInFlight removes an in-flight key, tolerating concurrent release.
func (m *Engine) releaseInFlight(key string) {
	m.inFlightMu.Lock()
	delete(m.inFlight, key)
	m.inFlightMu.Unlock()
}

// publish emits a remediation lifecycle event. Publish failures are logged
// but never block the engine: an unrecorded event must not suppress a
// remediation execution.
func (m *Engine) publish(ctx context.Context, typ event.Type, payload RunPayload) {
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

// newPayload builds a RunPayload from a rule and trigger event.
func newPayload(r *Rule, te triggerEvent, runID string) RunPayload {
	return RunPayload{
		RuleID:   r.ID,
		RuleName: r.Name,
		AssetID:  te.AssetID,
		TenantID: te.TenantID,
		RunID:    runID,
		Trigger:  te.Trigger,
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
