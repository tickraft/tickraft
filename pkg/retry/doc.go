// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package retry provides a generic retry mechanism with configurable backoff
// strategies for resilient operation execution.
//
// # Backoff Strategies
//
// The package offers two built-in backoff strategies:
//   - FixedInterval: returns a constant delay between retries.
//   - Exponential: returns an exponentially increasing delay capped by a maximum.
//
// # Exponential Backoff
//
// The Exponential strategy calculates delay as:
//
//	delay = Base * Multiplier^attempt
//
// Parameters:
//   - Base: initial delay, must be > 0. Default: 1s.
//   - Max: maximum delay cap, must be >= Base. Default: 30s.
//   - Multiplier: exponential growth factor, must be >= 1.0. Default: 2.0.
//   - Jitter: optional random perturbation to prevent thundering herd. Default: none.
//
// # Jitter
//
// Jitter randomizes the backoff delay to prevent synchronized retry storms
// in high-concurrency scenarios. The package provides two jitter strategies:
//   - FullJitter: uniformly randomizes the delay in [0, delay).
//   - ProportionalJitter: randomizes the delay by a configurable factor
//     clamped to [0.0, 1.0]. factor=0.0 applies no jitter, factor=1.0 is
//     equivalent to FullJitter, and intermediate values produce partial
//     jitter in [delay*(1-factor), delay). The formula is:
//     delay * (1.0 - factor + factor * rand.Float64()).
//
// # Usage
//
// Basic usage with defaults:
//
//	r, err := retry.New()
//	if err != nil {
//	    // handle error
//	}
//	err = r.Do(ctx, func() error {
//	    return someOperation()
//	})
//
// Custom multiplier (1.5x growth instead of 2x):
//
//	b, err := retry.NewExponential(time.Second, 30*time.Second, retry.WithMultiplier(1.5))
//	if err != nil {
//	    // handle error
//	}
//	r, err := retry.New(retry.WithBackoff(b))
//	if err != nil {
//	    // handle error
//	}
//	err = r.Do(ctx, fn)
//
// With FullJitter to prevent thundering herd:
//
//	b, err := retry.NewExponential(time.Second, 30*time.Second, retry.WithJitter(retry.NewFullJitter()))
//	if err != nil {
//	    // handle error
//	}
//	r, err := retry.New(retry.WithBackoff(b))
//	if err != nil {
//	    // handle error
//	}
//	err = r.Do(ctx, fn)
//
// With ProportionalJitter (factor=0.3 → delay randomized in [70%, 100%) of backoff):
//
//	b, err := retry.NewExponential(time.Second, 30*time.Second, retry.WithJitter(retry.NewProportionalJitter(0.3)))
//	if err != nil {
//	    // handle error
//	}
//	r, err := retry.New(retry.WithBackoff(b))
//	if err != nil {
//	    // handle error
//	}
//	err = r.Do(ctx, fn)
//
// Full customization:
//
//	r, err := retry.New(
//	    retry.WithMaxAttempts(5),
//	    retry.WithBackoff(retry.NewExponential(
//	        time.Second,
//	        30*time.Second,
//	        retry.WithMultiplier(1.5),
//	        retry.WithJitter(retry.NewFullJitter()),
//	    )),
//	    retry.WithRetryable(func(err error) bool {
//	        return !errors.Is(err, ErrPermanent)
//	    }),
//	)
//	if err != nil {
//	    // handle error
//	}
//	err = r.Do(ctx, fn)
package retry
