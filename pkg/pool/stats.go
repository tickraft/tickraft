// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package pool

import (
	"fmt"
	"sync/atomic"
)

// Stats is an instantaneous snapshot of pool runtime counters.
//
// All fields are monotonically non-decreasing except Pending, Active,
// and Workers, which describe the current in-flight state at the
// moment [Pool.Stats] was called. Values may be slightly stale by the
// time the caller inspects them and must not be relied upon for exact
// synchronization; they are intended for observability and monitoring.
type Stats struct {
	// Submitted is the total number of jobs accepted by the pool
	// since it was created, including those executed via the
	// RejectionCallerRuns policy.
	Submitted int64
	// Completed is the total number of jobs that finished
	// successfully.
	Completed int64
	// Failed is the total number of jobs that finished with a
	// non-nil error or that panicked.
	Failed int64
	// Discarded is the total number of jobs dropped by the
	// RejectionDiscardOldest policy.
	Discarded int64
	// Pending is the number of jobs currently waiting in the queue
	// to be picked up by a worker.
	Pending int64
	// Active is the number of workers currently executing a job.
	Active int64
	// Workers is the number of worker goroutines currently running,
	// including the warmup workers started by [New] and any lazily
	// grown workers.
	Workers int64
}

// String formats the snapshot as a single-line, key=value record
// suitable for structured log output. It implements the [fmt.Stringer]
// interface so a [Stats] value can be passed directly to zap.Stringer
// or zap.Any without an explicit fmt.Sprintf at the call site.
func (s Stats) String() string {
	return fmt.Sprintf("submitted=%d completed=%d failed=%d discarded=%d pending=%d active=%d workers=%d",
		s.Submitted, s.Completed, s.Failed, s.Discarded, s.Pending, s.Active, s.Workers)
}

// stats holds the atomic counters backing a [Stats] snapshot.
//
// Each counter is an independent [atomic.Int64] so that snapshot and
// increment operations are lock-free and safe for concurrent use by
// workers, submitters, and shutdown. The Workers counter is maintained
// on the pool struct because it tracks goroutine lifecycle rather than
// job flow.
type stats struct {
	submitted atomic.Int64
	completed atomic.Int64
	failed    atomic.Int64
	discarded atomic.Int64
	pending   atomic.Int64
	active    atomic.Int64
}

// snapshot returns an instantaneous [Stats] view of the counters.
//
// Because the counters are read independently, the returned struct is
// not a transactionally consistent point-in-time view; it is suitable
// for monitoring but not for synchronization.
func (s *stats) snapshot() Stats {
	return Stats{
		Submitted: s.submitted.Load(),
		Completed: s.completed.Load(),
		Failed:    s.failed.Load(),
		Discarded: s.discarded.Load(),
		Pending:   s.pending.Load(),
		Active:    s.active.Load(),
	}
}

// incSubmitted records a newly accepted job.
func (s *stats) incSubmitted() { s.submitted.Add(1) }

// incCompleted records a job that finished successfully.
func (s *stats) incCompleted() { s.completed.Add(1) }

// incFailed records a job that finished with an error or a panic.
func (s *stats) incFailed() { s.failed.Add(1) }

// incDiscarded records a job dropped by RejectionDiscardOldest.
func (s *stats) incDiscarded() { s.discarded.Add(1) }

// incPending records a job that has entered the bounded queue.
func (s *stats) incPending() { s.pending.Add(1) }

// decPending records that a job has left the queue and is about to be
// executed.
func (s *stats) decPending() { s.pending.Add(-1) }

// incActive records that a worker has started executing a job.
func (s *stats) incActive() { s.active.Add(1) }

// decActive records that a worker has finished executing a job.
func (s *stats) decActive() { s.active.Add(-1) }
