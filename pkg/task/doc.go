// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package task implements the task scheduling business module for tickraft.
//
// It owns the task lifecycle (Register/Update/Unschedule/Pause/Resume),
// dependency tracking, per-task concurrency control, event-driven triggers,
// and execution history. The Engine holds a scheduler.Engine instance
// and registers timed callbacks via Engine.Add/Remove; when a callback fires,
// the Engine performs dependency checks, concurrency control, and publishes
// ExecutionTriggered events on the event bus for the executor to consume.
//
// The actual execution is handled by the sibling pkg/executor package's
// Runner, which subscribes to ExecutionTriggered events and publishes
// event.TypeExecutionCompleted events when execution finishes. The Engine
// subscribes to ExecutionCompleted to update its internal dependency tracker and
// to StatusChange events to trigger event-driven tasks.
//
// Key abstractions:
//   - TaskEngine: the task scheduling engine contract.
//   - Engine: the core implementation, holding a scheduler.Engine.
//   - Task / Execution: the single dual-tag models (GORM + wire) for the
//     sys_schedule_task and sys_schedule_execution tables.
//   - Store / ExecutionStore: persistence SPIs for tasks and history.
package task
