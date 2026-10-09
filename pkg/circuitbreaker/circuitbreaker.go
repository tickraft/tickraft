// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package circuitbreaker implements a three-state circuit breaker used by
// notification channels to avoid hammering a degraded endpoint.
//
// The breaker transitions through three states:
//
//   - Closed: requests are allowed. Consecutive failures are counted; when
//     the count reaches FailureThreshold the breaker opens.
//   - Open: requests are rejected. After Cooldown elapses, the next Allow
//     transitions the breaker to HalfOpen and admits a probe request.
//   - HalfOpen: a limited number of probe requests (HalfOpenMax) are
//     admitted. A success closes the breaker; a failure reopens it.
//
// All methods are safe for concurrent use.
//
// # Context handling
//
// The breaker API intentionally does not accept a context.Context. All
// operations (Allow, RecordSuccess, RecordFailure, State) are non-blocking
// state mutations guarded by a mutex, so there is no I/O to cancel and no
// wait to interrupt. Callers are expected to honour context cancellation
// around the actual work (e.g. the HTTP call or SMTP delivery) and only
// invoke RecordSuccess/RecordFailure when the result is genuinely tied to
// the endpoint health, not to a client-initiated cancellation. The channel
// implementations in pkg/prism/channel follow this contract: they skip
// RecordFailure when the error is context.Canceled or
// context.DeadlineExceeded.
package circuitbreaker

import (
	"sync"
	"time"
)

// State enumerates the circuit breaker states.
type State int

const (
	// StateClosed allows all requests through.
	StateClosed State = iota
	// StateOpen rejects all requests until the cooldown elapses.
	StateOpen
	// StateHalfOpen admits a limited number of probe requests.
	StateHalfOpen
)

// String returns a human-readable representation of the state, suitable
// for logging and metrics labels.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// Default configuration values applied by New when the corresponding
// Config field is zero or negative.
const (
	defaultFailureThreshold = 5
	defaultCooldown         = 30 * time.Second
	defaultHalfOpenMax      = 1
)

// Config configures a CircuitBreaker.
type Config struct {
	// FailureThreshold is the number of consecutive failures that opens
	// the breaker. Defaults to 5 when non-positive.
	FailureThreshold int
	// Cooldown is how long the breaker stays open before transitioning
	// to half-open on the next Allow. Defaults to 30s when non-positive.
	Cooldown time.Duration
	// HalfOpenMax is the maximum number of probe requests admitted while
	// in half-open state. Defaults to 1 when non-positive.
	HalfOpenMax int
}

// CircuitBreaker is a three-state circuit breaker.
//
// Callers must pair a successful Allow with exactly one of RecordSuccess or
// RecordFailure. Recording a result without a prior Allow is also safe and
// is treated as a best-effort signal.
type CircuitBreaker struct {
	mu              sync.Mutex
	config          Config
	state           State
	failureCount    int
	lastFailureTime time.Time
	halfOpenPassed  int
}

// New creates a CircuitBreaker with the given configuration. Zero or
// negative Config fields are replaced with sensible defaults.
func New(cfg Config) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = defaultFailureThreshold
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = defaultCooldown
	}
	if cfg.HalfOpenMax <= 0 {
		cfg.HalfOpenMax = defaultHalfOpenMax
	}
	return &CircuitBreaker{
		config: cfg,
		state:  StateClosed,
	}
}

// Allow reports whether a request should be admitted.
//
// In Closed state it always returns true. In Open state it returns false
// until Cooldown has elapsed since the last failure, at which point it
// transitions to HalfOpen and returns true. In HalfOpen state it admits up
// to HalfOpenMax probe requests; further requests are rejected until a
// result is recorded.
//
// When Allow returns true the caller must eventually call RecordSuccess or
// RecordFailure.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.lastFailureTime) >= cb.config.Cooldown {
			cb.state = StateHalfOpen
			cb.halfOpenPassed = 1
			return true
		}
		return false
	case StateHalfOpen:
		if cb.halfOpenPassed < cb.config.HalfOpenMax {
			cb.halfOpenPassed++
			return true
		}
		return false
	}
	return false
}

// RecordSuccess records a successful request. It resets the failure
// counter and closes the breaker regardless of the previous state.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failureCount = 0
	cb.halfOpenPassed = 0
	cb.state = StateClosed
}

// RecordFailure records a failed request. In HalfOpen state it immediately
// reopens the breaker. In Closed state it increments the failure counter
// and opens the breaker once the threshold is reached. In Open state it
// refreshes lastFailureTime to extend the cooldown.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	now := time.Now()
	cb.lastFailureTime = now
	switch cb.state {
	case StateHalfOpen:
		cb.state = StateOpen
		cb.failureCount = cb.config.FailureThreshold
		cb.halfOpenPassed = 0
	case StateClosed:
		cb.failureCount++
		if cb.failureCount >= cb.config.FailureThreshold {
			cb.state = StateOpen
			cb.halfOpenPassed = 0
		}
	case StateOpen:
		// Already open; lastFailureTime refreshed above to extend cooldown.
	}
}

// State returns the current breaker state. The state is not recalculated
// against the cooldown, so an Open breaker whose cooldown has elapsed will
// still report Open until the next Allow call triggers the transition to
// HalfOpen. This method is intended for testing and logging only.
func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}
