// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package remediation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/tickraft/tickraft/pkg/circuitbreaker"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/quota"
	"go.uber.org/zap"
)

// Mirror types for the executor SPI. The Manager depends on these local
// interfaces instead of importing pkg/executor directly, because
// pkg/executor -> pkg/asset -> pkg/db -> internal/remediation would form an
// import cycle. The caller (typically in internal/service) wraps the
// concrete *executor.Registry and executor.Executor in adapters that
// satisfy these interfaces; the adapter converts between the local and
// executor request/result types.
const (
	// OpProbe mirrors executor.OpProbe. It selects the probe operation
	// on the resolved executor.
	OpProbe = "probe"
	// OpExecute mirrors executor.OpExecute. It selects the execute
	// operation on the resolved executor. The Manager uses this as the
	// default operation for remediation dispatches.
	OpExecute = "execute"
)

// ExecutionRequest mirrors executor.ExecutionRequest for the fields the
// Manager populates. The Operation field uses a string (OpProbe/OpExecute)
// rather than executor.Operation so this package stays free of the
// executor import.
type ExecutionRequest struct {
	// ID is the unique task identifier.
	ID int64
	// TenantID is the tenant identifier for multi-tenant isolation.
	TenantID int64
	// AssetID is the associated asset identifier.
	AssetID int64
	// ExecutorName identifies the executor to look up in the registry.
	ExecutorName string
	// Config stores executor-specific configuration as a JSON string.
	Config string
	// Operation is the operation type (OpProbe or OpExecute).
	Operation string
	// Timeout is the maximum execution duration.
	Timeout time.Duration
	// RunID is the unique identifier of this execution run, used for
	// idempotency control.
	RunID string
	// TriggerType records the execution trigger source. The Manager
	// always sets this to remediationTriggerType.
	TriggerType string
	// Metadata holds optional key-value extension data.
	Metadata map[string]string
}

// Result mirrors executor.Result for the fields the Manager consumes. The
// Status field uses a string rather than types.AssetStatus so this package
// stays free of the asset import.
type Result struct {
	// Status is the execution result status (e.g. statusNormal).
	Status string
	// StatusCode is the protocol-specific status code.
	StatusCode int
	// Body contains the response body or execution output.
	Body string
	// ErrorMsg describes the error when execution failed.
	ErrorMsg string
	// Duration is the total execution duration.
	Duration time.Duration
}

// Executor is the SPI for dispatching a remediation action. It mirrors
// the surface of executor.Executor used by the Manager.
type Executor interface {
	// Execute performs the operation specified by req and returns the
	// result.
	Execute(ctx context.Context, req ExecutionRequest) (*Result, error)
}

// Registry looks up executors by name. It mirrors the surface of
// *executor.Registry used by the Manager.
type Registry interface {
	// Lookup returns the executor registered under name, or an error if
	// no such executor is registered.
	Lookup(name string) (Executor, error)
}

// statusNormal is the types.AssetStatus value indicating a successful executor
// run. It mirrors types.AssetStatusNormal; the literal is duplicated here to
// avoid an import cycle (pkg/asset -> pkg/db -> internal/remediation).
const statusNormal = "normal"

// defaultQuotaRemediation is the maximum number of remediation records
// permitted within the quota window. It mirrors internal/quota.CeilingRemediation;
// the literal is duplicated here because internal/remediation cannot import
// internal/quota (internal packages are not importable across the pkg/ ->
// internal/ boundary).
const defaultQuotaRemediation = 5

// defaultDispatchTimeout is the maximum duration allowed for a single
// remediation executor invocation. It bounds the dispatch goroutine so a
// stuck executor cannot hold a dispatch slot indefinitely.
const defaultDispatchTimeout = 30 * time.Second

// defaultBreakerFailureThreshold is the number of consecutive dispatch
// failures required to open a per-rule circuit breaker.
const defaultBreakerFailureThreshold = 5

// defaultBreakerCooldown is the duration a per-rule circuit breaker
// remains open before transitioning to half-open.
const defaultBreakerCooldown = 30 * time.Second

// defaultBreakerHalfOpenMax is the number of probe requests admitted while
// a per-rule circuit breaker is in half-open state.
const defaultBreakerHalfOpenMax = 1

// quotaWindow is the sliding window over which the in-process quota check
// counts remediation records.
const quotaWindow = 24 * time.Hour

// Skip reason strings published with TypeRemediationSkipped events. They
// are part of the public event contract and must remain stable.
const (
	skipReasonCooldown       = "cooldown"
	skipReasonQuotaExceeded  = "quota_exceeded"
	skipReasonCircuitBreaker = "circuit_breaker"
)

// Trigger type strings published with RemediationPayload.TriggerType,
// derived from the source event type. They are part of the public event
// contract and must remain stable.
const (
	triggerTypeStatusChange   = "status_change"
	triggerTypeMetricExceeded = "metric_exceeded"
	triggerTypeLogMatched     = "log_matched"
	triggerTypeFaultDetected  = "fault_detected"
)

// remediationTriggerType is the TriggerType value set on every dispatched
// ExecutionRequest, identifying the execution as event-driven.
const remediationTriggerType = "event"

// Manager is the event-driven remediation decision maker. It subscribes
// to asset and telemetry source events on the event bus, matches them
// against the cached enabled Rule set, and dispatches matched rules
// through the executor.Registry with TriggerType=event.
//
// The Manager runs entirely in-process: there is no external queue, no
// separate worker process, and no remote control plane. All dispatching
// happens inside the Manager's event handlers, bounded by the Manager's
// lifecycle context.
//
// The Manager is safe for concurrent use. Start and Stop must be called
// at most once each; Start must complete before Stop is called.
type Manager struct {
	store    Store
	bus      event.Bus
	registry Registry
	logger   *zap.Logger
	quota    int

	// rulesCache holds the enabled rules loaded on Start and refreshed
	// by ReloadRules. rulesMu guards the slice pointer.
	rulesMu    sync.RWMutex
	rulesCache []*Rule

	// cooldowns maps "ruleID:assetKey" to the last trigger time. Used
	// to suppress repeat dispatches within a rule's CooldownSeconds
	// window.
	cooldownsMu sync.RWMutex
	cooldowns   map[string]time.Time

	// breakers maps ruleID to its dedicated circuit breaker. Breakers
	// are created lazily on first dispatch for a rule.
	breakersMu sync.RWMutex
	breakers   map[int64]*circuitbreaker.CircuitBreaker

	// subs holds the active event subscriptions, tracked for cleanup
	// on Stop.
	subs []event.Subscription

	// ctx and cancel bound the lifecycle of all dispatch goroutines
	// spawned by the Manager. Cancelled by Stop.
	ctx    context.Context
	cancel context.CancelFunc

	// startedMu guards started and the lifecycle context fields.
	startedMu sync.Mutex
	started   bool

	// wg tracks in-flight dispatch goroutines so Stop can wait for
	// them to finish.
	wg sync.WaitGroup
}

// Option configures a Manager at construction time.
type Option func(*Manager)

// WithStore sets the persistence store used for rules and records.
func WithStore(s Store) Option {
	return func(m *Manager) { m.store = s }
}

// WithBus sets the event bus the Manager subscribes to.
func WithBus(b event.Bus) Option {
	return func(m *Manager) { m.bus = b }
}

// WithRegistry sets the executor registry used to dispatch remediation
// actions. The registry must satisfy the local Registry interface; the
// caller is responsible for wrapping *executor.Registry in an adapter
// that converts between local and executor types.
func WithRegistry(r Registry) Option {
	return func(m *Manager) { m.registry = r }
}

// WithLogger sets the structured logger. Defaults to zap.L() when not
// provided.
func WithLogger(l *zap.Logger) Option {
	return func(m *Manager) { m.logger = l }
}

// WithQuota overrides the remediation quota ceiling. Defaults to
// defaultQuotaRemediation when not set. A value of 0 or negative
// disables the in-process quota check.
func WithQuota(n int) Option {
	return func(m *Manager) { m.quota = n }
}

// New creates a Manager with the given options. The Manager is not
// started; call Start to subscribe to events and begin dispatching.
func New(opts ...Option) *Manager {
	m := &Manager{
		cooldowns: make(map[string]time.Time),
		breakers:  make(map[int64]*circuitbreaker.CircuitBreaker),
		logger:    zap.L(),
		quota:     defaultQuotaRemediation,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Start subscribes the Manager to source events on the bus and loads
// the initial enabled-rule cache. The provided context bounds the
// lifecycle of all dispatch goroutines spawned by the Manager;
// cancelling it (or calling Stop) cancels in-flight dispatches.
//
// Returns an error if store, bus, or registry are not configured, or
// if Start has already been called.
func (m *Manager) Start(ctx context.Context) error {
	m.startedMu.Lock()
	defer m.startedMu.Unlock()
	if m.started {
		return errors.New("remediation: manager already started")
	}
	if m.store == nil {
		return errors.New("remediation: store not configured")
	}
	if m.bus == nil {
		return errors.New("remediation: bus not configured")
	}
	if m.registry == nil {
		return errors.New("remediation: registry not configured")
	}

	runCtx, cancel := context.WithCancel(ctx)
	m.ctx = runCtx
	m.cancel = cancel

	if err := m.ReloadRules(runCtx); err != nil {
		m.cleanupStart()
		return fmt.Errorf("remediation: load rules: %w", err)
	}

	if err := m.subscribe(); err != nil {
		m.cleanupStart()
		return fmt.Errorf("remediation: subscribe: %w", err)
	}

	m.started = true
	m.logger.Info("remediation manager started",
		zap.Int("rules", len(m.rulesCache)),
		zap.Int("quota", m.quota),
	)
	return nil
}

// cleanupStart rolls back the partial state set up by Start when a later
// step fails. The caller must hold m.startedMu.
func (m *Manager) cleanupStart() {
	if m.cancel != nil {
		m.cancel()
	}
	m.ctx = nil
	m.cancel = nil
	for _, s := range m.subs {
		s.Cancel()
	}
	m.subs = nil
}

// subscribe registers handlers for the four source event types. Each
// handler normalizes the source payload into a sourceEvent and dispatches
// it via handleSourceEvent. The lifecycle context captured here bounds
// the dispatch goroutines and is independent of the per-call context
// passed by the bus, so dispatches outlive short bus-handler timeouts.
func (m *Manager) subscribe() error {
	ctx := m.ctx
	subs := make([]event.Subscription, 0, 4)

	sub, err := event.Subscribe(m.bus, event.TypeAssetStatusChanged,
		func(_ context.Context, ev event.Event[event.StatusChangePayload]) error {
			assetKey := ev.Payload.AssetKey
			if assetKey == "" {
				assetKey = ev.Payload.AssetID
			}
			m.handleSourceEvent(ctx, sourceEvent{
				eventType:     event.TypeAssetStatusChanged,
				sourceEventID: ev.EventID,
				assetKey:      assetKey,
				assetID:       ev.Payload.AssetID,
				triggerType:   triggerTypeStatusChange,
				tenantID:      ev.Payload.TenantID,
			})
			return nil
		})
	if err != nil {
		return fmt.Errorf("subscribe status_changed: %w", err)
	}
	subs = append(subs, sub)

	sub, err = event.Subscribe(m.bus, event.TypeAssetFaultDetected,
		func(_ context.Context, ev event.Event[event.FaultPayload]) error {
			m.handleSourceEvent(ctx, sourceEvent{
				eventType:     event.TypeAssetFaultDetected,
				sourceEventID: ev.EventID,
				assetKey:      ev.Payload.AssetID,
				assetID:       ev.Payload.AssetID,
				triggerType:   triggerTypeFaultDetected,
				tenantID:      ev.Payload.TenantID,
			})
			return nil
		})
	if err != nil {
		m.cancelSubs(subs)
		return fmt.Errorf("subscribe fault_detected: %w", err)
	}
	subs = append(subs, sub)

	sub, err = event.Subscribe(m.bus, event.TypeTelemetryMetricExceeded,
		func(_ context.Context, ev event.Event[event.MetricExceededPayload]) error {
			m.handleSourceEvent(ctx, sourceEvent{
				eventType:     event.TypeTelemetryMetricExceeded,
				sourceEventID: ev.EventID,
				assetKey:      ev.Payload.AssetID,
				assetID:       ev.Payload.AssetID,
				triggerType:   triggerTypeMetricExceeded,
				tenantID:      ev.Payload.TenantID,
			})
			return nil
		})
	if err != nil {
		m.cancelSubs(subs)
		return fmt.Errorf("subscribe metric_exceeded: %w", err)
	}
	subs = append(subs, sub)

	sub, err = event.Subscribe(m.bus, event.TypeTelemetryLogMatched,
		func(_ context.Context, ev event.Event[event.LogMatchedPayload]) error {
			m.handleSourceEvent(ctx, sourceEvent{
				eventType:     event.TypeTelemetryLogMatched,
				sourceEventID: ev.EventID,
				assetKey:      ev.Payload.AssetID,
				assetID:       ev.Payload.AssetID,
				triggerType:   triggerTypeLogMatched,
				tenantID:      ev.Payload.TenantID,
			})
			return nil
		})
	if err != nil {
		m.cancelSubs(subs)
		return fmt.Errorf("subscribe log_matched: %w", err)
	}
	subs = append(subs, sub)

	m.subs = subs
	return nil
}

// cancelSubs unsubscribes a slice of subscriptions, used to roll back
// partial subscriptions when a later Subscribe call fails.
func (m *Manager) cancelSubs(subs []event.Subscription) {
	for _, s := range subs {
		s.Cancel()
	}
}

// Stop cancels the lifecycle context, unsubscribes all event
// subscriptions, and waits for in-flight dispatch goroutines to finish
// (or until ctx is cancelled). Returns an error if the wait timed out.
// Calling Stop on a Manager that was never started, or that has already
// been stopped, is a no-op.
func (m *Manager) Stop(ctx context.Context) error {
	m.startedMu.Lock()
	if !m.started {
		m.startedMu.Unlock()
		return nil
	}
	m.started = false
	cancel := m.cancel
	subs := m.subs
	m.subs = nil
	m.ctx = nil
	m.cancel = nil
	m.startedMu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, s := range subs {
		s.Cancel()
	}

	done := make(chan struct{})
	// goroutine lifecycle: bounded — exits after m.wg.Wait returns.
	// In-flight dispatches observe the cancelled lifecycle context and
	// exit promptly, so the wait is bounded in practice.
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return fmt.Errorf("remediation: stop: dispatches did not finish: %w", ctx.Err())
	}

	m.logger.Info("remediation manager stopped")
	return nil
}

// ReloadRules refreshes the in-memory enabled-rule cache from the store.
// Call this after rule definitions change to make the new rules visible
// to subsequent event dispatches without restarting the Manager.
func (m *Manager) ReloadRules(ctx context.Context) error {
	rules, err := m.store.ListEnabledRules(ctx)
	if err != nil {
		return fmt.Errorf("remediation: list enabled rules: %w", err)
	}
	m.rulesMu.Lock()
	m.rulesCache = rules
	m.rulesMu.Unlock()
	return nil
}

// sourceEvent is the normalized view of a source event used by the
// dispatch pipeline. It is independent of the source payload type.
type sourceEvent struct {
	eventType     event.Type
	sourceEventID string
	assetKey      string
	assetID       string
	triggerType   string
	tenantID      string
}

// handleSourceEvent matches the source event against the cached enabled
// rules and, when at least one rule matches, spawns a dispatch goroutine
// to run the full pipeline. The match is performed synchronously so the
// bus handler returns quickly when no rules apply.
func (m *Manager) handleSourceEvent(ctx context.Context, src sourceEvent) {
	rules := m.matchRules(src.eventType, src.assetKey)
	if len(rules) == 0 {
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() {
			if rec := recover(); rec != nil {
				m.logger.Error("panic recovered in remediation dispatch",
					zap.String("source_event_id", src.sourceEventID),
					zap.Any("panic", rec),
					zap.Stack("stack"),
				)
			}
		}()
		for _, rule := range rules {
			if rule == nil {
				continue
			}
			m.dispatchRule(ctx, rule, src)
		}
	}()
}

// matchRules returns the cached enabled rules whose EventType matches
// and whose AssetKey filter (if any) matches the asset key of the source
// event. The returned slice shares the cached rule pointers; callers
// must not mutate them.
func (m *Manager) matchRules(eventType event.Type, assetKey string) []*Rule {
	m.rulesMu.RLock()
	rules := m.rulesCache
	m.rulesMu.RUnlock()

	matched := make([]*Rule, 0, len(rules))
	for _, r := range rules {
		if r == nil {
			continue
		}
		if r.EventType != string(eventType) {
			continue
		}
		if r.AssetKey != "" && r.AssetKey != assetKey {
			continue
		}
		matched = append(matched, r)
	}
	return matched
}

// dispatchRule runs the dispatch pipeline for a single matched rule:
// cooldown check -> quota check -> circuit-breaker check -> publish
// triggered -> persist record -> execute -> publish started ->
// publish completed -> update record. Each skip or completion persists
// a Record row and publishes the corresponding remediation.* event.
func (m *Manager) dispatchRule(ctx context.Context, rule *Rule, src sourceEvent) {
	if !m.checkCooldown(rule.ID, src.assetKey, rule.CooldownSeconds) {
		m.skip(ctx, rule, src, skipReasonCooldown, "cooldown active")
		return
	}

	ok, err := m.checkQuota(ctx)
	if err != nil {
		// Best-effort check: a store error should not block dispatch.
		m.logger.Warn("remediation quota check failed, allowing dispatch",
			zap.Int64("rule_id", rule.ID),
			zap.Error(err),
		)
	} else if !ok {
		m.skip(ctx, rule, src, skipReasonQuotaExceeded, "quota exceeded")
		return
	}

	breaker := m.getOrCreateBreaker(rule.ID)
	if !breaker.Allow() {
		m.skip(ctx, rule, src, skipReasonCircuitBreaker, "circuit breaker open")
		return
	}

	// All guards passed: mark triggered and proceed with dispatch.
	m.markTriggered(rule.ID, src.assetKey)

	remediationID := newID()
	runID := newID()

	if err = m.publishTriggered(ctx, rule, src, remediationID, runID); err != nil {
		m.logger.Warn("failed to publish remediation triggered event",
			zap.Int64("rule_id", rule.ID),
			zap.Error(err),
		)
	}

	record := &Record{
		RuleID:        rule.ID,
		RuleName:      rule.Name,
		AssetKey:      src.assetKey,
		SourceEventID: src.sourceEventID,
		Status:        StatusTriggered,
	}
	if err = m.store.CreateRecord(ctx, record); err != nil {
		m.logger.Error("failed to persist triggered record",
			zap.Int64("rule_id", rule.ID),
			zap.Error(err),
		)
		// Proceed with dispatch even if the record could not be
		// persisted: the executor still runs, but lifecycle updates
		// will be skipped because record.ID is zero.
	}

	m.executeAndFinalize(ctx, rule, src, breaker, record, remediationID, runID)
}

// executeAndFinalize looks up the executor, publishes the started event,
// invokes the executor, and publishes the completion event. The breaker
// and Record row are updated based on the outcome.
func (m *Manager) executeAndFinalize(
	ctx context.Context,
	rule *Rule,
	src sourceEvent,
	breaker *circuitbreaker.CircuitBreaker,
	record *Record,
	remediationID, runID string,
) {
	exec, err := m.registry.Lookup(rule.ActionType)
	if err != nil {
		m.failDispatch(ctx, rule, src, breaker, record, remediationID, "executor lookup failed: "+err.Error())
		return
	}

	startedAt := time.Now()
	if err = m.publishStarted(ctx, rule, src, remediationID, startedAt); err != nil {
		m.logger.Warn("failed to publish remediation started event",
			zap.Int64("rule_id", rule.ID),
			zap.Error(err),
		)
	}
	if record.ID != 0 {
		record.Status = StatusStarted
		record.StartedAt = startedAt
		if err = m.store.UpdateRecord(ctx, record); err != nil {
			m.logger.Warn("failed to update record to started",
				zap.Int64("rule_id", rule.ID),
				zap.Error(err),
			)
		}
	}

	// Build and execute the request. The dispatch context is bounded
	// by the Manager's lifecycle context and a per-dispatch timeout.
	execCtx, cancel := context.WithTimeout(ctx, defaultDispatchTimeout)
	defer cancel()

	req := ExecutionRequest{
		ID:           record.ID,
		ExecutorName: rule.ActionType,
		Config:       rule.ActionPayload,
		Operation:    OpExecute,
		Timeout:      defaultDispatchTimeout,
		RunID:        runID,
		TriggerType:  remediationTriggerType,
		Metadata: map[string]string{
			"remediation_id":  remediationID,
			"source_event_id": src.sourceEventID,
			"rule_id":         strconv.FormatInt(rule.ID, 10),
		},
	}

	result, execErr := exec.Execute(execCtx, req)
	finishedAt := time.Now()
	if execErr != nil {
		m.failDispatchWithResult(ctx, rule, src, breaker, record, remediationID, startedAt, finishedAt, execErr, result)
		return
	}

	// Executor returned no error. Inspect the result status to decide
	// whether the dispatch genuinely succeeded.
	status, errMsg := "success", ""
	if result != nil && result.Status != statusNormal {
		status = "failure"
		errMsg = result.ErrorMsg
		if errMsg == "" {
			errMsg = "abnormal status: " + result.Status
		}
	}
	if status == "success" {
		breaker.RecordSuccess()
	} else {
		breaker.RecordFailure()
	}

	if err = m.publishCompleted(ctx, rule, src, remediationID, status, errMsg, startedAt, finishedAt); err != nil {
		m.logger.Warn("failed to publish remediation completed event",
			zap.Int64("rule_id", rule.ID),
			zap.Error(err),
		)
	}

	if record.ID != 0 {
		record.Status = StatusCompleted
		if status == "failure" {
			record.Status = StatusFailed
		}
		record.Error = errMsg
		record.StartedAt = startedAt
		record.FinishedAt = finishedAt
		if result != nil {
			record.TaskID = strconv.Itoa(result.StatusCode)
		}
		if err = m.store.UpdateRecord(ctx, record); err != nil {
			m.logger.Warn("failed to update record to completed",
				zap.Int64("rule_id", rule.ID),
				zap.Error(err),
			)
		}
	}
}

// failDispatch handles a dispatch that failed before the executor could
// run (e.g. executor lookup failure). It records the failure with the
// breaker, publishes a completion event with status=failure, and updates
// the Record row.
func (m *Manager) failDispatch(
	ctx context.Context,
	rule *Rule,
	src sourceEvent,
	breaker *circuitbreaker.CircuitBreaker,
	record *Record,
	remediationID, errMsg string,
) {
	breaker.RecordFailure()
	now := time.Now()

	if err := m.publishCompleted(ctx, rule, src, remediationID, "failure", errMsg, now, now); err != nil {
		m.logger.Warn("failed to publish remediation completed event",
			zap.Int64("rule_id", rule.ID),
			zap.Error(err),
		)
	}

	if record.ID != 0 {
		record.Status = StatusFailed
		record.Error = errMsg
		record.StartedAt = now
		record.FinishedAt = now
		if err := m.store.UpdateRecord(ctx, record); err != nil {
			m.logger.Warn("failed to update record to failed",
				zap.Int64("rule_id", rule.ID),
				zap.Error(err),
			)
		}
	}
}

// failDispatchWithResult handles a dispatch where the executor returned
// an error. It records the failure with the breaker, publishes a
// completion event with status=failure, and updates the Record row.
func (m *Manager) failDispatchWithResult(
	ctx context.Context,
	rule *Rule,
	src sourceEvent,
	breaker *circuitbreaker.CircuitBreaker,
	record *Record,
	remediationID string,
	startedAt, finishedAt time.Time,
	execErr error,
	result *Result,
) {
	breaker.RecordFailure()
	errMsg := execErr.Error()

	if err := m.publishCompleted(ctx, rule, src, remediationID, "failure", errMsg, startedAt, finishedAt); err != nil {
		m.logger.Warn("failed to publish remediation completed event",
			zap.Int64("rule_id", rule.ID),
			zap.Error(err),
		)
	}

	if record.ID != 0 {
		record.Status = StatusFailed
		record.Error = errMsg
		record.StartedAt = startedAt
		record.FinishedAt = finishedAt
		if result != nil {
			record.TaskID = strconv.Itoa(result.StatusCode)
		}
		if err := m.store.UpdateRecord(ctx, record); err != nil {
			m.logger.Warn("failed to update record to failed",
				zap.Int64("rule_id", rule.ID),
				zap.Error(err),
			)
		}
	}
}

// skip publishes a TypeRemediationSkipped event with the given reason
// and persists a Record row with status=skipped.
func (m *Manager) skip(
	ctx context.Context,
	rule *Rule,
	src sourceEvent,
	reason, message string,
) {
	if err := m.publishSkipped(ctx, rule, src, reason); err != nil {
		m.logger.Warn("failed to publish remediation skipped event",
			zap.Int64("rule_id", rule.ID),
			zap.Error(err),
		)
	}

	record := &Record{
		RuleID:        rule.ID,
		RuleName:      rule.Name,
		AssetKey:      src.assetKey,
		SourceEventID: src.sourceEventID,
		Status:        StatusSkipped,
		Error:         message,
	}
	if err := m.store.CreateRecord(ctx, record); err != nil {
		m.logger.Warn("failed to persist skipped record",
			zap.Int64("rule_id", rule.ID),
			zap.Error(err),
		)
	}
}

// checkCooldown reports whether the rule+asset combination is outside
// its cooldown window. A CooldownSeconds value of 0 or negative disables
// cooldown enforcement for the rule.
func (m *Manager) checkCooldown(ruleID int64, assetKey string, cooldownSeconds int) bool {
	if cooldownSeconds <= 0 {
		return true
	}
	key := cooldownKey(ruleID, assetKey)
	m.cooldownsMu.RLock()
	last, ok := m.cooldowns[key]
	m.cooldownsMu.RUnlock()
	if !ok {
		return true
	}
	return time.Since(last) >= time.Duration(cooldownSeconds)*time.Second
}

// markTriggered records the trigger time for a rule+asset combination.
func (m *Manager) markTriggered(ruleID int64, assetKey string) {
	key := cooldownKey(ruleID, assetKey)
	m.cooldownsMu.Lock()
	m.cooldowns[key] = time.Now()
	m.cooldownsMu.Unlock()
}

// cooldownKey builds the cooldown map key for a rule+asset combination.
func cooldownKey(ruleID int64, assetKey string) string {
	return strconv.FormatInt(ruleID, 10) + ":" + assetKey
}

// getOrCreateBreaker returns the circuit breaker for the given rule,
// creating one with default configuration if none exists. The create
// path uses a double-checked lock to avoid duplicate breakers under
// concurrent dispatch.
func (m *Manager) getOrCreateBreaker(ruleID int64) *circuitbreaker.CircuitBreaker {
	m.breakersMu.RLock()
	b, ok := m.breakers[ruleID]
	m.breakersMu.RUnlock()
	if ok {
		return b
	}

	m.breakersMu.Lock()
	defer m.breakersMu.Unlock()
	if b, ok := m.breakers[ruleID]; ok {
		return b
	}
	b = circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: defaultBreakerFailureThreshold,
		Cooldown:         defaultBreakerCooldown,
		HalfOpenMax:      defaultBreakerHalfOpenMax,
	})
	m.breakers[ruleID] = b
	return b
}

// checkQuota reports whether the count of remediation records created
// in the last 24h is below the quota ceiling. It is a best-effort
// in-process check; the authoritative quota enforcement lives in
// pkg/auth.
//
// A quota value of 0 or negative disables the check (returns true).
// The check uses ListRecords to fetch the most recent records and
// counts those whose CreatedAt falls inside the quota window. Because
// the records are ordered by descending ID, fetching the first
// quota+1 records is sufficient to detect a ceiling breach.
func (m *Manager) checkQuota(ctx context.Context) (bool, error) {
	// Query the active Provider so dynamic plan changes take effect.
	q := quota.Ceiling(quota.TypeRemediation)
	if q <= 0 {
		// Fall back to the configured local quota when the Provider
		// returns 0 (disabled).
		q = m.quota
	}
	if q <= 0 {
		return true, nil
	}
	since := time.Now().Add(-quotaWindow)
	limit := q + 1
	if limit <= 0 {
		return true, nil
	}
	records, _, err := m.store.ListRecords(ctx, limit, 0)
	if err != nil {
		return false, fmt.Errorf("remediation: list records for quota: %w", err)
	}
	count := 0
	for _, r := range records {
		if r == nil {
			continue
		}
		if !r.CreatedAt.Before(since) {
			count++
		}
	}
	return count < q, nil
}

// publishTriggered publishes a TypeRemediationTriggered event. The
// remediationID is also set as the bus event ID for end-to-end
// traceability.
func (m *Manager) publishTriggered(
	ctx context.Context,
	rule *Rule,
	src sourceEvent,
	remediationID, runID string,
) error {
	payload := event.RemediationPayload{
		RemediationID: remediationID,
		RuleID:        strconv.FormatInt(rule.ID, 10),
		RuleName:      rule.Name,
		AssetID:       src.assetID,
		TenantID:      src.tenantID,
		Action:        "triggered",
		TriggerType:   src.triggerType,
		SourceEventID: src.sourceEventID,
		ExecutorType:  rule.ActionType,
	}
	return event.Publish(ctx, m.bus, event.TypeRemediationTriggered, payload,
		event.WithEventID(remediationID),
		event.WithMetadata(map[string]string{
			"run_id":  runID,
			"rule_id": strconv.FormatInt(rule.ID, 10),
		}),
	)
}

// publishStarted publishes a TypeRemediationStarted event.
func (m *Manager) publishStarted(
	ctx context.Context,
	rule *Rule,
	src sourceEvent,
	remediationID string,
	startedAt time.Time,
) error {
	payload := event.RemediationPayload{
		RemediationID: remediationID,
		RuleID:        strconv.FormatInt(rule.ID, 10),
		RuleName:      rule.Name,
		AssetID:       src.assetID,
		TenantID:      src.tenantID,
		Action:        "started",
		TriggerType:   src.triggerType,
		SourceEventID: src.sourceEventID,
		ExecutorType:  rule.ActionType,
		StartedAt:     startedAt.UnixNano(),
	}
	return event.Publish(ctx, m.bus, event.TypeRemediationStarted, payload,
		event.WithEventID(remediationID),
	)
}

// publishCompleted publishes a TypeRemediationCompleted event with the
// final status, error message, and timing.
func (m *Manager) publishCompleted(
	ctx context.Context,
	rule *Rule,
	src sourceEvent,
	remediationID, status, errMsg string,
	startedAt, finishedAt time.Time,
) error {
	payload := event.RemediationPayload{
		RemediationID: remediationID,
		RuleID:        strconv.FormatInt(rule.ID, 10),
		RuleName:      rule.Name,
		AssetID:       src.assetID,
		TenantID:      src.tenantID,
		Action:        "completed",
		TriggerType:   src.triggerType,
		SourceEventID: src.sourceEventID,
		ExecutorType:  rule.ActionType,
		Status:        status,
		Error:         errMsg,
		StartedAt:     startedAt.UnixNano(),
		CompletedAt:   finishedAt.UnixNano(),
		Duration:      finishedAt.Sub(startedAt).Nanoseconds(),
	}
	return event.Publish(ctx, m.bus, event.TypeRemediationCompleted, payload,
		event.WithEventID(remediationID),
	)
}

// publishSkipped publishes a TypeRemediationSkipped event with the
// given skip reason.
func (m *Manager) publishSkipped(
	ctx context.Context,
	rule *Rule,
	src sourceEvent,
	reason string,
) error {
	payload := event.RemediationPayload{
		RuleID:        strconv.FormatInt(rule.ID, 10),
		RuleName:      rule.Name,
		AssetID:       src.assetID,
		TenantID:      src.tenantID,
		Action:        "skipped",
		TriggerType:   src.triggerType,
		SourceEventID: src.sourceEventID,
		ExecutorType:  rule.ActionType,
		SkipReason:    reason,
	}
	return event.Publish(ctx, m.bus, event.TypeRemediationSkipped, payload,
		event.WithMetadata(map[string]string{
			"rule_id": strconv.FormatInt(rule.ID, 10),
		}),
	)
}

// newID generates a 32-character hex identifier using crypto/rand. It
// falls back to a timestamp-based identifier if the system RNG fails.
// The returned value is suitable for RemediationID and RunID fields.
func newID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf[:])
}
