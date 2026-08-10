// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package timewheel

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/pool"
)

const (
	wheelSize     = 60
	secondsPerMin = 60

	// submitTimeout caps how long a callback dispatch waits for the
	// pool to accept the job. It keeps a saturated pool from blocking
	// the single-second tick loop; a rejected callback is logged and
	// dropped instead of stalling the wheel.
	submitTimeout = 100 * time.Millisecond
)

// entryLocation tracks where an entry is stored in the wheel for O(1) removal.
type entryLocation struct {
	layer int // 0 = seconds wheel, 1 = minutes wheel
	slot  int // slot index within the layer
}

// entryPool recycles [Entry] values to reduce GC pressure under high
// callback throughput. Entries are acquired in [hierarchicalWheel.AddAt]
// and returned to the pool when they expire (in [hierarchicalWheel.tick])
// or are explicitly removed (in [hierarchicalWheel.remove]).
//
// All mutable fields are cleared by [releaseEntry] before an entry is
// returned so that closures captured by Callback and references held by
// Metadata become unreachable as soon as the entry leaves the wheel.
var entryPool = sync.Pool{
	New: func() any { return &Entry{} },
}

// acquireEntry returns an [Entry] drawn from the pool, zeroed and ready
// for the caller to populate.
func acquireEntry() *Entry {
	return entryPool.Get().(*Entry) //nolint:errcheck // pool always yields *Entry
}

// releaseEntry clears all fields of e and returns it to the pool. It is
// safe to call with a nil pointer; the call is a no-op in that case.
func releaseEntry(e *Entry) {
	if e == nil {
		return
	}
	e.ID = 0
	e.Callback = nil
	e.ExpireAt = time.Time{}
	e.Metadata = nil
	entryPool.Put(e)
}

// hierarchicalWheel implements Wheel with a two-layer design:
// a seconds wheel (60 slots) and a minutes wheel (60 slots),
// driven by a single goroutine ticking every second.
type hierarchicalWheel struct {
	mu sync.Mutex

	seconds [wheelSize]map[EntryID]*Entry
	minutes [wheelSize]map[EntryID]*Entry
	secPtr  int // current position in the seconds wheel (0-59)
	minPtr  int // current position in the minutes wheel (0-59)
	index   map[EntryID]entryLocation

	// pool dispatches expired callbacks. When ownsPool is true the
	// wheel created the pool and shuts it down on Stop.
	pool     pool.Pool
	ownsPool bool

	// logger reports rejected dispatches; never nil.
	logger *zap.Logger

	// runCtx is the wheel's lifetime context. It is created in the
	// constructor and cancelled on Stop, and is used as the parent of
	// each per-submit timeout context so that shutting the wheel down
	// unblocks any in-flight Submit.
	runCtx    context.Context
	cancelRun context.CancelFunc

	// tickCancel cancels the tick loop started by Start.
	tickCancel context.CancelFunc
	done       chan struct{}
	started    atomic.Bool
}

// newHierarchicalWheel creates a hierarchical wheel from the resolved
// config. When cfg.pool is nil a default [pool.Pool] is created with
// cfg.workerSize workers (fallback [defaultWorkerSize]) and a queue
// sized at defaultQueueMultiplier × workers; the wheel owns and shuts
// down that default pool. An injected pool is never closed by the
// wheel.
//
// Returns an error if the internally-created default pool cannot be
// initialized. This path is unreachable in practice because the worker
// count is sanitized to a positive value, but the error is returned
// rather than panicking to honor the "no panic in business logic"
// rule.
func newHierarchicalWheel(cfg config) (*hierarchicalWheel, error) {
	if cfg.workerSize <= 0 {
		cfg.workerSize = defaultWorkerSize
	}
	logger := cfg.logger
	if logger == nil {
		logger = zap.NewNop()
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	w := &hierarchicalWheel{
		index:     make(map[EntryID]entryLocation),
		logger:    logger,
		runCtx:    runCtx,
		cancelRun: cancelRun,
	}
	for i := 0; i < wheelSize; i++ {
		w.seconds[i] = make(map[EntryID]*Entry)
		w.minutes[i] = make(map[EntryID]*Entry)
	}

	if cfg.pool != nil {
		w.pool = cfg.pool
		w.ownsPool = false
	} else {
		workers := cfg.workerSize
		queueSize := workers * defaultQueueMultiplier
		p, err := pool.New(
			pool.WithWorkers(workers),
			pool.WithQueueSize(queueSize),
		)
		if err != nil {
			// Unreachable in practice: workers and queueSize are
			// both strictly positive (workers is sanitized above,
			// queueSize = workers * defaultQueueMultiplier).
			// Returned as an error (not panic) to honor the
			// "no panic in business logic" rule. cancelRun
			// releases the context created above to avoid a leak.
			cancelRun()
			return nil, fmt.Errorf("timewheel: create default pool: %w", err)
		}
		w.pool = p
		w.ownsPool = true
	}

	return w, nil
}

// Add registers a callback to fire after the given duration.
func (w *hierarchicalWheel) Add(duration time.Duration, cb Callback) EntryID {
	return w.AddAt(time.Now().Add(duration), cb)
}

// AddAt registers a callback to fire at the specified absolute time.
func (w *hierarchicalWheel) AddAt(fireAt time.Time, cb Callback) EntryID {
	id := newEntryID()
	now := time.Now()

	// If already expired, dispatch immediately through the pool so the
	// caller's goroutine never spawns an unbounded callback goroutine.
	if !fireAt.After(now) {
		w.submitCallback(id, cb)
		return id
	}

	entry := acquireEntry()
	entry.ID = id
	entry.Callback = cb
	entry.ExpireAt = fireAt

	w.mu.Lock()
	defer w.mu.Unlock()

	delay := fireAt.Sub(now)
	totalSeconds := int(delay.Seconds())
	if totalSeconds < 1 {
		totalSeconds = 1
	}

	if totalSeconds < secondsPerMin {
		slot := (w.secPtr + totalSeconds) % wheelSize
		w.seconds[slot][id] = entry
		w.index[id] = entryLocation{layer: 0, slot: slot}
	} else {
		minuteOffset := totalSeconds / secondsPerMin
		slot := (w.minPtr + minuteOffset) % wheelSize
		w.minutes[slot][id] = entry
		w.index[id] = entryLocation{layer: 1, slot: slot}
	}

	return id
}

// Remove removes an entry by ID. No-op if not found.
func (w *hierarchicalWheel) Remove(id EntryID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.remove(id)
}

// remove removes an entry by ID without acquiring the lock and returns
// it to the entry pool. Caller must hold w.mu.
func (w *hierarchicalWheel) remove(id EntryID) {
	loc, ok := w.index[id]
	if !ok {
		return
	}
	var entry *Entry
	switch loc.layer {
	case 0:
		entry = w.seconds[loc.slot][id]
		delete(w.seconds[loc.slot], id)
	case 1:
		entry = w.minutes[loc.slot][id]
		delete(w.minutes[loc.slot], id)
	}
	delete(w.index, id)
	releaseEntry(entry)
}

// Renew resets the expiration timer for an entry.
// It removes the old entry and re-adds it with the new duration.
// Returns the new entry ID.
func (w *hierarchicalWheel) Renew(id EntryID, duration time.Duration) EntryID {
	w.mu.Lock()
	loc, ok := w.index[id]
	if !ok {
		w.mu.Unlock()
		// Entry not found; create a new entry with an empty callback.
		return w.Add(duration, func(EntryID) {})
	}

	var cb Callback
	switch loc.layer {
	case 0:
		if e, exists := w.seconds[loc.slot][id]; exists {
			cb = e.Callback
		}
	case 1:
		if e, exists := w.minutes[loc.slot][id]; exists {
			cb = e.Callback
		}
	}

	w.remove(id)
	w.mu.Unlock()

	if cb == nil {
		cb = func(EntryID) {}
	}
	return w.Add(duration, cb)
}

// Start begins the time wheel tick loop.
// It blocks until the context is cancelled or Stop is called.
func (w *hierarchicalWheel) Start(ctx context.Context) {
	d := make(chan struct{})

	w.mu.Lock()
	if w.started.Load() {
		w.mu.Unlock()
		return
	}
	ctx, w.tickCancel = context.WithCancel(ctx)
	w.done = d
	w.started.Store(true)
	w.mu.Unlock()

	w.tickLoop(ctx)
}

// tickLoop drives the wheel by ticking every second.
func (w *hierarchicalWheel) tickLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	defer close(w.done)

	for {
		select {
		case <-ticker.C:
			w.tick()
		case <-ctx.Done():
			return
		}
	}
}

// tick advances the wheel by one second, cascading from the minutes
// wheel when the seconds wheel completes a full rotation, and
// dispatching expired entries from the current seconds slot.
func (w *hierarchicalWheel) tick() {
	w.mu.Lock()

	// Advance the seconds pointer.
	w.secPtr = (w.secPtr + 1) % wheelSize

	// When the seconds wheel completes a full rotation, advance the
	// minutes pointer and cascade entries into the seconds wheel.
	if w.secPtr == 0 {
		w.minPtr = (w.minPtr + 1) % wheelSize
		w.cascade()
	}

	// Collect entries from the current seconds slot.
	var expired []*Entry
	for id, entry := range w.seconds[w.secPtr] {
		expired = append(expired, entry)
		delete(w.index, id)
	}
	// Clear the map in place to reuse its backing storage instead of
	// allocating a new map on every tick.
	for k := range w.seconds[w.secPtr] {
		delete(w.seconds[w.secPtr], k)
	}

	w.mu.Unlock()

	// Dispatch expired entries outside the lock through the pool. Each
	// entry is returned to the pool after its callback has been submitted
	// so the struct can be reused by a future Add/AddAt call.
	for _, entry := range expired {
		w.submitCallback(entry.ID, entry.Callback)
		releaseEntry(entry)
	}
}

// cascade moves entries from the current minute slot into the seconds
// wheel. Entries that are still beyond 60 seconds are reinserted into
// the minutes wheel at the appropriate slot.
// Caller must hold w.mu.
func (w *hierarchicalWheel) cascade() {
	// Collect entries from the current minute slot, then clear the
	// map in place to reuse its backing storage. Entries are gathered
	// into a slice first so the map can be safely cleared and reused
	// for reinsertion (including into the same slot).
	src := w.minutes[w.minPtr]
	entries := make([]*Entry, 0, len(src))
	for _, entry := range src {
		entries = append(entries, entry)
	}
	for k := range w.minutes[w.minPtr] {
		delete(w.minutes[w.minPtr], k)
	}

	for _, entry := range entries {
		delete(w.index, entry.ID)
		id := entry.ID

		remainingSeconds := int(time.Until(entry.ExpireAt).Seconds())
		if remainingSeconds < 1 {
			// Already expired; place in current seconds slot for immediate dispatch.
			w.seconds[w.secPtr][id] = entry
			w.index[id] = entryLocation{layer: 0, slot: w.secPtr}
		} else if remainingSeconds < secondsPerMin {
			// Within the next minute; place in the seconds wheel.
			slot := (w.secPtr + remainingSeconds) % wheelSize
			w.seconds[slot][id] = entry
			w.index[id] = entryLocation{layer: 0, slot: slot}
		} else {
			// Still beyond 60 seconds; reinsert into the minutes wheel.
			minuteOffset := remainingSeconds / secondsPerMin
			slot := (w.minPtr + minuteOffset) % wheelSize
			w.minutes[slot][id] = entry
			w.index[id] = entryLocation{layer: 1, slot: slot}
		}
	}
}

// submitCallback dispatches a callback through the configured pool.
//
// The submit is bounded by [submitTimeout] so that a saturated pool
// cannot block the tick loop: when the pool is full, rejecting, or
// shutting down, the callback is dropped and a warning is logged with
// the entry ID. The callback receives the pool's internal run context
// (not the wheel's tick context) so that a graceful pool Shutdown
// propagates cancellation to in-flight callbacks.
//
// A panicking callback is recovered here so that a buggy timer handler
// can never crash the pool worker or the wheel. The recovered value is
// logged at error level with the entry ID for diagnosis. This recovery
// is independent of any panic handling the injected [pool.Pool] may
// also perform; the inner recover always fires first.
func (w *hierarchicalWheel) submitCallback(id EntryID, cb Callback) {
	ctx, cancel := context.WithTimeout(w.runCtx, submitTimeout)
	defer cancel()
	if err := w.pool.Submit(ctx, pool.Lambda(func(ctx context.Context) error {
		defer func() {
			if r := recover(); r != nil {
				w.logger.Error("timewheel: callback panic recovered",
					zap.Int64("entry_id", int64(id)),
					zap.Any("panic", r),
				)
			}
		}()
		cb(id)
		return nil
	})); err != nil {
		w.logger.Warn("timewheel: callback dispatch rejected",
			zap.Int64("entry_id", int64(id)),
			zap.Error(err),
		)
	}
}

// Stop gracefully stops the time wheel.
//
// It cancels the internal tick loop, waits for the tick goroutine to
// exit, and — when the wheel owns its pool — shuts the pool down so
// queued and in-flight callbacks drain before returning. Injected
// pools are left open; their lifecycle is the caller's responsibility.
// Calling Stop on a wheel that was never started still releases the
// default pool to avoid goroutine leaks.
func (w *hierarchicalWheel) Stop(ctx context.Context) error {
	w.mu.Lock()
	wasStarted := w.started.CompareAndSwap(true, false)
	tickCancel := w.tickCancel
	done := w.done
	w.mu.Unlock()

	// Stop future ticks and unblock any in-flight dispatch submits so
	// the tick goroutine can exit promptly. runCtx is cancelled even
	// when the wheel was never started so the owned pool can drain.
	if wasStarted && tickCancel != nil {
		tickCancel()
	}
	w.cancelRun()

	if wasStarted {
		select {
		case <-done:
		case <-ctx.Done():
			// Even on caller timeout we still drain the owned pool
			// below to avoid goroutine leaks; the error is propagated
			// at the end.
		}
	}

	// When the wheel created the default pool, drain it so queued and
	// in-flight callbacks finish. An injected pool is not closed here.
	// Pool.Shutdown is idempotent, so repeated Stop calls are safe.
	if w.ownsPool {
		if err := w.pool.Shutdown(ctx); err != nil {
			return err
		}
	}

	if !wasStarted {
		return nil
	}
	select {
	case <-done:
		return nil
	default:
		return ctx.Err()
	}
}
