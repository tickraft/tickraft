// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package timewheel

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/pool"
)

// mustNewWheel constructs a wheel via [NewWheel] and fails the test on
// error. The default-pool initialization path is unreachable in
// practice (size is sanitized), so this helper keeps tests concise
// without ignoring errors.
func mustNewWheel(t *testing.T, size int) Wheel {
	t.Helper()
	w, err := NewWheel(size)
	if err != nil {
		t.Fatalf("NewWheel(%d) error: %v", size, err)
	}
	return w
}

// mustNew constructs a wheel via [New] and fails the test on error.
func mustNew(t *testing.T, opts ...Option) Wheel {
	t.Helper()
	w, err := New(opts...)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	return w
}

func TestAdd(t *testing.T) {
	wheel := mustNewWheel(t, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)

	var called atomic.Int32
	wheel.Add(2*time.Second, func(_ EntryID) {
		called.Add(1)
	})

	time.Sleep(3 * time.Second)

	if got := called.Load(); got != 1 {
		t.Fatalf("expected callback to be called once, got %d", got)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := wheel.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestAddAt(t *testing.T) {
	wheel := mustNewWheel(t, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)

	var called atomic.Int32
	fireAt := time.Now().Add(2 * time.Second)
	wheel.AddAt(fireAt, func(_ EntryID) {
		called.Add(1)
	})

	time.Sleep(3 * time.Second)

	if got := called.Load(); got != 1 {
		t.Fatalf("expected callback to be called once, got %d", got)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := wheel.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestRemove(t *testing.T) {
	wheel := mustNewWheel(t, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)

	var called atomic.Int32
	id := wheel.Add(2*time.Second, func(_ EntryID) {
		called.Add(1)
	})

	wheel.Remove(id)

	time.Sleep(3 * time.Second)

	if got := called.Load(); got != 0 {
		t.Fatalf("expected callback NOT to be called, got %d calls", got)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := wheel.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestRenew(t *testing.T) {
	wheel := mustNewWheel(t, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)

	var called atomic.Int32
	var mu sync.Mutex
	var calledAt time.Time
	start := time.Now()

	id := wheel.Add(2*time.Second, func(_ EntryID) {
		called.Add(1)
		mu.Lock()
		calledAt = time.Now()
		mu.Unlock()
	})

	// Renew with a longer duration; the callback should fire at ~4s, not ~2s.
	wheel.Renew(id, 4*time.Second)

	time.Sleep(5 * time.Second)

	if got := called.Load(); got != 1 {
		t.Fatalf("expected callback to be called once, got %d", got)
	}

	mu.Lock()
	elapsed := calledAt.Sub(start)
	mu.Unlock()
	if elapsed < 3*time.Second {
		t.Fatalf("expected callback to fire after ~4s, fired after %v", elapsed)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := wheel.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestStartStop(t *testing.T) {
	wheel := mustNewWheel(t, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan struct{})
	go func() {
		wheel.Start(ctx)
		close(startDone)
	}()

	// Give the wheel time to start ticking.
	time.Sleep(200 * time.Millisecond)

	// Add an entry while the wheel is running.
	var called atomic.Int32
	wheel.Add(2*time.Second, func(_ EntryID) {
		called.Add(1)
	})

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := wheel.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Verify that Start returned after Stop was called.
	select {
	case <-startDone:
		// Start returned as expected.
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Stop was called")
	}
}

func TestConcurrentAddRemove(t *testing.T) {
	wheel := mustNewWheel(t, 20)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)

	var wg sync.WaitGroup
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := wheel.Add(5*time.Second, func(_ EntryID) {})
			time.Sleep(10 * time.Millisecond)
			wheel.Remove(id)
		}()
	}

	wg.Wait()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := wheel.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestAddExpired(t *testing.T) {
	wheel := mustNewWheel(t, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)

	// Adding with a zero duration should dispatch immediately.
	var called atomic.Int32
	wheel.Add(0, func(_ EntryID) {
		called.Add(1)
	})

	time.Sleep(200 * time.Millisecond)

	if got := called.Load(); got != 1 {
		t.Fatalf("expected immediate callback, got %d calls", got)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := wheel.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestAddAtPast(t *testing.T) {
	wheel := mustNewWheel(t, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)

	// Adding with a past time should dispatch immediately.
	var called atomic.Int32
	wheel.AddAt(time.Now().Add(-1*time.Second), func(_ EntryID) {
		called.Add(1)
	})

	time.Sleep(200 * time.Millisecond)

	if got := called.Load(); got != 1 {
		t.Fatalf("expected immediate callback for past time, got %d calls", got)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := wheel.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestMultipleEntries(t *testing.T) {
	wheel := mustNewWheel(t, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)

	var count atomic.Int32

	// Add multiple entries with different durations.
	wheel.Add(1*time.Second, func(_ EntryID) { count.Add(1) })
	wheel.Add(2*time.Second, func(_ EntryID) { count.Add(1) })
	wheel.Add(3*time.Second, func(_ EntryID) { count.Add(1) })

	time.Sleep(4 * time.Second)

	if got := count.Load(); got != 3 {
		t.Fatalf("expected 3 callbacks, got %d", got)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := wheel.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

// TestWithPool verifies that a custom pool injected via [WithPool] is
// used to dispatch expired callbacks and that the wheel does NOT shut
// down an injected pool on Stop (the caller owns its lifecycle).
func TestWithPool(t *testing.T) {
	p, err := pool.New(pool.WithWorkers(2), pool.WithQueueSize(8))
	if err != nil {
		t.Fatalf("pool.New: %v", err)
	}

	wheel := mustNew(t, WithPool(p))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)

	var executed atomic.Int32
	wheel.Add(1*time.Second, func(_ EntryID) {
		executed.Add(1)
	})

	time.Sleep(3 * time.Second)

	if got := executed.Load(); got != 1 {
		t.Fatalf("expected 1 callback via injected pool, got %d", got)
	}

	// The injected pool must have received the submit.
	if got := p.Stats().Submitted; got < 1 {
		t.Fatalf("expected pool.Stats().Submitted >= 1, got %d", got)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := wheel.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// The wheel must NOT close the injected pool; verify it still
	// accepts submissions after Stop.
	if err := p.Submit(context.Background(), pool.Lambda(func(ctx context.Context) error {
		return nil
	})); err != nil {
		t.Fatalf("injected pool should remain usable after wheel Stop: %v", err)
	}

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("pool.Shutdown: %v", err)
	}
}

// TestPoolRejectionNoDeadlock verifies that a saturated pool does not
// deadlock the timewheel's tick loop. A tiny pool (1 worker, queue
// size 1) is filled with blocking callbacks; the remaining expired
// entries are rejected by the submit timeout. The wheel must remain
// stoppable.
func TestPoolRejectionNoDeadlock(t *testing.T) {
	p, err := pool.New(pool.WithWorkers(1), pool.WithQueueSize(1))
	if err != nil {
		t.Fatalf("pool.New: %v", err)
	}

	wheel := mustNew(t, WithPool(p))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)

	// Blocking callback: keeps the single worker (and the one queued
	// slot) occupied so subsequent dispatches are rejected.
	block := make(chan struct{})
	var executed atomic.Int32
	cb := func(_ EntryID) {
		<-block
		executed.Add(1)
	}

	// Submit many entries that all fire in the same tick. Only 2 can
	// be held by the pool (1 running + 1 queued); the rest are
	// rejected after submitTimeout without blocking the tick loop.
	const total = 50
	for i := 0; i < total; i++ {
		wheel.Add(1*time.Second, cb)
	}

	// Wait long enough for the 1-second tick to fire and the dispatch
	// loop to process all 50 entries (the rejected ones return after
	// submitTimeout each, so allow ample time).
	time.Sleep(8 * time.Second)

	// Release the blocked callbacks so the pool can drain.
	close(block)

	// Give the 2 in-flight callbacks a moment to finish.
	time.Sleep(500 * time.Millisecond)

	// The wheel must still be stoppable — this is the core assertion:
	// a saturated pool must not deadlock the tick loop.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	if err := wheel.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed (timewheel deadlocked by pool rejection): %v", err)
	}

	if got := executed.Load(); got > int32(total) {
		t.Fatalf("executed %d, expected at most %d", got, total)
	}
	t.Logf("executed %d/%d callbacks (rejection path exercised)", executed.Load(), total)

	// Clean up the injected pool.
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("pool.Shutdown: %v", err)
	}
}

// TestStopReleasesOwnedPool verifies that when no pool is injected the
// wheel creates and shuts down its own pool on Stop, so no goroutine
// leaks. It also verifies Stop on a never-started wheel still drains
// the default pool.
func TestStopReleasesOwnedPool(t *testing.T) {
	t.Run("started", func(t *testing.T) {
		wheel := mustNew(t, WithWorkerSize(2))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go wheel.Start(ctx)

		// Fire one callback to confirm the default pool works.
		var executed atomic.Int32
		wheel.Add(1*time.Second, func(_ EntryID) {
			executed.Add(1)
		})
		time.Sleep(3 * time.Second)
		if got := executed.Load(); got != 1 {
			t.Fatalf("expected 1 callback via default pool, got %d", got)
		}

		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := wheel.Stop(stopCtx); err != nil {
			t.Fatalf("Stop failed: %v", err)
		}
	})

	t.Run("never_started", func(t *testing.T) {
		// A wheel that was created but never started must still release
		// its default pool on Stop to avoid goroutine leaks.
		wheel := mustNew(t, WithWorkerSize(1))

		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := wheel.Stop(stopCtx); err != nil {
			t.Fatalf("Stop on never-started wheel failed: %v", err)
		}
	})
}

// TestStopOnClosedPool verifies that Stop returns without panicking or
// hanging when an injected pool was already shut down by the caller
// before Stop is invoked.
func TestStopOnClosedPool(t *testing.T) {
	p, err := pool.New(pool.WithWorkers(1), pool.WithQueueSize(1))
	if err != nil {
		t.Fatalf("pool.New: %v", err)
	}

	wheel := mustNew(t, WithPool(p))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wheel.Start(ctx)

	// Give the wheel a moment to start ticking.
	time.Sleep(200 * time.Millisecond)

	// Caller shuts down the injected pool first.
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("pool.Shutdown: %v", err)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	err = wheel.Stop(stopCtx)
	// The wheel's Stop must not panic or hang. Since the pool is
	// injected (not owned), the wheel does not call Shutdown on it; it
	// only waits for the tick goroutine to exit. The key invariant is
	// that Stop returns within the timeout.
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected Stop error after pool closed: %v", err)
	}
}

// TestCallbackPanicIsolated verifies that a panicking callback is
// recovered and does not crash the wheel or the pool worker. The wheel
// must remain fully usable after a callback panic: a subsequent entry
// still fires, and Stop completes without error.
func TestCallbackPanicIsolated(t *testing.T) {
	wheel := mustNewWheel(t, 4)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wheel.Start(ctx)

	// Register a callback that panics. The wheel must recover it and
	// keep ticking.
	wheel.Add(1*time.Second, func(_ EntryID) {
		panic("boom")
	})

	// Give the panicking callback time to fire and be recovered.
	time.Sleep(2500 * time.Millisecond)

	// The wheel must still be usable: a later entry must fire normally.
	var called atomic.Int32
	wheel.Add(1*time.Second, func(_ EntryID) {
		called.Add(1)
	})

	time.Sleep(2500 * time.Millisecond)

	if got := called.Load(); got != 1 {
		t.Fatalf("expected callback to fire once after a prior panic, got %d", got)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := wheel.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed after callback panic: %v", err)
	}
}
