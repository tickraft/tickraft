// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package scheduler provides the pure timing-scheduling engine for tickraft.
//
// The Engine is a pure timing component: it computes when entries should
// fire based on cron expressions, fixed intervals, one-time, or never
// schedules, invokes the registered Callback when the fire time arrives,
// and reschedules recurring entries automatically. The Engine carries NO
// task business semantics: it does not know about tasks, dependencies,
// concurrency control, or event-driven triggers. Those concerns belong to
// the sibling pkg/task package's Service.
//
// The typical usage pattern is:
//
//	eng, _ := scheduler.NewEngine()
//	eng.Start(ctx)
//	eng.Add(taskID, schedule, callback)
//	// ... when done:
//	eng.Remove(taskID)
//	eng.Stop(ctx)
//
// Key abstractions:
//   - Engine: the pure timing-scheduling engine interface (Add/Remove/Start/Stop).
//   - NoopEngine: a no-op Engine for testing or disabled scheduling.
//   - Schedule: alias for cron.Schedule, determines the next fire time.
//   - Callback: invoked by the Engine when a scheduled entry fires.
//   - ShardManager: distributed entry ownership filtering across engine
//     instances (used by the task.Engine for sharded deployments).
//
// Schedule constructors:
//   - NewConstantIntervalSchedule: fixed-interval recurring schedule.
//   - NewOneTimeSchedule: fires once at a specific time.
//   - NewImmediateSchedule: fires once immediately on the first Next() call.
//   - NewNeverSchedule: never fires (for event-driven entries managed
//     externally).
//
// Concurrency model:
//   - All goroutines are controlled by context.Context. The tick-loop
//     goroutine launched by Start exits when the context is cancelled or
//     Stop is called.
//   - Stop is graceful: it cancels the context, stops the time wheel, and
//     waits for the tick-loop goroutine to fully exit via sync.WaitGroup.
//   - Callback panics are isolated: onFire recovers panics with defer
//     recover() and logs them via zap structured logging. The entry is
//     always rescheduled regardless of whether the callback panicked.
//   - All shared state (entries, scheds, callbacks) is protected by
//     sync.RWMutex.
package scheduler
