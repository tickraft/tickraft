// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package executor

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/pool"
	"github.com/tickraft/tickraft/pkg/timewheel"
	"github.com/tickraft/tickraft/pkg/types"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// fakeExecutor is a test Executor that returns a configurable result.
type fakeExecutor struct {
	typ     string
	result  *Result
	err     error
	calls   atomic.Int32
	mu      sync.Mutex
	lastReq ExecutionRequest
}

func (f *fakeExecutor) Name() string             { return f.typ }
func (f *fakeExecutor) Capabilities() Capability { return CapExec }

func (f *fakeExecutor) Execute(_ context.Context, req ExecutionRequest) (*Result, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.lastReq = req
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &Result{Status: types.AssetStatusNormal}, nil
}

func (f *fakeExecutor) LastRequest() ExecutionRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastReq
}

// flakyExecutor fails (returns abnormal status) for the first failCount calls,
// then succeeds. Used to test retry behavior.
type flakyExecutor struct {
	typ       string
	failCount int32
	calls     atomic.Int32
}

func (f *flakyExecutor) Name() string             { return f.typ }
func (f *flakyExecutor) Capabilities() Capability { return CapExec }

func (f *flakyExecutor) Execute(_ context.Context, _ ExecutionRequest) (*Result, error) {
	n := f.calls.Add(1)
	if n <= f.failCount {
		return &Result{
			Status:   types.AssetStatusAbnormal,
			ErrorMsg: "transient failure",
		}, nil
	}
	return &Result{Status: types.AssetStatusNormal}, nil
}

// countingRecordStore is a test RecordStore that counts Save calls.
type countingRecordStore struct {
	calls atomic.Int32
	last  ExecutionRecord
	mu    sync.Mutex
}

func (c *countingRecordStore) Save(record ExecutionRecord) error {
	c.calls.Add(1)
	c.mu.Lock()
	c.last = record
	c.mu.Unlock()
	return nil
}

func (c *countingRecordStore) LastRecord() ExecutionRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseExecID parses the ExecutionID string field back to int64 for test
// assertions. It panics on failure because test payloads must always carry
// valid integer IDs.
func parseExecID(s string) int64 {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("parseExecID(%q): %v", s, err))
	}
	return id
}

// newTestRunner creates a Runner wired to a fresh event bus and registry.
// It returns the runner, the bus, and the registry for test interaction.
func newTestRunner(t *testing.T, registry *Registry) (Runner, event.Bus) {
	t.Helper()
	bus := event.NewBus()
	if registry == nil {
		registry = NewRegistry()
	}
	r, err := New(
		WithExecutorRegistry(registry),
		WithEventBus(bus),
		WithLogger(zap.NewNop()),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("start runner: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = r.Stop(ctx)
		_ = bus.Close()
	})
	r.SubscribeEvents(context.Background())
	return r, bus
}

// publishTrigger publishes an ExecutionTriggered event and returns.
func publishTrigger(bus event.Bus, payload event.ExecutionPayload, opts ...event.PublishOption) {
	if err := event.Publish(context.Background(), bus, event.TypeExecutionTriggered, payload, opts...); err != nil {
		panic(fmt.Sprintf("publishTrigger: %v", err))
	}
}

// subscribeCompleted registers a handler that sends TaskCompleted events to
// the returned channel.
func subscribeCompleted(bus event.Bus) <-chan event.Event[event.ExecutionPayload] {
	ch := make(chan event.Event[event.ExecutionPayload], 1)
	_, err := event.Subscribe[event.ExecutionPayload](bus, event.TypeExecutionCompleted, func(_ context.Context, e event.Event[event.ExecutionPayload]) error {
		ch <- e
		return nil
	})
	if err != nil {
		panic(fmt.Sprintf("subscribeCompleted: %v", err))
	}
	return ch
}

// waitCompleted waits for a TaskCompleted event or fails the test on timeout.
func waitCompleted(t *testing.T, ch <-chan event.Event[event.ExecutionPayload]) event.Event[event.ExecutionPayload] {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for TaskCompleted event")
		return event.Event[event.ExecutionPayload]{}
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestRunnerSubscribesToTaskTriggered verifies that the Runner subscribes to
// ExecutionTriggered events and dispatches them to the executor.
func TestRunnerSubscribesToTaskTriggered(t *testing.T) {
	exec := &fakeExecutor{typ: "test"}
	registry := NewRegistry()
	if err := registry.Register(exec); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	r, bus := newTestRunner(t, registry)
	_ = r

	completed := subscribeCompleted(bus)

	publishTrigger(bus, event.ExecutionPayload{
		ExecutionID:  "1",
		TenantID:     "10",
		AssetID:      "100",
		ExecutorType: "test",
	})

	waitCompleted(t, completed)

	if got := exec.calls.Load(); got != 1 {
		t.Errorf("executor calls: got %d, want 1", got)
	}
}

// TestRunnerExecutesLookedUpExecutor verifies that the Runner constructs the
// correct ExecutionRequest from the trigger payload and passes it to the
// executor.
func TestRunnerExecutesLookedUpExecutor(t *testing.T) {
	exec := &fakeExecutor{typ: "http"}
	registry := NewRegistry()
	if err := registry.Register(exec); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	r, bus := newTestRunner(t, registry)
	_ = r

	completed := subscribeCompleted(bus)

	wantID := int64(42)
	wantTenant := int64(7)
	wantAsset := int64(99)
	wantTimeout := 5 * time.Second
	wantMeta := map[string]string{"key": "value"}
	publishTrigger(bus, event.ExecutionPayload{
		ExecutionID:  "42",
		TenantID:     "7",
		AssetID:      "99",
		ExecutorType: "http",
		Config:       `{"url":"http://example.com"}`,
		Timeout:      int64(wantTimeout),
	}, event.WithMetadata(wantMeta))

	waitCompleted(t, completed)

	got := exec.LastRequest()
	if got.ID != wantID {
		t.Errorf("req.ID: got %d, want %d", got.ID, wantID)
	}
	if got.TenantID != wantTenant {
		t.Errorf("req.TenantID: got %d, want %d", got.TenantID, wantTenant)
	}
	if got.AssetID != wantAsset {
		t.Errorf("req.AssetID: got %d, want %d", got.AssetID, wantAsset)
	}
	if got.ExecutorName != "http" {
		t.Errorf("req.ExecutorName: got %q, want %q", got.ExecutorName, "http")
	}
	if got.Config != `{"url":"http://example.com"}` {
		t.Errorf("req.Config: got %q, want %q", got.Config, `{"url":"http://example.com"}`)
	}
	if got.Timeout != wantTimeout {
		t.Errorf("req.Timeout: got %v, want %v", got.Timeout, wantTimeout)
	}
	if got.Metadata["key"] != "value" {
		t.Errorf("req.Metadata: got %v, want key=value", got.Metadata)
	}
}

// TestRunnerPublishesTaskCompleted verifies that the Runner publishes a
// TaskCompleted event with the correct status and result after execution.
func TestRunnerPublishesTaskCompleted(t *testing.T) {
	exec := &fakeExecutor{
		typ: "test",
		result: &Result{
			Status:     types.AssetStatusNormal,
			StatusCode: 200,
			Body:       "ok",
		},
	}
	registry := NewRegistry()
	if err := registry.Register(exec); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	r, bus := newTestRunner(t, registry)
	_ = r

	completed := subscribeCompleted(bus)

	publishTrigger(bus, event.ExecutionPayload{
		ExecutionID:  "1",
		TenantID:     "10",
		AssetID:      "100",
		ExecutorType: "test",
	})

	ev := waitCompleted(t, completed)

	if parseExecID(ev.Payload.ExecutionID) != 1 {
		t.Errorf("payload.ExecutionID: got %q, want 1", ev.Payload.ExecutionID)
	}
	if parseExecID(ev.Payload.TenantID) != 10 {
		t.Errorf("payload.TenantID: got %q, want 10", ev.Payload.TenantID)
	}
	if parseExecID(ev.Payload.AssetID) != 100 {
		t.Errorf("payload.AssetID: got %q, want 100", ev.Payload.AssetID)
	}
	if types.AssetStatus(ev.Payload.Status) != types.AssetStatusNormal {
		t.Errorf("payload.Status: got %q, want %q", ev.Payload.Status, types.AssetStatusNormal)
	}
	if ev.Payload.StatusCode != 200 {
		t.Errorf("payload.StatusCode: got %d, want 200", ev.Payload.StatusCode)
	}
	if ev.Payload.Output != "ok" {
		t.Errorf("payload.Output: got %q, want %q", ev.Payload.Output, "ok")
	}
	if ev.Payload.Error != "" {
		t.Errorf("payload.Error: got %q, want empty", ev.Payload.Error)
	}
}

// TestRunnerPublishesTaskCompletedOnError verifies that the Runner publishes
// a TaskCompleted event with Abnormal status when the executor returns an
// error.
func TestRunnerPublishesTaskCompletedOnError(t *testing.T) {
	exec := &fakeExecutor{
		typ: "test",
		err: errors.New("connection refused"),
	}
	registry := NewRegistry()
	if err := registry.Register(exec); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	r, bus := newTestRunner(t, registry)
	_ = r

	completed := subscribeCompleted(bus)

	publishTrigger(bus, event.ExecutionPayload{
		ExecutionID:  "1",
		TenantID:     "10",
		AssetID:      "100",
		ExecutorType: "test",
	})

	ev := waitCompleted(t, completed)

	if types.AssetStatus(ev.Payload.Status) != types.AssetStatusAbnormal {
		t.Errorf("payload.Status: got %q, want %q", ev.Payload.Status, types.AssetStatusAbnormal)
	}
	if ev.Payload.Error == "" {
		t.Error("payload.Error should not be empty on error")
	}
}

// TestRunnerExecutorNotFound verifies that the Runner publishes a TaskCompleted
// event with Abnormal status when no executor is registered for the type.
func TestRunnerExecutorNotFound(t *testing.T) {
	registry := NewRegistry()

	r, bus := newTestRunner(t, registry)
	_ = r

	completed := subscribeCompleted(bus)

	publishTrigger(bus, event.ExecutionPayload{
		ExecutionID:  "1",
		TenantID:     "10",
		AssetID:      "100",
		ExecutorType: "nonexistent",
	})

	ev := waitCompleted(t, completed)

	if types.AssetStatus(ev.Payload.Status) != types.AssetStatusAbnormal {
		t.Errorf("payload.Status: got %q, want %q", ev.Payload.Status, types.AssetStatusAbnormal)
	}
	if ev.Payload.Error == "" {
		t.Error("payload.Error should not be empty when executor not found")
	}
}

// TestRunnerRetrySucceedsAfterFailures verifies that the Runner retries on
// abnormal status and succeeds when the executor eventually returns Normal.
func TestRunnerRetrySucceedsAfterFailures(t *testing.T) {
	exec := &flakyExecutor{
		typ:       "flaky",
		failCount: 2,
	}
	registry := NewRegistry()
	if err := registry.Register(exec); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	r, bus := newTestRunner(t, registry)
	_ = r

	completed := subscribeCompleted(bus)

	publishTrigger(bus, event.ExecutionPayload{
		ExecutionID:  "1",
		TenantID:     "1",
		AssetID:      "1",
		ExecutorType: "flaky",
	}, event.WithMetadata(map[string]string{
		"max_retries":    "3",
		"retry_interval": "1ms",
	}))

	ev := waitCompleted(t, completed)

	// 2 failures + 1 success = 3 total calls.
	if got := exec.calls.Load(); got != 3 {
		t.Errorf("executor calls: got %d, want 3", got)
	}
	if types.AssetStatus(ev.Payload.Status) != types.AssetStatusNormal {
		t.Errorf("payload.Status: got %q, want %q", ev.Payload.Status, types.AssetStatusNormal)
	}
}

// TestRunnerRetryExhausted verifies that the Runner retries the configured
// number of times and publishes Abnormal status when all attempts fail.
func TestRunnerRetryExhausted(t *testing.T) {
	exec := &flakyExecutor{
		typ:       "always-fail",
		failCount: 1000, // always fails
	}
	registry := NewRegistry()
	if err := registry.Register(exec); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	r, bus := newTestRunner(t, registry)
	_ = r

	completed := subscribeCompleted(bus)

	publishTrigger(bus, event.ExecutionPayload{
		ExecutionID:  "1",
		TenantID:     "1",
		AssetID:      "1",
		ExecutorType: "always-fail",
	}, event.WithMetadata(map[string]string{
		"max_retries":    "2",
		"retry_interval": "1ms",
	}))

	ev := waitCompleted(t, completed)

	// 1 initial + 2 retries = 3 total calls.
	if got := exec.calls.Load(); got != 3 {
		t.Errorf("executor calls: got %d, want 3", got)
	}
	if types.AssetStatus(ev.Payload.Status) != types.AssetStatusAbnormal {
		t.Errorf("payload.Status: got %q, want %q", ev.Payload.Status, types.AssetStatusAbnormal)
	}
}

// TestRunnerSavesExecutionRecord verifies that the Runner saves an execution
// record via the configured RecordStore.
func TestRunnerSavesExecutionRecord(t *testing.T) {
	exec := &fakeExecutor{
		typ: "test",
		result: &Result{
			Status:     types.AssetStatusNormal,
			StatusCode: 200,
			Body:       "ok",
		},
	}
	registry := NewRegistry()
	if err := registry.Register(exec); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	store := &countingRecordStore{}
	bus := event.NewBus()
	r, err := New(
		WithExecutorRegistry(registry),
		WithEventBus(bus),
		WithLogger(zap.NewNop()),
		WithRecordStore(store),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("start runner: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = r.Stop(ctx)
		_ = bus.Close()
	})
	r.SubscribeEvents(context.Background())

	completed := subscribeCompleted(bus)

	publishTrigger(bus, event.ExecutionPayload{
		ExecutionID:  "1",
		TenantID:     "10",
		AssetID:      "100",
		ExecutorType: "test",
	})

	waitCompleted(t, completed)

	if got := store.calls.Load(); got != 1 {
		t.Errorf("record store calls: got %d, want 1", got)
	}
	rec := store.LastRecord()
	if rec.TaskID != 1 {
		t.Errorf("record.TaskID: got %d, want 1", rec.TaskID)
	}
	if rec.Status != types.AssetStatusNormal {
		t.Errorf("record.Status: got %q, want %q", rec.Status, types.AssetStatusNormal)
	}
	if rec.StatusCode != 200 {
		t.Errorf("record.StatusCode: got %d, want 200", rec.StatusCode)
	}
	if rec.Output != "ok" {
		t.Errorf("record.Output: got %q, want %q", rec.Output, "ok")
	}
}

// TestRunnerStartWithoutBus verifies that Start returns ErrBusNotConfigured
// when no event bus is configured.
func TestRunnerStartWithoutBus(t *testing.T) {
	r, err := New(WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	err = r.Start(context.Background())
	if !errors.Is(err, errdefs.ErrBusNotConfigured) {
		t.Errorf("Start error: got %v, want ErrBusNotConfigured", err)
	}
}

// TestInferStatus verifies the status inference logic that mirrors the
// scheduler engine's doExecute method.
func TestInferStatus(t *testing.T) {
	tests := []struct {
		name     string
		result   *Result
		err      error
		wantStat types.AssetStatus
		wantMsg  string
	}{
		{
			name:     "normal result",
			result:   &Result{Status: types.AssetStatusNormal},
			wantStat: types.AssetStatusNormal,
		},
		{
			name:     "abnormal result with error message",
			result:   &Result{Status: types.AssetStatusAbnormal, ErrorMsg: "timeout"},
			wantStat: types.AssetStatusAbnormal,
			wantMsg:  "timeout",
		},
		{
			name:     "execution error",
			err:      errors.New("connection refused"),
			wantStat: types.AssetStatusAbnormal,
			wantMsg:  "connection refused",
		},
		{
			name:     "nil result and nil error defaults to abnormal",
			wantStat: types.AssetStatusAbnormal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, msg := inferStatus(tt.result, tt.err)
			if status != tt.wantStat {
				t.Errorf("status: got %q, want %q", status, tt.wantStat)
			}
			if msg != tt.wantMsg {
				t.Errorf("errorMsg: got %q, want %q", msg, tt.wantMsg)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Pool integration tests (Task 6.4)
// ---------------------------------------------------------------------------

// blockingExecutor signals each start via started and blocks until release is
// closed or ctx is cancelled. Used to saturate the pool deterministically.
type blockingExecutor struct {
	typ     string
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (b *blockingExecutor) Name() string             { return b.typ }
func (b *blockingExecutor) Capabilities() Capability { return CapExec }

func (b *blockingExecutor) Execute(ctx context.Context, _ ExecutionRequest) (*Result, error) {
	b.calls.Add(1)
	select {
	case b.started <- struct{}{}:
	default:
	}
	select {
	case <-b.release:
		return &Result{Status: types.AssetStatusNormal}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// newPoolRunner creates a Runner wired to an injected pool, fresh event bus,
// and the given registry. It returns the runner, bus, and pool for teardown.
func newPoolRunner(t *testing.T, registry *Registry, p pool.Pool) (Runner, event.Bus, pool.Pool) {
	t.Helper()
	bus := event.NewBus()
	if registry == nil {
		registry = NewRegistry()
	}
	r, err := New(
		WithExecutorRegistry(registry),
		WithEventBus(bus),
		WithLogger(zap.NewNop()),
		WithPool(p),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("start runner: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = r.Stop(ctx)
		_ = bus.Close()
	})
	r.SubscribeEvents(context.Background())
	return r, bus, p
}

// TestRunnerWithInjectedPool verifies that an explicitly injected pool is used
// for task dispatch: a published trigger executes and produces a TaskCompleted
// event, proving the job flowed through the injected pool.
func TestRunnerWithInjectedPool(t *testing.T) {
	p, err := pool.New(pool.WithWorkers(2), pool.WithQueueSize(4))
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Shutdown(ctx)
	})

	exec := &fakeExecutor{typ: "test"}
	registry := NewRegistry()
	if err := registry.Register(exec); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	r, bus, _ := newPoolRunner(t, registry, p)
	_ = r

	completed := subscribeCompleted(bus)

	publishTrigger(bus, event.ExecutionPayload{
		ExecutionID:  "1",
		TenantID:     "10",
		AssetID:      "100",
		ExecutorType: "test",
	})

	waitCompleted(t, completed)

	if got := exec.calls.Load(); got != 1 {
		t.Errorf("executor calls: got %d, want 1", got)
	}

	// The injected pool must remain usable after Stop (caller owns it).
	// Submit a trivial job to confirm the pool was NOT closed by Stop.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := r.Stop(stopCtx); err != nil {
		t.Fatalf("stop runner: %v", err)
	}

	ran := make(chan struct{})
	if err := p.Submit(context.Background(), pool.Lambda(func(ctx context.Context) error {
		close(ran)
		return nil
	})); err != nil {
		t.Fatalf("submit to injected pool after Stop: %v", err)
	}
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("injected pool job did not run after Stop")
	}
}

// TestRunnerPoolCallerRuns verifies that when the injected pool is saturated
// and configured with RejectionCallerRuns, the dispatch path runs the task
// inline on the caller's goroutine instead of dropping it.
//
// Pool config: 1 worker, queue size 1, RejectionCallerRuns. The first trigger
// occupies the single worker (blocking executor), the second fills the queue,
// and the third — a quick executor — must run inline and complete while the
// worker is still busy.
func TestRunnerPoolCallerRuns(t *testing.T) {
	p, err := pool.New(
		pool.WithWorkers(1),
		pool.WithQueueSize(1),
		pool.WithRejectionPolicy(pool.RejectionCallerRuns),
	)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.Shutdown(ctx)
	})

	blockExec := &blockingExecutor{
		typ:     "block",
		started: make(chan struct{}, 8),
		release: make(chan struct{}),
	}
	quickExec := &fakeExecutor{typ: "quick"}
	registry := NewRegistry()
	if err := registry.Register(blockExec); err != nil {
		t.Fatalf("register block executor: %v", err)
	}
	if err := registry.Register(quickExec); err != nil {
		t.Fatalf("register quick executor: %v", err)
	}

	r, bus, _ := newPoolRunner(t, registry, p)
	_ = r

	// Use a generously buffered channel so A/B completion events do not
	// block the dispatch loop after C's event has been consumed.
	completed := make(chan event.Event[event.ExecutionPayload], 8)
	_, err = event.Subscribe[event.ExecutionPayload](bus, event.TypeExecutionCompleted, func(_ context.Context, e event.Event[event.ExecutionPayload]) error {
		completed <- e
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Trigger A: occupies the single worker and blocks.
	publishTrigger(bus, event.ExecutionPayload{
		ExecutionID:  "1",
		TenantID:     "1",
		AssetID:      "1",
		ExecutorType: "block",
		Timeout:      int64(30 * time.Second),
	})
	waitStarted(t, blockExec.started)

	// Trigger B: fills the 1-slot queue (Submit returns immediately).
	publishTrigger(bus, event.ExecutionPayload{
		ExecutionID:  "2",
		TenantID:     "1",
		AssetID:      "2",
		ExecutorType: "block",
		Timeout:      int64(30 * time.Second),
	})

	// Trigger C: pool is saturated (worker busy + queue full). Under
	// RejectionCallerRuns it runs inline and completes synchronously,
	// producing a TaskCompleted event before A is released.
	publishTrigger(bus, event.ExecutionPayload{
		ExecutionID:  "3",
		TenantID:     "1",
		AssetID:      "3",
		ExecutorType: "quick",
		Timeout:      int64(5 * time.Second),
	})

	ev := waitCompleted(t, completed)
	if parseExecID(ev.Payload.ExecutionID) != 3 {
		t.Errorf("completed ID: got %q, want 3 (inline quick task)", ev.Payload.ExecutionID)
	}
	if types.AssetStatus(ev.Payload.Status) != types.AssetStatusNormal {
		t.Errorf("completed status: got %q, want %q", ev.Payload.Status, types.AssetStatusNormal)
	}

	// Release the blocked tasks so the runner can drain and shut down.
	close(blockExec.release)
}

// waitStarted waits for a blocking executor to signal it has started or fails
// the test on timeout.
func waitStarted(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for blocking executor to start")
	}
}

// TestRunnerStopClosesDefaultPool verifies that Stop shuts down the default
// pool created by New (when no pool is injected). After Stop, submitting to
// the default pool returns pool.ErrClosed.
func TestRunnerStopClosesDefaultPool(t *testing.T) {
	bus := event.NewBus()
	r, err := New(
		WithEventBus(bus),
		WithLogger(zap.NewNop()),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := r.Stop(stopCtx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	_ = bus.Close()

	rr := r.(*runner)
	if !rr.poolOwned {
		t.Fatal("expected poolOwned=true for default pool")
	}
	if err := rr.pool.Submit(context.Background(), pool.Lambda(func(ctx context.Context) error {
		return nil
	})); !errors.Is(err, pool.ErrClosed) {
		t.Errorf("submit to default pool after Stop: got %v, want pool.ErrClosed", err)
	}
}

// TestRunnerStopDoesNotCloseInjectedPool verifies that Stop leaves an injected
// pool open. After Stop, the injected pool must still accept and run jobs.
func TestRunnerStopDoesNotCloseInjectedPool(t *testing.T) {
	p, err := pool.New(pool.WithWorkers(1), pool.WithQueueSize(2))
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Shutdown(ctx)
	})

	bus := event.NewBus()
	r, err := New(
		WithEventBus(bus),
		WithLogger(zap.NewNop()),
		WithPool(p),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := r.Stop(stopCtx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	_ = bus.Close()

	rr := r.(*runner)
	if rr.poolOwned {
		t.Fatal("expected poolOwned=false for injected pool")
	}

	ran := make(chan struct{})
	if err := p.Submit(context.Background(), pool.Lambda(func(ctx context.Context) error {
		close(ran)
		return nil
	})); err != nil {
		t.Fatalf("submit to injected pool after Stop: %v", err)
	}
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("injected pool job did not run after Stop")
	}
}

// ---------------------------------------------------------------------------
// Time wheel integration tests
// ---------------------------------------------------------------------------

// newWheelRunner creates a Runner wired to a fresh event bus, registry, and
// a real [timewheel.Wheel] for async retry. It returns the runner, the bus,
// and the wheel for teardown. The wheel's tick loop is started in a goroutine.
func newWheelRunner(t *testing.T, registry *Registry) (Runner, event.Bus, timewheel.Wheel) {
	t.Helper()
	bus := event.NewBus()
	if registry == nil {
		registry = NewRegistry()
	}
	wheel, err := timewheel.New(timewheel.WithWorkerSize(4))
	if err != nil {
		t.Fatalf("create wheel: %v", err)
	}
	wheelCtx, wheelCancel := context.WithCancel(context.Background())
	go wheel.Start(wheelCtx)

	r, err := New(
		WithExecutorRegistry(registry),
		WithEventBus(bus),
		WithLogger(zap.NewNop()),
		WithTimeWheel(wheel),
	)
	if err != nil {
		wheelCancel()
		t.Fatalf("New() error = %v", err)
	}
	if err := r.Start(context.Background()); err != nil {
		wheelCancel()
		t.Fatalf("start runner: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = r.Stop(ctx)
		wheelCancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = wheel.Stop(stopCtx)
		_ = bus.Close()
	})
	r.SubscribeEvents(context.Background())
	return r, bus, wheel
}

// TestRunnerAsyncRetrySucceedsAfterFailures verifies that the Runner uses
// DoAsync (via the time wheel) for retry scheduling: a flaky executor that
// fails twice then succeeds produces a Normal completion event after the
// retries fire from the wheel's tick loop.
func TestRunnerAsyncRetrySucceedsAfterFailures(t *testing.T) {
	exec := &flakyExecutor{
		typ:       "flaky",
		failCount: 2,
	}
	registry := NewRegistry()
	if err := registry.Register(exec); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	r, bus, _ := newWheelRunner(t, registry)
	_ = r

	completed := subscribeCompleted(bus)

	publishTrigger(bus, event.ExecutionPayload{
		ExecutionID:  "1",
		TenantID:     "1",
		AssetID:      "1",
		ExecutorType: "flaky",
	}, event.WithMetadata(map[string]string{
		"max_retries":    "3",
		"retry_interval": "1s",
	}))

	ev := waitCompleted(t, completed)

	// 2 failures + 1 success = 3 total calls.
	if got := exec.calls.Load(); got != 3 {
		t.Errorf("executor calls: got %d, want 3", got)
	}
	if types.AssetStatus(ev.Payload.Status) != types.AssetStatusNormal {
		t.Errorf("payload.Status: got %q, want %q", ev.Payload.Status, types.AssetStatusNormal)
	}
}

// TestRunnerAsyncRetryExhausted verifies that the Runner exhausts all retry
// attempts via the async time wheel path and publishes Abnormal status when
// every attempt fails.
func TestRunnerAsyncRetryExhausted(t *testing.T) {
	exec := &flakyExecutor{
		typ:       "always-fail",
		failCount: 1000,
	}
	registry := NewRegistry()
	if err := registry.Register(exec); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	r, bus, _ := newWheelRunner(t, registry)
	_ = r

	completed := subscribeCompleted(bus)

	publishTrigger(bus, event.ExecutionPayload{
		ExecutionID:  "1",
		TenantID:     "1",
		AssetID:      "1",
		ExecutorType: "always-fail",
	}, event.WithMetadata(map[string]string{
		"max_retries":    "2",
		"retry_interval": "1s",
	}))

	ev := waitCompleted(t, completed)

	// 1 initial + 2 retries = 3 total calls.
	if got := exec.calls.Load(); got != 3 {
		t.Errorf("executor calls: got %d, want 3", got)
	}
	if types.AssetStatus(ev.Payload.Status) != types.AssetStatusAbnormal {
		t.Errorf("payload.Status: got %q, want %q", ev.Payload.Status, types.AssetStatusAbnormal)
	}
}

// TestRunnerAsyncRetryNoRetrySucceeds verifies that a task without retry
// config still succeeds through the async path when a wheel is injected.
func TestRunnerAsyncRetryNoRetrySucceeds(t *testing.T) {
	exec := &fakeExecutor{
		typ: "test",
		result: &Result{
			Status:     types.AssetStatusNormal,
			StatusCode: 200,
			Body:       "ok",
		},
	}
	registry := NewRegistry()
	if err := registry.Register(exec); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	r, bus, _ := newWheelRunner(t, registry)
	_ = r

	completed := subscribeCompleted(bus)

	publishTrigger(bus, event.ExecutionPayload{
		ExecutionID:  "1",
		TenantID:     "10",
		AssetID:      "100",
		ExecutorType: "test",
	})

	ev := waitCompleted(t, completed)

	if got := exec.calls.Load(); got != 1 {
		t.Errorf("executor calls: got %d, want 1", got)
	}
	if types.AssetStatus(ev.Payload.Status) != types.AssetStatusNormal {
		t.Errorf("payload.Status: got %q, want %q", ev.Payload.Status, types.AssetStatusNormal)
	}
}
