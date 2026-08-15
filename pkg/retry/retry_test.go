// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package retry

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/timewheel"
)

func TestFixedInterval(t *testing.T) {
	b := NewFixedInterval(100 * time.Millisecond)
	for i := range 5 {
		if got := b.Next(i); got != 100*time.Millisecond {
			t.Errorf("FixedInterval.Next(%d) = %v, want %v", i, got, 100*time.Millisecond)
		}
	}
}

func TestExponential_DefaultMultiplier(t *testing.T) {
	b, err := NewExponential(time.Second, 30*time.Second)
	if err != nil {
		t.Fatalf("NewExponential() error = %v", err)
	}

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, 30 * time.Second}, // capped at max
		{10, 30 * time.Second},
	}
	for _, tt := range tests {
		if got := b.Next(tt.attempt); got != tt.want {
			t.Errorf("Exponential.Next(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestExponential_CustomMultiplier(t *testing.T) {
	opt, err := WithMultiplier(1.5)
	if err != nil {
		t.Fatalf("WithMultiplier() error = %v", err)
	}
	b, err := NewExponential(time.Second, 30*time.Second, opt)
	if err != nil {
		t.Fatalf("NewExponential() error = %v", err)
	}

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},         // 1 * 1.5^0 = 1
		{1, 1500 * time.Millisecond}, // 1 * 1.5^1 = 1.5s
		{2, 2250 * time.Millisecond}, // 1 * 1.5^2 = 2.25s
		{3, 3375 * time.Millisecond}, // 1 * 1.5^3 = 3.375s
	}
	for _, tt := range tests {
		if got := b.Next(tt.attempt); got != tt.want {
			t.Errorf("Exponential.Next(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestExponential_OverflowProtection(t *testing.T) {
	opt, err := WithMultiplier(3.0)
	if err != nil {
		t.Fatalf("WithMultiplier() error = %v", err)
	}
	b, err := NewExponential(time.Second, 5*time.Second, opt)
	if err != nil {
		t.Fatalf("NewExponential() error = %v", err)
	}

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second}, // 1 * 3^0 = 1
		{1, 3 * time.Second}, // 1 * 3^1 = 3
		{2, 5 * time.Second}, // 1 * 3^2 = 9, capped at 5
		{3, 5 * time.Second}, // capped
	}
	for _, tt := range tests {
		if got := b.Next(tt.attempt); got != tt.want {
			t.Errorf("Exponential.Next(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestFullJitter(t *testing.T) {
	j := NewFullJitter()
	d := 1 * time.Second
	for range 100 {
		got := j.Apply(d)
		if got < 0 || got >= d {
			t.Errorf("FullJitter.Apply(%v) = %v, want in [0, %v)", d, got, d)
		}
	}
}

func TestFullJitter_Zero(t *testing.T) {
	j := NewFullJitter()
	if got := j.Apply(0); got != 0 {
		t.Errorf("FullJitter.Apply(0) = %v, want 0", got)
	}
}

func TestProportionalJitter_Range(t *testing.T) {
	d := 1 * time.Second
	j := NewProportionalJitter(0.3)
	lo := time.Duration(float64(d) * 0.7)
	for range 200 {
		got := j.Apply(d)
		if got < lo || got >= d {
			t.Errorf("ProportionalJitter(0.3).Apply(%v) = %v, want in [%v, %v)", d, got, lo, d)
		}
	}
}

func TestProportionalJitter_FullFactor(t *testing.T) {
	d := 1 * time.Second
	j := NewProportionalJitter(1.0)
	for range 200 {
		got := j.Apply(d)
		if got < 0 || got >= d {
			t.Errorf("ProportionalJitter(1.0).Apply(%v) = %v, want in [0, %v)", d, got, d)
		}
	}
}

func TestProportionalJitter_ZeroFactor(t *testing.T) {
	d := 1 * time.Second
	j := NewProportionalJitter(0.0)
	for range 100 {
		if got := j.Apply(d); got != d {
			t.Errorf("ProportionalJitter(0.0).Apply(%v) = %v, want %v", d, got, d)
		}
	}
}

func TestProportionalJitter_Clamp(t *testing.T) {
	tests := []struct {
		input  float64
		expect float64
	}{
		{-0.5, 0.0},
		{1.5, 1.0},
		{2.0, 1.0},
		{-1.0, 0.0},
	}
	for _, tt := range tests {
		// ProportionalJitter clamps silently; verify the clamped behavior.
		j := NewProportionalJitter(tt.input)
		got := j.Apply(1 * time.Second)
		// For clamped-to-0 factor, Apply returns the input unchanged.
		// For clamped-to-1 factor, Apply returns a value in [0, d).
		if tt.expect == 0.0 {
			if got != 1*time.Second {
				t.Errorf("clamped factor %v: Apply = %v, want %v (no jitter)", tt.input, got, 1*time.Second)
			}
		} else {
			if got < 0 || got >= 1*time.Second {
				t.Errorf("clamped factor %v: Apply = %v, want in [0, %v)", tt.input, got, 1*time.Second)
			}
		}
	}
}

func TestProportionalJitter_ZeroDuration(t *testing.T) {
	j := NewProportionalJitter(0.5)
	if got := j.Apply(0); got != 0 {
		t.Errorf("ProportionalJitter.Apply(0) = %v, want 0", got)
	}
}

func TestProportionalJitter_Concurrent(t *testing.T) {
	j := NewProportionalJitter(0.5)
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := j.Apply(1 * time.Second)
			if got < 500*time.Millisecond || got >= 1*time.Second {
				t.Errorf("concurrent ProportionalJitter.Apply = %v, out of range", got)
			}
		}()
	}
	wg.Wait()
}

func TestExponential_WithJitter(t *testing.T) {
	b, err := NewExponential(time.Second, 30*time.Second, WithJitter(NewFullJitter()))
	if err != nil {
		t.Fatalf("NewExponential() error = %v", err)
	}
	for attempt := range 5 {
		got := b.Next(attempt)
		if got < 0 || got >= 30*time.Second {
			t.Errorf("Exponential with jitter Next(%d) = %v, out of range [0, 30s)", attempt, got)
		}
	}
}

func TestExponential_ErrorInvalidBase(t *testing.T) {
	_, err := NewExponential(0, time.Second)
	if err == nil {
		t.Error("expected error for base <= 0")
	}
}

func TestExponential_ErrorMaxLessThanBase(t *testing.T) {
	_, err := NewExponential(time.Second, time.Millisecond)
	if err == nil {
		t.Error("expected error for max < base")
	}
}

func TestWithMultiplier_ErrorLessThanOne(t *testing.T) {
	_, err := WithMultiplier(0.5)
	if err == nil {
		t.Error("expected error for multiplier < 1.0")
	}
}

func TestExponential_ConcurrentNext(t *testing.T) {
	b, err := NewExponential(time.Second, 30*time.Second, WithJitter(NewFullJitter()))
	if err != nil {
		t.Fatalf("NewExponential() error = %v", err)
	}
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(attempt int) {
			defer wg.Done()
			got := b.Next(attempt)
			if got < 0 || got > 30*time.Second {
				t.Errorf("concurrent Next(%d) = %v, out of range", attempt, got)
			}
		}(i % 10)
	}
	wg.Wait()
}

func TestDo_SuccessOnFirstAttempt(t *testing.T) {
	r, err := New(WithMaxAttempts(3), WithBackoff(NewFixedInterval(time.Millisecond)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	calls := 0
	if err = r.Do(context.Background(), func() error {
		calls++
		return nil
	}); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestDo_SuccessAfterRetries(t *testing.T) {
	r, err := New(WithMaxAttempts(3), WithBackoff(NewFixedInterval(time.Millisecond)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	calls := 0
	if err = r.Do(context.Background(), func() error {
		calls++
		if calls < 3 {
			return errors.New("fail")
		}
		return nil
	}); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestDo_MaxAttemptsExhausted(t *testing.T) {
	r, err := New(WithMaxAttempts(3), WithBackoff(NewFixedInterval(time.Millisecond)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	calls := 0
	if err = r.Do(context.Background(), func() error {
		calls++
		return errors.New("always fail")
	}); err == nil {
		t.Error("expected error, got nil")
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestDo_ContextCancelled(t *testing.T) {
	r, err := New(WithMaxAttempts(10), WithBackoff(NewFixedInterval(5*time.Second)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	calls := 0
	if err = r.Do(ctx, func() error {
		calls++
		return errors.New("fail")
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call before context cancel, got %d", calls)
	}
}

func TestWithRetryable(t *testing.T) {
	permanentErr := errors.New("permanent")

	r, err := New(
		WithMaxAttempts(5),
		WithBackoff(NewFixedInterval(time.Millisecond)),
		WithRetryable(func(err error) bool {
			return !errors.Is(err, permanentErr)
		}),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	calls := 0
	if err = r.Do(context.Background(), func() error {
		calls++
		return permanentErr
	}); !errors.Is(err, permanentErr) {
		t.Errorf("expected permanentErr, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call (non-retryable), got %d", calls)
	}
}

func TestNew_Defaults(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if r.maxAttempts != 3 {
		t.Errorf("default maxAttempts = %d, want 3", r.maxAttempts)
	}
	if r.retryable(errors.New("any")) != true {
		t.Error("default retryable should return true for all errors")
	}
}

func TestNew_ErrorInvalidMaxAttempts(t *testing.T) {
	if _, err := New(WithMaxAttempts(0)); err == nil {
		t.Error("expected error for maxAttempts = 0, got nil")
	}
	if _, err := New(WithMaxAttempts(-1)); err == nil {
		t.Error("expected error for maxAttempts < 0, got nil")
	}
}

func TestNew_ErrorNilBackoff(t *testing.T) {
	if _, err := New(WithBackoff(nil)); err == nil {
		t.Error("expected error for nil backoff, got nil")
	}
}

func TestDo_ContextCancelledBeforeFirstAttempt(t *testing.T) {
	r, err := New(WithMaxAttempts(3), WithBackoff(NewFixedInterval(time.Millisecond)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	if err = r.Do(ctx, func() error {
		calls++
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if calls != 0 {
		t.Errorf("expected 0 calls (ctx cancelled before first attempt), got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// DoAsync tests
// ---------------------------------------------------------------------------

// mockWheel is a test double for timewheel.Wheel that dispatches callbacks
// asynchronously with precise timing (no second-level rounding) so tests run
// fast. It is safe for concurrent use.
type mockWheel struct {
	ctx    context.Context
	cancel context.CancelFunc
	nextID int64
}

func newMockWheel() *mockWheel {
	ctx, cancel := context.WithCancel(context.Background())
	return &mockWheel{ctx: ctx, cancel: cancel}
}

func (m *mockWheel) Add(duration time.Duration, cb timewheel.Callback) timewheel.EntryID {
	id := timewheel.EntryID(atomic.AddInt64(&m.nextID, 1))
	go func() {
		if duration > 0 {
			t := time.NewTimer(duration)
			defer t.Stop()
			select {
			case <-t.C:
			case <-m.ctx.Done():
				return
			}
		}
		select {
		case <-m.ctx.Done():
			return
		default:
		}
		cb(id)
	}()
	return id
}

func (m *mockWheel) AddAt(_ time.Time, cb timewheel.Callback) timewheel.EntryID {
	return m.Add(0, cb)
}

func (m *mockWheel) Remove(_ timewheel.EntryID) {}

func (m *mockWheel) Renew(_ timewheel.EntryID, duration time.Duration) timewheel.EntryID {
	return m.Add(duration, func(timewheel.EntryID) {})
}

func (m *mockWheel) Start(_ context.Context) {}

func (m *mockWheel) Stop(_ context.Context) error {
	m.cancel()
	return nil
}

// doAsyncRunner runs DoAsync and waits for onComplete with a timeout.
func doAsyncRunner(t *testing.T, r *Retry, ctx context.Context, op Operation, wheel timewheel.Wheel) (error, bool) {
	t.Helper()
	resultCh := make(chan error, 1)
	err := r.DoAsync(ctx, op, wheel, func(e error) {
		resultCh <- e
	})
	if err != nil {
		return err, false
	}
	select {
	case e := <-resultCh:
		return e, true
	case <-time.After(5 * time.Second):
		t.Fatal("DoAsync timed out waiting for onComplete")
		return nil, false
	}
}

func TestDoAsync_SuccessOnFirstAttempt(t *testing.T) {
	r, err := New(WithMaxAttempts(3), WithBackoff(NewFixedInterval(time.Millisecond)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	wheel := newMockWheel()
	defer wheel.Stop(context.Background())

	var calls atomic.Int32
	gotErr, ok := doAsyncRunner(t, r, context.Background(), func() error {
		calls.Add(1)
		return nil
	}, wheel)
	if !ok {
		t.Fatal("onComplete was not called")
	}
	if gotErr != nil {
		t.Errorf("expected nil error, got %v", gotErr)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 call, got %d", calls.Load())
	}
}

func TestDoAsync_SuccessAfterRetries(t *testing.T) {
	r, err := New(WithMaxAttempts(3), WithBackoff(NewFixedInterval(time.Millisecond)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	wheel := newMockWheel()
	defer wheel.Stop(context.Background())

	var calls atomic.Int32
	gotErr, ok := doAsyncRunner(t, r, context.Background(), func() error {
		n := calls.Add(1)
		if n < 3 {
			return errors.New("fail")
		}
		return nil
	}, wheel)
	if !ok {
		t.Fatal("onComplete was not called")
	}
	if gotErr != nil {
		t.Errorf("expected nil error, got %v", gotErr)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 calls, got %d", calls.Load())
	}
}

func TestDoAsync_MaxAttemptsExhausted(t *testing.T) {
	r, err := New(WithMaxAttempts(3), WithBackoff(NewFixedInterval(time.Millisecond)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	wheel := newMockWheel()
	defer wheel.Stop(context.Background())

	var calls atomic.Int32
	gotErr, ok := doAsyncRunner(t, r, context.Background(), func() error {
		calls.Add(1)
		return errors.New("always fail")
	}, wheel)
	if !ok {
		t.Fatal("onComplete was not called")
	}
	if gotErr == nil {
		t.Error("expected error, got nil")
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 calls, got %d", calls.Load())
	}
}

func TestDoAsync_NonRetryableError(t *testing.T) {
	permanentErr := errors.New("permanent")
	r, err := New(
		WithMaxAttempts(5),
		WithBackoff(NewFixedInterval(time.Millisecond)),
		WithRetryable(func(e error) bool {
			return !errors.Is(e, permanentErr)
		}),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	wheel := newMockWheel()
	defer wheel.Stop(context.Background())

	var calls atomic.Int32
	gotErr, ok := doAsyncRunner(t, r, context.Background(), func() error {
		calls.Add(1)
		return permanentErr
	}, wheel)
	if !ok {
		t.Fatal("onComplete was not called")
	}
	if !errors.Is(gotErr, permanentErr) {
		t.Errorf("expected permanentErr, got %v", gotErr)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 call (non-retryable), got %d", calls.Load())
	}
}

func TestDoAsync_ContextCancelled(t *testing.T) {
	r, err := New(WithMaxAttempts(10), WithBackoff(NewFixedInterval(500*time.Millisecond)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	wheel := newMockWheel()
	defer wheel.Stop(context.Background())

	ctx, cancel := context.WithCancel(context.Background())

	var calls atomic.Int32
	resultCh := make(chan error, 1)
	if e := r.DoAsync(ctx, func() error {
		calls.Add(1)
		return errors.New("fail")
	}, wheel, func(e error) {
		resultCh <- e
	}); e != nil {
		t.Fatalf("DoAsync() error = %v", e)
	}

	// Wait for the first attempt to execute, then cancel the context.
	// The next retry is scheduled with a 500ms delay; cancellation should
	// short-circuit it once the callback fires.
	select {
	case <-resultCh:
		t.Fatal("onComplete called too early")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()

	select {
	case e := <-resultCh:
		if !errors.Is(e, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for onComplete after cancel")
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 call before context cancel, got %d", calls.Load())
	}
}

func TestDoAsync_NilWheelReturnsError(t *testing.T) {
	r, err := New(WithMaxAttempts(3))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	called := false
	e := r.DoAsync(context.Background(), func() error { return nil }, nil, func(error) {
		called = true
	})
	if e == nil {
		t.Error("expected error for nil wheel, got nil")
	}
	if called {
		t.Error("onComplete should not be called on setup error")
	}
}

func TestDoAsync_DoesNotBlockCaller(t *testing.T) {
	r, err := New(WithMaxAttempts(3), WithBackoff(NewFixedInterval(2*time.Second)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	wheel := newMockWheel()
	defer wheel.Stop(context.Background())

	// If DoAsync blocks, the done channel will not be closed promptly.
	done := make(chan struct{})
	go func() {
		_ = r.DoAsync(context.Background(), func() error {
			return errors.New("fail")
		}, wheel, func(error) {})
		close(done)
	}()

	select {
	case <-done:
		// Good: DoAsync returned without blocking.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("DoAsync blocked the caller's goroutine")
	}
}
