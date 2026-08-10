// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package event

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBusInterface(t *testing.T) {
	var bus = NewBus()
	defer bus.Close()

	if bus == nil {
		t.Fatal("NewBus returned nil")
	}
}

func TestBusClose(t *testing.T) {
	bus := NewBus()
	if err := bus.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// double close should not panic
	if err := bus.Close(); err != nil {
		t.Fatalf("double close: %v", err)
	}
}

func TestPublishAfterClose(t *testing.T) {
	bus := NewBus()
	if err := bus.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{})
	if !errors.Is(err, ErrBusClosed) {
		t.Errorf("expected ErrBusClosed, got %v", err)
	}
}

func TestSubscribeAfterClose(t *testing.T) {
	bus := NewBus()
	if err := bus.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	_, err := bus.Subscribe(TypeExecutionTriggered, func(context.Context, Envelope) error {
		return nil
	})
	if !errors.Is(err, ErrBusClosed) {
		t.Errorf("expected ErrBusClosed, got %v", err)
	}
}

func TestGenericPublishSubscribe(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	received := make(chan Event[ExecutionPayload], 1)
	sub, err := Subscribe[ExecutionPayload](bus, TypeExecutionTriggered,
		func(ctx context.Context, e Event[ExecutionPayload]) error {
			received <- e
			return nil
		},
	)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	payload := ExecutionPayload{
		TaskID:       "task-001",
		ExecutionID:  "exec-001",
		ExecutorType: "http",
		Category:     "Actuator",
		TenantID:     "tenant-001",
		Action:       "triggered",
	}
	if err := Publish(context.Background(), bus, TypeExecutionTriggered, payload); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case got := <-received:
		if got.Payload.TaskID != payload.TaskID {
			t.Errorf("task_id: got %q, want %q", got.Payload.TaskID, payload.TaskID)
		}
		if got.Payload.ExecutionID != payload.ExecutionID {
			t.Errorf("execution_id: got %q, want %q", got.Payload.ExecutionID, payload.ExecutionID)
		}
		if got.Type != TypeExecutionTriggered {
			t.Errorf("type: got %q, want %q", got.Type, TypeExecutionTriggered)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestGenericSubscribeWithTaskPayload(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	received := make(chan Event[TaskPayload], 1)
	sub, err := Subscribe[TaskPayload](bus, TypeTaskCreated,
		func(ctx context.Context, e Event[TaskPayload]) error {
			received <- e
			return nil
		},
	)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	payload := TaskPayload{
		TaskID:       "task-001",
		TaskName:     "test-task",
		ExecutorType: "local",
		TenantID:     "tenant-001",
		Action:       "created",
	}
	if err := Publish(context.Background(), bus, TypeTaskCreated, payload); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case got := <-received:
		if got.Payload.TaskID != payload.TaskID {
			t.Errorf("task_id: got %q, want %q", got.Payload.TaskID, payload.TaskID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestSubscriptionCancel(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var count atomic.Int32
	sub, err := Subscribe[ExecutionPayload](bus, TypeExecutionTriggered,
		func(ctx context.Context, e Event[ExecutionPayload]) error {
			count.Add(1)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// cancel before publishing
	sub.Cancel()

	if err := Publish(context.Background(), bus, TypeExecutionTriggered, ExecutionPayload{}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	if count.Load() != 0 {
		t.Errorf("canceled subscription should not receive events, got %d", count.Load())
	}
}

func TestSubscriptionID(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	sub1, err := Subscribe[ExecutionPayload](bus, TypeExecutionTriggered,
		func(ctx context.Context, e Event[ExecutionPayload]) error { return nil },
	)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub1.Cancel()

	sub2, err := Subscribe[ExecutionPayload](bus, TypeExecutionTriggered,
		func(ctx context.Context, e Event[ExecutionPayload]) error { return nil },
	)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub2.Cancel()

	if sub1.ID() == sub2.ID() {
		t.Error("subscription IDs should be unique")
	}
	if sub1.ID() == "" {
		t.Error("subscription ID should not be empty")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	var handler1Called, handler2Called atomic.Bool

	sub1, err := Subscribe[ExecutionPayload](bus, TypeExecutionCompleted,
		func(ctx context.Context, e Event[ExecutionPayload]) error {
			handler1Called.Store(true)
			wg.Done()
			return nil
		},
	)
	if err != nil {
		t.Fatalf("subscribe 1: %v", err)
	}
	defer sub1.Cancel()

	sub2, err := Subscribe[ExecutionPayload](bus, TypeExecutionCompleted,
		func(ctx context.Context, e Event[ExecutionPayload]) error {
			handler2Called.Store(true)
			wg.Done()
			return nil
		},
	)
	if err != nil {
		t.Fatalf("subscribe 2: %v", err)
	}
	defer sub2.Cancel()

	if err := Publish(context.Background(), bus, TypeExecutionCompleted, ExecutionPayload{}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handlers")
	}

	if !handler1Called.Load() {
		t.Error("handler1 was not called")
	}
	if !handler2Called.Load() {
		t.Error("handler2 was not called")
	}
}

func TestNonGenericPublishSubscribe(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	received := make(chan Envelope, 1)
	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		received <- env
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{TaskID: "task-001"}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case env := <-received:
		if env.Type != TypeExecutionTriggered {
			t.Errorf("type: got %q, want %q", env.Type, TypeExecutionTriggered)
		}
		if env.EventID == "" {
			t.Error("event_id should be auto-generated")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestPublishWithOptions(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	received := make(chan Envelope, 1)
	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		received <- env
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	customID := "custom-event-id"
	customTenant := "tenant-custom"
	customMeta := map[string]string{"source": "test"}

	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithEventID(customID),
		WithTenantID(customTenant),
		WithMetadata(customMeta),
		WithPriority(5),
	); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case env := <-received:
		if env.EventID != customID {
			t.Errorf("event_id: got %q, want %q", env.EventID, customID)
		}
		if env.TenantID != customTenant {
			t.Errorf("tenant_id: got %q, want %q", env.TenantID, customTenant)
		}
		if env.Priority != 5 {
			t.Errorf("priority: got %d, want 5", env.Priority)
		}
		if env.Metadata["source"] != "test" {
			t.Errorf("metadata[source]: got %q, want %q", env.Metadata["source"], "test")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestPublishNoSubscribers(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	// Publishing an event with no subscribers should be silently dropped.
	err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{})
	if err != nil {
		t.Errorf("publish with no subscribers should not return error, got %v", err)
	}
}

func TestNoopFailedEventStore(t *testing.T) {
	store := NoopFailedEventStore{}
	err := store.Save(context.Background(), Envelope{}, errors.New("test"))
	if err != nil {
		t.Errorf("NoopFailedEventStore.Save should return nil, got %v", err)
	}
}

func TestNewBusWithOptions(t *testing.T) {
	bus := NewBus(
		WithBufferSize(512),
		WithDefaultTimeout(5*time.Second),
		WithDebug(true),
	)
	defer bus.Close()

	cb := bus.(*channelBus)
	if cb.bufferSize != 512 {
		t.Errorf("bufferSize: got %d, want 512", cb.bufferSize)
	}
	if cb.defaultTimeout != 5*time.Second {
		t.Errorf("defaultTimeout: got %v, want 5s", cb.defaultTimeout)
	}
	if !cb.debug {
		t.Error("debug should be true")
	}
}
