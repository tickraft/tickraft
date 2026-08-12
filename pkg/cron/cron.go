// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cron

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Lambda is the callback for a cron task.
type Lambda func(context.Context)

// Run invokes the underlying callback, satisfying the Job interface.
func (l Lambda) Run(ctx context.Context) {
	l(ctx)
}

// Job represents a schedulable unit of work invoked by the cron scheduler
// at each scheduled tick.
type Job interface {
	Run(context.Context)
}

// Entry stores a registered schedule and its callback.
type Entry struct {
	ID       int64
	Schedule Schedule
	Job      Job
	next     time.Time
	index    int
}

// entryHeap implements container/heap.Interface for *Entry, ordered by next fire time.
type entryHeap []*Entry

func (h entryHeap) Len() int           { return len(h) }
func (h entryHeap) Less(i, j int) bool { return h[i].next.Before(h[j].next) }
func (h entryHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index, h[j].index = i, j
}
func (h *entryHeap) Push(x any) {
	n := len(*h)
	item := x.(*Entry) //nolint:errcheck // type guaranteed by construction: heap only stores *Entry
	item.index = n
	*h = append(*h, item)
}
func (h *entryHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	*h = old[:n-1]
	return item
}

// Option configures a Crontab.
type Option interface {
	apply(*Crontab)
}

type workerSizeOption int

func (o workerSizeOption) apply(c *Crontab) {
	c.workerSize = int(o)
}

// WithWorkerSize sets the number of worker goroutines.
func WithWorkerSize(n int) Option {
	return workerSizeOption(n)
}

type contextOption struct {
	ctx context.Context
}

func (o contextOption) apply(c *Crontab) {
	if o.ctx != nil {
		c.ctx = o.ctx
	}
}

// WithContext sets the parent context for the Crontab.
func WithContext(ctx context.Context) Option {
	return contextOption{ctx: ctx}
}

type loggerOption struct {
	logger *zap.Logger
}

func (o loggerOption) apply(c *Crontab) {
	if o.logger != nil {
		c.logger = o.logger
	}
}

// WithLogger sets the structured logger used for panic recovery and lifecycle
// diagnostics. Defaults to a no-op logger when not provided.
func WithLogger(logger *zap.Logger) Option {
	return loggerOption{logger: logger}
}

// Crontab coordinates scheduled entries with a min-heap, worker pool, and reused timer.
type Crontab struct {
	ctx        context.Context
	entries    map[int64]*Entry // O(1) lookup by ID
	heap       entryHeap        // min-heap by next fire time
	timer      *time.Timer      // reused timer
	done       chan struct{}    // stop signal
	wake       chan struct{}    // signal scheduleLoop to re-check timer
	running    bool
	workerSize int
	jobs       chan Job       // job dispatch channel
	wg         sync.WaitGroup // track in-flight workers
	logger     *zap.Logger    // structured logger for panic recovery and diagnostics
	mu         sync.Mutex
}

// New creates an empty cron tab with optional configuration.
func New(options ...Option) *Crontab {
	c := &Crontab{
		entries:    make(map[int64]*Entry),
		heap:       make(entryHeap, 0),
		done:       make(chan struct{}),
		wake:       make(chan struct{}, 1),
		workerSize: 16,
		ctx:        context.Background(),
		logger:     zap.NewNop(),
	}

	for _, o := range options {
		o.apply(c)
	}

	c.workerSize = max(c.workerSize, 1)

	c.jobs = make(chan Job, c.workerSize)

	c.start(c.ctx)

	return c
}

// Add adds a job entry. It computes the next run time immediately.
// If an entry with the same ID already exists, it is replaced.
func (c *Crontab) Add(id int64, schedule Schedule, job Job) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return fmt.Errorf("cron: already stopped")
	}

	if _, exists := c.entries[id]; exists {
		c.remove(id)
	}

	entry := &Entry{ID: id, Schedule: schedule, Job: job}
	entry.next = schedule.Next(time.Now())
	c.entries[id] = entry
	heap.Push(&c.heap, entry)
	c.rescheduleTimer()

	return nil
}

// Remove deletes a job entry.
func (c *Crontab) Remove(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remove(id)
	c.rescheduleTimer()
}

func (c *Crontab) remove(id int64) {
	entry, exists := c.entries[id]
	if !exists {
		return
	}
	delete(c.entries, id)
	heap.Remove(&c.heap, entry.index)
}

// Start begins the execution loop with a worker pool.
func (c *Crontab) start(ctx context.Context) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.rescheduleTimer()
	c.mu.Unlock()

	for i := 0; i < c.workerSize; i++ {
		c.wg.Add(1)
		// goroutine lifecycle: pool-owned — worker selects on ctx.Done,
		// c.done, and c.jobs; tracked by c.wg so Stop can wait for all
		// workers to exit before returning.
		go func() {
			defer c.wg.Done()
			c.worker(ctx)
		}()
	}

	// scheduleLoop is tracked by c.wg alongside the workers so Stop waits
	// for it to observe c.done (closed by Stop) and exit, avoiding a
	// post-Stop goroutine leak.
	c.wg.Add(1)
	// goroutine lifecycle: bound to ctx and c.done (closed by Stop);
	// scheduleLoop selects on ctx.Done and c.done and exits; tracked by
	// c.wg so Stop can wait for it.
	go c.scheduleLoop(ctx)
}

// run invokes a job with panic isolation. A panicking job is recovered
// and logged via zap so that a single faulty job cannot crash the scheduler
// or sibling workers. The recovered panic is never re-propagated, honoring
// the "no panic in business logic" rule.
func (c *Crontab) run(ctx context.Context, job Job) {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("cron job panic recovered",
				zap.Any("panic", r),
				zap.Stack("stack"),
			)
		}
	}()
	job.Run(ctx)
}

func (c *Crontab) worker(ctx context.Context) {
	for {
		select {
		case job, ok := <-c.jobs:
			if !ok {
				return
			}
			c.run(ctx, job)
		case <-ctx.Done():
			return
		case <-c.done:
			return
		}
	}
}

func (c *Crontab) scheduleLoop(ctx context.Context) {
	defer c.wg.Done()
	for {
		c.mu.Lock()
		timer := c.timer
		c.mu.Unlock()

		if timer == nil {
			select {
			case <-ctx.Done():
				return
			case <-c.done:
				return
			case <-c.wake:
				continue
			}
		}

		select {
		case <-timer.C:
			c.runDueJobs(ctx)
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-c.wake:
			continue
		}
	}
}

func (c *Crontab) runDueJobs(ctx context.Context) {
	now := time.Now()

	var due []Job
	c.mu.Lock()
	for c.heap.Len() > 0 {
		entry := c.heap[0]
		if entry.next.After(now) {
			break
		}
		heap.Pop(&c.heap)
		due = append(due, entry.Job)
		entry.next = entry.Schedule.Next(now)
		if !entry.next.IsZero() {
			heap.Push(&c.heap, entry)
		} else {
			delete(c.entries, entry.ID)
		}
	}
	c.rescheduleTimer()
	c.mu.Unlock()

	for _, job := range due {
		select {
		case c.jobs <- job:
		case <-c.done:
			return
		case <-ctx.Done():
			return
		default:
			// Worker pool full; run inline to avoid unbounded goroutine creation.
			c.run(ctx, job)
		}
	}
}

func (c *Crontab) rescheduleTimer() {
	if !c.running {
		return
	}

	if c.heap.Len() == 0 {
		if c.timer != nil {
			c.timer.Stop()
		}
		return
	}

	next := c.heap[0].next
	duration := max(time.Until(next), 0)
	if c.timer != nil {
		if !c.timer.Stop() {
			select {
			case <-c.timer.C:
			default:
			}
		}
		c.timer.Reset(duration)
	} else {
		c.timer = time.NewTimer(duration)
	}

	// Wake scheduleLoop to pick up the new or reset timer.
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Stop cancels the manager and waits for workers to finish.
func (c *Crontab) Stop(ctx context.Context) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}

	c.running = false
	close(c.done)
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.mu.Unlock()

	done := make(chan struct{})
	// goroutine lifecycle: bounded — waits for c.wg to drain (workers and
	// scheduleLoop) after they observe c.done; exits after close(done).
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("cron: stop timeout: %w", ctx.Err())
	}
}
