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

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/types"
)

// fakeStore is an in-memory RuleStore for testing the manager decision logic
// without a database. It records UpdateRuleStatus / UpdateLastRun /
// RecordExecutionOutcome calls so tests can assert circuit-breaker and
// cooldown state transitions.
type fakeStore struct {
	mu       sync.Mutex
	rules    []*Rule
	status   map[int64]string
	failures map[int64]int
	lastRun  map[int64]time.Time
}

func newFakeStore(rules ...*Rule) *fakeStore {
	return &fakeStore{
		rules:    rules,
		status:   map[int64]string{},
		failures: map[int64]int{},
		lastRun:  map[int64]time.Time{},
	}
}

func (s *fakeStore) GetRules(_ context.Context, _, _ int64, _ string) ([]*Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Return deep copies reflecting the latest persisted status/counter so
	// the manager reads fresh circuit-breaker state on each handle, mirroring
	// a real GORM store that loads current rows on every query.
	out := make([]*Rule, 0, len(s.rules))
	for _, r := range s.rules {
		cp := *r
		if status, ok := s.status[r.ID]; ok {
			cp.Status = status
		}
		if failures, ok := s.failures[r.ID]; ok {
			cp.ConsecutiveFailures = failures
		}
		if lr, ok := s.lastRun[r.ID]; ok {
			lr := lr
			cp.LastRunAt = &lr
		}
		out = append(out, &cp)
	}
	return out, nil
}

func (s *fakeStore) UpdateRuleStatus(_ context.Context, ruleID int64, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[ruleID] = status
	return nil
}

func (s *fakeStore) UpdateLastRun(_ context.Context, ruleID int64, lastRunAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastRun[ruleID] = lastRunAt
	return nil
}

// RecordExecutionOutcome mirrors the real store's atomic semantics: success
// resets the counter, failure increments it and pauses the rule when the
// row's own threshold is reached.
func (s *fakeStore) RecordExecutionOutcome(_ context.Context, ruleID int64, success bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if success {
		s.failures[ruleID] = 0
		return nil
	}
	s.failures[ruleID]++
	for _, r := range s.rules {
		if r.ID == ruleID && r.CircuitBreakerThreshold > 0 && s.failures[ruleID] >= r.CircuitBreakerThreshold {
			s.status[ruleID] = string(StatusPaused)
		}
	}
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

// waitForCalls polls until the operator has recorded at least n calls or
// the deadline expires, returning the final call count.
func waitForCalls(t *testing.T, op *recordingOperator, n int) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for op.callCount() < n && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return op.callCount()
}

func TestManagerEmptyConditionMatchesAll(t *testing.T) {
	store := newFakeStore(&Rule{
		ID: 1, Enabled: true, Status: string(StatusActive),
		TriggerEventType: string(TriggerMetric), ExecutorType: "local",
		Cooldown: 0, CircuitBreakerThreshold: 0,
	})
	op := &recordingOperator{name: "local", success: true}
	m := newTestManager(t, store, op)

	m.handle(context.Background(), triggerEvent{Trigger: string(TriggerMetric), AssetID: 7})

	if got := waitForCalls(t, op, 1); got != 1 {
		t.Fatalf("expected 1 execution, got %d", got)
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

	m.handle(context.Background(), triggerEvent{Trigger: string(TriggerMetric), AssetID: 1})

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
		m.handle(context.Background(), triggerEvent{Trigger: string(TriggerMetric), AssetID: 1})
		waitForCalls(t, op, i+1)
		time.Sleep(20 * time.Millisecond)
	}

	store.mu.Lock()
	status := store.status[3]
	failures := store.failures[3]
	store.mu.Unlock()

	if status != string(StatusPaused) {
		t.Fatalf("expected rule paused after threshold, got status %q", status)
	}
	if failures != 3 {
		t.Fatalf("expected 3 consecutive failures, got %d", failures)
	}

	// A 4th trigger must be skipped by the circuit breaker (not executed).
	calls := op.callCount()
	m.handle(context.Background(), triggerEvent{Trigger: string(TriggerMetric), AssetID: 1})
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
		m.handle(context.Background(), triggerEvent{Trigger: string(TriggerMetric), AssetID: 1})
		waitForCalls(t, op, op.callCount()+1)
		time.Sleep(10 * time.Millisecond)
	}
	// A success resets the consecutive-failure count.
	op.mu.Lock()
	op.success = true
	op.mu.Unlock()
	m.handle(context.Background(), triggerEvent{Trigger: string(TriggerMetric), AssetID: 1})
	waitForCalls(t, op, 3)
	time.Sleep(20 * time.Millisecond)

	store.mu.Lock()
	failures := store.failures[4]
	status := store.status[4]
	store.mu.Unlock()
	if failures != 0 {
		t.Fatalf("expected failures reset to 0 after success, got %d", failures)
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
	m.handle(context.Background(), triggerEvent{Trigger: string(TriggerMetric), AssetID: 9})
	m.handle(context.Background(), triggerEvent{Trigger: string(TriggerMetric), AssetID: 9})

	waitForCalls(t, op, 1)
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
		// Only execute when the observed metric value exceeds the threshold.
		Expression: `metric.value > threshold`,
	})
	op := &recordingOperator{name: "local", success: true}
	m := newTestManager(t, store, op)

	// Below threshold: no execution.
	m.handle(context.Background(), triggerEvent{
		Trigger: string(TriggerMetric), AssetID: 1,
		MetricName: "cpu", MetricValue: 50, Threshold: 90,
	})
	time.Sleep(50 * time.Millisecond)
	if op.callCount() != 0 {
		t.Fatalf("expected 0 executions for non-matching condition, got %d", op.callCount())
	}

	// Above threshold: execution.
	m.handle(context.Background(), triggerEvent{
		Trigger: string(TriggerMetric), AssetID: 1,
		MetricName: "cpu", MetricValue: 150, Threshold: 90,
	})
	if got := waitForCalls(t, op, 1); got != 1 {
		t.Fatalf("expected 1 execution for matching condition, got %d", got)
	}
	_ = m.Stop(context.Background())
}

// TestManagerConditionExpressionVariables exercises the documented
// RemediationEnv variables through real rule evaluation, including the
// domain shapes (metric.name, status.previous, asset.name).
func TestManagerConditionExpressionVariables(t *testing.T) {
	cases := []struct {
		name       string
		trigger    string
		expression string
		te         triggerEvent
		res        *asset.Asset
		wantExec   bool
	}{
		{
			name: "metric domain fields", trigger: string(TriggerMetric),
			expression: `trigger == "metric" && metric.name == "cpu" && metric.value > 90`,
			te:         triggerEvent{Trigger: string(TriggerMetric), MetricName: "cpu", MetricValue: 95},
			wantExec:   true,
		},
		{
			name: "log content and keyword", trigger: string(TriggerLog),
			expression: `content contains "OutOfMemory" && keyword matches "oom|killed"`,
			te:         triggerEvent{Trigger: string(TriggerLog), Keyword: "oom", Content: "java OutOfMemoryError"},
			wantExec:   true,
		},
		{
			name: "log level mismatch", trigger: string(TriggerLog),
			expression: `level == "error"`,
			te:         triggerEvent{Trigger: string(TriggerLog), Level: "warn"},
			wantExec:   false,
		},
		{
			name: "status transition fields", trigger: string(TriggerStatusChange),
			expression: `status.previous == "normal" && status.current == "abnormal"`,
			te: triggerEvent{
				Trigger: string(TriggerStatusChange), PrevStatus: "normal", CurrStatus: "abnormal",
			},
			wantExec: true,
		},
		{
			name: "enriched asset fields", trigger: string(TriggerMetric),
			expression: `asset.name == "web-1" && asset.type == "host" && asset.tags["env"] == "prod"`,
			te:         triggerEvent{Trigger: string(TriggerMetric), AssetID: 7, MetricName: "cpu", MetricValue: 95},
			res: &asset.Asset{
				ID: 7, Name: "web-1", AssetType: types.AssetType("host"), Metadata: `{"env":"prod"}`,
			},
			wantExec: true,
		},
		{
			name: "asset enrichment miss leaves empty fields", trigger: string(TriggerMetric),
			expression: `asset.name == "web-1"`,
			te:         triggerEvent{Trigger: string(TriggerMetric), AssetID: 7, MetricName: "cpu", MetricValue: 95},
			res:        nil,
			wantExec:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore(&Rule{
				ID: 10, Enabled: true, Status: string(StatusActive),
				TriggerEventType: tc.trigger, ExecutorType: "local",
				Expression: tc.expression,
			})
			op := &recordingOperator{name: "local", success: true}
			m, err := New(
				WithStore(store),
				WithOperators(op),
				WithExecutionPoolSize(2),
				WithLogger(zap.NewNop()),
				WithAssetStore(fakeAssetStoreFor(tc.te.AssetID, tc.res)),
			)
			if err != nil {
				t.Fatalf("New manager: %v", err)
			}
			m.handle(context.Background(), tc.te)
			waitForCalls(t, op, 1)
			time.Sleep(30 * time.Millisecond)
			got := op.callCount()
			if tc.wantExec && got != 1 {
				t.Errorf("expected 1 execution, got %d", got)
			}
			if !tc.wantExec && got != 0 {
				t.Errorf("expected no execution, got %d", got)
			}
			_ = m.Stop(context.Background())
		})
	}
}

// fakeAssetStoreFor builds an asset.Store resolving only the given id.
func fakeAssetStoreFor(id int64, res *asset.Asset) asset.Store {
	return &stubAssetStore{byID: map[int64]*asset.Asset{id: res}}
}

// stubAssetStore adapts a map into the asset.Store surface.
type stubAssetStore struct {
	asset.NoopStore
	byID map[int64]*asset.Asset
}

func (s *stubAssetStore) GetByID(_ context.Context, id int64) (*asset.Asset, error) {
	if a, ok := s.byID[id]; ok && a != nil {
		return a, nil
	}
	return nil, errdefs.ErrNotFound
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

	m.handle(context.Background(), triggerEvent{Trigger: string(TriggerMetric), AssetID: 1})
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

// TestLocalOperatorJudgmentDrivesSuccess pins the judgment chain for the
// local operator: the executor config's optional "expression" key is
// applied to the result, so the user-defined success standard drives the
// Success flag the circuit breaker counts (rule-engine-design §6.3).
func TestLocalOperatorJudgmentDrivesSuccess(t *testing.T) {
	op := NewLocalOperator(nil, WithOperatorLogger(zap.NewNop()))

	cases := []struct {
		name    string
		config  string
		success bool
	}{
		{"zero exit judged failing", `{"command":"true","expression":"code != 0"}`, false},
		{"zero exit judged successful", `{"command":"true","expression":"code == 0"}`, true},
		{"non-zero exit overridden to success", `{"command":"false","expression":"code == 1"}`, true},
		{"no expression keeps protocol default", `{"command":"true"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := op.Execute(context.Background(), ExecutionRequest{
				RuleID: 1, AssetID: 1, Config: tc.config,
			})
			if err != nil {
				t.Fatalf("execute error: %v", err)
			}
			if res.Success != tc.success {
				t.Errorf("Success = %v, want %v (ErrorMsg=%q)", res.Success, tc.success, res.ErrorMsg)
			}
		})
	}
}

// fakeWrappedExec is a minimal executor.Executor for operator tests.
type fakeWrappedExec struct{ result *executor.Result }

func (f *fakeWrappedExec) Name() string                      { return "fake" }
func (f *fakeWrappedExec) Capabilities() executor.Capability { return executor.CapExec }
func (f *fakeWrappedExec) Execute(context.Context, executor.ExecutionRequest) (*executor.Result, error) {
	return f.result, nil
}

// TestExecutorOperatorJudgmentDrivesSuccess pins the same judgment chain
// for the generic executor-backed operator.
func TestExecutorOperatorJudgmentDrivesSuccess(t *testing.T) {
	fe := &fakeWrappedExec{result: &executor.Result{
		Status:     types.AssetStatusNormal,
		StatusCode: 200,
		Body:       "ok",
	}}

	op := NewExecutorOperator("fake", fe, zap.NewNop())
	res, err := op.Execute(context.Background(), ExecutionRequest{
		RuleID: 1, Config: `{"expression":"code != 200"}`,
	})
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if res.Success {
		t.Error("expected Success=false when the judgment expression fails the result")
	}

	op2 := NewExecutorOperator("fake", fe, zap.NewNop())
	res, err = op2.Execute(context.Background(), ExecutionRequest{
		RuleID: 1, Config: `{"expression":"code == 200"}`,
	})
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !res.Success {
		t.Error("expected Success=true when the judgment expression passes")
	}
}

// TestParseExecutorTimeout covers the optional executor_config timeout
// key: duration strings, bare seconds, and absent/invalid values.
func TestParseExecutorTimeout(t *testing.T) {
	cases := []struct {
		config string
		want   time.Duration
	}{
		{`{}`, 0},
		{`{"timeout":"90s"}`, 90 * time.Second},
		{`{"timeout":"1m30s"}`, 90 * time.Second},
		{`{"timeout":45}`, 45 * time.Second},
		{`{"timeout":"nope"}`, 0},
		{`{"timeout":-5}`, 0},
		{``, 0},
		{`not-json`, 0},
	}
	for _, tc := range cases {
		if got := parseExecutorTimeout(tc.config); got != tc.want {
			t.Errorf("parseExecutorTimeout(%q) = %v, want %v", tc.config, got, tc.want)
		}
	}
}

// TestParseID covers the malformed-payload guard (D-10): identifiers that
// do not parse are rejected instead of falling back to 0.
func TestParseID(t *testing.T) {
	for _, v := range []string{"", "abc", "-1", "1.5", "99999999999999999999"} {
		if _, ok := parseID(v); ok {
			t.Errorf("parseID(%q) = ok, want rejected", v)
		}
	}
	if id, ok := parseID("42"); !ok || id != 42 {
		t.Errorf("parseID(42) = %d, %v; want 42, true", id, ok)
	}
	if id, ok := parseID("0"); !ok || id != 0 {
		t.Errorf("parseID(0) = %d, %v; want 0, true (legitimate global scope)", id, ok)
	}
}
