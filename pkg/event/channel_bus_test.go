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

func TestPrioritySorting(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var mu sync.Mutex
	var received []int
	ready := make(chan struct{})
	release := make(chan struct{})
	handlerStarted := make(chan struct{}, 1)

	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		select {
		case handlerStarted <- struct{}{}:
		default:
		}
		<-release // Block until all events are enqueued.
		mu.Lock()
		received = append(received, env.Priority)
		if len(received) == 4 {
			close(ready)
		}
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	// Publish a blocking event (lowest priority, will be popped first and block the handler).
	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithPriority(0),
	); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Wait until the handler is invoked (the blocking event has been popped).
	<-handlerStarted

	// While the handler is blocked, enqueue the remaining events.
	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithPriority(1),
	); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithPriority(10),
	); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithPriority(5),
	); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Release the handler; the consumer goroutine pops the remaining events by priority.
	close(release)

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 4 {
		t.Fatalf("received %d events, want 4", len(received))
	}
	// Expected order: 0 (blocking event popped first) -> 10 -> 5 -> 1 (sorted by priority).
	if received[0] != 0 || received[1] != 10 || received[2] != 5 || received[3] != 1 {
		t.Errorf("priority order: got %v, want [0 10 5 1]", received)
	}
}

func TestSamePriorityFIFO(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var mu sync.Mutex
	var received []string
	ready := make(chan struct{})

	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		mu.Lock()
		received = append(received, env.Metadata["seq"])
		if len(received) == 3 {
			close(ready)
		}
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	// Same priority, dispatched in publish order (FIFO). The generated event
	// IDs are opaque, so the publish order is tagged via metadata.
	for _, id := range []string{"first", "second", "third"} {
		if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
			WithMetadata(map[string]string{"seq": id}),
			WithPriority(5),
		); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	mu.Lock()
	defer mu.Unlock()
	if received[0] != "first" || received[1] != "second" || received[2] != "third" {
		t.Errorf("FIFO order: got %v, want [first second third]", received)
	}
}

func TestAsyncPublishDoesNotBlock(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	start := time.Now()
	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Errorf("async publish blocked for %v, want < 50ms", elapsed)
	}
}

func TestPanicRecovery(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	done := make(chan struct{}, 2)

	sub1, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		panic("intentional panic")
	})
	if err != nil {
		t.Fatalf("subscribe 1: %v", err)
	}
	defer sub1.Cancel()

	sub2, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		done <- struct{}{}
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe 2: %v", err)
	}
	defer sub2.Cancel()

	// The panic in the first handler should be recovered and the second
	// handler still invoked for the same event.
	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{}); err != nil {
		t.Fatalf("publish should not fail after panic recovery: %v", err)
	}
	waitForEvents(t, done, 1)

	// Subsequent events should still be processed normally.
	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{}); err != nil {
		t.Fatalf("publish after panic: %v", err)
	}
	waitForEvents(t, done, 1)
}

// waitForEvents receives n delivery signals from done, failing the test on
// timeout. It is the async replacement for the removed sync-publish mode.
func waitForEvents(t *testing.T, done <-chan struct{}, n int) {
	t.Helper()
	for range n {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for event delivery")
		}
	}
}

func TestHandlerTimeout(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var completed atomic.Bool
	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		select {
		case <-time.After(2 * time.Second):
			completed.Store(true)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, WithTimeout(100*time.Millisecond))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	start := time.Now()
	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// Close drains the queue and waits for the consumer goroutine, so the
	// timed-out handler invocation has fully finished once it returns.
	if err := bus.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	elapsed := time.Since(start)

	if completed.Load() {
		t.Error("handler should have been timed out, not completed")
	}
	// The 100ms handler timeout fires long before the 2s handler body; the
	// generous upper bound only guards against a lost timeout.
	if elapsed > 1500*time.Millisecond {
		t.Errorf("timeout took too long: %v", elapsed)
	}
}

func TestHandlerRetry(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var attempts atomic.Int32
	succeeded := make(chan struct{})
	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		count := attempts.Add(1)
		if count < 3 {
			return errors.New("transient error")
		}
		close(succeeded)
		return nil
	}, WithRetry(3, 10*time.Millisecond))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case <-succeeded:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for retry success")
	}

	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestHandlerRetryAllFail(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var attempts atomic.Int32
	saved := make(chan struct{}, 1)

	store := &mockFailedEventStore{
		saveFunc: func(ctx context.Context, env Envelope, err error) error {
			select {
			case saved <- struct{}{}:
			default:
			}
			return nil
		},
	}

	cb := bus.(*channelBus)
	cb.failedStore = store

	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		attempts.Add(1)
		return errors.New("permanent error")
	}, WithRetry(2, 10*time.Millisecond))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case <-saved:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for failed-event persistence")
	}

	// 1 initial + 2 retries = 3 attempts
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestExponentialBackoff(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var timestamps []time.Time
	var mu sync.Mutex
	failed := make(chan struct{}, 1)

	store := &mockFailedEventStore{
		saveFunc: func(ctx context.Context, env Envelope, err error) error {
			select {
			case failed <- struct{}{}:
			default:
			}
			return nil
		},
	}
	bus.(*channelBus).failedStore = store

	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		mu.Lock()
		timestamps = append(timestamps, time.Now())
		mu.Unlock()
		return errors.New("always fail")
	}, WithRetry(3, 50*time.Millisecond))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// The failed-event store fires only after the final retry, so receiving
	// here means all attempts have completed.
	select {
	case <-failed:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for retry exhaustion")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(timestamps) != 4 { // 1 initial + 3 retries
		t.Fatalf("expected 4 attempts, got %d", len(timestamps))
	}

	// Verify backoff intervals: 50ms, 100ms, 200ms.
	// Allow some tolerance due to scheduling precision.
	interval1 := timestamps[1].Sub(timestamps[0])
	interval2 := timestamps[2].Sub(timestamps[1])
	interval3 := timestamps[3].Sub(timestamps[2])

	if interval1 < 40*time.Millisecond || interval1 > 100*time.Millisecond {
		t.Errorf("interval 1: got %v, want ~50ms", interval1)
	}
	if interval2 < 80*time.Millisecond || interval2 > 160*time.Millisecond {
		t.Errorf("interval 2: got %v, want ~100ms", interval2)
	}
	if interval3 < 160*time.Millisecond || interval3 > 300*time.Millisecond {
		t.Errorf("interval 3: got %v, want ~200ms", interval3)
	}
}

func TestMemoryPoolReuse(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	received := make(chan Envelope, 5)
	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		received <- env
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	// Publish multiple events to verify the memory pool works correctly.
	for i := range 5 {
		if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{}); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	for i := range 5 {
		select {
		case <-received:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for event %d", i)
		}
	}
}

func TestConcurrentPublish(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var count atomic.Int32
	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		count.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	const n = 200
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{}); err != nil {
				t.Errorf("publish: %v", err)
			}
		}()
	}
	wg.Wait()

	// Close drains the queues and waits for the consumer goroutine, so every
	// published event has been dispatched once Close returns.
	if err := bus.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	got := count.Load()
	if got != n {
		t.Errorf("received %d events, want %d", got, n)
	}
}

func TestQueueFullDrop(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	// Use a blocked handler so the default-capacity queue fills up.
	processing := make(chan struct{})
	started := make(chan struct{}, 1)
	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-processing // Block until the test signals.
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	// The first event is popped by the consumer and blocks in the handler;
	// once started fires, exactly defaultBufferSize queued events fill the
	// queue.
	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{}); err != nil {
		t.Fatalf("publish first: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler start")
	}
	for range defaultBufferSize {
		if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{}); err != nil {
			t.Fatalf("publish fill: %v", err)
		}
	}

	// The event beyond the queue capacity is dropped, not returned as an
	// error: dropping is the bus's backpressure contract.
	err = bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{})
	if err != nil {
		t.Errorf("publish when full should not return error, got %v", err)
	}

	// Release the handler.
	close(processing)
}

func TestEnvelopePoolAcquireRelease(t *testing.T) {
	env := acquireEnvelope()
	env.Type = TypeExecutionTriggered
	env.Payload = ExecutionPayload{TaskID: "test"}
	env.EventID = "evt-001"
	env.TenantID = "tenant-001"
	env.Priority = 5
	env.Metadata = map[string]string{"key": "value"}

	releaseEnvelope(env)

	// Verify that all fields have been cleared.
	if env.Type != "" {
		t.Errorf("Type not cleared: %q", env.Type)
	}
	if env.Payload != nil {
		t.Error("Payload not cleared")
	}
	if env.EventID != "" {
		t.Errorf("EventID not cleared: %q", env.EventID)
	}
	if env.TenantID != "" {
		t.Errorf("TenantID not cleared: %q", env.TenantID)
	}
	if env.Priority != 0 {
		t.Errorf("Priority not cleared: %d", env.Priority)
	}
	if env.Metadata != nil {
		t.Error("Metadata not cleared")
	}
}

func TestGenerateEventID(t *testing.T) {
	id1 := generateEventID()
	id2 := generateEventID()

	if id1 == "" {
		t.Error("event ID should not be empty")
	}
	if id1 == id2 {
		t.Error("event IDs should be unique")
	}
}

// mockFailedEventStore is a mock failed-event store used for testing.
type mockFailedEventStore struct {
	saveFunc func(ctx context.Context, env Envelope, err error) error
}

func (m *mockFailedEventStore) Save(ctx context.Context, env Envelope, err error) error {
	if m.saveFunc != nil {
		return m.saveFunc(ctx, env, err)
	}
	return nil
}
