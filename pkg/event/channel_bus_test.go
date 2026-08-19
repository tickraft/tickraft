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
		received = append(received, env.EventID)
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

	// Same priority, dispatched in publish order (FIFO).
	for _, id := range []string{"first", "second", "third"} {
		if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
			WithEventID(id),
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

func TestSyncPublish(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var called atomic.Bool
	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		called.Store(true)
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	// Publish in sync mode: the Handler must complete before Publish returns.
	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithSync(),
	); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if !called.Load() {
		t.Error("handler should have been called before Publish returned in sync mode")
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

	var normalCalled atomic.Bool

	sub1, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		panic("intentional panic")
	})
	if err != nil {
		t.Fatalf("subscribe 1: %v", err)
	}
	defer sub1.Cancel()

	sub2, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		normalCalled.Store(true)
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe 2: %v", err)
	}
	defer sub2.Cancel()

	// Publish in sync mode; the panic should be recovered.
	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithSync(),
	); err != nil {
		t.Fatalf("publish should not fail after panic recovery: %v", err)
	}

	if !normalCalled.Load() {
		t.Error("normal handler should still be called after panic in another handler")
	}

	// Subsequent events should still be processed normally.
	normalCalled.Store(false)
	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithSync(),
	); err != nil {
		t.Fatalf("publish after panic: %v", err)
	}
	if !normalCalled.Load() {
		t.Error("handler should still work after previous panic")
	}
}

func TestHandlerTimeout(t *testing.T) {
	bus := NewBus(
		WithDefaultTimeout(100 * time.Millisecond),
	)
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
	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithSync(),
	); err != nil {
		t.Fatalf("publish: %v", err)
	}
	elapsed := time.Since(start)

	if completed.Load() {
		t.Error("handler should have been timed out, not completed")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("timeout took too long: %v", elapsed)
	}
}

func TestHandlerRetry(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var attempts atomic.Int32
	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		count := attempts.Add(1)
		if count < 3 {
			return errors.New("transient error")
		}
		return nil
	}, WithRetry(3, 10*time.Millisecond))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithSync(),
	); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestHandlerRetryAllFail(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var attempts atomic.Int32
	var savedEnv atomic.Value
	var savedErr atomic.Value

	store := &mockFailedEventStore{
		saveFunc: func(ctx context.Context, env Envelope, err error) error {
			savedEnv.Store(env)
			savedErr.Store(err)
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

	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithSync(),
	); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// 1 initial + 2 retries = 3 attempts
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}

	if savedEnv.Load() == nil {
		t.Error("failed event should have been saved")
	}
	if savedErr.Load() == nil {
		t.Error("error should have been saved")
	}
}

func TestHandlerFilter(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var mu sync.Mutex
	var received []string
	ready := make(chan struct{})

	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		mu.Lock()
		received = append(received, env.TenantID)
		if len(received) == 1 {
			close(ready)
		}
		mu.Unlock()
		return nil
	}, WithFilter(func(env Envelope) bool {
		return env.TenantID == "tenant-001"
	}))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	// Matching event.
	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithTenantID("tenant-001"),
	); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// Non-matching event.
	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithTenantID("tenant-002"),
	); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	// Wait briefly to ensure the non-matching event does not arrive.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Errorf("expected 1 event, got %d", len(received))
	}
	if len(received) > 0 && received[0] != "tenant-001" {
		t.Errorf("expected tenant-001, got %s", received[0])
	}
}

func TestExponentialBackoff(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var timestamps []time.Time
	var mu sync.Mutex

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

	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithSync(),
	); err != nil {
		t.Fatalf("publish: %v", err)
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

	// Wait for all events to be processed.
	time.Sleep(1 * time.Second)
	got := count.Load()
	if got != n {
		t.Errorf("received %d events, want %d", got, n)
	}
}

func TestQueueFullDrop(t *testing.T) {
	bus := NewBus(WithBufferSize(2))
	defer bus.Close()

	// Use a slow handler to fill up the queue.
	processing := make(chan struct{})
	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		<-processing // Block until the test signals.
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	// Publish 2 events to fill the queue.
	for i := range 2 {
		if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{}); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	// The 3rd event should be dropped (queue full).
	err = bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{})
	if err != nil {
		t.Errorf("publish when full should not return error, got %v", err)
	}

	// Release the handler.
	close(processing)
}

func TestWithSyncModeSubscriber(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var called atomic.Bool
	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		called.Store(true)
		return nil
	}, WithSyncMode())
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithSync(),
	); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if !called.Load() {
		t.Error("sync mode subscriber should have been called")
	}
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

func TestWithJitter_Clamp(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	tests := []struct {
		input  float64
		expect float64
	}{
		{-0.5, 0.0},
		{0.0, 0.0},
		{0.3, 0.3},
		{0.5, 0.5},
		{1.0, 1.0},
		{1.5, 1.0},
	}

	for _, tt := range tests {
		cfg := &subscribeConfig{}
		WithJitter(tt.input).apply(cfg)
		if cfg.jitter != tt.expect {
			t.Errorf("WithJitter(%v): got %v, want %v", tt.input, cfg.jitter, tt.expect)
		}
	}
}

func TestJitteredBackoff(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	baseBackoff := 100 * time.Millisecond
	jitterFactor := 0.5

	var timestamps []time.Time
	var mu sync.Mutex

	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		mu.Lock()
		timestamps = append(timestamps, time.Now())
		mu.Unlock()
		return errors.New("always fail")
	}, WithRetry(3, baseBackoff), WithJitter(jitterFactor))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithSync(),
	); err != nil {
		t.Fatalf("publish: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(timestamps) != 4 {
		t.Fatalf("expected 4 attempts, got %d", len(timestamps))
	}

	// Expected exponential backoffs: 100ms, 200ms, 400ms.
	// With jitter=0.5, each backoff is in [0.5*exponential, exponential].
	// Allow scheduling tolerance.
	for i := 1; i < len(timestamps); i++ {
		interval := timestamps[i].Sub(timestamps[i-1])
		exponential := float64(baseBackoff) * float64(int64(1)<<uint(i-1))
		minAllowed := time.Duration(exponential*0.5) - 30*time.Millisecond
		maxAllowed := time.Duration(exponential) + 50*time.Millisecond

		if interval < minAllowed {
			t.Errorf("interval %d: got %v, want >= %v", i, interval, minAllowed)
		}
		if interval > maxAllowed {
			t.Errorf("interval %d: got %v, want <= %v", i, interval, maxAllowed)
		}
	}
}

func TestJitterZero(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var timestamps []time.Time
	var mu sync.Mutex

	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		mu.Lock()
		timestamps = append(timestamps, time.Now())
		mu.Unlock()
		return errors.New("always fail")
	}, WithRetry(3, 50*time.Millisecond), WithJitter(0.0))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{},
		WithSync(),
	); err != nil {
		t.Fatalf("publish: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(timestamps) != 4 {
		t.Fatalf("expected 4 attempts, got %d", len(timestamps))
	}

	// With jitter=0.0, behavior must match pure exponential: 50ms, 100ms, 200ms.
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

func TestJitterWithContextCancel(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	ctx, cancel := context.WithCancel(context.Background())

	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		return errors.New("always fail")
	}, WithRetry(5, 1*time.Second), WithJitter(1.0))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	// Cancel the context shortly after publishing to interrupt the jittered backoff.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	// Publish synchronously; the handler will fail, enter jittered backoff,
	// and the context cancellation should terminate it.
	_ = bus.Publish(ctx, TypeExecutionTriggered, ExecutionPayload{}, WithSync())
	elapsed := time.Since(start)

	// The first attempt is immediate; the backoff before the second attempt
	// is up to 1s. We cancel at 50ms, so total elapsed should be well under 1s.
	if elapsed > 500*time.Millisecond {
		t.Errorf("context cancel did not interrupt backoff: elapsed %v", elapsed)
	}
}
