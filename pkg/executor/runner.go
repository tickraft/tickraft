// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package executor

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/pool"
	"github.com/tickraft/tickraft/pkg/timewheel"
	"go.uber.org/zap"
)

// Runner is the execution engine that subscribes to execution trigger events,
// executes tasks through a bounded worker pool, and publishes completion
// events with retry support.
type Runner interface {
	// Start initializes the runner and marks it as ready to process events.
	Start(ctx context.Context) error
	// Stop gracefully stops the runner, waiting for in-flight workers to finish.
	Stop(ctx context.Context) error
	// SubscribeEvents subscribes to event.TypeExecutionTriggered on the event bus.
	// The provided context controls the lifetime of dispatched executions.
	SubscribeEvents(ctx context.Context)
}

// RecordStore persists execution records.
// Implementations must be safe for concurrent use.
type RecordStore interface {
	// Save persists an execution record.
	Save(record ExecutionRecord) error
}

// noopRecordStore is a no-op implementation used when no store is configured.
type noopRecordStore struct{}

func (noopRecordStore) Save(_ ExecutionRecord) error { return nil }

// runner is the default Runner implementation.
type runner struct {
	registry  *Registry
	bus       event.Bus
	logger    *zap.Logger
	records   RecordStore
	pool      pool.Pool
	poolOwned bool
	wheel     timewheel.Wheel
	wg        sync.WaitGroup

	// mu protects started, runCtx, cancel, and sub.
	mu      sync.RWMutex
	runCtx  context.Context
	cancel  context.CancelFunc
	started bool
	sub     event.Subscription
}

// Compile-time assertion that runner implements Runner.
var _ Runner = (*runner)(nil)

// Option configures a Runner.
type Option interface {
	apply(*runnerOptions)
}

// runnerOptions holds the resolved runner configuration.
type runnerOptions struct {
	registry       *Registry
	workerPoolSize int
	bus            event.Bus
	logger         *zap.Logger
	records        RecordStore
	pool           pool.Pool
	wheel          timewheel.Wheel
}

type funcOption func(*runnerOptions)

func (f funcOption) apply(o *runnerOptions) { f(o) }

// WithExecutorRegistry sets the executor registry.
func WithExecutorRegistry(registry *Registry) Option {
	return funcOption(func(o *runnerOptions) { o.registry = registry })
}

// WithWorkerPoolSize sets the max concurrent executions for the default
// worker pool. Default is 100. This option is ignored when [WithPool] is
// used to inject an explicit pool; in that case the injected pool's worker
// count governs concurrency.
func WithWorkerPoolSize(n int) Option {
	return funcOption(func(o *runnerOptions) { o.workerPoolSize = n })
}

// WithPool injects an explicit [pool.Pool] for task dispatch. The caller
// retains ownership of the pool and is responsible for shutting it down;
// the runner's Stop method will not close an injected pool. When this
// option is not set, the runner creates a default pool internally and
// closes it on Stop.
func WithPool(p pool.Pool) Option {
	return funcOption(func(o *runnerOptions) { o.pool = p })
}

// WithEventBus sets the event bus for subscribing to trigger events and
// publishing completion events.
func WithEventBus(bus event.Bus) Option {
	return funcOption(func(o *runnerOptions) { o.bus = bus })
}

// WithLogger sets the structured logger.
func WithLogger(logger *zap.Logger) Option {
	return funcOption(func(o *runnerOptions) { o.logger = logger })
}

// WithRecordStore sets the record store for persisting execution records.
// If not set, a no-op store is used.
func WithRecordStore(store RecordStore) Option {
	return funcOption(func(o *runnerOptions) { o.records = store })
}

// WithTimeWheel injects a [timewheel.Wheel] used to schedule asynchronous
// retry delays. When a wheel is provided, task execution uses
// [retry.Retry.DoAsync] so retry delays are scheduled as one-shot wheel
// callbacks instead of blocking the worker goroutine on a timer. When no
// wheel is injected, the runner falls back to the synchronous
// [retry.Retry.Do] method.
//
// The caller owns the wheel's lifecycle: the runner neither starts nor stops
// it. The caller must start the wheel before publishing trigger events and
// stop it after stopping the runner.
func WithTimeWheel(wheel timewheel.Wheel) Option {
	return funcOption(func(o *runnerOptions) { o.wheel = wheel })
}

// defaultWorkerPoolSize is the worker count used when no pool is injected
// and WithWorkerPoolSize is not set. It matches the previous semaphore
// capacity so the default concurrency budget is unchanged.
const defaultWorkerPoolSize = 100

// New creates a new Runner with the given options.
//
// When [WithPool] is not used, a default [pool.Pool] is created internally
// with [defaultWorkerPoolSize] workers (or the value from [WithWorkerPoolSize])
// and [pool.RejectionCallerRuns] so that, when the pool is saturated, the
// task runs inline on the dispatching goroutine — matching the previous
// worker-semaphore semantics. The default pool is closed by Stop; an
// injected pool is owned by the caller and left untouched.
//
// Returns an error if the internally-created worker pool cannot be
// initialized. This path is unreachable in practice because the worker
// count is sanitized to a positive value, but the error is returned
// rather than panicking to honor the "no panic in business logic" rule.
func New(opts ...Option) (Runner, error) {
	o := &runnerOptions{
		registry:       NewRegistry(),
		workerPoolSize: defaultWorkerPoolSize,
		logger:         zap.NewNop(),
		records:        noopRecordStore{},
	}
	for _, opt := range opts {
		opt.apply(o)
	}

	r := &runner{
		registry: o.registry,
		bus:      o.bus,
		logger:   o.logger,
		records:  o.records,
		wheel:    o.wheel,
	}

	if o.pool != nil {
		r.pool = o.pool
		r.poolOwned = false
	} else {
		workers := o.workerPoolSize
		if workers <= 0 {
			workers = runtime.NumCPU() * 2
		}
		p, err := pool.New(
			pool.WithWorkers(workers),
			pool.WithRejectionPolicy(pool.RejectionCallerRuns),
		)
		if err != nil {
			// Unreachable in practice: workers > 0 is guaranteed
			// above and the queue size defaults to a positive
			// constant. Returned as an error (not panic) to honor
			// the "no panic in business logic" rule.
			return nil, fmt.Errorf("executor: create default pool: %w", err)
		}
		r.pool = p
		r.poolOwned = true
	}

	return r, nil
}

// Start initializes the runner.
// It validates that an event bus is configured and derives a cancellable
// context so Stop can signal shutdown to in-flight work.
func (r *runner) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return nil
	}
	if r.bus == nil {
		return errdefs.ErrBusNotConfigured
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.runCtx = runCtx
	r.cancel = cancel
	r.started = true
	r.logger.Info("executor runner started")
	return nil
}

// Stop gracefully stops the runner.
// It cancels the internal context, waits for in-flight workers to finish
// (or until the provided context is cancelled), and — when the runner owns
// its default pool — shuts the pool down. Injected pools are left open;
// the caller is responsible for their lifecycle.
func (r *runner) Stop(ctx context.Context) error {
	r.mu.Lock()
	if !r.started {
		r.mu.Unlock()
		return nil
	}
	r.started = false
	if r.cancel != nil {
		r.cancel()
	}
	sub := r.sub
	r.sub = nil
	r.mu.Unlock()

	// Cancel the event subscription before waiting so no new dispatch
	// calls can add to the WaitGroup after it has been drained.
	if sub != nil {
		sub.Cancel()
	}

	done := make(chan struct{})
	// goroutine lifecycle: bounded — drains in-flight execution goroutines
	// spawned by dispatch; exits after r.wg.Wait() returns. In-flight doExecute
	// jobs observe the cancelled runCtx (cancelled above) and exit promptly,
	// so the wait is bounded in practice.
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return fmt.Errorf("executor: stop: workers did not finish: %w", ctx.Err())
	}

	// Shut down the default pool only. An injected pool is owned by the
	// caller and must not be closed here. By this point wg.Wait has
	// returned, so the pool has no in-flight runner jobs; Shutdown just
	// drains its worker goroutines.
	if r.poolOwned && r.pool != nil {
		if err := r.pool.Shutdown(ctx); err != nil {
			r.logger.Warn("default pool shutdown returned error",
				zap.Error(err),
			)
		}
	}

	r.logger.Info("executor runner stopped")
	return nil
}

// SubscribeEvents subscribes to event.TypeExecutionTriggered on the event bus.
// When a trigger event is received, the payload is dispatched to the worker
// pool for execution. If Start was called, the Start context is used for
// execution; otherwise the provided context is used.
func (r *runner) SubscribeEvents(ctx context.Context) {
	if r.bus == nil {
		return
	}
	r.mu.RLock()
	runCtx := r.runCtx
	r.mu.RUnlock()
	if runCtx == nil {
		runCtx = ctx
	}
	sub, err := event.Subscribe[event.ExecutionPayload](r.bus, event.TypeExecutionTriggered, func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
		r.dispatch(runCtx, ev)
		return nil
	})
	if err != nil {
		r.logger.Error("failed to subscribe to execution triggered events",
			zap.Error(err),
		)
		return
	}
	r.mu.Lock()
	r.sub = sub
	r.mu.Unlock()
}

// dispatch routes a trigger payload to the worker pool for execution.
//
// The task is submitted to the configured [pool.Pool]. The default pool
// uses [pool.RejectionCallerRuns], so when the pool is saturated the job
// runs synchronously in the caller's goroutine — matching the previous
// worker-semaphore semantics and providing backpressure without dropping
// work. The runner's run context (ctx) is captured inside the job so that
// Stop can cancel in-flight executions; the pool's own internal context
// is intentionally ignored.
//
// The WaitGroup add is released by doExecute (via the release callback)
// when the task — including any async retries — has fully completed. This
// allows Stop to wait for tasks that are in a retry delay scheduled on the
// time wheel.
func (r *runner) dispatch(ctx context.Context, ev event.Event[event.ExecutionPayload]) {
	payload := ev.Payload
	taskID, _ := strconv.ParseInt(payload.ExecutionID, 10, 64)
	tenantID, _ := strconv.ParseInt(payload.TenantID, 10, 64)
	assetID, _ := strconv.ParseInt(payload.AssetID, 10, 64)
	req := ExecutionRequest{
		ID:           taskID,
		TenantID:     tenantID,
		AssetID:      assetID,
		ExecutorName: payload.ExecutorType,
		Config:       payload.Config,
		Operation:    parseOperation(payload.Action),
		Timeout:      time.Duration(payload.Timeout),
		RunID:        payload.RunID,
		TriggerType:  payload.TriggerType,
		Metadata:     ev.Metadata,
	}

	// Guard against adding to a drained WaitGroup after Stop. The read
	// lock is held across wg.Add(1) so that Stop's write lock (which
	// sets started=false before calling wg.Wait) cannot interleave.
	r.mu.RLock()
	if !r.started {
		r.mu.RUnlock()
		r.logger.Debug("dispatch skipped: runner stopped",
			zap.Int64("task_id", req.ID),
		)
		return
	}
	r.wg.Add(1)
	r.mu.RUnlock()

	// Capture the dispatch context (the runner's runCtx) so the job
	// respects Stop cancellation. The pool's internal context, passed
	// to the Lambda, is intentionally ignored.
	execCtx := ctx
	// release is guarded by sync.Once so it is safe to call from both
	// onComplete and the panic recovery handler in doExecute without
	// risk of double-Done on the WaitGroup.
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(r.wg.Done) }
	job := pool.Lambda(func(_ context.Context) error {
		r.doExecute(execCtx, req, release)
		return nil
	})

	if err := r.pool.Submit(ctx, job); err != nil {
		// Submit failed: the pool is closed, the dispatch context was
		// cancelled, or (under non-default RejectionPolicies) the job
		// was discarded. Run inline so the task is not silently dropped,
		// preserving the previous "never drop" contract. doExecute will
		// call release when done.
		r.logger.Warn("pool submit failed, running task inline",
			zap.Int64("task_id", req.ID),
			zap.Error(err),
		)
		r.doExecute(execCtx, req, release)
	}
}
