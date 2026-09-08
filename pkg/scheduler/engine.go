// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package scheduler

import "context"

// Engine is the pure timing-scheduling engine. It does NOT carry any task
// business semantics. Business modules (pkg/task, pkg/telemetry) own an
// Engine instance and call Add/Remove to manage timed callbacks.
//
// The Engine is responsible only for:
//   - Computing the next fire time from a Schedule.
//   - Invoking the Callback when the fire time arrives.
//   - Rescheduling recurring entries automatically.
//
// It does NOT know about tasks, dependencies, concurrency control, or
// event-driven triggers; those concerns belong to the task.TaskEngine.
type Engine interface {
	// Add registers a timed callback under the given id. The engine
	// computes the next fire time from schedule, invokes callback when
	// it fires, and reschedules automatically for recurring schedules.
	// If an entry with the same id already exists it is replaced.
	Add(id int64, schedule Schedule, callback Callback) error

	// Remove removes the timed callback associated with id. No-op if
	// the id is not registered.
	Remove(id int64) error

	// Start begins the engine's tick loop. The engine must be started
	// before Add can fire callbacks. Start is idempotent: calling it on
	// an already-running engine is a no-op. The context controls the
	// lifetime of the internal tick loop.
	Start(ctx context.Context) error

	// Stop gracefully stops the engine, cancelling the tick loop and
	// waiting for in-flight callbacks to complete. Stop is idempotent.
	Stop(ctx context.Context) error
}

// Callback is invoked by the Engine when a scheduled entry fires. The id
// passed to the callback is the same id provided to Add, allowing the
// caller to look up the associated business entity (e.g., a task).
type Callback func(id int64)
