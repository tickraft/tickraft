// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"time"

	"github.com/tickraft/tickraft/pkg/task"
)

// Filter holds optional filtering criteria for listing tasks. A zero-value
// Filter matches all tasks. Both fields are optional and can be combined.
type Filter struct {
	// Group filters tasks by an exact group match.
	Group string
	// Tags filters tasks to those having at least one of the specified tags.
	Tags []string
}

// ExecutionFilter holds optional server-side filtering criteria for listing
// executions. A zero-value filter matches all executions.
type ExecutionFilter struct {
	// Status filters by the execution status (success, failed, running,
	// unknown).
	Status string
	// ExecutorType filters by executor type (http, tcp, ...).
	ExecutorType string
	// TaskName filters by a case-insensitive substring match on the task name.
	TaskName string
	// TriggerType filters by the trigger source (schedule, manual, event).
	TriggerType string
}

// ExecutionStats holds aggregated execution statistics for a time range.
// SuccessRate is a percentage (0-100); a zero-total range yields 0.
// AverageDurationMs is the mean execution duration in milliseconds.
// Daily is present only when the request asked for a days-bucketed series.
type ExecutionStats struct {
	TotalExecutions   int64            `json:"total_executions"`
	SuccessCount      int64            `json:"success_count"`
	FailureCount      int64            `json:"failure_count"`
	SuccessRate       float64          `json:"success_rate"`
	AverageDurationMs float64          `json:"average_duration_ms"`
	Daily             []task.DailyStat `json:"daily,omitempty"`
}

// Service defines the operations for managing scheduled tasks.
//
// The model types (task.Task / task.Execution) are shared across the wire,
// engine, and persistence layers; request bodies bind directly onto them.
// Fields the API must not touch carry json:"-" on the model, which the
// binder honors, so there is no separate wire type to convert through.
type Service interface {
	// ListTasks returns a page of tasks matching the given filter and the total
	// count. A zero-value Filter returns all tasks.
	ListTasks(ctx context.Context, page, size int, filter Filter) ([]*task.Task, int64, error)
	// GetTask returns a single task by ID.
	GetTask(ctx context.Context, id int64) (*task.Task, error)
	// CreateTask creates a new task from the given request.
	CreateTask(ctx context.Context, req *task.Task) (*task.Task, error)
	// UpdateTask updates an existing task identified by ID.
	UpdateTask(ctx context.Context, id int64, req *task.Task) (*task.Task, error)
	// DeleteTask deletes a task by ID.
	DeleteTask(ctx context.Context, id int64) error
	// TriggerTask triggers an immediate execution of a task.
	TriggerTask(ctx context.Context, id int64) error
	// PauseTask pauses a task by removing it from the scheduling wheel.
	// The task configuration is preserved and can be resumed via ResumeTask.
	PauseTask(ctx context.Context, id int64) error
	// ResumeTask resumes a paused task by re-adding it to the scheduling wheel.
	ResumeTask(ctx context.Context, id int64) error
	// ListExecutions returns a page of executions matching the filter and the
	// total count. taskID <= 0 matches executions of all tasks.
	ListExecutions(ctx context.Context, taskID int64, page, size int,
		filter ExecutionFilter) ([]*task.Execution, int64, error)
	// GetExecution returns a single execution record by ID. A positive taskID
	// additionally requires the record to belong to that task.
	GetExecution(ctx context.Context, taskID, id int64) (*task.Execution, error)
	// CopyTask creates a new task by cloning the configuration of an existing
	// task identified by id. The new task is assigned a fresh ID and the given
	// name; an empty name defaults to "<source name> (copy)". The source task
	// is not modified. Returns ErrTaskNotFound when the source ID does not
	// exist.
	CopyTask(ctx context.Context, id int64, newName string) (*task.Task, error)
	// GetExecutionStats returns aggregated execution statistics for the given
	// time range (inclusive on both ends). A positive taskID scopes the
	// aggregation to that task's executions; zero aggregates across all
	// tasks. A positive days switches the range to the last N UTC days and
	// additionally returns a zero-filled per-day series in Daily.
	GetExecutionStats(ctx context.Context, from, to time.Time, taskID int64, days int) (ExecutionStats, error)
}
