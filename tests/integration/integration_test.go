// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package integration_test verifies cross-module communication between
// the Scheduler, Executor, and Collector modules through the event bus.
//
// The three-module architecture separates concerns as follows:
//   - Scheduler: publishes ExecutionTriggered events when tasks are due.
//   - Executor Runner: subscribes to ExecutionTriggered, executes tasks, and
//     publishes ExecutionCompleted events.
//   - Collector: independently observes resources and publishes StatusChange
//     events; it is fully decoupled from the scheduler.
package integration_test

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/scheduler"
	"github.com/tickraft/tickraft/pkg/task"
	collectapi "github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/types"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// remediationExecutor is a test executor that simulates a remediation action.
// It returns StatusNormal to indicate successful remediation.
type remediationExecutor struct {
	mu    sync.Mutex
	calls []executor.ExecutionRequest
}

func (e *remediationExecutor) Name() string                      { return "remediation" }
func (e *remediationExecutor) Capabilities() executor.Capability { return executor.CapExec }

// Execute records the request and returns a Normal status result.
func (e *remediationExecutor) Execute(_ context.Context, req executor.ExecutionRequest) (*executor.Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, req)
	return &executor.Result{
		Status:   types.AssetStatusNormal,
		Body:     "recovered",
		Duration: time.Millisecond,
	}, nil
}

// Calls returns a copy of the recorded execution requests.
func (e *remediationExecutor) Calls() []executor.ExecutionRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]executor.ExecutionRequest{}, e.calls...)
}

// mockAssetStore implements asset.Store for integration testing.
type mockAssetStore struct {
	mu        sync.RWMutex
	resources map[int64]*asset.Asset
}

func newMockAssetStore() *mockAssetStore {
	return &mockAssetStore{resources: make(map[int64]*asset.Asset)}
}

func (s *mockAssetStore) CountByStatus(_ context.Context) (map[string]int64, error) {
	return map[string]int64{}, nil
}

func (s *mockAssetStore) Create(_ context.Context, r *asset.Asset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources[r.ID] = r
	return nil
}

func (s *mockAssetStore) Update(_ context.Context, r *asset.Asset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources[r.ID] = r
	return nil
}

func (s *mockAssetStore) GetByID(_ context.Context, id int64) (*asset.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.resources[id]
	if !ok {
		return nil, fmt.Errorf("asset not found: %d", id)
	}
	return r, nil
}

func (s *mockAssetStore) GetByKey(_ context.Context, tenantID int64, key string) (*asset.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.resources {
		if r.TenantID == tenantID && r.AssetKey == key {
			return r, nil
		}
	}
	return nil, fmt.Errorf("asset not found: tenant=%d key=%s", tenantID, key)
}

func (s *mockAssetStore) UpdateStatus(_ context.Context, id int64, status types.AssetStatus, activeAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.resources[id]; ok {
		r.Status = status
		r.LastActiveAt = activeAt
	}
	return nil
}

func (s *mockAssetStore) Migrate(_ context.Context) error { return nil }

func (s *mockAssetStore) List(_ context.Context, page, size int, _ asset.ListFilter) ([]*asset.Asset, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := int64(len(s.resources))
	all := make([]*asset.Asset, 0, total)
	for _, r := range s.resources {
		all = append(all, r)
	}
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	offset := (page - 1) * size
	if offset >= int(total) {
		return nil, total, nil
	}
	end := offset + size
	if end > int(total) {
		end = int(total)
	}
	return all[offset:end], total, nil
}

func (s *mockAssetStore) Delete(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.resources, id)
	return nil
}

func (s *mockAssetStore) CountByType(_ context.Context, tenantID int64, assetType types.AssetType) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int64
	for _, r := range s.resources {
		if r.TenantID == tenantID && r.AssetType == assetType {
			count++
		}
	}
	return count, nil
}

func (s *mockAssetStore) ExistsByKey(_ context.Context, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.resources {
		if r.AssetKey == key {
			return true, nil
		}
	}
	return false, nil
}

func (s *mockAssetStore) ListKeyset(_ context.Context, _ pagination.PageRequest) (pagination.PageResult[*asset.Asset], error) {
	return pagination.PageResult[*asset.Asset]{}, nil
}

// GetStatus returns the current status of a asset from the store.
func (s *mockAssetStore) GetStatus(id int64) types.AssetStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r, ok := s.resources[id]; ok {
		return r.Status
	}
	return types.AssetStatusUnknown
}

// mockProcessor implements collectapi.Processor for integration testing.
// It is safe for concurrent use.
type mockProcessor struct {
	mu     sync.Mutex
	calls  int
	result *collectapi.ProcessResult
}

func (p *mockProcessor) Type() types.AssetType { return types.AssetTypeDevice }

// Process records the call and returns the configured result.
func (p *mockProcessor) Process(_ context.Context, _ *collectapi.Telemetry) (*collectapi.ProcessResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.result, nil
}

func (p *mockProcessor) OnTimeout(_ context.Context, _ int64) error { return nil }

// Calls returns the number of times Process was invoked.
func (p *mockProcessor) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// mockMetricStore implements collectapi.MetricStore for integration testing.
type mockMetricStore struct {
	mu      sync.Mutex
	metrics []collectapi.CollectMetric
}

func (s *mockMetricStore) SaveMetric(_ context.Context, metric *collectapi.CollectMetric) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metrics = append(s.metrics, *metric)
	return nil
}

func (s *mockMetricStore) SaveMetricsBatch(_ context.Context, metrics []*collectapi.CollectMetric) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range metrics {
		s.metrics = append(s.metrics, *m)
	}
	return nil
}

func (s *mockMetricStore) QueryMetrics(_ context.Context, _ int64, _ int64, _ string, _ time.Time, _ time.Time, _ int) ([]collectapi.CollectMetric, error) {
	return nil, nil
}

// Count returns the number of saved metric records.
func (s *mockMetricStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.metrics)
}

// mockLogStore implements collectapi.LogStore for integration testing.
type mockLogStore struct {
	mu   sync.Mutex
	logs []collectapi.CollectLog
}

func (s *mockLogStore) SaveLog(_ context.Context, log *collectapi.CollectLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, *log)
	return nil
}

func (s *mockLogStore) SaveLogsBatch(_ context.Context, logs []*collectapi.CollectLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range logs {
		s.logs = append(s.logs, *l)
	}
	return nil
}

func (s *mockLogStore) QueryLogs(_ context.Context, _ int64, _ int64, _ string, _ time.Time, _ time.Time, _ int) ([]collectapi.CollectLog, error) {
	return nil, nil
}

// Count returns the number of saved log records.
func (s *mockLogStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.logs)
}

// ---------------------------------------------------------------------------
// SubTask 23.1: TestFaultRemediationLoop
// ---------------------------------------------------------------------------

// TestFaultRemediationLoop verifies the complete fault remediation loop in the new
// three-module architecture:
//  1. Collector detects asset abnormal -> publishes StatusChange event
//  2. Scheduler receives StatusChange -> publishes ExecutionTriggered event
//  3. Executor Runner receives ExecutionTriggered -> executes task -> publishes ExecutionCompleted
//  4. Scheduler receives ExecutionCompleted -> updates dependency tracker
func TestFaultRemediationLoop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a shared event bus.
	bus := event.NewBus()

	// --- Set up mock executor ---
	exec := &remediationExecutor{}
	registry := executor.NewRegistry()
	if err := registry.Register(exec); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	// --- Set up executor Runner ---
	runner, err := executor.New(
		executor.WithExecutorRegistry(registry),
		executor.WithEventBus(bus),
		executor.WithLogger(zap.NewNop()),
	)
	if err != nil {
		t.Fatalf("executor.New() error = %v", err)
	}
	if err := runner.Start(ctx); err != nil {
		t.Fatalf("start runner: %v", err)
	}
	runner.SubscribeEvents(ctx)
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = runner.Stop(stopCtx)
		_ = bus.Close()
	})

	// --- Set up Scheduler ---
	sched, err := task.NewService(
		task.WithEventBus(bus),
		task.WithLogger(zap.NewNop()),
	)
	if err != nil {
		t.Fatalf("create scheduler: %v", err)
	}
	sched.SubscribeEvents(ctx)
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = sched.Stop(stopCtx)
	})

	// Register an event-driven task for asset 1.
	tk := task.Task{
		ID:           100,
		TenantID:     1,
		AssetID:      1,
		ExecutorName: "remediation",
		Timeout:      5 * time.Second,
		Metadata: map[string]string{
			"schedule_type": "event",
		},
	}
	if err := sched.Register(ctx, tk); err != nil {
		t.Fatalf("register event task: %v", err)
	}

	// Subscribe to ExecutionCompleted to verify the full chain.
	completedCh := make(chan event.Event[event.ExecutionPayload], 1)
	_, _ = event.Subscribe[event.ExecutionPayload](bus, event.TypeExecutionCompleted, func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
		completedCh <- ev
		return nil
	})

	// --- Set up Collector ---
	store := newMockAssetStore()
	if err := store.Create(ctx, &asset.Asset{
		ID:        1,
		TenantID:  1,
		AssetType: types.AssetTypeDevice,
		AssetKey:  "device-1",
		Status:    types.AssetStatusNormal,
	}); err != nil {
		t.Fatalf("create asset: %v", err)
	}

	processorReg := collectapi.NewProcessorRegistry()
	proc := &mockProcessor{result: &collectapi.ProcessResult{
		PrevStatus: types.AssetStatusNormal,
		CurrStatus: types.AssetStatusAbnormal,
		Reason:     "device unreachable",
	}}
	if err := processorReg.Register(proc); err != nil {
		t.Fatalf("register processor: %v", err)
	}

	c, err := collectapi.New(
		collectapi.WithProcessorRegistry(processorReg),
		collectapi.WithAssetStore(store),
		collectapi.WithEventBus(bus),
		collectapi.WithLogger(zap.NewNop()),
	)
	if err != nil {
		t.Fatalf("create telemetry: %v", err)
	}
	if err := c.Start(ctx); err != nil {
		t.Fatalf("start telemetry: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = c.Stop(stopCtx)
	})

	if err := c.RegisterAsset(ctx, collectapi.Config{
		AssetID: 1,
		Timeout: 300,
	}); err != nil {
		t.Fatalf("register asset: %v", err)
	}

	// --- Step 1: Simulate Collector detecting abnormal status ---
	c.Submit(&collectapi.Telemetry{
		AssetID:     1,
		TenantID:    1,
		AssetType:   types.AssetTypeDevice,
		SourceType:  "integration_test",
		CollectedAt: time.Now(),
		Status:      types.AssetStatusAbnormal,
	})

	// --- Verify the full event chain ---
	// Collector -> StatusChange -> Scheduler -> ExecutionTriggered -> Runner -> ExecutionCompleted
	select {
	case ev := <-completedCh:
		if ev.Payload.ExecutionID != "100" {
			t.Errorf("ExecutionCompleted task ID: got %q, want %q", ev.Payload.ExecutionID, "100")
		}
		if ev.Payload.TenantID != "1" {
			t.Errorf("ExecutionCompleted tenant ID: got %q, want %q", ev.Payload.TenantID, "1")
		}
		if ev.Payload.AssetID != "1" {
			t.Errorf("ExecutionCompleted asset ID: got %q, want %q", ev.Payload.AssetID, "1")
		}
		if ev.Payload.Status != string(types.AssetStatusNormal) {
			t.Errorf("ExecutionCompleted status: got %q, want %q", ev.Payload.Status, types.AssetStatusNormal)
		}
		if ev.Payload.Output != "recovered" {
			t.Errorf("ExecutionCompleted body: got %q, want %q", ev.Payload.Output, "recovered")
		}
		if ev.Payload.Error != "" {
			t.Errorf("ExecutionCompleted error: got %q, want empty", ev.Payload.Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ExecutionCompleted event")
	}

	// --- Verify the remediation executor was called ---
	calls := exec.Calls()
	if len(calls) == 0 {
		t.Fatal("expected remediation task to be triggered, but no calls recorded")
	}
	if calls[0].ID != 100 {
		t.Errorf("remediation task ID: got %d, want 100", calls[0].ID)
	}
	if calls[0].AssetID != 1 {
		t.Errorf("remediation task AssetID: got %d, want 1", calls[0].AssetID)
	}
	if calls[0].ExecutorName != "remediation" {
		t.Errorf("remediation task ExecutorName: got %q, want %q", calls[0].ExecutorName, "remediation")
	}
}

// ---------------------------------------------------------------------------
// SubTask 23.2: TestCollectorIndependentRun
// ---------------------------------------------------------------------------

// TestCollectorIndependentRun verifies that the telemetry operates fully
// independently of the scheduler and executor. It exercises the complete
// pipeline: Submit -> Validator -> Processor -> Aggregator -> Persistence.
//
// The test verifies:
//   - Valid data passes validation, is processed, aggregated, and persisted.
//   - Invalid data (non-existent asset) is discarded by the validator.
func TestCollectorIndependentRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// --- Set up asset store ---
	store := newMockAssetStore()
	if err := store.Create(ctx, &asset.Asset{
		ID:        1,
		TenantID:  1,
		AssetType: types.AssetTypeDevice,
		AssetKey:  "device-1",
		Status:    types.AssetStatusUnknown,
	}); err != nil {
		t.Fatalf("create asset: %v", err)
	}

	// --- Set up mock stores for persistence ---
	metricStore := &mockMetricStore{}
	logStore := &mockLogStore{}

	// --- Set up mock processor ---
	processorReg := collectapi.NewProcessorRegistry()
	proc := &mockProcessor{result: &collectapi.ProcessResult{
		PrevStatus: types.AssetStatusUnknown,
		CurrStatus: types.AssetStatusNormal,
		Reason:     "device online",
	}}
	if err := processorReg.Register(proc); err != nil {
		t.Fatalf("register processor: %v", err)
	}

	// --- Set up Collector (no scheduler, no executor) ---
	bus := event.NewBus()
	c, err := collectapi.New(
		collectapi.WithProcessorRegistry(processorReg),
		collectapi.WithAssetStore(store),
		collectapi.WithEventBus(bus),
		collectapi.WithLogger(zap.NewNop()),
		collectapi.WithAggregationWindow(50*time.Millisecond),
		collectapi.WithMetricStore(metricStore),
		collectapi.WithLogStore(logStore),
	)
	if err != nil {
		t.Fatalf("create telemetry: %v", err)
	}
	if err := c.Start(ctx); err != nil {
		t.Fatalf("start telemetry: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = c.Stop(stopCtx)
		_ = bus.Close()
	})

	// Register the asset for observation.
	if err := c.RegisterAsset(ctx, collectapi.Config{
		AssetID: 1,
		Timeout: 300,
	}); err != nil {
		t.Fatalf("register asset: %v", err)
	}

	// --- Submit a valid telemetry with metrics and log content ---
	c.Submit(&collectapi.Telemetry{
		AssetID:     1,
		TenantID:    1,
		AssetType:   types.AssetTypeDevice,
		SourceType:  "webhook",
		CollectedAt: time.Now(),
		Status:      types.AssetStatusNormal,
		Metrics: map[string]float64{
			"cpu_usage": 75.5,
		},
		LogContent: "device reported healthy",
		LogLevel:   "INFO",
	})

	// --- Submit an invalid telemetry (non-existent asset) ---
	c.Submit(&collectapi.Telemetry{
		AssetID:     999,
		TenantID:    1,
		AssetType:   types.AssetTypeDevice,
		SourceType:  "webhook",
		CollectedAt: time.Now(),
		Status:      types.AssetStatusNormal,
	})

	// Wait for the telemetry to be processed by the processLoop goroutine.
	time.Sleep(200 * time.Millisecond)

	// --- Verify: processor was called once (valid telemetry only) ---
	if procCalls := proc.Calls(); procCalls != 1 {
		t.Errorf("processor calls: got %d, want 1 (invalid telemetry should be discarded)", procCalls)
	}

	// --- Verify: log was persisted directly ---
	if logCount := logStore.Count(); logCount != 1 {
		t.Errorf("log store count: got %d, want 1", logCount)
	}

	// --- Verify: metrics were aggregated and persisted ---
	// The aggregation window is 50ms. The aggregator flushes every 25ms
	// (window/2). Waiting 500ms gives plenty of time for the window to
	// expire and the flush to be processed by the consumer goroutine.
	time.Sleep(500 * time.Millisecond)

	// One metric produces 5 aggregated records: _avg, _max, _min, _count, _sum.
	if metricCount := metricStore.Count(); metricCount != 5 {
		t.Errorf("metric store count: got %d, want 5 (one metric aggregated into avg/max/min/count/sum)", metricCount)
	}
}

// ---------------------------------------------------------------------------
// SubTask 23.3: TestExecutorRunnerIntegration
// ---------------------------------------------------------------------------

// TestExecutorRunnerIntegration verifies the executor Runner's full event flow:
// ExecutionTriggered -> execute -> ExecutionCompleted.
//
// The test publishes an ExecutionTriggered event on the event bus and verifies that:
//   - The Runner receives the event and dispatches it to the executor.
//   - The executor is called with the correct ExecutionRequest.
//   - The Runner publishes an ExecutionCompleted event with the correct status
//     inferred from the execution result.
func TestExecutorRunnerIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bus := event.NewBus()

	// --- Set up mock executor ---
	exec := &remediationExecutor{}
	registry := executor.NewRegistry()
	if err := registry.Register(exec); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	// --- Set up Runner ---
	runner, err := executor.New(
		executor.WithExecutorRegistry(registry),
		executor.WithEventBus(bus),
		executor.WithLogger(zap.NewNop()),
	)
	if err != nil {
		t.Fatalf("executor.New() error = %v", err)
	}
	if err := runner.Start(ctx); err != nil {
		t.Fatalf("start runner: %v", err)
	}
	runner.SubscribeEvents(ctx)
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = runner.Stop(stopCtx)
		_ = bus.Close()
	})

	// --- Subscribe to ExecutionCompleted events ---
	completedCh := make(chan event.Event[event.ExecutionPayload], 1)
	_, _ = event.Subscribe[event.ExecutionPayload](bus, event.TypeExecutionCompleted, func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
		completedCh <- ev
		return nil
	})

	// --- Publish an ExecutionTriggered event ---
	wantTaskID := int64(200)
	wantTenantID := int64(1)
	wantResourceID := int64(10)
	wantTimeout := 5 * time.Second

	_ = event.Publish(context.Background(), bus, event.TypeExecutionTriggered, event.ExecutionPayload{
		ExecutionID:  strconv.FormatInt(wantTaskID, 10),
		TenantID:     strconv.FormatInt(wantTenantID, 10),
		AssetID:      strconv.FormatInt(wantResourceID, 10),
		ExecutorType: "remediation",
		Timeout:      int64(wantTimeout),
	})

	// --- Verify ExecutionCompleted event ---
	select {
	case ev := <-completedCh:
		if ev.Payload.ExecutionID != strconv.FormatInt(wantTaskID, 10) {
			t.Errorf("ExecutionCompleted task ID: got %q, want %d", ev.Payload.ExecutionID, wantTaskID)
		}
		if ev.Payload.TenantID != strconv.FormatInt(wantTenantID, 10) {
			t.Errorf("ExecutionCompleted tenant ID: got %q, want %d", ev.Payload.TenantID, wantTenantID)
		}
		if ev.Payload.AssetID != strconv.FormatInt(wantResourceID, 10) {
			t.Errorf("ExecutionCompleted asset ID: got %q, want %d", ev.Payload.AssetID, wantResourceID)
		}
		// The remediation executor returns StatusNormal, so the inferred status
		// should be Normal.
		if ev.Payload.Status != string(types.AssetStatusNormal) {
			t.Errorf("ExecutionCompleted status: got %q, want %q", ev.Payload.Status, types.AssetStatusNormal)
		}
		if ev.Payload.Output != "recovered" {
			t.Errorf("ExecutionCompleted body: got %q, want %q", ev.Payload.Output, "recovered")
		}
		if ev.Payload.Error != "" {
			t.Errorf("ExecutionCompleted error: got %q, want empty", ev.Payload.Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ExecutionCompleted event")
	}

	// --- Verify executor was called with the correct request ---
	calls := exec.Calls()
	if len(calls) != 1 {
		t.Fatalf("executor calls: got %d, want 1", len(calls))
	}
	got := calls[0]
	if got.ID != wantTaskID {
		t.Errorf("executor request ID: got %d, want %d", got.ID, wantTaskID)
	}
	if got.TenantID != wantTenantID {
		t.Errorf("executor request TenantID: got %d, want %d", got.TenantID, wantTenantID)
	}
	if got.AssetID != wantResourceID {
		t.Errorf("executor request AssetID: got %d, want %d", got.AssetID, wantResourceID)
	}
	if got.ExecutorName != "remediation" {
		t.Errorf("executor request ExecutorName: got %q, want %q", got.ExecutorName, "remediation")
	}
	if got.Timeout != wantTimeout {
		t.Errorf("executor request Timeout: got %v, want %v", got.Timeout, wantTimeout)
	}
}

// ---------------------------------------------------------------------------
// SubTask 23.4: TestShardDistribution
// ---------------------------------------------------------------------------

// TestShardDistribution verifies that in a sharded deployment, only the node
// owning a task triggers it. Two scheduler instances are configured with
// ModuloShardStrategy (total=2): shard 0 owns even task IDs, shard 1 owns
// odd task IDs.
//
// The test registers the same set of tasks on both schedulers (simulating
// shared task data in a distributed deployment) and verifies that:
//   - Shard 0 only triggers tasks with even IDs (2, 4, 6).
//   - Shard 1 only triggers tasks with odd IDs (1, 3, 5).
//   - Each task is triggered by exactly one shard.
func TestShardDistribution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Two separate event buses so we can distinguish which node triggered.
	bus0 := event.NewBus()
	defer bus0.Close()
	bus1 := event.NewBus()
	defer bus1.Close()

	// --- Shard 0: owns even task IDs (taskID % 2 == 0) ---
	sched0, err := task.NewService(
		task.WithShardManager(scheduler.NewShardManager(2, 0)),
		task.WithEventBus(bus0),
		task.WithLogger(zap.NewNop()),
	)
	if err != nil {
		t.Fatalf("create scheduler shard 0: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = sched0.Stop(stopCtx)
	})

	// --- Shard 1: owns odd task IDs (taskID % 2 == 1) ---
	sched1, err := task.NewService(
		task.WithShardManager(scheduler.NewShardManager(2, 1)),
		task.WithEventBus(bus1),
		task.WithLogger(zap.NewNop()),
	)
	if err != nil {
		t.Fatalf("create scheduler shard 1: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = sched1.Stop(stopCtx)
	})

	// Subscribe to ExecutionTriggered on each bus.
	triggered0 := make(chan event.Event[event.ExecutionPayload], 10)
	_, _ = event.Subscribe[event.ExecutionPayload](bus0, event.TypeExecutionTriggered, func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
		triggered0 <- ev
		return nil
	})
	triggered1 := make(chan event.Event[event.ExecutionPayload], 10)
	_, _ = event.Subscribe[event.ExecutionPayload](bus1, event.TypeExecutionTriggered, func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
		triggered1 <- ev
		return nil
	})

	// Register tasks on both schedulers with "once" schedule (fires immediately).
	// In a real deployment, both nodes load the same tasks from the DB.
	taskIDs := []int64{1, 2, 3, 4, 5, 6}
	for _, id := range taskIDs {
		tk := task.Task{
			ID:           id,
			TenantID:     1,
			AssetID:      id,
			ExecutorName: "webhook",
			Timeout:      5 * time.Second,
			Metadata: map[string]string{
				"schedule_type": "once",
			},
		}
		if err := sched0.Register(ctx, tk); err != nil {
			t.Fatalf("sched0 register task %d: %v", id, err)
		}
		if err := sched1.Register(ctx, tk); err != nil {
			t.Fatalf("sched1 register task %d: %v", id, err)
		}
	}

	// Collect triggered task IDs from each shard.
	// Each shard should trigger exactly 3 tasks (half of 6).
	collectN := func(ch <-chan event.Event[event.ExecutionPayload], n int, timeout time.Duration) []int64 {
		var ids []int64
		deadline := time.After(timeout)
		for len(ids) < n {
			select {
			case ev := <-ch:
				id, _ := strconv.ParseInt(ev.Payload.ExecutionID, 10, 64)
				ids = append(ids, id)
			case <-deadline:
				return ids
			}
		}
		return ids
	}

	shard0Triggers := collectN(triggered0, 3, 5*time.Second)
	shard1Triggers := collectN(triggered1, 3, 5*time.Second)

	// Sort for deterministic comparison.
	sort.Slice(shard0Triggers, func(i, j int) bool { return shard0Triggers[i] < shard0Triggers[j] })
	sort.Slice(shard1Triggers, func(i, j int) bool { return shard1Triggers[i] < shard1Triggers[j] })

	// --- Verify shard 0 owns even task IDs ---
	expectedShard0 := []int64{2, 4, 6}
	if !equalInt64Slices(shard0Triggers, expectedShard0) {
		t.Errorf("shard 0 triggers: got %v, want %v", shard0Triggers, expectedShard0)
	}

	// --- Verify shard 1 owns odd task IDs ---
	expectedShard1 := []int64{1, 3, 5}
	if !equalInt64Slices(shard1Triggers, expectedShard1) {
		t.Errorf("shard 1 triggers: got %v, want %v", shard1Triggers, expectedShard1)
	}

	// --- Verify no task was triggered by both shards ---
	allTriggers := make(map[int64]int)
	for _, id := range shard0Triggers {
		allTriggers[id]++
	}
	for _, id := range shard1Triggers {
		allTriggers[id]++
	}
	for id, count := range allTriggers {
		if count > 1 {
			t.Errorf("task %d was triggered by both shards", id)
		}
	}

	// --- Verify all tasks were triggered ---
	if len(allTriggers) != len(taskIDs) {
		t.Errorf("total triggered tasks: got %d, want %d", len(allTriggers), len(taskIDs))
	}
}

// equalInt64Slices returns true if two []int64 slices are equal.
func equalInt64Slices(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
