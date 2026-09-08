// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package pool

import (
	"fmt"
	"runtime"
	"time"
)

// ioStallThreshold is the queue-saturation threshold for IO-bound
// pools. IO jobs routinely block for seconds (network, disk), so a
// five-minute threshold avoids false positives while still catching
// sustained saturation.
const ioStallThreshold = 5 * time.Minute

// defaultSize returns n when positive, otherwise runtime.NumCPU. It
// guarantees a non-zero worker count so the derived queue size is
// always positive.
func defaultSize(n int) int {
	if n <= 0 {
		return runtime.NumCPU()
	}
	return n
}

// NewIOPool returns a [Pool] tuned for IO-bound work: a deeper queue
// (size*8) to absorb latency variance, the [RejectionCallerRuns]
// policy so submitters throttle themselves instead of blocking or
// dropping work, and stall detection with a five-minute threshold. The
// stall handler is left nil; callers that need to react to stalls
// should construct the pool with [New] and
// [WithStallDetection] directly.
//
// When size is non-positive it defaults to [runtime.NumCPU]. The
// returned pool is already started and must be released with
// [Pool.Shutdown].
//
// Returns an error if the pool cannot be initialized. This path is
// unreachable in practice because size is sanitized to a positive
// value, but the error is returned rather than panicking to honor
// the "no panic in domain logic" rule.
func NewIOPool(size int) (Pool, error) {
	size = defaultSize(size)
	p, err := New(
		WithWorkers(size),
		WithQueueSize(size*8),
		WithRejectionPolicy(RejectionCallerRuns),
		WithStallDetection(ioStallThreshold, nil),
	)
	if err != nil {
		// Unreachable in practice: size is sanitized to >= 1 and
		// queueSize is size*8, both strictly positive. Returned as an
		// error (not panic) to honor the "no panic in domain
		// logic" rule.
		return nil, fmt.Errorf("pool: create IO pool: %w", err)
	}
	return p, nil
}
