// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/timewheel"
	"github.com/tickraft/tickraft/pkg/types"
	"go.uber.org/zap"
)

// --- Mock implementations for manager testing ---

// mgrMockStore implements asset.Store for testing.
type mgrMockStore struct {
	mu     sync.RWMutex
	assets map[int64]*asset.Asset
}

func newMgrMockStore() *mgrMockStore {
	return &mgrMockStore{assets: make(map[int64]*asset.Asset)}
}

func (s *mgrMockStore) CountByStatus(_ context.Context) (map[string]int64, error) {
	return map[string]int64{}, nil
}

func (s *mgrMockStore) Create(_ context.Context, r *asset.Asset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assets[r.ID] = r
	return nil
}

func (s *mgrMockStore) Update(_ context.Context, r *asset.Asset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assets[r.ID] = r
	return nil
}

func (s *mgrMockStore) GetByID(_ context.Context, id int64) (*asset.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.assets[id]
	if !ok {
		return nil, fmt.Errorf("asset not found: %d", id)
	}
	return r, nil
}

func (s *mgrMockStore) GetByKey(_ context.Context, tenantID int64, key string) (*asset.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.assets {
		if r.TenantID == tenantID && r.AssetKey == key {
			return r, nil
		}
	}
	return nil, fmt.Errorf("asset not found: tenant=%d key=%s", tenantID, key)
}

func (s *mgrMockStore) UpdateStatus(_ context.Context, id int64, status types.AssetStatus, activeAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.assets[id]; ok {
		r.Status = status
		r.LastActiveAt = activeAt
	}
	return nil
}

func (s *mgrMockStore) Migrate(_ context.Context) error { return nil }

func (s *mgrMockStore) List(_ context.Context, page, size int, _ asset.ListFilter) ([]*asset.Asset, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := int64(len(s.assets))
	all := make([]*asset.Asset, 0, total)
	for _, r := range s.assets {
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

func (s *mgrMockStore) Delete(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.assets, id)
	return nil
}

func (s *mgrMockStore) CountByType(_ context.Context, tenantID int64, assetType types.AssetType) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int64
	for _, r := range s.assets {
		if r.TenantID == tenantID && r.AssetType == assetType {
			count++
		}
	}
	return count, nil
}

func (s *mgrMockStore) ExistsByKey(_ context.Context, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.assets {
		if r.AssetKey == key {
			return true, nil
		}
	}
	return false, nil
}

func (s *mgrMockStore) ListKeyset(_ context.Context, _ pagination.PageRequest) (pagination.PageResult[*asset.Asset], error) {
	return pagination.PageResult[*asset.Asset]{}, nil
}

// mgrMockProcessor implements Processor for testing.
type mgrMockProcessor struct {
	mu           sync.Mutex
	processCalls int
	timeoutCalls int
	result       *ProcessResult
}

func (m *mgrMockProcessor) Type() types.AssetType { return types.AssetTypeDevice }
func (m *mgrMockProcessor) Process(_ context.Context, _ *Telemetry) (*ProcessResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processCalls++
	return m.result, nil
}
func (m *mgrMockProcessor) OnTimeout(_ context.Context, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.timeoutCalls++
	return nil
}

// --- stateManager tests ---

// mustNewTestWheel creates a time wheel for tests, failing on error.
// The default-pool initialization path is unreachable in practice
// (size is sanitized), so this helper keeps tests concise without
// ignoring errors.
func mustNewTestWheel(t *testing.T, size int) timewheel.Wheel {
	t.Helper()
	w, err := timewheel.NewWheel(size)
	if err != nil {
		t.Fatalf("timewheel.NewWheel(%d) error: %v", size, err)
	}
	return w
}

// mustNewManager creates a Collector via [New] and fails the test on
// error. The default-pool initialization path is unreachable in
// practice, so this helper keeps tests concise without ignoring
// errors.
func mustNewManager(t *testing.T, opts ...Option) Collector {
	t.Helper()
	m, err := New(opts...)
	if err != nil {
		t.Fatalf("telemetry.New error: %v", err)
	}
	return m
}

func TestStateManagerRegisterUnregister(t *testing.T) {
	wheel := mustNewTestWheel(t, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)
	defer wheel.Stop(context.Background())

	store := newMgrMockStore()
	sm := newStateManager(store, nil, wheel, zap.NewNop(), nil)

	sm.RegisterAsset(1, 30*time.Second)

	// Verify the asset is in the cache.
	if status := sm.GetStatus(1); status != types.AssetStatusUnknown {
		t.Errorf("expected status unknown, got %q", status)
	}

	sm.UnregisterAsset(1)

	// After unregister, status should be unknown (not found).
	if status := sm.GetStatus(1); status != types.AssetStatusUnknown {
		t.Errorf("expected status unknown after unregister, got %q", status)
	}
}

func TestStateManagerUpdateActive(t *testing.T) {
	wheel := mustNewTestWheel(t, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)
	defer wheel.Stop(context.Background())

	store := newMgrMockStore()
	sm := newStateManager(store, nil, wheel, zap.NewNop(), nil)

	sm.RegisterAsset(1, 60*time.Second)

	// UpdateActive should not panic for a registered asset.
	sm.UpdateActive(1)

	// UpdateActive for an unregistered asset should not panic.
	sm.UpdateActive(999)
}

func TestStateManagerUpdateStatus(t *testing.T) {
	wheel := mustNewTestWheel(t, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)
	defer wheel.Stop(context.Background())

	store := newMgrMockStore()
	_ = store.Create(context.Background(), &asset.Asset{
		ID:        1,
		TenantID:  1,
		AssetType: types.AssetTypeDevice,
		AssetKey:  "device-1",
		Status:    types.AssetStatusUnknown,
	})

	sm := newStateManager(store, nil, wheel, zap.NewNop(), nil)
	sm.RegisterAsset(1, 60*time.Second)

	// Update from unknown to normal.
	changed, err := sm.UpdateStatus(context.Background(), 1, types.AssetStatusNormal, "device online")
	if err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}
	if !changed {
		t.Error("expected status change")
	}
	if status := sm.GetStatus(1); status != types.AssetStatusNormal {
		t.Errorf("expected status normal, got %q", status)
	}

	// Update to the same status should not report a change.
	changed, err = sm.UpdateStatus(context.Background(), 1, types.AssetStatusNormal, "no change")
	if err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}
	if changed {
		t.Error("expected no status change for same status")
	}

	// Update from normal to offline.
	changed, err = sm.UpdateStatus(context.Background(), 1, types.AssetStatusOffline, "timeout")
	if err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}
	if !changed {
		t.Error("expected status change")
	}
	if status := sm.GetStatus(1); status != types.AssetStatusOffline {
		t.Errorf("expected status offline, got %q", status)
	}
}

// --- emitter tests ---

func TestEmitterEmitStatusChange(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	received := make(chan event.StatusChangePayload, 1)
	_, _ = event.Subscribe[event.StatusChangePayload](bus, event.TypeAssetStatusChanged, func(_ context.Context, e event.Event[event.StatusChangePayload]) error {
		received <- e.Payload
		return nil
	})

	em := newEmitter(bus, zap.NewNop())

	payload := event.StatusChangePayload{
		AssetID:    "1",
		TenantID:   "1",
		AssetType:  string(types.AssetTypeDevice),
		PrevStatus: string(types.AssetStatusNormal),
		CurrStatus: string(types.AssetStatusOffline),
		Reason:     "timeout",
	}
	em.EmitStatusChange(context.Background(), payload)

	select {
	case got := <-received:
		if got.AssetID != "1" {
			t.Errorf("AssetID: got %q, want %q", got.AssetID, "1")
		}
		if got.PrevStatus != string(types.AssetStatusNormal) {
			t.Errorf("PrevStatus: got %q, want %q", got.PrevStatus, types.AssetStatusNormal)
		}
		if got.CurrStatus != string(types.AssetStatusOffline) {
			t.Errorf("CurrStatus: got %q, want %q", got.CurrStatus, types.AssetStatusOffline)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for status change event")
	}
}

func TestEmitterEmitMetricAlert(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	received := make(chan event.MetricExceededPayload, 1)
	_, _ = event.Subscribe[event.MetricExceededPayload](bus, event.TypeTelemetryMetricExceeded, func(_ context.Context, e event.Event[event.MetricExceededPayload]) error {
		received <- e.Payload
		return nil
	})

	em := newEmitter(bus, zap.NewNop())

	payload := event.MetricExceededPayload{
		AssetID:     "1",
		TenantID:    "1",
		MetricName:  "rtt_ms",
		MetricValue: 1500,
		Threshold:   1000,
		Operator:    ">",
	}
	em.EmitMetricAlert(context.Background(), payload)

	select {
	case got := <-received:
		if got.MetricName != "rtt_ms" {
			t.Errorf("MetricName: got %q, want %q", got.MetricName, "rtt_ms")
		}
		if got.MetricValue != 1500 {
			t.Errorf("MetricValue: got %f, want 1500", got.MetricValue)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for metric alert event")
	}
}

func TestEmitterEmitLogAlert(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	received := make(chan event.LogMatchedPayload, 1)
	_, _ = event.Subscribe[event.LogMatchedPayload](bus, event.TypeTelemetryLogMatched, func(_ context.Context, e event.Event[event.LogMatchedPayload]) error {
		received <- e.Payload
		return nil
	})

	em := newEmitter(bus, zap.NewNop())

	payload := event.LogMatchedPayload{
		AssetID:  "1",
		TenantID: "1",
		Level:    "error",
		Keyword:  "OOM",
		Content:  "Out of memory detected",
	}
	em.EmitLogAlert(context.Background(), payload)

	select {
	case got := <-received:
		if got.Keyword != "OOM" {
			t.Errorf("Keyword: got %q, want %q", got.Keyword, "OOM")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for log alert event")
	}
}

func TestEmitterEmitAlerts(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	var metricCount int
	var logCount int
	var mu sync.Mutex

	_, _ = event.Subscribe[event.MetricExceededPayload](bus, event.TypeTelemetryMetricExceeded, func(_ context.Context, _ event.Event[event.MetricExceededPayload]) error {
		mu.Lock()
		metricCount++
		mu.Unlock()
		return nil
	})
	_, _ = event.Subscribe[event.LogMatchedPayload](bus, event.TypeTelemetryLogMatched, func(_ context.Context, _ event.Event[event.LogMatchedPayload]) error {
		mu.Lock()
		logCount++
		mu.Unlock()
		return nil
	})

	em := newEmitter(bus, zap.NewNop())

	alerts := []AlertContext{
		{Level: "critical", Title: "Host Down", Message: "no response"},
		{Level: "warning", Title: "High CPU", Message: "cpu at 95%"},
		{Level: "info", Title: "Recovery", Message: "service restored"},
	}
	em.EmitAlerts(context.Background(), alerts, 1, 1)

	// Wait for events to be processed.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if metricCount != 2 {
		t.Errorf("metric alert count: got %d, want 2", metricCount)
	}
	if logCount != 1 {
		t.Errorf("log alert count: got %d, want 1", logCount)
	}
}

// --- Manager lifecycle tests ---

func TestManagerStartStop(t *testing.T) {
	store := newMgrMockStore()
	bus := event.NewBus()
	defer bus.Close()

	processorReg := NewProcessorRegistry()

	mgr := mustNewManager(t,
		WithProcessorRegistry(processorReg),
		WithAssetStore(store),
		WithEventBus(bus),
		WithLogger(zap.NewNop()),
	)

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if err := mgr.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestManagerDoubleStart(t *testing.T) {
	store := newMgrMockStore()
	bus := event.NewBus()
	defer bus.Close()

	mgr := mustNewManager(t,
		WithAssetStore(store),
		WithEventBus(bus),
		WithLogger(zap.NewNop()),
	)

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	// Second start should be a no-op.
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("second Start failed: %v", err)
	}

	if err := mgr.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestManagerRegisterUnregisterAsset(t *testing.T) {
	store := newMgrMockStore()
	bus := event.NewBus()
	defer bus.Close()

	processorReg := NewProcessorRegistry()
	proc := &mgrMockProcessor{result: &ProcessResult{}}
	if err := processorReg.Register(proc); err != nil {
		t.Fatalf("failed to register processor: %v", err)
	}

	mgr := mustNewManager(t,
		WithProcessorRegistry(processorReg),
		WithAssetStore(store),
		WithEventBus(bus),
		WithLogger(zap.NewNop()),
	)

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer mgr.Stop(ctx)

	// Create a asset in the store.
	_ = store.Create(ctx, &asset.Asset{
		ID:        1,
		TenantID:  1,
		AssetType: types.AssetTypeDevice,
		AssetKey:  "device-1",
		Status:    types.AssetStatusUnknown,
	})

	config := Config{
		AssetID: 1,
		Timeout: 30,
	}

	if err := mgr.RegisterAsset(ctx, config); err != nil {
		t.Fatalf("RegisterAsset failed: %v", err)
	}

	if err := mgr.UnregisterAsset(ctx, 1); err != nil {
		t.Fatalf("UnregisterAsset failed: %v", err)
	}
}

func TestManagerRegisterAssetValidation(t *testing.T) {
	store := newMgrMockStore()
	bus := event.NewBus()
	defer bus.Close()

	mgr := mustNewManager(t,
		WithAssetStore(store),
		WithEventBus(bus),
		WithLogger(zap.NewNop()),
	)

	ctx := context.Background()

	// AssetID must be positive.
	err := mgr.RegisterAsset(ctx, Config{AssetID: 0, Timeout: 30})
	if err == nil {
		t.Error("expected error for zero AssetID")
	}

	// Timeout must be positive.
	err = mgr.RegisterAsset(ctx, Config{AssetID: 1, Timeout: 0})
	if err == nil {
		t.Error("expected error for zero Timeout")
	}
}

// --- Integration test: Manager telemetry processing pipeline ---

func TestManagerSubmit(t *testing.T) {
	store := newMgrMockStore()
	bus := event.NewBus()
	defer bus.Close()

	processorReg := NewProcessorRegistry()
	proc := &mgrMockProcessor{result: &ProcessResult{
		PrevStatus: types.AssetStatusUnknown,
		CurrStatus: types.AssetStatusNormal,
		Reason:     "device online",
	}}
	if err := processorReg.Register(proc); err != nil {
		t.Fatalf("failed to register processor: %v", err)
	}

	c := mustNewManager(t,
		WithProcessorRegistry(processorReg),
		WithAssetStore(store),
		WithEventBus(bus),
		WithLogger(zap.NewNop()),
	)
	mgr := c.(*Manager)

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer mgr.Stop(ctx)

	// Create a asset in the store so the processor pipeline can resolve it.
	_ = store.Create(ctx, &asset.Asset{
		ID:        1,
		TenantID:  1,
		AssetType: types.AssetTypeDevice,
		AssetKey:  "device-1",
		Status:    types.AssetStatusUnknown,
	})

	mgr.Submit(&Telemetry{
		AssetID:     1,
		TenantID:    1,
		AssetType:   types.AssetTypeDevice,
		SourceType:  "mock_listener",
		CollectedAt: time.Now(),
		Status:      types.AssetStatusNormal,
	})

	// Wait for processing.
	time.Sleep(200 * time.Millisecond)

	proc.mu.Lock()
	if proc.processCalls != 1 {
		t.Errorf("processCalls: got %d, want 1", proc.processCalls)
	}
	proc.mu.Unlock()
}

// --- Timeout detection tests ---
//
// These tests verify that the stateManager's time-wheel-based timeout
// detection fires the onTimeout callback for both scenarios:
//   - Startup timeout: a asset is registered but never reports.
//   - Run timeout: a asset that was reporting stops reporting.
//
// The time wheel ticks every second and truncates sub-second durations
// (int(delay.Seconds())), so a 3s timeout has an effective resolution of
// ~2s. The tests use a 3s timeout with 500ms heartbeats to ensure the
// renewed entry always stays ahead of the wheel pointer during active
// heartbeating.

// TestStateManagerStartupTimeout verifies that a asset which is
// registered but never sends a report triggers the onTimeout callback
// after the configured grace period elapses.
func TestStateManagerStartupTimeout(t *testing.T) {
	wheel := mustNewTestWheel(t, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)
	defer wheel.Stop(context.Background())

	timeoutCalled := make(chan int64, 1)
	onTimeout := func(_ context.Context, assetID int64) {
		select {
		case timeoutCalled <- assetID:
		default:
		}
	}

	sm := newStateManager(newMgrMockStore(), nil, wheel, zap.NewNop(), onTimeout)

	// Register a asset with a 3-second timeout but never send a report.
	sm.RegisterAsset(1, 3*time.Second)

	select {
	case id := <-timeoutCalled:
		if id != 1 {
			t.Errorf("timeout assetID: got %d, want 1", id)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("startup timeout was not detected within 10s")
	}
}

// TestStateManagerRunTimeout verifies that a asset which initially
// reports (renewing its timeout entry via UpdateActive) but then stops
// reporting triggers the onTimeout callback after the grace period
// elapses from the last heartbeat.
func TestStateManagerRunTimeout(t *testing.T) {
	wheel := mustNewTestWheel(t, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)
	defer wheel.Stop(context.Background())

	timeoutCalled := make(chan int64, 1)
	onTimeout := func(_ context.Context, assetID int64) {
		select {
		case timeoutCalled <- assetID:
		default:
		}
	}

	sm := newStateManager(newMgrMockStore(), nil, wheel, zap.NewNop(), onTimeout)

	// Register with a 3-second timeout (effective ~2s after truncation).
	sm.RegisterAsset(1, 3*time.Second)

	// Send heartbeats every 500ms for ~4 seconds. The renewed entry
	// always stays at least 2 slots ahead of the wheel pointer, so
	// onTimeout must not fire during this period.
	for i := 0; i < 8; i++ {
		sm.UpdateActive(1)
		time.Sleep(500 * time.Millisecond)
	}

	// Confirm no timeout has fired yet while heartbeats were active.
	select {
	case <-timeoutCalled:
		t.Fatal("timeout fired while heartbeats were still active")
	default:
		// Good: no premature timeout.
	}

	// Stop sending heartbeats; the run timeout should now fire.
	select {
	case id := <-timeoutCalled:
		if id != 1 {
			t.Errorf("timeout assetID: got %d, want 1", id)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run timeout was not detected within 10s after heartbeats stopped")
	}
}

// TestStateManagerNoTimeoutWhileHeartbeating verifies that continuous
// heartbeats prevent the timeout from firing indefinitely (within a
// reasonable window that exceeds the configured timeout).
func TestStateManagerNoTimeoutWhileHeartbeating(t *testing.T) {
	wheel := mustNewTestWheel(t, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)
	defer wheel.Stop(context.Background())

	timeoutCalled := make(chan int64, 1)
	onTimeout := func(_ context.Context, assetID int64) {
		select {
		case timeoutCalled <- assetID:
		default:
		}
	}

	sm := newStateManager(newMgrMockStore(), nil, wheel, zap.NewNop(), onTimeout)

	// Register with a 3-second timeout (effective ~2s after truncation).
	sm.RegisterAsset(1, 3*time.Second)

	// Send heartbeats every 500ms for 6 seconds (well past the 3s timeout).
	// The timeout must not fire while heartbeats are active.
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		sm.UpdateActive(1)
		time.Sleep(500 * time.Millisecond)
		select {
		case <-timeoutCalled:
			t.Fatal("timeout fired while heartbeats were active")
		default:
		}
	}
}
