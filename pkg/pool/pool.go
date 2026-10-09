// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package pool

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// ErrClosed is returned by [Pool.Submit] when the pool has been
// shut down or is shutting down.
var ErrClosed = errors.New("pool: closed")

// ErrQueueFull is returned by [Pool.Submit] under the
// [RejectionDiscardOldest] policy when no oldest job can be discarded
// to make room for the newly submitted one.
var ErrQueueFull = errors.New("pool: queue is full")

// Job is the unit of work executed by the pool.
//
// Implementations receive the pool's internal run context so that a
// forced [Pool.Shutdown] (one whose caller context expires) can
// propagate cancellation to in-flight jobs. A non-nil return value is
// treated as a logical failure and, when an [ErrorHandler] is
// configured, reported through it together with the original Job
// instance.
type Job interface {
	// Run executes the job. The provided ctx is the pool's internal
	// run context; a graceful Shutdown does not cancel it, while a
	// forced Shutdown (caller context expired) does.
	Run(ctx context.Context) error
}

// Lambda adapts a func(ctx) error into a [Job]. The zero value is
// not a valid Job; callers must pass a non-nil function.
type Lambda func(ctx context.Context) error

// Run implements [Job] by delegating to the underlying function.
func (f Lambda) Run(ctx context.Context) error { return f(ctx) }

// Pool is a bounded worker goroutine pool.
//
// A Pool is created with [New] and must be released with
// [Pool.Shutdown] to avoid goroutine leaks. All methods are safe for
// concurrent use by multiple goroutines.
type Pool interface {
	// Submit queues a job for execution by a worker.
	//
	// When the task queue is full and no additional worker can be
	// grown, Submit reacts according to the configured
	// [RejectionPolicy]. Under [RejectionAbort] it blocks until a
	// slot becomes available, ctx is cancelled, or the pool is shut
	// down. The provided ctx only governs the Submit call itself;
	// the job executes with the pool's internal context.
	Submit(ctx context.Context, job Job) error

	// Shutdown gracefully stops the pool.
	//
	// It stops accepting new submissions and waits for all queued
	// and in-flight jobs to finish before returning. If ctx is
	// cancelled before all workers have exited, the internal run
	// context is cancelled to force workers to stop and ctx.Err()
	// is returned; remaining queued jobs are abandoned in that
	// case. Repeated calls are idempotent and return nil.
	Shutdown(ctx context.Context) error

	// Stats returns an instantaneous snapshot of pool counters.
	//
	// The snapshot is not transactionally consistent and is intended
	// for observability only.
	Stats() Stats
}

// pool is the canonical [Pool] implementation.
type pool struct {
	config

	jobCh     chan Job
	closedCh  chan struct{}
	done      chan struct{}
	stallDone chan struct{}

	wg sync.WaitGroup

	stats          stats
	closed         atomic.Bool
	currentWorkers atomic.Int64

	runCtx    context.Context
	cancelRun context.CancelFunc
}

// New creates and starts a [Pool] configured by the provided options.
//
// Defaults: workers = [runtime.NumCPU], queueSize = 1024,
// rejectionPolicy = [RejectionAbort]. The pool pre-starts
// min(workers, 4) workers eagerly and grows the rest lazily as the
// queue fills. It returns an error if the configured worker count or
// queue size is not positive.
func New(options ...Option) (Pool, error) {
	cfg := config{
		workers:         runtime.NumCPU(),
		queueSize:       defaultQueueSize,
		rejectionPolicy: RejectionAbort,
	}
	for _, o := range options {
		o.apply(&cfg)
	}
	if cfg.workers <= 0 {
		return nil, fmt.Errorf("pool: workers must be > 0, got %d", cfg.workers)
	}
	if cfg.queueSize <= 0 {
		return nil, fmt.Errorf("pool: queue size must be > 0, got %d", cfg.queueSize)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	p := &pool{
		config:    cfg,
		jobCh:     make(chan Job, cfg.queueSize),
		closedCh:  make(chan struct{}),
		done:      make(chan struct{}),
		runCtx:    runCtx,
		cancelRun: cancel,
	}

	// Eager warmup: start a small number of workers so the first
	// submitted jobs run without waiting for lazy growth. The
	// remainder are grown on demand as the queue fills.
	warmup := defaultWarmup
	warmup = min(warmup, cfg.workers)
	p.currentWorkers.Store(int64(warmup))
	p.wg.Add(warmup)
	for range warmup {
		// goroutine lifecycle: pool-owned — worker selects on ctx.Done
		// and p.jobCh; tracked by p.wg so Shutdown can wait for all
		// workers to drain and exit before returning.
		go p.worker(runCtx)
	}

	// Stall detection runs only when explicitly configured. The
	// goroutine listens on runCtx for shutdown and signals
	// stallDone so [Pool.Shutdown] can wait for it deterministically.
	if cfg.stallThreshold > 0 {
		p.stallDone = make(chan struct{})
		// goroutine lifecycle: bound to runCtx (cancelled by Shutdown via
		// p.cancelRun); closes p.stallDone on exit so Shutdown can wait for
		// it deterministically via waitStallDone.
		go p.stallLoop(runCtx)
	}

	return p, nil
}

// worker is the consumer goroutine loop.
//
// It exits when the pool's run context is cancelled (forced shutdown)
// or when the task channel is closed and drained (graceful shutdown).
// On exit it releases its slot in currentWorkers so lazy growth can
// reuse the cap if the pool is later stressed again (it cannot, since
// Shutdown is terminal, but the bookkeeping stays consistent).
func (p *pool) worker(ctx context.Context) {
	defer p.wg.Done()
	defer p.currentWorkers.Add(-1)
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-p.jobCh:
			if !ok {
				return
			}
			// The job has left the queue; track it before
			// execution so Pending reflects the queue depth.
			p.stats.decPending()
			p.execute(ctx, job)
		}
	}
}

// tryGrowWorker attempts to start one additional worker goroutine if
// the current count is below the configured cap. The compare-and-swap
// loop handles concurrent growers safely: only the goroutine that
// successfully CAS-increments currentWorkers starts the worker.
func (p *pool) tryGrowWorker() {
	for {
		cur := p.currentWorkers.Load()
		if cur >= int64(p.workers) {
			return
		}
		if p.currentWorkers.CompareAndSwap(cur, cur+1) {
			p.wg.Add(1)
			// goroutine lifecycle: pool-owned — same lifecycle as warmup
			// workers; tracked by p.wg so Shutdown can wait for all
			// workers (warmup + lazy-grown) to exit before returning.
			go p.worker(p.runCtx)
			return
		}
	}
}

// execute runs a single job, tracking panics and errors and
// updating statistics exactly once.
//
// The panic recovery runs in an inner closure so that the outer
// tracking can distinguish a panic (reported via panicHandler) from
// a returned error (reported via errorHandler). A job that panics is
// counted as failed and never additionally reported to the error
// handler.
func (p *pool) execute(ctx context.Context, job Job) {
	p.stats.incActive()
	defer p.stats.decActive()

	var panicked bool
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				if p.panicHandler != nil {
					p.panicHandler(job, r)
				}
			}
		}()
		err = job.Run(ctx)
	}()

	switch {
	case panicked:
		p.stats.incFailed()
	case err != nil:
		if p.errorHandler != nil {
			p.errorHandler(job, err)
		}
		p.stats.incFailed()
	default:
		p.stats.incCompleted()
	}
}

// Submit implements [Pool.Submit].
//
// The enqueue path is structured as: fast closed-check, non-blocking
// enqueue, lazy worker growth + retry, and finally the configured
// rejection policy. Each blocking select watches both the caller
// context and the closedCh so that cancellation and shutdown are
// observed promptly.
func (p *pool) Submit(ctx context.Context, job Job) error {
	// Fast-path closed check so that a shut-down pool returns
	// immediately without entering the enqueue select.
	select {
	case <-p.closedCh:
		return ErrClosed
	default:
	}

	// First non-blocking enqueue attempt: succeeds immediately when
	// the queue has capacity.
	select {
	case p.jobCh <- job:
		p.stats.incSubmitted()
		p.stats.incPending()
		return nil
	case <-p.closedCh:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Queue full: try lazy worker growth, then retry once. The full
	// queue itself is the backlog signal that justifies growing.
	if p.currentWorkers.Load() < int64(p.workers) {
		p.tryGrowWorker()
		select {
		case p.jobCh <- job:
			p.stats.incSubmitted()
			p.stats.incPending()
			return nil
		case <-p.closedCh:
			return ErrClosed
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	// Still full: defer to the configured rejection policy.
	return p.handleRejection(ctx, job)
}

// handleRejection applies the configured [RejectionPolicy] when the
// queue is saturated and no more workers can be grown.
func (p *pool) handleRejection(ctx context.Context, job Job) error {
	switch p.rejectionPolicy {
	case RejectionCallerRuns:
		// Execute the job synchronously in the caller's goroutine
		// instead of enqueueing it. Submitted is counted so
		// throughput stats include caller-run jobs.
		p.stats.incSubmitted()
		p.execute(p.runCtx, job)
		return nil
	case RejectionDiscardOldest:
		// Drop the oldest queued job to make room. If no job can
		// be discarded (the queue was drained concurrently),
		// return ErrQueueFull instead of recursing.
		select {
		case <-p.jobCh:
			p.stats.decPending()
			p.stats.incDiscarded()
		default:
			return ErrQueueFull
		}
		// Enqueue the new job now that a slot has been freed.
		select {
		case p.jobCh <- job:
			p.stats.incSubmitted()
			p.stats.incPending()
			return nil
		case <-p.closedCh:
			return ErrClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	default:
		// Abort semantics, also used as the fallback for unknown policies.
		// Block until a slot is available, ctx is cancelled, or
		// the pool is shut down. Passing a pre-cancelled ctx
		// yields immediate rejection.
		select {
		case p.jobCh <- job:
			p.stats.incSubmitted()
			p.stats.incPending()
			return nil
		case <-p.closedCh:
			return ErrClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Shutdown implements [Pool.Shutdown].
//
// Both the graceful and timeout branches cancel the internal run
// context so the stallLoop goroutine (if any) observes ctx.Done and
// exits; we then wait on stallDone to guarantee no goroutine leak
// before returning.
func (p *pool) Shutdown(ctx context.Context) error {
	// Idempotent: only the first caller flips the flag and tears down.
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Stop accepting new submissions, then close the task channel so
	// workers drain the remaining queued jobs and exit.
	close(p.closedCh)
	close(p.jobCh)

	// goroutine lifecycle: bounded — waits for p.wg to drain (all workers)
	// after the task channel is closed; exits after close(p.done) so
	// Shutdown can select on p.done.
	go func() {
		p.wg.Wait()
		close(p.done)
	}()

	select {
	case <-p.done:
		// Graceful: all workers have exited. Cancel runCtx so the
		// stallLoop (if any) unblocks and close stallDone.
		p.cancelRun()
		p.waitStallDone()
		return nil
	case <-ctx.Done():
		// Forced: cancel runCtx to interrupt in-flight jobs and
		// release workers, then wait for them and the stallLoop
		// to finish before propagating the ctx error.
		p.cancelRun()
		<-p.done
		p.waitStallDone()
		return ctx.Err()
	}
}

// waitStallDone blocks until the stallLoop goroutine has exited. It is
// a no-op when stall detection is disabled (stallDone is nil).
func (p *pool) waitStallDone() {
	if p.stallDone != nil {
		<-p.stallDone
	}
}

// stallLoop polls the queue at threshold/4 (minimum 50ms) and invokes
// stallHandler once the queue has been continuously full for at least
// stallThreshold. The handler fires at most once per stall episode;
// recovery (a slot freeing) resets the state so the next stall fires
// again. The loop exits when runCtx is cancelled and closes stallDone
// via defer so [Pool.Shutdown] can wait deterministically.
func (p *pool) stallLoop(ctx context.Context) {
	defer close(p.stallDone)

	interval := p.stallThreshold / 4
	interval = max(interval, 50*time.Millisecond)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var stalling bool
	var fullSince time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// A bounded channel is "full" when len == cap. This
			// is racy with enqueue/dequeue but is the right
			// signal for stall detection: sustained saturation.
			full := len(p.jobCh) == cap(p.jobCh)
			if full {
				if fullSince.IsZero() {
					fullSince = time.Now()
				}
				if !stalling && time.Since(fullSince) >= p.stallThreshold {
					stalling = true
					if p.stallHandler != nil {
						p.stallHandler(p.Stats())
					}
				}
			} else {
				fullSince = time.Time{}
				stalling = false
			}
		}
	}
}

// Stats implements [Pool.Stats].
func (p *pool) Stats() Stats {
	s := p.stats.snapshot()
	s.Workers = p.currentWorkers.Load()
	return s
}
