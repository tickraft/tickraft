// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"time"
)

// Task represents a scheduled task definition for the task management API.
type Task struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Executor    string         `json:"executor"` // executor type: http, tcp, local, webhook, icmp
	Schedule    string         `json:"schedule"` // cron expression or interval
	Enabled     bool           `json:"enabled"`
	Config      map[string]any `json:"config,omitempty"`
	Group       string         `json:"group,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	// RunID is the unique identifier for idempotency control of the task.
	RunID string `json:"run_id,omitempty"`
	// RetryPolicy is the retry strategy: "fixed" or "exponential".
	RetryPolicy string `json:"retry_policy,omitempty"`
	// Concurrency controls per-task concurrent execution
	// (0=unlimited, 1=no concurrent execution).
	Concurrency int       `json:"concurrency,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Filter holds optional filtering criteria for listing tasks. A zero-value
// Filter matches all tasks. Both fields are optional and can be combined.
type Filter struct {
	// Group filters tasks by an exact group match.
	Group string
	// Tags filters tasks to those having at least one of the specified tags.
	Tags []string
}

// Execution represents a single execution record of a task.
type Execution struct {
	ID        int64      `json:"id"`
	TaskID    int64      `json:"task_id"`
	TaskName  string     `json:"task_name,omitempty"`
	// ExecutorType is the executor that produced the record
	// (http, tcp, local, webhook, icmp).
	ExecutorType string `json:"executor_type,omitempty"`
	Status       string `json:"status"` // pending, running, success, failed
	Output       string `json:"output"`
	Error        string `json:"error,omitempty"`
	// StatusCode is the protocol-specific status code (e.g. HTTP status).
	StatusCode int `json:"status_code,omitempty"`
	// Duration is the execution duration in milliseconds.
	Duration   int64      `json:"duration,omitempty"`
	RetryCount int        `json:"retry_count,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// ExecutionFilter holds optional server-side filtering criteria for listing
// executions. A zero-value filter matches all executions.
type ExecutionFilter struct {
	// Status filters by the execution lifecycle status
	// (pending, running, success, failed).
	Status string
	// ExecutorType filters by executor type (http, tcp, ...).
	ExecutorType string
	// TaskName filters by a case-insensitive substring match on the task name.
	TaskName string
}

// ExecutionStats holds aggregated execution statistics for a time range.
// SuccessRate is a percentage (0-100); a zero-total range yields 0.
// AverageDurationMs is the mean execution duration in milliseconds.
type ExecutionStats struct {
	TotalExecutions   int64   `json:"total_executions"`
	SuccessCount      int64   `json:"success_count"`
	FailureCount      int64   `json:"failure_count"`
	SuccessRate       float64 `json:"success_rate"`
	AverageDurationMs float64 `json:"average_duration_ms"`
}

// Service defines the operations for managing scheduled tasks.
type Service interface {
	// ListTasks returns a page of tasks matching the given filter and the total
	// count. A zero-value Filter returns all tasks.
	ListTasks(ctx context.Context, page, size int, filter Filter) ([]Task, int64, error)
	// GetTask returns a single task by ID.
	GetTask(ctx context.Context, id int64) (*Task, error)
	// CreateTask creates a new task from the given request.
	CreateTask(ctx context.Context, req *Task) (*Task, error)
	// UpdateTask updates an existing task identified by ID.
	UpdateTask(ctx context.Context, id int64, req *Task) (*Task, error)
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
	ListExecutions(ctx context.Context, taskID int64, page, size int, filter ExecutionFilter) ([]Execution, int64, error)
	// GetExecution returns a single execution record by ID. A positive taskID
	// additionally requires the record to belong to that task.
	GetExecution(ctx context.Context, taskID, id int64) (*Execution, error)
	// CopyTask creates a new task by cloning the configuration of an existing
	// task identified by id. The new task is assigned a fresh ID and the given
	// name; an empty name defaults to "<source name> (copy)". The source task
	// is not modified. Returns ErrTaskNotFound when the source ID does not
	// exist.
	CopyTask(ctx context.Context, id int64, newName string) (*Task, error)
	// GetExecutionStats returns aggregated execution statistics for the given
	// time range (inclusive on both ends).
	GetExecutionStats(ctx context.Context, from, to time.Time) (ExecutionStats, error)
}
