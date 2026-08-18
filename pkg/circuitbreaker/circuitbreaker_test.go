// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package circuitbreaker

import (
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

// TestStateString verifies that State.String returns the expected
// human-readable names for every state and falls back to "unknown" for
// out-of-range values.
func TestStateString(t *testing.T) {
	cases := []struct {
		state State
		want  string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half_open"},
		{State(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.state.String(); got != tc.want {
			t.Errorf("State(%d).String(): got %q, want %q", tc.state, got, tc.want)
		}
	}
}

// TestNewDefaults verifies that New applies default values for zero fields.
func TestNewDefaults(t *testing.T) {
	cb := New(Config{})
	if cb.config.FailureThreshold != defaultFailureThreshold {
		t.Errorf("FailureThreshold: got %d, want %d", cb.config.FailureThreshold, defaultFailureThreshold)
	}
	if cb.config.Cooldown != defaultCooldown {
		t.Errorf("Cooldown: got %v, want %v", cb.config.Cooldown, defaultCooldown)
	}
	if cb.config.HalfOpenMax != defaultHalfOpenMax {
		t.Errorf("HalfOpenMax: got %d, want %d", cb.config.HalfOpenMax, defaultHalfOpenMax)
	}
	if cb.state != StateClosed {
		t.Errorf("state: got %v, want %v", cb.state, StateClosed)
	}
}

// TestNewWithExplicitConfig verifies that explicit config values are
// preserved when they are positive.
func TestNewWithExplicitConfig(t *testing.T) {
	cfg := Config{FailureThreshold: 3, Cooldown: 10 * time.Second, HalfOpenMax: 2}
	cb := New(cfg)
	if cb.config.FailureThreshold != 3 {
		t.Errorf("FailureThreshold: got %d, want 3", cb.config.FailureThreshold)
	}
	if cb.config.Cooldown != 10*time.Second {
		t.Errorf("Cooldown: got %v, want %v", cb.config.Cooldown, 10*time.Second)
	}
	if cb.config.HalfOpenMax != 2 {
		t.Errorf("HalfOpenMax: got %d, want 2", cb.config.HalfOpenMax)
	}
}

// TestNewWithNegativeConfig verifies that negative config values fall back
// to defaults.
func TestNewWithNegativeConfig(t *testing.T) {
	cb := New(Config{FailureThreshold: -1, Cooldown: -1, HalfOpenMax: -1})
	if cb.config.FailureThreshold != defaultFailureThreshold {
		t.Errorf("FailureThreshold: got %d, want %d", cb.config.FailureThreshold, defaultFailureThreshold)
	}
	if cb.config.Cooldown != defaultCooldown {
		t.Errorf("Cooldown: got %v, want %v", cb.config.Cooldown, defaultCooldown)
	}
	if cb.config.HalfOpenMax != defaultHalfOpenMax {
		t.Errorf("HalfOpenMax: got %d, want %d", cb.config.HalfOpenMax, defaultHalfOpenMax)
	}
}

// ---------------------------------------------------------------------------
// Closed state
// ---------------------------------------------------------------------------

// TestClosedAllowReturnsTrue verifies that Allow returns true while closed.
func TestClosedAllowReturnsTrue(t *testing.T) {
	cb := New(Config{})
	if !cb.Allow() {
		t.Fatal("Allow(): got false, want true in Closed state")
	}
	if cb.State() != StateClosed {
		t.Errorf("State(): got %v, want %v", cb.State(), StateClosed)
	}
}

// TestRecordSuccessResetsFailureCount verifies that a success after some
// failures resets the counter so the threshold is not accumulated over
// time.
func TestRecordSuccessResetsFailureCount(t *testing.T) {
	cb := New(Config{FailureThreshold: 3, Cooldown: 50 * time.Millisecond, HalfOpenMax: 1})

	// Two failures: not enough to open.
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateClosed {
		t.Fatalf("State(): got %v, want %v", cb.State(), StateClosed)
	}

	// Success resets the counter.
	cb.RecordSuccess()

	// Two more failures: still not enough because counter was reset.
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateClosed {
		t.Fatalf("State(): got %v, want %v after reset", cb.State(), StateClosed)
	}

	// Third failure now opens the breaker.
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatalf("State(): got %v, want %v", cb.State(), StateOpen)
	}
}

// ---------------------------------------------------------------------------
// Closed -> Open
// ---------------------------------------------------------------------------

// TestFailuresOpenBreaker verifies that reaching the failure threshold
// transitions to Open and Allow starts rejecting.
func TestFailuresOpenBreaker(t *testing.T) {
	cb := New(Config{FailureThreshold: 3, Cooldown: 50 * time.Millisecond, HalfOpenMax: 1})

	for i := range 3 {
		if !cb.Allow() {
			t.Fatalf("Allow() #%d: got false, want true", i)
		}
		cb.RecordFailure()
	}
	if cb.State() != StateOpen {
		t.Fatalf("State(): got %v, want %v", cb.State(), StateOpen)
	}
	if cb.Allow() {
		t.Fatal("Allow(): got true, want false in Open state")
	}
}

// TestFailuresBelowThresholdStayClosed verifies that failures below the
// threshold keep the breaker closed.
func TestFailuresBelowThresholdStayClosed(t *testing.T) {
	cb := New(Config{FailureThreshold: 5, Cooldown: 50 * time.Millisecond, HalfOpenMax: 1})
	for range 4 {
		cb.RecordFailure()
	}
	if cb.State() != StateClosed {
		t.Fatalf("State(): got %v, want %v", cb.State(), StateClosed)
	}
}

// ---------------------------------------------------------------------------
// Open -> HalfOpen
// ---------------------------------------------------------------------------

// TestCooldownTransitionsToHalfOpen verifies that after the cooldown
// elapses, Allow transitions to HalfOpen and admits a probe.
func TestCooldownTransitionsToHalfOpen(t *testing.T) {
	cb := New(Config{FailureThreshold: 1, Cooldown: 50 * time.Millisecond, HalfOpenMax: 1})

	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatalf("State(): got %v, want %v", cb.State(), StateOpen)
	}

	// Immediately after, still open.
	if cb.Allow() {
		t.Fatal("Allow() before cooldown: got true, want false")
	}

	time.Sleep(60 * time.Millisecond)

	if !cb.Allow() {
		t.Fatal("Allow() after cooldown: got false, want true")
	}
	if cb.State() != StateHalfOpen {
		t.Fatalf("State(): got %v, want %v", cb.State(), StateHalfOpen)
	}
}

// ---------------------------------------------------------------------------
// HalfOpen -> Closed
// ---------------------------------------------------------------------------

// TestHalfOpenRecordSuccessCloses verifies that a success in HalfOpen
// transitions to Closed and resets the failure counter so the breaker does
// not reopen on a single subsequent failure.
func TestHalfOpenRecordSuccessCloses(t *testing.T) {
	cb := New(Config{FailureThreshold: 3, Cooldown: 50 * time.Millisecond, HalfOpenMax: 1})

	// Open the breaker with 3 failures.
	for range 3 {
		cb.RecordFailure()
	}
	if cb.State() != StateOpen {
		t.Fatalf("State(): got %v, want %v", cb.State(), StateOpen)
	}
	time.Sleep(60 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("Allow(): got false, want true")
	}
	if cb.State() != StateHalfOpen {
		t.Fatalf("State(): got %v, want %v", cb.State(), StateHalfOpen)
	}

	cb.RecordSuccess()
	if cb.State() != StateClosed {
		t.Fatalf("State(): got %v, want %v", cb.State(), StateClosed)
	}
	// Failure counter must be reset so the next failure does not immediately
	// reopen. With threshold 3, two failures should stay closed.
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateClosed {
		t.Fatalf("State() after two failures post-reset: got %v, want %v", cb.State(), StateClosed)
	}
	// Third failure reopens.
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatalf("State() after third failure: got %v, want %v", cb.State(), StateOpen)
	}
}

// ---------------------------------------------------------------------------
// HalfOpen -> Open
// ---------------------------------------------------------------------------

// TestHalfOpenRecordFailureReopens verifies that a failure in HalfOpen
// transitions back to Open.
func TestHalfOpenRecordFailureReopens(t *testing.T) {
	cb := New(Config{FailureThreshold: 1, Cooldown: 50 * time.Millisecond, HalfOpenMax: 1})

	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("Allow(): got false, want true")
	}
	if cb.State() != StateHalfOpen {
		t.Fatalf("State(): got %v, want %v", cb.State(), StateHalfOpen)
	}

	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatalf("State(): got %v, want %v", cb.State(), StateOpen)
	}
	// Allow should reject again until the new cooldown elapses.
	if cb.Allow() {
		t.Fatal("Allow(): got true, want false after reopening")
	}
}

// ---------------------------------------------------------------------------
// HalfOpenMax admission control
// ---------------------------------------------------------------------------

// TestHalfOpenMaxAdmission verifies that HalfOpenMax limits the number of
// probe requests admitted in HalfOpen state.
func TestHalfOpenMaxAdmission(t *testing.T) {
	cb := New(Config{FailureThreshold: 1, Cooldown: 50 * time.Millisecond, HalfOpenMax: 3})

	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)

	// First 3 probes are admitted.
	for i := range 3 {
		if !cb.Allow() {
			t.Fatalf("Allow() #%d: got false, want true", i)
		}
	}
	// 4th probe is rejected without a result recorded.
	if cb.Allow() {
		t.Fatal("Allow() #4: got true, want false (HalfOpenMax exceeded)")
	}
	if cb.State() != StateHalfOpen {
		t.Fatalf("State(): got %v, want %v", cb.State(), StateHalfOpen)
	}
}

// TestHalfOpenMaxResetsOnSuccess verifies that after a success closes the
// breaker, HalfOpenMax probe counter is reset for the next half-open cycle.
func TestHalfOpenMaxResetsOnSuccess(t *testing.T) {
	cb := New(Config{FailureThreshold: 1, Cooldown: 50 * time.Millisecond, HalfOpenMax: 2})

	// First cycle.
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("Allow() #1: got false, want true")
	}
	cb.RecordSuccess()
	if cb.State() != StateClosed {
		t.Fatalf("State(): got %v, want %v", cb.State(), StateClosed)
	}

	// Second cycle: should be able to admit probes again.
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("Allow() cycle 2 #1: got false, want true")
	}
	if !cb.Allow() {
		t.Fatal("Allow() cycle 2 #2: got false, want true")
	}
	if cb.Allow() {
		t.Fatal("Allow() cycle 2 #3: got true, want false (HalfOpenMax exceeded)")
	}
}

// ---------------------------------------------------------------------------
// State() reporting
// ---------------------------------------------------------------------------

// TestStateDoesNotTriggerTransition verifies that State() reports Open
// even when the cooldown has elapsed, without transitioning to HalfOpen.
func TestStateDoesNotTriggerTransition(t *testing.T) {
	cb := New(Config{FailureThreshold: 1, Cooldown: 50 * time.Millisecond, HalfOpenMax: 1})
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)

	// State should still report Open; only Allow triggers the transition.
	if cb.State() != StateOpen {
		t.Fatalf("State(): got %v, want %v (no side effect)", cb.State(), StateOpen)
	}
	if !cb.Allow() {
		t.Fatal("Allow(): got false, want true")
	}
	if cb.State() != StateHalfOpen {
		t.Fatalf("State(): got %v, want %v", cb.State(), StateHalfOpen)
	}
}

// ---------------------------------------------------------------------------
// Concurrency safety
// ---------------------------------------------------------------------------

// TestConcurrentAccess exercises Allow/RecordSuccess/RecordFailure under
// concurrent goroutines to verify no races or panics. Run with -race to
// detect data races.
func TestConcurrentAccess(t *testing.T) {
	cb := New(Config{FailureThreshold: 10, Cooldown: 20 * time.Millisecond, HalfOpenMax: 2})

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 100 {
				if cb.Allow() {
					// Simulate work then record a result. Mix of success/failure.
					if j%3 == 0 {
						cb.RecordFailure()
					} else {
						cb.RecordSuccess()
					}
				}
				_ = cb.State()
			}
		}()
	}
	wg.Wait()
	// No assertion beyond not panicking / not racing; the breaker should end
	// in a valid state.
	_ = cb.State()
}

// TestConcurrentAllowOnly verifies that many concurrent Allow calls do not
// race or panic even without recording results.
func TestConcurrentAllowOnly(t *testing.T) {
	cb := New(Config{FailureThreshold: 1, Cooldown: 10 * time.Millisecond, HalfOpenMax: 1})
	cb.RecordFailure() // open the breaker

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				_ = cb.Allow()
			}
		}()
	}
	wg.Wait()
}
