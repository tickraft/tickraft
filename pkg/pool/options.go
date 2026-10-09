// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package pool

import "time"

// defaultQueueSize is the bounded task queue capacity used when no
// explicit queue size is configured.
const defaultQueueSize = 1024

// defaultWarmup is the number of worker goroutines started eagerly by
// [New]. Additional workers up to the configured cap are started
// lazily when the queue shows signs of backlog.
const defaultWarmup = 4

// RejectionPolicy controls how [Pool.Submit] behaves when the bounded
// queue is full and no additional worker can be grown.
type RejectionPolicy int

const (
	// RejectionAbort blocks Submit until a queue slot becomes
	// available, the caller context is cancelled, or the pool is
	// shut down. It is the default policy.
	RejectionAbort RejectionPolicy = iota
	// RejectionCallerRuns executes the job synchronously in the
	// goroutine that called Submit instead of enqueueing it. Submit
	// returns only after the job has finished.
	RejectionCallerRuns
	// RejectionDiscardOldest drops the oldest queued job to make
	// room for the newly submitted one. If no oldest job can be
	// discarded (the queue was drained concurrently), Submit returns
	// [ErrQueueFull].
	RejectionDiscardOldest
)

// ErrorHandler is invoked synchronously by a worker goroutine when a
// job returns a non-nil error. The original [Job] is passed in so
// callers can identify the task by type assertion or attached
// metadata, resubmit it, or attach logging tags. The handler must not
// block for long periods.
type ErrorHandler func(job Job, err error)

// PanicHandler is invoked synchronously by a worker goroutine when a
// job panics. The original [Job] and the recovered value are passed
// in for diagnosis and resubmission.
type PanicHandler func(job Job, reason any)

// config holds the effective configuration of a pool after applying
// all [Option] values.
type config struct {
	workers         int
	queueSize       int
	errorHandler    ErrorHandler
	panicHandler    PanicHandler
	rejectionPolicy RejectionPolicy
	stallThreshold  time.Duration
	stallHandler    func(Stats)
}

// Option configures a [Pool] at construction time.
//
// Options are applied in the order they are passed to [New]. An
// invalid option value (for example a non-positive worker count) is
// not reported by the option itself; it is validated by [New] which
// returns an error instead.
type Option interface {
	apply(*config)
}

// workersOption sets the number of worker goroutines.
type workersOption int

func (o workersOption) apply(c *config) { c.workers = int(o) }

// WithWorkers sets the upper bound on worker goroutines that consume
// jobs from the queue. [New] pre-starts min(workers, 4) workers and
// grows the rest lazily on backlog. A non-positive value causes [New]
// to return an error.
func WithWorkers(n int) Option { return workersOption(n) }

// queueSizeOption sets the bounded task queue capacity.
type queueSizeOption int

func (o queueSizeOption) apply(c *config) { c.queueSize = int(o) }

// WithQueueSize sets the capacity of the bounded task queue. When the
// queue is full, [Pool.Submit] reacts according to the configured
// [RejectionPolicy]. A non-positive value causes [New] to return an
// error.
func WithQueueSize(n int) Option { return queueSizeOption(n) }

// errorHandlerOption sets the callback invoked on job errors.
type errorHandlerOption struct{ fn ErrorHandler }

func (o errorHandlerOption) apply(c *config) { c.errorHandler = o.fn }

// WithErrorHandler registers a callback invoked whenever a job returns
// a non-nil error. The handler receives the original [Job] and the
// returned error. When no handler is configured the error is only
// counted in [Stats].
func WithErrorHandler(fn ErrorHandler) Option {
	return errorHandlerOption{fn: fn}
}

// panicHandlerOption sets the callback invoked on recovered panics.
type panicHandlerOption struct{ fn PanicHandler }

func (o panicHandlerOption) apply(c *config) { c.panicHandler = o.fn }

// WithPanicHandler registers a callback invoked whenever a job
// panics. The handler receives the original [Job] and the recovered
// value. When no handler is configured the panic value is only
// counted in [Stats] and otherwise discarded.
func WithPanicHandler(fn PanicHandler) Option {
	return panicHandlerOption{fn: fn}
}

// rejectionPolicyOption sets the [RejectionPolicy] used on a full
// queue.
type rejectionPolicyOption struct{ p RejectionPolicy }

func (o rejectionPolicyOption) apply(c *config) { c.rejectionPolicy = o.p }

// WithRejectionPolicy sets the behavior of [Pool.Submit] when the
// queue is full and no additional worker can be grown. The default is
// [RejectionAbort].
func WithRejectionPolicy(p RejectionPolicy) Option {
	return rejectionPolicyOption{p: p}
}

// stallDetectionOption enables background stall detection.
type stallDetectionOption struct {
	threshold time.Duration
	handler   func(Stats)
}

func (o stallDetectionOption) apply(c *config) {
	c.stallThreshold = o.threshold
	c.stallHandler = o.handler
}

// WithStallDetection enables background detection of long-full
// queues. When threshold > 0, a goroutine polls the queue at
// threshold/4 (minimum 50ms) and invokes handler with the current
// [Stats] once the queue has been continuously full for at least
// threshold. The handler is not invoked again until the queue
// recovers (a slot frees) and fills again, so a single stall episode
// produces exactly one callback. When threshold == 0 no goroutine is
// started and the option is a no-op.
func WithStallDetection(threshold time.Duration, handler func(Stats)) Option {
	return stallDetectionOption{threshold: threshold, handler: handler}
}
