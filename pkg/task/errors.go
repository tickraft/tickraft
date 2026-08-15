// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import "errors"

var (
	// ErrTaskNotFound is returned when the referenced task does not exist.
	ErrTaskNotFound = errors.New("task: task not found")
	// ErrExecutionNotFound is returned when the referenced execution record
	// does not exist.
	ErrExecutionNotFound = errors.New("task: execution not found")
	// ErrTaskAlreadyPaused is returned when Pause is called on a task that
	// is already paused (not scheduled on the time wheel).
	ErrTaskAlreadyPaused = errors.New("task: task already paused")
	// ErrTaskNotPaused is returned when Resume is called on a task that is
	// not paused (already scheduled on the time wheel).
	ErrTaskNotPaused = errors.New("task: task not paused")
	// ErrTaskRunning is returned (or used as a skip reason) when a task
	// execution is skipped because a previous execution is still running
	// and the task's Concurrency is set to 1 (no concurrent execution).
	ErrTaskRunning = errors.New("task: previous execution still running")
	// ErrDependencyNotMet is returned (or logged) when a task's upstream
	// dependency has not yet completed with StatusNormal. The Manager's
	// onFire path uses this sentinel when recording the skip reason so
	// that callers and observability tooling can distinguish "dependency
	// not met" from a generic execution failure. The task remains
	// scheduled and will be retried on the next fire time; callers that
	// want to surface the condition to API clients (e.g. translate to
	// HTTP 422/409) can match against this sentinel via errors.Is.
	ErrDependencyNotMet = errors.New("task: dependency not met")
	// ErrIntervalTooSmall is returned when an interval-based schedule has
	// an interval shorter than the configured minimum. Callers (API layer,
	// worker bootstrap) can match against this sentinel via errors.Is to
	// translate it into an appropriate HTTP error (e.g. 400 Bad Request).
	ErrIntervalTooSmall = errors.New("task: schedule interval is smaller than the minimum allowed")
)
