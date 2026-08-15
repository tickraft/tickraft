// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package retry

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/tickraft/tickraft/pkg/timewheel"
)

// Backoff computes the wait duration before the next retry attempt.
type Backoff interface {
	// Next returns the delay for the given attempt number (0-based).
	Next(attempt int) time.Duration
}

// Jitter applies random perturbation to a backoff delay to prevent
// thundering herd problems in high-concurrency retry scenarios.
type Jitter interface {
	// Apply returns a jittered duration derived from d.
	Apply(d time.Duration) time.Duration
}

// FullJitter randomizes the delay uniformly in [0, d).
// It uses math/rand/v2 for thread-safe random number generation.
type FullJitter struct{}

// NewFullJitter creates a FullJitter instance.
func NewFullJitter() Jitter {
	return &FullJitter{}
}

// Apply returns a uniformly random duration in [0, d), satisfying the Jitter interface.
func (f *FullJitter) Apply(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(d)))
}

// ProportionalJitter randomizes the delay by a configurable factor.
// The factor is clamped to [0.0, 1.0] at construction time:
//   - factor=0.0: no jitter, the delay is returned unchanged.
//   - factor=1.0: full jitter, delay randomized in [0, d) (equivalent to FullJitter).
//   - 0.0 < factor < 1.0: partial jitter, delay randomized in [d*(1-factor), d).
//
// The jittered delay formula is: d * (1.0 - factor + factor * rand.Float64()).
// This matches the jitter convention used by pkg/event so that retry backoff
// and event-bus backoff share the same partial-jitter semantics.
type ProportionalJitter struct {
	factor float64
}

// NewProportionalJitter creates a ProportionalJitter with the given factor.
// The factor is clamped to [0.0, 1.0]: values below 0.0 become 0.0 (no jitter)
// and values above 1.0 become 1.0 (full jitter).
func NewProportionalJitter(factor float64) Jitter {
	switch {
	case factor < 0.0:
		factor = 0.0
	case factor > 1.0:
		factor = 1.0
	}
	return &ProportionalJitter{factor: factor}
}

// Apply returns a jittered duration in [d*(1-factor), d), satisfying the Jitter
// interface. When factor is 0.0 the delay is returned unchanged.
func (p *ProportionalJitter) Apply(d time.Duration) time.Duration {
	if d <= 0 || p.factor == 0.0 {
		return d
	}
	scale := 1.0 - p.factor + p.factor*rand.Float64()
	return time.Duration(float64(d) * scale)
}

// FixedInterval always returns the same duration regardless of attempt.
type FixedInterval struct {
	interval time.Duration
}

// NewFixedInterval creates a FixedInterval backoff with the given duration.
func NewFixedInterval(d time.Duration) Backoff {
	return &FixedInterval{interval: d}
}

// Next returns the configured interval, ignoring the attempt number.
func (f *FixedInterval) Next(_ int) time.Duration {
	return f.interval
}

// Exponential returns base * multiplier^attempt, capped at max.
// By default the multiplier is 2.0 and no jitter is applied.
type Exponential struct {
	base       time.Duration
	max        time.Duration
	multiplier float64
	jitter     Jitter
}

// NewExponential creates an Exponential backoff with the given base and max.
// Optional ExponentialOption values can be passed to customize the multiplier
// and jitter behavior.
//
// Parameters:
//   - base: initial delay, must be > 0
//   - max: maximum delay cap, must be >= base
//
// Returns an error if base <= 0, max < base, or multiplier <= 0.
func NewExponential(base, max time.Duration, options ...ExponentialOption) (Backoff, error) {
	if base <= 0 {
		return nil, fmt.Errorf("retry: base must be > 0, got %v", base)
	}
	if max < base {
		return nil, fmt.Errorf("retry: max must be >= base, got max=%v base=%v", max, base)
	}
	e := &Exponential{
		base:       base,
		max:        max,
		multiplier: 2.0,
	}
	for _, o := range options {
		o.apply(e)
	}

	if e.multiplier <= 0 {
		return nil, fmt.Errorf("retry: multiplier must be > 0, got %v", e.multiplier)
	}

	return e, nil
}

// Next computes the backoff delay for the given attempt number (0-based),
// applying the configured jitter, and capped at max.
func (e *Exponential) Next(attempt int) time.Duration {
	d := float64(e.base)
	for i := 0; i < attempt; i++ {
		d *= e.multiplier
		if d >= float64(e.max) {
			return e.withJitter(e.max)
		}
	}
	if d > float64(e.max) {
		return e.withJitter(e.max)
	}
	return e.withJitter(time.Duration(d))
}

func (e *Exponential) withJitter(d time.Duration) time.Duration {
	if e.jitter != nil {
		return e.jitter.Apply(d)
	}
	return d
}

// ExponentialOption configures an Exponential backoff.
type ExponentialOption interface {
	apply(*Exponential)
}

type multiplierOption float64

func (o multiplierOption) apply(e *Exponential) {
	e.multiplier = float64(o)
}

// WithMultiplier sets the exponential multiplier.
// Default is 2.0. Returns an error option if m < 1.0.
func WithMultiplier(m float64) (ExponentialOption, error) {
	if m < 1.0 {
		return nil, fmt.Errorf("retry: multiplier must be >= 1.0, got %v", m)
	}
	return multiplierOption(m), nil
}

type jitterOption struct {
	j Jitter
}

func (o jitterOption) apply(e *Exponential) {
	e.jitter = o.j
}

// WithJitter sets the jitter strategy for the Exponential backoff.
func WithJitter(j Jitter) ExponentialOption {
	return jitterOption{j: j}
}

// Option configures a Retry.
type Option interface {
	apply(*Retry)
}

type maxAttemptsOption int

func (o maxAttemptsOption) apply(r *Retry) {
	r.maxAttempts = int(o)
}

// WithMaxAttempts sets the maximum number of attempts (including the first call).
func WithMaxAttempts(n int) Option {
	return maxAttemptsOption(n)
}

type backoffOption struct {
	b Backoff
}

func (o backoffOption) apply(r *Retry) {
	r.backoff = o.b
}

// WithBackoff sets the backoff strategy.
func WithBackoff(b Backoff) Option {
	return backoffOption{b: b}
}

type retryableOption struct {
	fn func(error) bool
}

func (o retryableOption) apply(r *Retry) {
	r.retryable = o.fn
}

// WithRetryable sets the predicate that determines if an error is retryable.
func WithRetryable(fn func(error) bool) Option {
	return retryableOption{fn: fn}
}

// Retry executes a function with configurable retry and backoff behavior.
type Retry struct {
	maxAttempts int
	backoff     Backoff
	retryable   func(error) bool
}

// New creates a Retry with the given options.
// Defaults: maxAttempts=3, backoff=Exponential(base=1s, max=30s), retryable=all errors.
// Returns an error if the default backoff configuration is invalid or if
// maxAttempts is configured to a non-positive value.
func New(options ...Option) (*Retry, error) {
	backoff, err := NewExponential(time.Second, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("retry: create default backoff: %w", err)
	}
	r := &Retry{
		maxAttempts: 3,
		backoff:     backoff,
		retryable:   func(_ error) bool { return true },
	}
	for _, o := range options {
		o.apply(r)
	}
	if r.maxAttempts <= 0 {
		return nil, fmt.Errorf("retry: max attempts must be > 0, got %d", r.maxAttempts)
	}
	if r.backoff == nil {
		return nil, fmt.Errorf("retry: backoff must not be nil")
	}
	return r, nil
}

// Do executes fn with retry behavior according to the Retry configuration.
// On success it returns nil immediately. On a retryable error it waits per
// the backoff strategy and retries, up to maxAttempts. If ctx is cancelled
// before an attempt or during a wait, it returns ctx.Err() immediately.
func (r *Retry) Do(ctx context.Context, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < r.maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := fn(); err != nil {
			lastErr = err
			if !r.retryable(err) {
				return err
			}
			if attempt == r.maxAttempts-1 {
				return lastErr
			}
			wait := r.backoff.Next(attempt)
			if wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
			continue
		}
		return nil
	}
	return lastErr
}

// Operation is the function executed with retry behavior.
type Operation func() error

// DoAsync executes the operation with asynchronous retry using a time wheel.
//
// The first attempt and all retries are dispatched through the wheel's worker
// pool, so the caller's goroutine is never blocked — not even for the first
// invocation. Retry delays are scheduled via wheel.Add as one-shot callbacks
// instead of blocking on a timer.
//
// onComplete is invoked exactly once with the final result: nil on success,
// or the last error on failure (including context cancellation). It is always
// called from the wheel's callback goroutine, never from the caller's.
//
// Returns an error only for setup failures (nil wheel or nil op); in that
// case onComplete is not called. If the wheel is nil the caller should fall
// back to the synchronous [Retry.Do] method.
func (r *Retry) DoAsync(ctx context.Context, op Operation, wheel timewheel.Wheel, onComplete func(error)) error {
	if wheel == nil {
		return fmt.Errorf("retry: time wheel is nil")
	}
	if op == nil {
		return fmt.Errorf("retry: operation is nil")
	}
	if onComplete == nil {
		onComplete = func(error) {}
	}
	r.scheduleAttempt(ctx, op, wheel, onComplete, 0, 0)
	return nil
}

// scheduleAttempt registers a wheel callback that executes op for the given
// attempt after waiting for delay. If op fails and retries remain, the next
// attempt is scheduled with the backoff delay; otherwise onComplete is
// invoked with the final result. The context is checked at the start of each
// callback so cancellation propagates without waiting for the delay to elapse.
func (r *Retry) scheduleAttempt(ctx context.Context, op Operation, wheel timewheel.Wheel, onComplete func(error), attempt int, delay time.Duration) {
	wheel.Add(delay, func(timewheel.EntryID) {
		select {
		case <-ctx.Done():
			onComplete(ctx.Err())
			return
		default:
		}
		if err := op(); err != nil {
			if !r.retryable(err) || attempt >= r.maxAttempts-1 {
				onComplete(err)
				return
			}
			nextDelay := r.backoff.Next(attempt)
			r.scheduleAttempt(ctx, op, wheel, onComplete, attempt+1, nextDelay)
			return
		}
		onComplete(nil)
	})
}
