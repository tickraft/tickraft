// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tickraft/tickraft/pkg/cron"
	"github.com/tickraft/tickraft/pkg/timewheel"
	"go.uber.org/zap"
)

// defaultWheelWorkerSize is the worker pool size used by the internal time
// wheel for dispatching fire callbacks. Since fire callbacks only reschedule
// and invoke the user-provided Callback, a modest pool is sufficient.
const defaultWheelWorkerSize = 100

// engine is the default Engine implementation. It uses a hierarchical time
// wheel for timing and exposes only Add/Remove/Start/Stop.
//
// The engine holds no business state: it maps entry IDs to Schedules and
// time-wheel EntryIDs, and reschedules automatically after each fire. All
// business semantics (task lifecycle, dependency tracking, event-driven
// triggers) are owned by the caller (e.g., task.Service).
type engine struct {
	wheel  timewheel.Wheel
	logger *zap.Logger

	// mu protects entries, scheds, callbacks, and started.
	mu        sync.RWMutex
	entries   map[int64]timewheel.EntryID // id -> time wheel entry ID
	scheds    map[int64]cron.Schedule     // id -> parsed schedule
	callbacks map[int64]Callback          // id -> user callback

	// cancel stops the time wheel tick loop.
	cancel context.CancelFunc

	// started indicates whether the engine has been started.
	started bool

	// wg tracks the goroutine launched by Start so that Stop can wait
	// for it to fully exit before returning.
	wg sync.WaitGroup
}

// Compile-time assertion that engine implements Engine.
var _ Engine = (*engine)(nil)

// EngineOption configures an Engine.
type EngineOption interface {
	apply(*engineOptions)
}

// engineOptions holds the resolved Engine configuration.
type engineOptions struct {
	Logger *zap.Logger
}

type funcEngineOption func(*engineOptions)

func (f funcEngineOption) apply(o *engineOptions) { f(o) }

// WithEngineLogger sets the structured logger for the Engine.
// The default logger is a no-op logger.
func WithEngineLogger(l *zap.Logger) EngineOption {
	return funcEngineOption(func(o *engineOptions) { o.Logger = l })
}

// NewEngine creates a new Engine with the given options. The time wheel is
// initialized but NOT started; the caller must call Start before Add can
// fire callbacks.
//
// Returns an error if the internal time wheel cannot be initialized (see
// timewheel.New for details). The error path is unreachable in practice
// but is returned rather than panicking to honor the "no panic in business
// logic" rule.
func NewEngine(opts ...EngineOption) (Engine, error) {
	o := &engineOptions{
		Logger: zap.NewNop(),
	}
	for _, opt := range opts {
		opt.apply(o)
	}

	wheel, err := timewheel.NewWheel(defaultWheelWorkerSize)
	if err != nil {
		return nil, fmt.Errorf("scheduler: create time wheel: %w", err)
	}

	return &engine{
		wheel:     wheel,
		logger:    o.Logger,
		entries:   make(map[int64]timewheel.EntryID),
		scheds:    make(map[int64]cron.Schedule),
		callbacks: make(map[int64]Callback),
	}, nil
}

// Add registers a timed callback under the given id. If an entry with the
// same id already exists, the old time-wheel entry is removed first. The
// engine computes the next fire time from schedule and adds a time-wheel
// entry; when it fires, the callback is invoked and the entry is
// automatically rescheduled for recurring schedules.
func (e *engine) Add(id int64, schedule Schedule, callback Callback) error {
	if schedule == nil {
		return fmt.Errorf("scheduler: schedule is nil for id %d", id)
	}

	e.mu.Lock()
	// Remove existing entry if present.
	if oldEntryID, ok := e.entries[id]; ok {
		e.wheel.Remove(oldEntryID)
	}
	e.scheds[id] = schedule
	e.callbacks[id] = callback
	e.scheduleNextLocked(id, schedule)
	e.mu.Unlock()

	return nil
}

// Remove removes the timed callback associated with id. No-op if the id is
// not registered.
func (e *engine) Remove(id int64) error {
	e.mu.Lock()
	entryID, ok := e.entries[id]
	if ok {
		delete(e.entries, id)
	}
	delete(e.scheds, id)
	delete(e.callbacks, id)
	e.mu.Unlock()

	if ok {
		e.wheel.Remove(entryID)
	}

	return nil
}

// Start begins the engine's tick loop. Start is idempotent: calling it on
// an already-running engine is a no-op. The context controls the lifetime
// of the internal tick loop; cancelling it stops the wheel.
func (e *engine) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.started {
		e.mu.Unlock()
		return nil
	}
	startCtx, cancel := context.WithCancel(ctx)
	e.cancel = cancel
	e.started = true
	e.wg.Add(1)
	e.mu.Unlock()

	// goroutine lifecycle: bound to startCtx (cancelled by engine.Stop via
	// e.cancel); tracked by e.wg so Stop can wait for the wheel tick loop
	// to fully exit before returning.
	go func() {
		defer e.wg.Done()
		e.wheel.Start(startCtx)
	}()

	e.logger.Info("scheduler engine started")
	return nil
}

// Stop gracefully stops the engine, cancelling the tick loop and stopping
// the time wheel. Stop is idempotent. It waits for the internal tick-loop
// goroutine to fully exit before returning.
func (e *engine) Stop(ctx context.Context) error {
	e.mu.Lock()
	if !e.started {
		e.mu.Unlock()
		return nil
	}
	e.started = false
	if e.cancel != nil {
		e.cancel()
	}
	e.mu.Unlock()

	if err := e.wheel.Stop(ctx); err != nil {
		return fmt.Errorf("scheduler: stop time wheel: %w", err)
	}

	// Wait for the tick-loop goroutine to fully exit so callers can be
	// certain no callback will fire after Stop returns.
	e.wg.Wait()

	e.logger.Info("scheduler engine stopped")
	return nil
}

// scheduleNextLocked computes the next fire time from the schedule and adds
// a time wheel entry for the id. The caller MUST hold e.mu.
func (e *engine) scheduleNextLocked(id int64, sched cron.Schedule) {
	next := sched.Next(time.Now())
	if next.IsZero() {
		// One-time schedule that has already fired, or never-schedule.
		e.logger.Debug("schedule has no next fire time",
			zap.Int64("id", id),
		)
		return
	}

	delay := max(time.Until(next), 0)

	cb := e.callbacks[id]
	entryID := e.wheel.Add(delay, func(_ timewheel.EntryID) {
		e.onFire(id, cb)
	})
	e.entries[id] = entryID

	e.logger.Debug("scheduled next execution",
		zap.Int64("id", id),
		zap.Time("next", next),
		zap.Duration("delay", delay),
	)
}

// onFire is the internal callback invoked when a time wheel entry expires.
// It invokes the user-provided callback and reschedules the entry.
//
// Panic isolation: a panic in the user-provided callback is recovered and
// logged with zap structured logging. The entry is always rescheduled
// regardless of whether the callback panicked, ensuring recurring
// schedules continue firing even if a single invocation fails.
func (e *engine) onFire(id int64, cb Callback) {
	// Reschedule first (in a defer) so that a panic in the user callback
	// does not prevent the next firing of recurring schedules. This defer
	// runs after the panic-recovery defer below (LIFO order), so by the
	// time it runs the panic has already been recovered.
	defer func() {
		e.mu.Lock()
		sched := e.scheds[id]
		if sched != nil {
			e.scheduleNextLocked(id, sched)
		}
		e.mu.Unlock()
	}()

	// Recover from user-callback panics to protect the time-wheel worker.
	// The panic is logged at error level with the entry id and stack trace.
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("scheduler: callback panic recovered",
				zap.Int64("id", id),
				zap.Any("panic", r),
				zap.Stack("stack"),
			)
		}
	}()

	if cb != nil {
		cb(id)
	}
}
