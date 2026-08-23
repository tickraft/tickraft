// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/scheduler"
	"github.com/tickraft/tickraft/pkg/types"
)

// mockStore is an in-memory Store implementation used to test the
// Service persistence/restore paths without a real database.
type mockStore struct {
	mu      sync.Mutex
	tasks   map[int64]*Task
	saveErr error // optional error to return from Save
	listErr error // optional error to return from List
	delErr  error // optional error to return from Delete
}

func newMockStore() *mockStore {
	return &mockStore{tasks: make(map[int64]*Task)}
}

func (m *mockStore) Save(_ context.Context, task *Task) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *task
	m.tasks[task.ID] = &cp
	return nil
}

func (m *mockStore) Get(_ context.Context, id int64) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return nil, ErrTaskNotFound
	}
	cp := *t
	return &cp, nil
}

func (m *mockStore) List(_ context.Context, _ ListOptions) ([]*Task, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		cp := *t
		result = append(result, &cp)
	}
	return result, nil
}

func (m *mockStore) Delete(_ context.Context, id int64) error {
	if m.delErr != nil {
		return m.delErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tasks, id)
	return nil
}

// Migrate is a no-op for the in-memory mock store.
func (m *mockStore) Migrate(_ context.Context) error { return nil }

// --- Service in-memory store Tests ---

func TestTaskManagerSetGet(t *testing.T) {
	m := mustNewManager()
	tk := Task{ID: 1, ExecutorType: "mock"}

	m.setTask(tk)

	got, err := m.getTask(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != 1 {
		t.Errorf("got ID %d, want 1", got.ID)
	}
}

func TestTaskManagerGetNotFound(t *testing.T) {
	m := mustNewManager()

	_, err := m.getTask(999)
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("got error %v, want ErrTaskNotFound", err)
	}
}

func TestTaskManagerDelete(t *testing.T) {
	m := mustNewManager()
	tk := Task{ID: 1, ExecutorType: "mock"}

	m.setTask(tk)
	m.deleteTask(1)

	_, err := m.getTask(1)
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("got error %v, want ErrTaskNotFound", err)
	}
}

func TestTaskManagerList(t *testing.T) {
	m := mustNewManager()
	m.setTask(Task{ID: 1})
	m.setTask(Task{ID: 2})

	list := m.listTasks()
	if len(list) != 2 {
		t.Errorf("got %d tasks, want 2", len(list))
	}
}

// --- dependencyChecker Tests ---

func TestDependencyCheckerCanExecute(t *testing.T) {
	dc := newDependencyChecker()

	// No status recorded yet.
	if dc.CanExecute(1) {
		t.Error("should not allow execution without recorded status")
	}

	// Record success.
	dc.UpdateStatus(1, types.AssetStatusNormal)
	if !dc.CanExecute(1) {
		t.Error("should allow execution after upstream success")
	}

	// Record failure.
	dc.UpdateStatus(1, types.AssetStatusAbnormal)
	if dc.CanExecute(1) {
		t.Error("should not allow execution after upstream failure")
	}
}

func TestDependencyCheckerReset(t *testing.T) {
	dc := newDependencyChecker()

	dc.UpdateStatus(1, types.AssetStatusNormal)
	dc.Reset(1)

	if dc.CanExecute(1) {
		t.Error("should not allow execution after reset")
	}
}

// --- Service Register / Unschedule / Update Tests ---

func TestTaskManagerRegister(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	tk := Task{
		ID:             1,
		ExecutorType:   "webhook",
		Enabled:        true,
		Schedule:       "1h",
		TimeoutSeconds: 5,
	}

	if err := m.Register(context.Background(), tk); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Verify task is stored.
	got, err := m.getTask(1)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.ExecutorType != "webhook" {
		t.Errorf("executor type = %q, want %q", got.ExecutorType, "webhook")
	}

	// Verify schedule is stored.
	m.mu.RLock()
	_, schedOk := m.scheds[1]
	schedType := m.scheduleTypes[1]
	m.mu.RUnlock()
	if !schedOk {
		t.Error("schedule should be stored after register")
	}
	if schedType != ScheduleTypeInterval {
		t.Errorf("schedule type = %q, want %q", schedType, ScheduleTypeInterval)
	}
}

// TestTaskManagerRegisterDisabled verifies that a disabled task is stored
// but not put on the wheel.
func TestTaskManagerRegisterDisabled(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	tk := Task{
		ID:           1,
		ExecutorType: "webhook",
		Enabled:      false,
		Schedule:     "1h",
	}
	if err := m.Register(context.Background(), tk); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := m.getTask(1); err != nil {
		t.Fatalf("disabled task should be stored: %v", err)
	}
	m.mu.RLock()
	_, schedOk := m.scheds[1]
	m.mu.RUnlock()
	if schedOk {
		t.Error("disabled task should not be scheduled")
	}
}

func TestTaskManagerUnschedule(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	tk := Task{
		ID:           1,
		ExecutorType: "webhook",
		Enabled:      true,
		Schedule:     "1h",
	}

	if err := m.Register(context.Background(), tk); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := m.Unschedule(context.Background(), 1); err != nil {
		t.Fatalf("unschedule: %v", err)
	}

	// Verify task is removed.
	_, err := m.getTask(1)
	if err == nil {
		t.Error("task should be removed after unschedule")
	}
}

func TestTaskManagerUpdate(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	tk := Task{
		ID:             1,
		ExecutorType:   "webhook",
		Enabled:        true,
		Schedule:       "1h",
		TimeoutSeconds: 5,
	}

	if err := m.Register(context.Background(), tk); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Update with new schedule.
	updatedTask := Task{
		ID:             1,
		ExecutorType:   "webhook",
		Enabled:        true,
		Schedule:       "2h",
		TimeoutSeconds: 10,
	}

	if err := m.Update(context.Background(), updatedTask); err != nil {
		t.Fatalf("update: %v", err)
	}

	// Verify task is updated.
	got, err := m.getTask(1)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.TimeoutSeconds != 10 || got.Timeout() != 10*time.Second {
		t.Errorf("timeout = %d (%v), want 10s", got.TimeoutSeconds, got.Timeout())
	}
	m.mu.RLock()
	schedType := m.scheduleTypes[1]
	m.mu.RUnlock()
	if got.Schedule != "2h" || schedType != ScheduleTypeInterval {
		t.Errorf("schedule after update = %q (type %v), want 2h (interval)", got.Schedule, schedType)
	}
}

func TestTaskManagerStop(t *testing.T) {
	m := newTestManager(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// Stop is idempotent.
	if err := m.Stop(ctx); err != nil {
		t.Fatalf("stop (idempotent): %v", err)
	}
}

func TestTaskManagerScheduleNotFound(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	err := m.Schedule(context.Background(), 999)
	if err == nil {
		t.Error("expected error for non-existent task")
	}
}

func TestTaskManagerRegisterInvalidCron(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	tk := Task{
		ID:           1,
		ExecutorType: "webhook",
		Enabled:      true,
		Schedule:     "invalid-cron",
	}

	err := m.Register(context.Background(), tk)
	if err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

// --- Service Schedule / trigger Tests ---

// TestTaskManagerSchedulePublishesTaskTriggered verifies that Schedule publishes an
// ExecutionTriggered event on the event bus with the correct payload.
func TestTaskManagerSchedulePublishesTaskTriggered(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	// Subscribe to ExecutionTriggered events.
	triggered := make(chan event.Event[event.ExecutionPayload], 1)
	_, _ = event.Subscribe[event.ExecutionPayload](m.bus, event.TypeExecutionTriggered,
		func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
			triggered <- ev
			return nil
		})

	tk := Task{
		ID:                   42,
		TenantID:             7,
		AssetID:              99,
		ExecutorType:         "webhook",
		Enabled:              true,
		Schedule:             "1h",
		Config:               map[string]any{"url": "http://example.com"},
		TimeoutSeconds:       5,
		MaxRetries:           2,
		RetryIntervalSeconds: 30,
		Metadata:             map[string]string{"key": "value"},
	}
	if err := m.Register(context.Background(), tk); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := m.Schedule(context.Background(), 42); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	select {
	case ev := <-triggered:
		if ev.Type != event.TypeExecutionTriggered {
			t.Errorf("event type = %q, want %q", ev.Type, event.TypeExecutionTriggered)
		}
		if ev.Payload.ExecutionID != "42" {
			t.Errorf("payload.ExecutionID = %q, want %q", ev.Payload.ExecutionID, "42")
		}
		if ev.Payload.TenantID != "7" {
			t.Errorf("payload.TenantID = %q, want %q", ev.Payload.TenantID, "7")
		}
		if ev.Payload.AssetID != "99" {
			t.Errorf("payload.AssetID = %q, want %q", ev.Payload.AssetID, "99")
		}
		if ev.Payload.ExecutorType != "webhook" {
			t.Errorf("payload.ExecutorType = %q, want %q", ev.Payload.ExecutorType, "webhook")
		}
		if ev.Payload.Config != `{"url":"http://example.com"}` {
			t.Errorf("payload.Config = %q, unexpected", ev.Payload.Config)
		}
		if ev.Payload.TimeoutSeconds != 5 {
			t.Errorf("payload.TimeoutSeconds = %d, want 5", ev.Payload.TimeoutSeconds)
		}
		if ev.Payload.MaxRetries != 2 {
			t.Errorf("payload.MaxRetries = %d, want 2", ev.Payload.MaxRetries)
		}
		if ev.Payload.RetryIntervalSeconds != 30 {
			t.Errorf("payload.RetryIntervalSeconds = %d, want 30", ev.Payload.RetryIntervalSeconds)
		}
		if ev.Payload.TriggerType != string(TriggerTypeManual) {
			t.Errorf("payload.TriggerType = %q, want %q (manual trigger path)",
				ev.Payload.TriggerType, TriggerTypeManual)
		}
		if ev.Metadata["key"] != "value" {
			t.Errorf("metadata[key] = %q, want %q", ev.Metadata["key"], "value")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ExecutionTriggered event")
	}
}

// --- Service onFire Tests ---

// TestTaskManagerOnFirePublishesTaskTriggered verifies that onFire publishes an
// ExecutionTriggered event when a scheduled task fires.
func TestTaskManagerOnFirePublishesTaskTriggered(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	// Subscribe to ExecutionTriggered events.
	triggered := make(chan event.Event[event.ExecutionPayload], 1)
	_, _ = event.Subscribe[event.ExecutionPayload](m.bus, event.TypeExecutionTriggered,
		func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
			triggered <- ev
			return nil
		})

	// Register a task with a sub-millisecond interval so it fires immediately.
	tk := Task{
		ID:             1,
		ExecutorType:   "webhook",
		Enabled:        true,
		Schedule:       "1ms",
		TimeoutSeconds: 5,
	}
	if err := m.Register(context.Background(), tk); err != nil {
		t.Fatalf("register: %v", err)
	}

	select {
	case ev := <-triggered:
		if ev.Payload.ExecutionID != "1" {
			t.Errorf("payload.ExecutionID = %q, want %q", ev.Payload.ExecutionID, "1")
		}
		if ev.Payload.TriggerType != string(TriggerTypeSchedule) {
			t.Errorf("payload.TriggerType = %q, want %q (wheel-fire path)",
				ev.Payload.TriggerType, TriggerTypeSchedule)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for ExecutionTriggered event from onFire")
	}
}

// TestTaskManagerOnFireShardFiltering verifies that onFire skips tasks not owned by
// the current shard and still reschedules them.
func TestTaskManagerOnFireShardFiltering(t *testing.T) {
	// Create a manager with sharding: total=2, index=0.
	// This node owns only even task IDs (taskID % 2 == 0).
	m := newTestManager(t, scheduler.NewShardManager(2, 0))
	defer func() { _ = m.Stop(context.Background()) }()

	triggered := make(chan event.Event[event.ExecutionPayload], 10)
	_, _ = event.Subscribe[event.ExecutionPayload](m.bus, event.TypeExecutionTriggered,
		func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
			triggered <- ev
			return nil
		})

	// Register an odd task ID (not owned by this shard).
	oddTask := Task{
		ID:           1,
		ExecutorType: "webhook",
		Enabled:      true,
		Schedule:     "10ms",
	}
	if err := m.Register(context.Background(), oddTask); err != nil {
		t.Fatalf("register odd task: %v", err)
	}

	// The odd task should NOT trigger.
	select {
	case <-triggered:
		t.Fatal("odd task ID should not trigger on shard 0")
	case <-time.After(500 * time.Millisecond):
		// Expected: no event published.
	}

	// Register an even task ID (owned by this shard).
	evenTask := Task{
		ID:           2,
		ExecutorType: "webhook",
		Enabled:      true,
		Schedule:     "10ms",
	}
	if err := m.Register(context.Background(), evenTask); err != nil {
		t.Fatalf("register even task: %v", err)
	}

	// The even task should trigger.
	select {
	case ev := <-triggered:
		if ev.Payload.ExecutionID != "2" {
			t.Errorf("payload.ExecutionID = %q, want %q", ev.Payload.ExecutionID, "2")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for ExecutionTriggered event for even task ID")
	}
}

// TestTaskManagerOnFireDependencyNotMet verifies that onFire skips a task whose
// dependency has not completed successfully.
func TestTaskManagerOnFireDependencyNotMet(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	triggered := make(chan event.Event[event.ExecutionPayload], 10)
	_, _ = event.Subscribe[event.ExecutionPayload](m.bus, event.TypeExecutionTriggered,
		func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
			triggered <- ev
			return nil
		})

	// Register a task that depends on task 100 (which has no recorded status).
	tk := Task{
		ID:        1,
		DependsOn: 100,
		Enabled:   true,
		Schedule:  "10ms",
	}
	if err := m.Register(context.Background(), tk); err != nil {
		t.Fatalf("register: %v", err)
	}

	// The task should NOT trigger because dependency is not met.
	select {
	case <-triggered:
		t.Fatal("task should not trigger when dependency is not met")
	case <-time.After(500 * time.Millisecond):
		// Expected: no event published.
	}
}

// --- Service SubscribeEvents Tests ---

// waitFor polls cond until it returns true or the timeout elapses. It is used
// to synchronize tests with the asynchronous event bus.
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

// TestTaskManagerSubscribeEventsTaskCompleted verifies that SubscribeEvents updates
// the dependency checker when an ExecutionCompleted event is received.
func TestTaskManagerSubscribeEventsTaskCompleted(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	// Subscribe to events (wires ExecutionCompleted handler).
	m.SubscribeEvents(context.Background())

	// Initially, dependency for task 100 is not met.
	if m.deps.CanExecute(100) {
		t.Fatal("dependency should not be met before ExecutionCompleted event")
	}

	// Publish an ExecutionCompleted event with Normal status.
	_ = event.Publish(context.Background(), m.bus, event.TypeExecutionCompleted, event.ExecutionPayload{
		ExecutionID: "100",
		Status:      string(types.AssetStatusNormal),
	})

	// Publish dispatches asynchronously; poll until the subscriber has
	// updated the dependency checker.
	if !waitFor(2*time.Second, func() bool { return m.deps.CanExecute(100) }) {
		t.Fatal("dependency should be met after ExecutionCompleted with Normal status")
	}

	// Publish an ExecutionCompleted event with Abnormal status.
	_ = event.Publish(context.Background(), m.bus, event.TypeExecutionCompleted, event.ExecutionPayload{
		ExecutionID: "100",
		Status:      string(types.AssetStatusAbnormal),
	})

	if !waitFor(2*time.Second, func() bool { return !m.deps.CanExecute(100) }) {
		t.Fatal("dependency should not be met after ExecutionCompleted with Abnormal status")
	}
}

// TestTaskManagerSubscribeEventsStatusChangeTriggersEventTask verifies that
// SubscribeEvents triggers event-driven tasks when a asset becomes abnormal.
func TestTaskManagerSubscribeEventsStatusChangeTriggersEventTask(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	m.SubscribeEvents(context.Background())

	triggered := make(chan event.Event[event.ExecutionPayload], 10)
	_, _ = event.Subscribe[event.ExecutionPayload](m.bus, event.TypeExecutionTriggered,
		func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
			triggered <- ev
			return nil
		})

	// Register an event-driven task associated with asset 50.
	tk := Task{
		ID:      1,
		AssetID: 50,
		Enabled: true,
	}
	if err := m.Register(context.Background(), tk); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Publish a StatusChange event for asset 50 becoming abnormal.
	_ = event.Publish(context.Background(), m.bus, event.TypeAssetStatusChanged, event.StatusChangePayload{
		AssetID:    "50",
		CurrStatus: string(types.AssetStatusAbnormal),
	})

	select {
	case ev := <-triggered:
		if ev.Payload.ExecutionID != "1" {
			t.Errorf("payload.ExecutionID = %q, want %q", ev.Payload.ExecutionID, "1")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ExecutionTriggered event from StatusChange handler")
	}
}

// TestTaskManagerSubscribeEventsStatusChangeIgnoresNormal verifies that
// SubscribeEvents does NOT trigger event-driven tasks when a asset becomes
// normal.
func TestTaskManagerSubscribeEventsStatusChangeIgnoresNormal(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	m.SubscribeEvents(context.Background())

	triggered := make(chan event.Event[event.ExecutionPayload], 10)
	_, _ = event.Subscribe[event.ExecutionPayload](m.bus, event.TypeExecutionTriggered,
		func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
			triggered <- ev
			return nil
		})

	tk := Task{
		ID:      1,
		AssetID: 50,
		Enabled: true,
	}
	if err := m.Register(context.Background(), tk); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Publish a StatusChange event for asset 50 becoming Normal.
	_ = event.Publish(context.Background(), m.bus, event.TypeAssetStatusChanged, event.StatusChangePayload{
		AssetID:    "50",
		CurrStatus: string(types.AssetStatusNormal),
	})

	select {
	case <-triggered:
		t.Fatal("event-driven task should NOT trigger on Normal status change")
	case <-time.After(200 * time.Millisecond):
		// Expected: no event published.
	}
}

// TestTaskManagerSubscribeEventsStatusChangeSkipsDisabled verifies that a
// disabled event-driven task is not triggered by a matching status change.
func TestTaskManagerSubscribeEventsStatusChangeSkipsDisabled(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	m.SubscribeEvents(context.Background())

	triggered := make(chan event.Event[event.ExecutionPayload], 10)
	_, _ = event.Subscribe[event.ExecutionPayload](m.bus, event.TypeExecutionTriggered,
		func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
			triggered <- ev
			return nil
		})

	tk := Task{
		ID:      1,
		AssetID: 50,
		Enabled: false,
	}
	if err := m.Register(context.Background(), tk); err != nil {
		t.Fatalf("register: %v", err)
	}

	_ = event.Publish(context.Background(), m.bus, event.TypeAssetStatusChanged, event.StatusChangePayload{
		AssetID:    "50",
		CurrStatus: string(types.AssetStatusAbnormal),
	})

	select {
	case <-triggered:
		t.Fatal("disabled event-driven task should NOT trigger")
	case <-time.After(200 * time.Millisecond):
		// Expected: no event published.
	}
}

// TestTaskManagerSubscribeEventsStatusChangeAssetismatch verifies that
// SubscribeEvents does NOT trigger event-driven tasks when the asset ID
// does not match.
func TestTaskManagerSubscribeEventsStatusChangeAssetismatch(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	m.SubscribeEvents(context.Background())

	triggered := make(chan event.Event[event.ExecutionPayload], 10)
	_, _ = event.Subscribe[event.ExecutionPayload](m.bus, event.TypeExecutionTriggered,
		func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
			triggered <- ev
			return nil
		})

	tk := Task{
		ID:      1,
		AssetID: 50,
		Enabled: true,
	}
	if err := m.Register(context.Background(), tk); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Publish a StatusChange event for a different asset (99).
	_ = event.Publish(context.Background(), m.bus, event.TypeAssetStatusChanged, event.StatusChangePayload{
		AssetID:    "99",
		CurrStatus: string(types.AssetStatusAbnormal),
	})

	select {
	case <-triggered:
		t.Fatal("event-driven task should NOT trigger for a non-matching asset")
	case <-time.After(200 * time.Millisecond):
		// Expected: no event published.
	}
}

// TestTaskManagerSubscribeEventsStatusChangeShardFiltering verifies that
// SubscribeEvents respects shard ownership when triggering event-driven tasks.
func TestTaskManagerSubscribeEventsStatusChangeShardFiltering(t *testing.T) {
	m := newTestManager(t, scheduler.NewShardManager(2, 0)) // owns even task IDs
	defer func() { _ = m.Stop(context.Background()) }()

	m.SubscribeEvents(context.Background())

	triggered := make(chan event.Event[event.ExecutionPayload], 10)
	_, _ = event.Subscribe[event.ExecutionPayload](m.bus, event.TypeExecutionTriggered,
		func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
			triggered <- ev
			return nil
		})

	// Register an event-driven task with odd ID (not owned by shard 0).
	oddTask := Task{
		ID:      1, // 1 % 2 == 1, owned by shard 1
		AssetID: 50,
		Enabled: true,
	}
	if err := m.Register(context.Background(), oddTask); err != nil {
		t.Fatalf("register odd task: %v", err)
	}

	// Publish a StatusChange event for asset 50 becoming abnormal.
	_ = event.Publish(context.Background(), m.bus, event.TypeAssetStatusChanged, event.StatusChangePayload{
		AssetID:    "50",
		CurrStatus: string(types.AssetStatusAbnormal),
	})

	select {
	case <-triggered:
		t.Fatal("odd task ID should not trigger on shard 0")
	case <-time.After(200 * time.Millisecond):
		// Expected: no event published.
	}

	// Register an event-driven task with even ID (owned by shard 0).
	evenTask := Task{
		ID:      2, // 2 % 2 == 0, owned by shard 0
		AssetID: 50,
		Enabled: true,
	}
	if err := m.Register(context.Background(), evenTask); err != nil {
		t.Fatalf("register even task: %v", err)
	}

	// Publish another StatusChange event for asset 50.
	_ = event.Publish(context.Background(), m.bus, event.TypeAssetStatusChanged, event.StatusChangePayload{
		AssetID:    "50",
		CurrStatus: string(types.AssetStatusAbnormal),
	})

	select {
	case ev := <-triggered:
		if ev.Payload.ExecutionID != "2" {
			t.Errorf("payload.ExecutionID = %q, want %q", ev.Payload.ExecutionID, "2")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ExecutionTriggered event for even task ID")
	}
}

// --- Schedule parse Tests ---

func TestParseScheduleCron(t *testing.T) {
	sched, err := parseSchedule("*/5 * * * *")
	if err != nil {
		t.Fatalf("parse cron: %v", err)
	}
	next := sched.Next(time.Now())
	if next.IsZero() {
		t.Error("expected non-zero next time")
	}
}

func TestParseScheduleInterval(t *testing.T) {
	sched, err := parseSchedule("10s")
	if err != nil {
		t.Fatalf("parse interval: %v", err)
	}
	next := sched.Next(time.Now())
	if next.IsZero() {
		t.Error("expected non-zero next time")
	}
}

func TestParseScheduleEventDrivenIsNever(t *testing.T) {
	sched, err := parseSchedule("")
	if err != nil {
		t.Fatalf("parse empty schedule: %v", err)
	}
	if !sched.Next(time.Now()).IsZero() {
		t.Error("event-driven (empty) schedule should never fire")
	}
}

func TestParseScheduleInvalidCron(t *testing.T) {
	_, err := parseSchedule("invalid-cron")
	if err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

// --- Service Restore Tests ---

func TestTaskManagerRestoreNoStore(t *testing.T) {
	m := mustNewManager()
	// No store configured; Restore should be a no-op.
	if err := m.Restore(context.Background()); err != nil {
		t.Fatalf("Restore with no store: %v", err)
	}
	if len(m.listTasks()) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(m.listTasks()))
	}
}

func TestTaskManagerRestoreFromStore(t *testing.T) {
	store := newMockStore()
	// Pre-populate the store with tasks as if they were persisted before a restart.
	seedTasks := []*Task{
		{ID: 1, TenantID: 10, ExecutorType: "webhook", Enabled: true, Schedule: "*/5 * * * *"},
		{ID: 2, TenantID: 10, ExecutorType: "http", Enabled: true, Schedule: "30s"},
	}
	for _, t := range seedTasks {
		_ = store.Save(context.Background(), t)
	}

	m := newTestManagerWithStore(t, store, nil)
	defer func() { _ = m.Stop(context.Background()) }()
	if err := m.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	list := m.listTasks()
	if len(list) != 2 {
		t.Fatalf("expected 2 restored tasks, got %d", len(list))
	}

	got, err := m.getTask(1)
	if err != nil {
		t.Fatalf("Get 1: %v", err)
	}
	if got.ExecutorType != "webhook" {
		t.Errorf("task 1 ExecutorType = %q, want %q", got.ExecutorType, "webhook")
	}
}

func TestTaskManagerRestoreListError(t *testing.T) {
	store := newMockStore()
	store.listErr = errors.New("db unavailable")

	m := newTestManagerWithStore(t, store, nil)
	defer func() { _ = m.Stop(context.Background()) }()
	err := m.Restore(context.Background())
	if err == nil {
		t.Fatal("expected error from Restore when List fails")
	}
}

func TestTaskManagerRestoreIsIdempotent(t *testing.T) {
	store := newMockStore()
	_ = store.Save(context.Background(), &Task{ID: 1, ExecutorType: "webhook"})

	m := newTestManagerWithStore(t, store, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	// First restore.
	if err := m.Restore(context.Background()); err != nil {
		t.Fatalf("Restore (1): %v", err)
	}
	// Inject a transient in-memory task that is NOT in the store by writing
	// directly to the map. Going through setTask would persist it to the store
	// (since a store is configured), defeating the purpose of this test.
	m.taskMu.Lock()
	m.tasks[99] = Task{ID: 99, ExecutorType: "transient"}
	m.taskMu.Unlock()

	// Second restore should replace the in-memory map entirely.
	if err := m.Restore(context.Background()); err != nil {
		t.Fatalf("Restore (2): %v", err)
	}

	list := m.listTasks()
	if len(list) != 1 {
		t.Fatalf("expected 1 task after second Restore, got %d", len(list))
	}
	if _, err := m.getTask(99); err == nil {
		t.Error("transient task 99 should have been replaced by Restore")
	}
}

// --- Service Persistence Tests ---

func TestTaskManagerSetPersistsToStore(t *testing.T) {
	store := newMockStore()
	m := newTestManagerWithStore(t, store, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	m.setTask(Task{ID: 1, ExecutorType: "webhook"})

	// Verify the task was persisted.
	got, err := store.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got.ExecutorType != "webhook" {
		t.Errorf("persisted ExecutorType = %q, want %q", got.ExecutorType, "webhook")
	}
}

func TestTaskManagerDeletePersistsToStore(t *testing.T) {
	store := newMockStore()
	m := newTestManagerWithStore(t, store, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	m.setTask(Task{ID: 1, ExecutorType: "webhook"})
	m.deleteTask(1)

	// Verify the task was deleted from the store.
	_, err := store.Get(context.Background(), 1)
	if err == nil {
		t.Error("expected error from store.Get after Delete, got nil")
	}
}

func TestTaskManagerSetStoreErrorDoesNotAffectMemory(t *testing.T) {
	store := newMockStore()
	store.saveErr = errors.New("db write failed")
	m := newTestManagerWithStore(t, store, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	// setTask should still update in-memory state even if the store write fails.
	m.setTask(Task{ID: 1, ExecutorType: "webhook"})

	got, err := m.getTask(1)
	if err != nil {
		t.Fatalf("Get after failed store write: %v", err)
	}
	if got.ExecutorType != "webhook" {
		t.Errorf("in-memory ExecutorType = %q, want %q", got.ExecutorType, "webhook")
	}
}

func TestTaskManagerDeleteStoreErrorDoesNotAffectMemory(t *testing.T) {
	store := newMockStore()
	store.delErr = errors.New("db delete failed")
	m := newTestManagerWithStore(t, store, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	m.setTask(Task{ID: 1, ExecutorType: "webhook"})
	m.deleteTask(1)

	// In-memory deletion should succeed even if the store delete fails.
	_, err := m.getTask(1)
	if err == nil {
		t.Error("expected in-memory task to be deleted despite store error")
	}
}

func TestTaskManagerNoStoreSkipsPersistence(t *testing.T) {
	m := mustNewManager() // no store
	m.setTask(Task{ID: 1, ExecutorType: "webhook"})

	// In-memory state should be updated.
	got, err := m.getTask(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ExecutorType != "webhook" {
		t.Errorf("ExecutorType = %q, want %q", got.ExecutorType, "webhook")
	}
}

// --- Service Restore Schedule Tests ---

func TestTaskManagerRestoreSchedulesTasks(t *testing.T) {
	store := newMockStore()
	// Seed the store with a task that has an interval schedule.
	seedTask := &Task{
		ID:             1,
		TenantID:       10,
		ExecutorType:   "webhook",
		Enabled:        true,
		Schedule:       "1h",
		TimeoutSeconds: 5,
	}
	_ = store.Save(context.Background(), seedTask)

	m := newTestManagerWithStore(t, store, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	if err := m.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Verify the task is in memory.
	got, err := m.getTask(1)
	if err != nil {
		t.Fatalf("Get restored task: %v", err)
	}
	if got.ExecutorType != "webhook" {
		t.Errorf("ExecutorType = %q, want %q", got.ExecutorType, "webhook")
	}

	// Verify the task is scheduled in the manager.
	m.mu.RLock()
	_, schedOk := m.scheds[1]
	schedType := m.scheduleTypes[1]
	m.mu.RUnlock()
	if !schedOk {
		t.Error("expected schedule to be stored after Restore")
	}
	if schedType != ScheduleTypeInterval {
		t.Errorf("schedule type = %q, want %q", schedType, ScheduleTypeInterval)
	}
}

// TestTaskManagerRestoreSkipsDisabledTask verifies that a disabled task is
// loaded into memory but not scheduled during Restore.
func TestTaskManagerRestoreSkipsDisabledTask(t *testing.T) {
	store := newMockStore()
	seedTask := &Task{
		ID:           1,
		TenantID:     10,
		ExecutorType: "webhook",
		Enabled:      false,
		Schedule:     "1h",
	}
	_ = store.Save(context.Background(), seedTask)

	m := newTestManagerWithStore(t, store, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	if err := m.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if _, err := m.getTask(1); err != nil {
		t.Fatalf("disabled task should be loaded into memory: %v", err)
	}
	m.mu.RLock()
	_, schedOk := m.scheds[1]
	m.mu.RUnlock()
	if schedOk {
		t.Error("disabled task should not be scheduled during Restore")
	}
}

func TestTaskManagerRestoreEventDrivenTask(t *testing.T) {
	store := newMockStore()
	seedTask := &Task{
		ID:       2,
		TenantID: 10,
		AssetID:  50,
		Enabled:  true,
	}
	_ = store.Save(context.Background(), seedTask)

	m := newTestManagerWithStore(t, store, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	if err := m.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	m.mu.RLock()
	_, inEventIdx := m.eventDrivenTasks[2]
	schedType := m.scheduleTypes[2]
	m.mu.RUnlock()
	if !inEventIdx {
		t.Error("expected restored event-driven task to be in eventDrivenTasks index")
	}
	if schedType != ScheduleTypeEvent {
		t.Errorf("schedule type = %q, want %q", schedType, ScheduleTypeEvent)
	}
}

func TestTaskManagerRestoreSkipsInvalidSchedule(t *testing.T) {
	store := newMockStore()
	// A task with an invalid cron expression should be skipped during Restore.
	seedTask := &Task{
		ID:           3,
		TenantID:     10,
		ExecutorType: "webhook",
		Enabled:      true,
		Schedule:     "invalid-cron-expr",
	}
	_ = store.Save(context.Background(), seedTask)

	m := newTestManagerWithStore(t, store, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	if err := m.Restore(context.Background()); err != nil {
		t.Fatalf("Restore should not fail on invalid schedule: %v", err)
	}

	// The task should be loaded into memory...
	got, err := m.getTask(3)
	if err != nil {
		t.Fatalf("task should be in memory even if scheduling was skipped: %v", err)
	}
	if got.ID != 3 {
		t.Errorf("ID = %d, want 3", got.ID)
	}

	// ...but NOT scheduled in the time wheel.
	m.mu.RLock()
	_, schedOk := m.scheds[3]
	m.mu.RUnlock()
	if schedOk {
		t.Error("task with invalid schedule should not be scheduled")
	}
}

func TestTaskManagerRestorePublishesForShortIntervalTask(t *testing.T) {
	store := newMockStore()
	seedTask := &Task{
		ID:           1,
		TenantID:     10,
		ExecutorType: "webhook",
		Enabled:      true,
		Schedule:     "1ms",
	}
	_ = store.Save(context.Background(), seedTask)

	m := newTestManagerWithStore(t, store, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	// Subscribe to ExecutionTriggered events to verify the restored task
	// fires on its schedule.
	triggered := make(chan event.Event[event.ExecutionPayload], 1)
	_, _ = event.Subscribe[event.ExecutionPayload](m.bus, event.TypeExecutionTriggered,
		func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
			triggered <- ev
			return nil
		})

	if err := m.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	select {
	case ev := <-triggered:
		if ev.Payload.ExecutionID != "1" {
			t.Errorf("payload.ExecutionID = %q, want %q", ev.Payload.ExecutionID, "1")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for ExecutionTriggered event from restored task")
	}
}

// --- Helpers ---

// mustNewManager creates a Service with default options, failing the test
// on error. Used for in-memory tests that don't need a store or shard manager.
func mustNewManager() *Service {
	m, err := NewService(WithLogger(zap.NewNop()))
	if err != nil {
		panic(fmt.Sprintf("NewService error: %v", err))
	}
	return m
}

// newTestManager creates a Service for testing with an optional ShardManager.
// If shardManager is nil, no sharding is applied (all tasks are owned).
func newTestManager(tb testing.TB, shardManager *scheduler.ShardManager) *Service {
	tb.Helper()
	opts := []Option{WithLogger(zap.NewNop())}
	if shardManager != nil {
		opts = append(opts, WithShardManager(shardManager))
	}
	m, err := NewService(opts...)
	if err != nil {
		tb.Fatalf("NewService error: %v", err)
	}
	return m
}

// newTestManagerWithStore creates a Service for testing with a Store
// and an optional ShardManager. If shardManager is nil, no sharding is applied.
func newTestManagerWithStore(tb testing.TB, store Store, shardManager *scheduler.ShardManager) *Service {
	tb.Helper()
	opts := []Option{
		WithLogger(zap.NewNop()),
		WithStore(store),
	}
	if shardManager != nil {
		opts = append(opts, WithShardManager(shardManager))
	}
	m, err := NewService(opts...)
	if err != nil {
		tb.Fatalf("NewService error: %v", err)
	}
	return m
}

// --- Benchmarks ---

// BenchmarkTaskManagerStatusChangeHandler measures the StatusChange handler
// with a large task set. The eventDrivenTasks index makes this O(event_tasks)
// instead of a full scan with per-item lock acquisition.
func BenchmarkTaskManagerStatusChangeHandler(b *testing.B) {
	m, err := NewService(WithLogger(zap.NewNop()))
	if err != nil {
		b.Fatalf("NewService error: %v", err)
	}
	defer func() { _ = m.Stop(context.Background()) }()

	// Register many non-event tasks that must NOT be scanned.
	for i := int64(1); i <= 10000; i++ {
		if err := m.Register(context.Background(), Task{
			ID:       i,
			Enabled:  true,
			Schedule: "1h",
		}); err != nil {
			b.Fatalf("register interval task %d: %v", i, err)
		}
	}
	// Register a small set of event-driven tasks matching asset 50.
	for i := int64(10001); i <= 10100; i++ {
		if err := m.Register(context.Background(), Task{
			ID:      i,
			AssetID: 50,
			Enabled: true,
		}); err != nil {
			b.Fatalf("register event task %d: %v", i, err)
		}
	}

	payload := event.StatusChangePayload{
		AssetID:    "50",
		CurrStatus: string(types.AssetStatusAbnormal),
	}

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		m.handleStatusChange(payload)
	}
}

// --- Concurrency control (tryClaimRunning) Tests ---

// TestTryClaimRunningFirstCallSucceeds verifies that the first claim for a
// task succeeds and marks it as running.
func TestTryClaimRunningFirstCallSucceeds(t *testing.T) {
	m := mustNewManager()
	defer func() { _ = m.Stop(context.Background()) }()

	if !m.tryClaimRunning(1) {
		t.Fatal("first claim should succeed")
	}
}

// TestTryClaimRunningSecondCallFails verifies that a second claim while the
// task is already running is rejected, providing the Concurrency == 1 guard.
func TestTryClaimRunningSecondCallFails(t *testing.T) {
	m := mustNewManager()
	defer func() { _ = m.Stop(context.Background()) }()

	if !m.tryClaimRunning(1) {
		t.Fatal("first claim should succeed")
	}
	if m.tryClaimRunning(1) {
		t.Fatal("second claim should fail while task is running")
	}
}

// TestTryClaimRunningReleaseAllowsReclaim verifies that after releaseRunning
// the slot can be claimed again.
func TestTryClaimRunningReleaseAllowsReclaim(t *testing.T) {
	m := mustNewManager()
	defer func() { _ = m.Stop(context.Background()) }()

	if !m.tryClaimRunning(1) {
		t.Fatal("first claim should succeed")
	}
	m.releaseRunning(1)
	if !m.tryClaimRunning(1) {
		t.Fatal("claim after release should succeed")
	}
}

// TestTryClaimRunningConcurrent verifies under concurrent access that exactly
// one goroutine wins the claim. This is a regression test for the TOCTOU race
// that existed when the check and set were performed under separate locks.
func TestTryClaimRunningConcurrent(t *testing.T) {
	m := mustNewManager()
	defer func() { _ = m.Stop(context.Background()) }()

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	var wins int32
	var mu sync.Mutex
	start := make(chan struct{})
	for range goroutines {
		go func() {
			defer wg.Done()
			<-start
			if m.tryClaimRunning(42) {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if wins != 1 {
		t.Errorf("expected exactly 1 winning claim, got %d", wins)
	}
}

// --- Restore idempotency (stale schedule cleanup) Tests ---

// TestRestoreClearsStaleSchedules verifies that calling Restore a second time
// removes engine entries for tasks that are no longer in the store. This is a
// regression test for the stale-schedule leak where scheds/scheduleTypes were
// not cleared between Restore calls.
func TestRestoreClearsStaleSchedules(t *testing.T) {
	store := newMockStore()
	_ = store.Save(context.Background(), &Task{
		ID:           1,
		ExecutorType: "webhook",
		Enabled:      true,
		Schedule:     "1h",
	})

	m := newTestManagerWithStore(t, store, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	if err := m.Restore(context.Background()); err != nil {
		t.Fatalf("Restore (1): %v", err)
	}

	m.mu.RLock()
	_, hasSched := m.scheds[1]
	m.mu.RUnlock()
	if !hasSched {
		t.Fatal("expected task 1 to be scheduled after first Restore")
	}

	// Remove the task from the store and Restore again: the stale entry
	// must be gone.
	_ = store.Delete(context.Background(), 1)
	if err := m.Restore(context.Background()); err != nil {
		t.Fatalf("Restore (2): %v", err)
	}

	m.mu.RLock()
	_, hasSchedAfter := m.scheds[1]
	m.mu.RUnlock()
	if hasSchedAfter {
		t.Fatal("stale schedule for deleted task 1 should be cleared after second Restore")
	}
}
