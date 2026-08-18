// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package remediation

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/event"
)

// fakeStore is an in-memory RuleStore for testing the manager decision logic
// without a database. It records UpdateRuleStatus / UpdateLastRun calls so
// tests can assert circuit-breaker and cooldown state transitions.
type fakeStore struct {
	mu       sync.Mutex
	rules    []*Rule
	status   map[int64]string
	metadata map[int64]string
	lastRun  map[int64]time.Time
}

func newFakeStore(rules ...*Rule) *fakeStore {
	return &fakeStore{
		rules:    rules,
		status:   map[int64]string{},
		metadata: map[int64]string{},
		lastRun:  map[int64]time.Time{},
	}
}

func (s *fakeStore) GetRules(_ context.Context, _, _ int64, _ string) ([]*Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Return deep copies reflecting the latest persisted status/metadata so
	// the manager reads fresh circuit-breaker state on each handle, mirroring
	// a real GORM store that loads current rows on every query.
	out := make([]*Rule, 0, len(s.rules))
	for _, r := range s.rules {
		cp := *r
		if status, ok := s.status[r.ID]; ok {
			cp.Status = status
		}
		if md, ok := s.metadata[r.ID]; ok {
			cp.Metadata = md
		}
		if lr, ok := s.lastRun[r.ID]; ok {
			lr := lr
			cp.LastRunAt = &lr
		}
		out = append(out, &cp)
	}
	return out, nil
}

func (s *fakeStore) UpdateRuleStatus(_ context.Context, ruleID int64, status, metadata string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[ruleID] = status
	s.metadata[ruleID] = metadata
	return nil
}

func (s *fakeStore) UpdateLastRun(_ context.Context, ruleID int64, lastRunAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastRun[ruleID] = lastRunAt
	return nil
}

// recordingOperator is a fake Operator that lets tests control the outcome
// (success/failure) and count executions.
type recordingOperator struct {
	mu        sync.Mutex
	name      string
	success   bool
	execErr   error
	calls     int
	lastDelay time.Duration
}

func (o *recordingOperator) Name() string { return o.name }

func (o *recordingOperator) Execute(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error) {
	o.mu.Lock()
	o.calls++
	success := o.success
	execErr := o.execErr
	o.mu.Unlock()
	if o.lastDelay > 0 {
		select {
		case <-time.After(o.lastDelay):
		case <-ctx.Done():
		}
	}
	if execErr != nil {
		return nil, execErr
	}
	return &ExecutionResult{Success: success, Output: "ok"}, nil
}

func (o *recordingOperator) callCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.calls
}

func newTestManager(t *testing.T, store RuleStore, op Operator) *Manager {
	t.Helper()
	m, err := New(
		WithStore(store),
		WithOperators(op),
		WithExecutionPoolSize(2),
		WithLogger(zap.NewNop()),
	)
	if err != nil {
		t.Fatalf("New manager: %v", err)
	}
	return m
}

func TestManagerEmptyConditionMatchesAll(t *testing.T) {
	store := newFakeStore(&Rule{
		ID: 1, Enabled: true, Status: string(StatusActive),
		TriggerEventType: string(TriggerMetric), ExecutorType: "local",
		Cooldown: 0, CircuitBreakerThreshold: 0,
	})
	op := &recordingOperator{name: "local", success: true}
	m := newTestManager(t, store, op)

	m.handle(context.Background(), EventContext{Type: string(TriggerMetric), AssetID: 7})

	// Execution is asynchronous via the pool; wait for it to complete.
	deadline := time.Now().Add(2 * time.Second)
	for op.callCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if op.callCount() != 1 {
		t.Fatalf("expected 1 execution, got %d", op.callCount())
	}
	_ = m.Stop(context.Background())
}

func TestManagerCooldownSkipsRepeat(t *testing.T) {
	now := time.Now()
	store := newFakeStore(&Rule{
		ID: 2, Enabled: true, Status: string(StatusActive),
		TriggerEventType: string(TriggerMetric), ExecutorType: "local",
		Cooldown: 300, CircuitBreakerThreshold: 0,
		LastRunAt: &now,
	})
	op := &recordingOperator{name: "local", success: true}
	m := newTestManager(t, store, op)

	m.handle(context.Background(), EventContext{Type: string(TriggerMetric), AssetID: 1})

	// Within the cooldown window, the rule must be skipped (no execution).
	time.Sleep(100 * time.Millisecond)
	if op.callCount() != 0 {
		t.Fatalf("expected 0 executions within cooldown, got %d", op.callCount())
	}
	_ = m.Stop(context.Background())
}

func TestManagerCircuitBreakerPausesAfterThreshold(t *testing.T) {
	store := newFakeStore(&Rule{
		ID: 3, Enabled: true, Status: string(StatusActive),
		TriggerEventType: string(TriggerMetric), ExecutorType: "local",
		Cooldown: 0, CircuitBreakerThreshold: 3,
	})
	op := &recordingOperator{name: "local", success: false}
	m := newTestManager(t, store, op)

	// Trigger 3 consecutive failures. The circuit breaker should trip after
	// the 3rd failure and pause the rule.
	for i := range 3 {
		m.handle(context.Background(), EventContext{Type: string(TriggerMetric), AssetID: 1})
		deadline := time.Now().Add(time.Second)
		for op.callCount() <= i && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(20 * time.Millisecond)
	}

	store.mu.Lock()
	status := store.status[3]
	md := parseMetadata(store.metadata[3])
	store.mu.Unlock()

	if status != string(StatusPaused) {
		t.Fatalf("expected rule paused after threshold, got status %q", status)
	}
	if md.ConsecutiveFailures != 3 {
		t.Fatalf("expected 3 consecutive failures, got %d", md.ConsecutiveFailures)
	}

	// A 4th trigger must be skipped by the circuit breaker (not executed).
	calls := op.callCount()
	m.handle(context.Background(), EventContext{Type: string(TriggerMetric), AssetID: 1})
	time.Sleep(50 * time.Millisecond)
	if op.callCount() != calls {
		t.Fatalf("expected no further execution after circuit breaker, got %d new calls", op.callCount()-calls)
	}
	_ = m.Stop(context.Background())
}

func TestManagerSuccessResetsCircuitBreaker(t *testing.T) {
	store := newFakeStore(&Rule{
		ID: 4, Enabled: true, Status: string(StatusActive),
		TriggerEventType: string(TriggerMetric), ExecutorType: "local",
		Cooldown: 0, CircuitBreakerThreshold: 3,
	})
	op := &recordingOperator{name: "local", success: false}
	m := newTestManager(t, store, op)

	// Two failures (below threshold).
	for range 2 {
		m.handle(context.Background(), EventContext{Type: string(TriggerMetric), AssetID: 1})
		time.Sleep(30 * time.Millisecond)
	}
	// A success resets the consecutive-failure count.
	op.mu.Lock()
	op.success = true
	op.mu.Unlock()
	m.handle(context.Background(), EventContext{Type: string(TriggerMetric), AssetID: 1})
	time.Sleep(30 * time.Millisecond)

	store.mu.Lock()
	md := parseMetadata(store.metadata[4])
	status := store.status[4]
	store.mu.Unlock()
	if md.ConsecutiveFailures != 0 {
		t.Fatalf("expected failures reset to 0 after success, got %d", md.ConsecutiveFailures)
	}
	if status == string(StatusPaused) {
		t.Fatalf("rule must not be paused after a success reset")
	}
	_ = m.Stop(context.Background())
}

func TestManagerIdempotencyBlocksConcurrentDuplicate(t *testing.T) {
	store := newFakeStore(&Rule{
		ID: 5, Enabled: true, Status: string(StatusActive),
		TriggerEventType: string(TriggerMetric), ExecutorType: "local",
		Cooldown: 0, CircuitBreakerThreshold: 0,
	})
	// A slow operator keeps the first execution in flight while a second
	// trigger for the same (rule, asset) arrives.
	op := &recordingOperator{name: "local", success: true, lastDelay: 100 * time.Millisecond}
	m := newTestManager(t, store, op)

	// Fire two triggers back-to-back for the same asset; the second must be
	// skipped by idempotency while the first is in flight.
	m.handle(context.Background(), EventContext{Type: string(TriggerMetric), AssetID: 9})
	m.handle(context.Background(), EventContext{Type: string(TriggerMetric), AssetID: 9})

	deadline := time.Now().Add(2 * time.Second)
	for op.callCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	// Allow the in-flight execution to finish.
	time.Sleep(200 * time.Millisecond)

	if got := op.callCount(); got != 1 {
		t.Fatalf("expected exactly 1 execution (idempotency), got %d", got)
	}
	_ = m.Stop(context.Background())
}

func TestManagerConditionExpressionFilters(t *testing.T) {
	store := newFakeStore(&Rule{
		ID: 6, Enabled: true, Status: string(StatusActive),
		TriggerEventType: string(TriggerMetric), ExecutorType: "local",
		Cooldown: 0, CircuitBreakerThreshold: 0,
		// Only execute when the metric value exceeds 100.
		ConditionExpr: "metric_value > 100",
	})
	op := &recordingOperator{name: "local", success: true}
	m := newTestManager(t, store, op)

	// Below threshold: no execution.
	m.handle(context.Background(), EventContext{Type: string(TriggerMetric), AssetID: 1, MetricValue: 50})
	time.Sleep(50 * time.Millisecond)
	if op.callCount() != 0 {
		t.Fatalf("expected 0 executions for non-matching condition, got %d", op.callCount())
	}

	// Above threshold: execution.
	m.handle(context.Background(), EventContext{Type: string(TriggerMetric), AssetID: 1, MetricValue: 150})
	deadline := time.Now().Add(time.Second)
	for op.callCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if op.callCount() != 1 {
		t.Fatalf("expected 1 execution for matching condition, got %d", op.callCount())
	}
	_ = m.Stop(context.Background())
}

func TestManagerPublishesLifecycleEvents(t *testing.T) {
	bus := event.NewBus()
	store := newFakeStore(&Rule{
		ID: 7, Enabled: true, Status: string(StatusActive),
		TriggerEventType: string(TriggerMetric), ExecutorType: "local",
		Cooldown: 0, CircuitBreakerThreshold: 0,
	})
	op := &recordingOperator{name: "local", success: true}

	var gotMu sync.Mutex
	got := map[event.Type]bool{}
	_, _ = event.Subscribe[RunPayload](bus, event.TypeRemediationCompleted,
		func(_ context.Context, ev event.Event[RunPayload]) error {
			gotMu.Lock()
			got[ev.Type] = ev.Payload.Success
			gotMu.Unlock()
			return nil
		})

	m, err := New(
		WithStore(store),
		WithEventBus(bus),
		WithOperators(op),
		WithExecutionPoolSize(2),
		WithLogger(zap.NewNop()),
	)
	if err != nil {
		t.Fatalf("New manager: %v", err)
	}

	m.handle(context.Background(), EventContext{Type: string(TriggerMetric), AssetID: 1})
	deadline := time.Now().Add(2 * time.Second)
	for {
		gotMu.Lock()
		done := got[event.TypeRemediationCompleted]
		gotMu.Unlock()
		if done || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	gotMu.Lock()
	success := got[event.TypeRemediationCompleted]
	gotMu.Unlock()
	if !success {
		t.Fatalf("expected RemediationCompleted success event, got %v", got)
	}
	_ = m.Stop(context.Background())
	_ = bus.Close()
}

func TestLocalOperatorExecutesCommand(t *testing.T) {
	op := NewLocalOperator(nil, WithOperatorLogger(zap.NewNop()))
	// Run `true` equivalent: on POSIX sh prints nothing; use a command that
	// exits 0. "echo hi" writes "hi" to stdout.
	cfg := `{"command":"echo","args":["hi"]}`
	res, err := op.Execute(context.Background(), ExecutionRequest{
		RuleID: 1, AssetID: 1, Config: cfg,
	})
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got failure: %q", res.ErrorMsg)
	}
	if res.Output != "hi\n" {
		t.Fatalf("unexpected output %q", res.Output)
	}
}

func TestLocalOperatorFailingCommandReportsFailure(t *testing.T) {
	op := NewLocalOperator(nil, WithOperatorLogger(zap.NewNop()))
	cfg := `{"command":"false"}`
	res, err := op.Execute(context.Background(), ExecutionRequest{
		RuleID: 1, AssetID: 1, Config: cfg,
	})
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if res.Success {
		t.Fatalf("expected failure for non-zero exit, got success")
	}
}

func TestParseMetadataHandlesEmptyAndInvalid(t *testing.T) {
	if md := parseMetadata(""); md.ConsecutiveFailures != 0 {
		t.Fatalf("empty metadata should parse to zero, got %d", md.ConsecutiveFailures)
	}
	if md := parseMetadata("not-json"); md.ConsecutiveFailures != 0 {
		t.Fatalf("invalid metadata should parse to zero, got %d", md.ConsecutiveFailures)
	}
	md := ruleMetadata{ConsecutiveFailures: 4}
	out := parseMetadata(md.serialize())
	if out.ConsecutiveFailures != 4 {
		t.Fatalf("round-trip failed: got %d", out.ConsecutiveFailures)
	}
}
