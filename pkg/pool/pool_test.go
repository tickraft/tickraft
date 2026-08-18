// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package pool

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor polls [Pool.Stats] until cond returns true or the deadline
// expires. It avoids flaky fixed sleeps in tests that depend on
// asynchronous worker progress.
func waitFor(t *testing.T, p Pool, cond func(Stats) bool, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond(p.Stats()) {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("condition not met within %v, last stats=%+v", d, p.Stats())
}

// counterJob is a struct-based [Job] used to verify the Job interface
// is honored and the original instance is forwarded to handlers.
type counterJob struct {
	counter *atomic.Int64
	err     error
	panicV  any
}

func (j *counterJob) Run(ctx context.Context) error {
	j.counter.Add(1)
	if j.panicV != nil {
		panic(j.panicV)
	}
	return j.err
}

// TestJobInterface verifies that both struct-based Jobs and [Lambda]
// are accepted by Submit and have Run invoked exactly once.
func TestJobInterface(t *testing.T) {
	p, err := New(WithWorkers(2), WithQueueSize(4))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	}()

	var structCount, lambdaCount atomic.Int64

	cj := &counterJob{counter: &structCount}
	if err := p.Submit(context.Background(), cj); err != nil {
		t.Fatalf("submit struct job: %v", err)
	}
	if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
		lambdaCount.Add(1)
		return nil
	})); err != nil {
		t.Fatalf("submit: %v", err)
	}

	waitFor(t, p, func(s Stats) bool { return s.Completed == 2 }, time.Second)

	if got := structCount.Load(); got != 1 {
		t.Fatalf("struct job Run count = %d, want 1", got)
	}
	if got := lambdaCount.Load(); got != 1 {
		t.Fatalf("lambda job Run count = %d, want 1", got)
	}
}

// TestSubmitSuccess verifies that a job returning nil increments the
// completed counter and is observable via Stats.
func TestSubmitSuccess(t *testing.T) {
	p, err := New(WithWorkers(1), WithQueueSize(1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	}()

	var done atomic.Int64
	if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
		done.Add(1)
		return nil
	})); err != nil {
		t.Fatalf("submit: %v", err)
	}

	waitFor(t, p, func(s Stats) bool { return s.Completed == 1 }, time.Second)

	if got := done.Load(); got != 1 {
		t.Fatalf("done = %d, want 1", got)
	}
	if got := p.Stats().Submitted; got != 1 {
		t.Fatalf("Submitted = %d, want 1", got)
	}
}

// TestSubmitError verifies that a job returning an error triggers the
// ErrorHandler with the original [Job] instance and the returned
// error, and is counted as Failed exactly once.
func TestSubmitError(t *testing.T) {
	var (
		mu     sync.Mutex
		gotJob Job
		gotErr error
	)
	p, err := New(
		WithWorkers(1), WithQueueSize(1),
		WithErrorHandler(func(job Job, e error) {
			mu.Lock()
			defer mu.Unlock()
			gotJob = job
			gotErr = e
		}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	}()

	sentinel := errors.New("boom")
	var ran atomic.Int64
	job := &counterJob{
		counter: &ran,
		err:     sentinel,
	}
	if err := p.Submit(context.Background(), job); err != nil {
		t.Fatalf("submit: %v", err)
	}

	waitFor(t, p, func(s Stats) bool { return s.Failed == 1 }, time.Second)

	mu.Lock()
	defer mu.Unlock()
	if !errors.Is(gotErr, sentinel) {
		t.Fatalf("error handler got %v, want %v", gotErr, sentinel)
	}
	cj, ok := gotJob.(*counterJob)
	if !ok {
		t.Fatalf("error handler got job type %T, want *counterJob", gotJob)
	}
	if cj != job {
		t.Fatal("error handler received a different job instance")
	}
	if ran.Load() != 1 {
		t.Fatalf("job Run count = %d, want 1", ran.Load())
	}
}

// TestSubmitPanic verifies that a panicking job is recovered, reported
// to the PanicHandler with the original [Job] instance, counted as
// Failed, and does not kill the worker.
func TestSubmitPanic(t *testing.T) {
	var (
		mu       sync.Mutex
		gotJob   Job
		gotPanic any
	)
	p, err := New(
		WithWorkers(1), WithQueueSize(1),
		WithPanicHandler(func(job Job, r any) {
			mu.Lock()
			defer mu.Unlock()
			gotJob = job
			gotPanic = r
		}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	}()

	panicVal := "kaboom"
	var ran atomic.Int64
	job := &counterJob{counter: &ran, panicV: panicVal}
	if err := p.Submit(context.Background(), job); err != nil {
		t.Fatalf("submit: %v", err)
	}

	waitFor(t, p, func(s Stats) bool { return s.Failed == 1 }, time.Second)

	mu.Lock()
	if gotPanic != panicVal {
		t.Fatalf("panic handler got %v, want %v", gotPanic, panicVal)
	}
	cj, ok := gotJob.(*counterJob)
	if !ok {
		t.Fatalf("panic handler got job type %T, want *counterJob", gotJob)
	}
	if cj != job {
		t.Fatal("panic handler received a different job instance")
	}
	mu.Unlock()

	// The worker must remain usable after a panic.
	if err := p.Submit(context.Background(), Lambda(func(context.Context) error {
		return nil
	})); err != nil {
		t.Fatalf("submit after panic: %v", err)
	}
	waitFor(t, p, func(s Stats) bool { return s.Completed == 1 }, time.Second)
}

// TestSubmitPanicNoHandler verifies that a panic is swallowed (no
// propagation) and the worker keeps running when no panic handler is
// configured.
func TestSubmitPanicNoHandler(t *testing.T) {
	p, err := New(WithWorkers(1), WithQueueSize(1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	}()

	if err := p.Submit(context.Background(), Lambda(func(context.Context) error {
		panic("no handler")
	})); err != nil {
		t.Fatalf("submit: %v", err)
	}

	waitFor(t, p, func(s Stats) bool { return s.Failed == 1 }, time.Second)

	if err := p.Submit(context.Background(), Lambda(func(context.Context) error {
		return nil
	})); err != nil {
		t.Fatalf("submit after panic: %v", err)
	}
	waitFor(t, p, func(s Stats) bool { return s.Completed == 1 }, time.Second)
}

// TestNewDefaults verifies that a pool created with no options has the
// documented default worker count and the warmup pre-start.
func TestNewDefaults(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	}()

	s := p.Stats()
	expectedWarmup := runtime.NumCPU()
	expectedWarmup = min(expectedWarmup, defaultWarmup)
	if s.Workers != int64(expectedWarmup) {
		t.Fatalf("Workers = %d, want %d (warmup)", s.Workers, expectedWarmup)
	}

	// Sanity: a pool that has just been created has no activity yet
	// (workers may have started, but no jobs have run).
	if s.Submitted != 0 || s.Completed != 0 || s.Failed != 0 {
		t.Fatalf("initial stats nonzero: %+v", s)
	}
}

// TestNewInvalidConfig verifies that invalid worker and queue sizes
// are rejected by New.
func TestNewInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		opt  Option
	}{
		{"workers zero", WithWorkers(0)},
		{"workers negative", WithWorkers(-1)},
		{"queue zero", WithQueueSize(0)},
		{"queue negative", WithQueueSize(-1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(tt.opt)
			if err == nil {
				if serr := p.Shutdown(context.Background()); serr != nil {
					t.Fatalf("shutdown: %v", serr)
				}
				t.Fatal("expected error, got nil")
			}
		})
	}
}

// TestShutdownGraceful verifies that Shutdown waits for all submitted
// jobs to complete before returning.
func TestShutdownGraceful(t *testing.T) {
	p, err := New(WithWorkers(4), WithQueueSize(8))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var completed atomic.Int64
	const n = 100
	for i := range n {
		if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
			completed.Add(1)
			return nil
		})); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if got := completed.Load(); got != n {
		t.Fatalf("completed = %d, want %d", got, n)
	}
	if got := p.Stats().Completed; got != n {
		t.Fatalf("Stats Completed = %d, want %d", got, n)
	}
}

// TestShutdownTimeout verifies that Shutdown returns
// context.DeadlineExceeded when the provided context expires before
// workers finish, and that workers are forcibly stopped via runCtx
// cancellation.
func TestShutdownTimeout(t *testing.T) {
	p, err := New(WithWorkers(1), WithQueueSize(1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	started := make(chan struct{})
	if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})); err != nil {
		t.Fatalf("submit: %v", err)
	}
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = p.Shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error = %v, want DeadlineExceeded", err)
	}
}

// TestShutdownIdempotent verifies that calling Shutdown more than once
// returns nil and does not panic.
func TestShutdownIdempotent(t *testing.T) {
	p, err := New(WithWorkers(1), WithQueueSize(1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("first shutdown: %v", err)
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("second shutdown: %v", err)
	}
}

// TestSubmitClosed verifies that Submit returns ErrClosed after
// Shutdown has been called.
func TestSubmitClosed(t *testing.T) {
	p, err := New(WithWorkers(1), WithQueueSize(1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
		return nil
	})); !errors.Is(err, ErrClosed) {
		t.Fatalf("Submit error = %v, want ErrClosed", err)
	}
}

// TestRejectionAbort verifies that under the default Abort policy a
// Submit on a full queue blocks and returns ctx.Err() when the caller
// context expires.
func TestRejectionAbort(t *testing.T) {
	p, err := New(WithWorkers(1), WithQueueSize(1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	}()

	release := make(chan struct{})
	// Block the single worker.
	if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
		<-release
		return nil
	})); err != nil {
		t.Fatalf("submit blocker: %v", err)
	}
	// Fill the queue.
	if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
		return nil
	})); err != nil {
		t.Fatalf("submit filler: %v", err)
	}
	waitFor(t, p, func(s Stats) bool { return s.Active == 1 && s.Pending == 1 }, time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = p.Submit(ctx, Lambda(func(ctx context.Context) error { return nil }))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Submit error = %v, want DeadlineExceeded", err)
	}
	close(release)
}

// TestRejectionCallerRuns verifies that under the CallerRuns policy a
// Submit on a full queue executes the job synchronously in the
// caller's goroutine: Submit returns only after the job body has run.
func TestRejectionCallerRuns(t *testing.T) {
	p, err := New(
		WithWorkers(1), WithQueueSize(1),
		WithRejectionPolicy(RejectionCallerRuns),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	}()

	release := make(chan struct{})
	// Block the single worker.
	if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
		<-release
		return nil
	})); err != nil {
		t.Fatalf("submit blocker: %v", err)
	}
	// Wait for the blocker to become active so the queue is empty
	// and the filler is enqueued rather than caller-run.
	waitFor(t, p, func(s Stats) bool { return s.Active == 1 }, time.Second)
	// Fill the queue.
	if err := p.Submit(context.Background(), Lambda(func(context.Context) error {
		return nil
	})); err != nil {
		t.Fatalf("submit filler: %v", err)
	}
	waitFor(t, p, func(s Stats) bool { return s.Active == 1 && s.Pending == 1 }, time.Second)

	// Next Submit must execute synchronously: by the time it
	// returns, the job body has run.
	var executed atomic.Bool
	done := make(chan error, 1)
	go func() {
		done <- p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
			executed.Store(true)
			return nil
		}))
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		if !executed.Load() {
			t.Fatal("CallerRuns job did not execute before Submit returned")
		}
	case <-time.After(time.Second):
		t.Fatal("submit did not return within timeout")
	}
	close(release)
}

// TestRejectionDiscardOldest verifies that under the DiscardOldest
// policy a Submit on a full queue drops the oldest queued job
// (incrementing Discarded) and enqueues the new one.
func TestRejectionDiscardOldest(t *testing.T) {
	p, err := New(
		WithWorkers(1), WithQueueSize(1),
		WithRejectionPolicy(RejectionDiscardOldest),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	}()

	release := make(chan struct{})
	// Block the single worker.
	if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
		<-release
		return nil
	})); err != nil {
		t.Fatalf("submit blocker: %v", err)
	}
	// Wait for the blocker to become active so the queue is empty
	// and the "old" job is enqueued rather than discarding the blocker.
	waitFor(t, p, func(s Stats) bool { return s.Active == 1 }, time.Second)
	// Fill the queue with an "old" job that should be discarded.
	var oldRan atomic.Bool
	if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
		oldRan.Store(true)
		return nil
	})); err != nil {
		t.Fatalf("submit old: %v", err)
	}
	waitFor(t, p, func(s Stats) bool { return s.Pending == 1 && s.Active == 1 }, time.Second)

	// Submit a new job: should discard the old one and enqueue new.
	if err := p.Submit(context.Background(), Lambda(func(context.Context) error {
		return nil
	})); err != nil {
		t.Fatalf("submit new: %v", err)
	}

	if got := p.Stats().Discarded; got != 1 {
		t.Fatalf("Discarded = %d, want 1", got)
	}

	// Release the blocker so the worker drains the queue.
	close(release)
	waitFor(t, p, func(s Stats) bool { return s.Completed == 2 }, time.Second)

	// The discarded "old" job must never have run.
	if oldRan.Load() {
		t.Fatal("discarded old job ran")
	}
}

// TestLazyWorkerStartup verifies that New pre-starts only the warmup
// workers and that additional workers are grown lazily as the queue
// fills.
func TestLazyWorkerStartup(t *testing.T) {
	p, err := New(WithWorkers(100), WithQueueSize(8))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	}()

	// Initial warmup.
	if got := p.Stats().Workers; got != defaultWarmup {
		t.Fatalf("initial Workers = %d, want %d", got, defaultWarmup)
	}

	// Block all warmup workers so the queue starts filling.
	release := make(chan struct{})
	for i := range defaultWarmup {
		if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
			<-release
			return nil
		})); err != nil {
			t.Fatalf("submit blocker %d: %v", i, err)
		}
	}
	waitFor(t, p, func(s Stats) bool { return s.Active == int64(defaultWarmup) }, time.Second)

	// Fill the queue to capacity. After this the queue is full.
	for i := range 8 {
		if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
			<-release
			return nil
		})); err != nil {
			t.Fatalf("submit filler %d: %v", i, err)
		}
	}

	// Further submits find the queue full and grow workers. Each
	// grow + retry either enqueues immediately (the new worker
	// drains a slot) or briefly falls through to Abort which then
	// succeeds once the new worker drains.
	for i := range 10 {
		if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
			<-release
			return nil
		})); err != nil {
			t.Fatalf("submit growth %d: %v", i, err)
		}
	}

	waitFor(t, p, func(s Stats) bool { return s.Workers > int64(defaultWarmup) }, time.Second)
	if got := p.Stats().Workers; got <= int64(defaultWarmup) {
		t.Fatalf("Workers = %d, expected > %d after backlog", got, defaultWarmup)
	}
	close(release)
}

// TestStallDetection verifies that the stall handler fires exactly
// once per stall episode, resets when the queue recovers, and fires
// again on the next episode.
func TestStallDetection(t *testing.T) {
	var (
		mu    sync.Mutex
		count int
	)
	p, err := New(
		WithWorkers(1), WithQueueSize(1),
		WithStallDetection(150*time.Millisecond, func(s Stats) {
			mu.Lock()
			defer mu.Unlock()
			count++
		}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	}()

	release := make(chan struct{})
	// Block worker.
	if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
		<-release
		return nil
	})); err != nil {
		t.Fatalf("submit blocker: %v", err)
	}
	waitFor(t, p, func(s Stats) bool { return s.Active == 1 }, time.Second)
	// Fill queue.
	if err := p.Submit(context.Background(), Lambda(func(context.Context) error {
		return nil
	})); err != nil {
		t.Fatalf("submit filler: %v", err)
	}
	waitFor(t, p, func(s Stats) bool { return s.Pending == 1 }, time.Second)

	// Wait for the first stall trigger (threshold=150ms, ticks every 50ms).
	waitFor(t, p, func(Stats) bool {
		mu.Lock()
		defer mu.Unlock()
		return count >= 1
	}, time.Second)

	// Release to drain the queue and reset stall state.
	close(release)
	waitFor(t, p, func(s Stats) bool { return s.Pending == 0 && s.Completed == 2 }, time.Second)

	// Allow the stallLoop to observe the recovery and reset.
	time.Sleep(200 * time.Millisecond)

	// Second stall episode: block again and fill the queue.
	release2 := make(chan struct{})
	if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
		<-release2
		return nil
	})); err != nil {
		t.Fatalf("submit blocker2: %v", err)
	}
	waitFor(t, p, func(s Stats) bool { return s.Active == 1 }, time.Second)
	if err := p.Submit(context.Background(), Lambda(func(context.Context) error {
		return nil
	})); err != nil {
		t.Fatalf("submit filler2: %v", err)
	}
	waitFor(t, p, func(s Stats) bool { return s.Pending == 1 }, time.Second)

	// Wait for the second stall trigger.
	waitFor(t, p, func(Stats) bool {
		mu.Lock()
		defer mu.Unlock()
		return count >= 2
	}, time.Second)

	close(release2)

	mu.Lock()
	defer mu.Unlock()
	if count != 2 {
		t.Fatalf("stall handler fired %d times, want 2", count)
	}
}

// TestConcurrentSubmit verifies that concurrent submitters and workers
// produce accurate statistics under the race detector.
func TestConcurrentSubmit(t *testing.T) {
	p, err := New(WithWorkers(runtime.NumCPU()), WithQueueSize(256))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const goroutines = 16
	const perG = 100
	total := int64(goroutines * perG)

	var wg sync.WaitGroup
	wg.Add(int(total))
	var done atomic.Int64
	var submitErr atomic.Value // stores error
	var subErrOnce sync.Once
	for range goroutines {
		go func() {
			for range perG {
				if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
					defer wg.Done()
					done.Add(1)
					return nil
				})); err != nil {
					wg.Done()
					subErrOnce.Do(func() { submitErr.Store(err) })
					return
				}
			}
		}()
	}
	wg.Wait()

	if v := submitErr.Load(); v != nil {
		t.Fatalf("concurrent submit: %v", v.(error))
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if got := done.Load(); got != total {
		t.Fatalf("done = %d, want %d", got, total)
	}
	if got := p.Stats().Submitted; got != total {
		t.Fatalf("Submitted = %d, want %d", got, total)
	}
	if got := p.Stats().Completed; got != total {
		t.Fatalf("Completed = %d, want %d", got, total)
	}
}

// TestNoGoroutineLeak verifies that all worker goroutines (and the
// stallLoop when configured) exit after Shutdown.
func TestNoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()
	p, err := New(
		WithWorkers(4), WithQueueSize(8),
		WithStallDetection(100*time.Millisecond, func(Stats) {}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(10)
	for i := range 10 {
		if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
			defer wg.Done()
			return nil
		})); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}
	wg.Wait()

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	// Give the runtime a moment to recycle exited goroutines.
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before+1 {
		t.Fatalf("goroutine leak: before=%d after=%d", before, after)
	}
}

// TestStatsSnapshot verifies that Stats reports Pending and Active
// consistently with the actual queue and worker state.
func TestStatsSnapshot(t *testing.T) {
	p, err := New(WithWorkers(2), WithQueueSize(4))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	}()

	s := p.Stats()
	if s.Submitted != 0 || s.Completed != 0 || s.Failed != 0 || s.Pending != 0 || s.Active != 0 {
		t.Fatalf("initial stats nonzero: %+v", s)
	}

	release := make(chan struct{})
	if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
		<-release
		return nil
	})); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Wait until the task becomes active, then ensure Stats returns
	// promptly and reports Active == 1.
	waitFor(t, p, func(s Stats) bool { return s.Active == 1 }, time.Second)

	statsDone := make(chan Stats, 1)
	go func() { statsDone <- p.Stats() }()
	select {
	case s := <-statsDone:
		if s.Active != 1 {
			t.Fatalf("Active = %d, want 1", s.Active)
		}
		if s.Submitted != 1 {
			t.Fatalf("Submitted = %d, want 1", s.Submitted)
		}
	case <-time.After(time.Second):
		t.Fatal("Stats blocked while worker busy")
	}

	close(release)

	// After the task finishes, Pending must return to zero so the
	// counter reflects the actual queue depth rather than growing
	// monotonically.
	waitFor(t, p, func(s Stats) bool { return s.Pending == 0 && s.Completed == 1 }, time.Second)
}
