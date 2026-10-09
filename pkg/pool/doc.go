// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package pool provides a general-purpose goroutine pool that executes
// [Job] values through a bounded queue with a configurable number of
// worker goroutines.
//
// The pool supports lazy worker growth (warmup plus on-demand
// expansion), pluggable [RejectionPolicy] values for queue
// saturation, and optional stall detection for queues that stay full
// for too long. All operations are safe for concurrent use and driven
// by [context.Context] for cancellation.
//
// Jobs are submitted via [Pool.Submit]. A job is any type implementing
// the [Job] interface; [Lambda] adapts a plain func(ctx) error into a
// Job. Returned errors are reported through an optional
// [ErrorHandler]; recovered panics through an optional [PanicHandler].
// In both cases the original [Job] instance is forwarded to the
// handler so callers can identify the task by type assertion or
// attached metadata, retry resubmission, or attach logging tags.
//
// The package has zero external dependencies and relies exclusively on
// the Go standard library.
//
// A minimal usage example:
//
//	p, err := pool.New()
//	if err != nil {
//	    return err
//	}
//	defer p.Shutdown(context.Background())
//
//	err = p.Submit(ctx, pool.Lambda(func(ctx context.Context) error {
//	    // do work
//	    return nil
//	}))
package pool
