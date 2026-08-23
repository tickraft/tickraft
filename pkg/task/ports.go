// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"time"
)

// ListOptions holds optional filtering criteria for listing tasks. A zero-value
// ListOptions returns all tasks (no filtering). Both fields are optional and
// can be combined: when both Group and Tags are set, only tasks matching the
// group AND having at least one of the requested tags are returned.
type ListOptions struct {
	// Group filters tasks by an exact group match. An empty string matches all.
	Group string
	// Tags filters tasks to those having at least one of the specified tags.
	// An empty or nil slice matches all tasks.
	Tags []string
}

// Store persists scheduler tasks across restarts.
// Implementations must be safe for concurrent use.
// A nil store indicates that persistence is disabled (in-memory only).
type Store interface {
	// Save creates or updates a task in the persistent store.
	Save(ctx context.Context, task *Task) error
	// Get retrieves a task by its ID.
	Get(ctx context.Context, id int64) (*Task, error)
	// List returns tasks matching the given options. A zero-value ListOptions
	// returns all tasks with no filtering.
	List(ctx context.Context, opts ListOptions) ([]*Task, error)
	// Delete removes a task by its ID.
	Delete(ctx context.Context, id int64) error
	// Migrate creates or updates the sys_schedule_task table schema.
	Migrate(ctx context.Context) error
}

// ExecutionStatsResult holds aggregated execution statistics for a time range.
// All counts are scoped to executions whose created_at falls within [from, to].
// SuccessRate is expressed as a percentage (0-100); a zero-total range yields 0.
// AverageDurationMs is the mean execution duration in milliseconds.
type ExecutionStatsResult struct {
	TotalExecutions   int64   `json:"total_executions"`
	SuccessCount      int64   `json:"success_count"`
	FailureCount      int64   `json:"failure_count"`
	SuccessRate       float64 `json:"success_rate"`
	AverageDurationMs float64 `json:"average_duration_ms"`
}

// DailyStat holds one day's aggregated execution counts, keyed by the
// calendar date of created_at (server-local, YYYY-MM-DD form). Days
// without executions are absent from store results; callers zero-fill
// gaps when a contiguous series is required (trend charts).
type DailyStat struct {
	Date    string `json:"date"`
	Total   int64  `json:"total"`
	Success int64  `json:"success"`
	Failed  int64  `json:"failed"`
}

// ExecutionQuery holds optional filtering criteria for querying execution
// history. A zero-value query matches all executions.
type ExecutionQuery struct {
	// TaskID restricts the result to a single task; values <= 0 match all
	// tasks.
	TaskID int64
	// TaskIDs restricts the result to the given task IDs. When non-nil, an
	// empty slice matches nothing (used when a task-name search yields no
	// tasks). Ignored when nil.
	TaskIDs []int64
	// Status filters by the persisted execution status ("success",
	// "failed", "running", "unknown"); empty matches all.
	Status string
	// ExecutorType filters by executor type; empty matches all.
	ExecutorType string
	// TriggerType filters by the trigger source ("schedule", "manual",
	// "event"); empty matches all.
	TriggerType string
}

// ExecutionStore persists task execution history.
// Implementations must be safe for concurrent use.
type ExecutionStore interface {
	// Save records a single execution in the persistent store.
	Save(ctx context.Context, exec *Execution) error
	// List returns execution history for the given task ID, ordered by
	// most recent first, limited to at most limit records. A non-positive
	// limit returns all records for the task.
	List(ctx context.Context, taskID int64, limit int) ([]*Execution, error)
	// Query returns a page of executions matching the filter, ordered by
	// most recent first, along with the total count of matching rows.
	Query(ctx context.Context, q ExecutionQuery, page, size int) ([]*Execution, int64, error)
	// Get retrieves a single execution record by its ID.
	Get(ctx context.Context, id int64) (*Execution, error)
	// DeleteExecutionsOlderThan removes all execution records whose
	// created_at timestamp is strictly before the given time. This is used
	// for retention-based cleanup of stale execution history.
	DeleteExecutionsOlderThan(ctx context.Context, before time.Time) error
	// Stats returns aggregated execution statistics for the given time
	// range (inclusive on both ends). A positive taskID scopes the
	// aggregation to that task's executions; zero aggregates across all
	// tasks. A range with no executions returns a zero-valued result.
	// SuccessCount counts executions whose status indicates success;
	// FailureCount counts those indicating failure; non-terminal statuses
	// are included in TotalExecutions but not in either count.
	Stats(ctx context.Context, from, to time.Time, taskID int64) (ExecutionStatsResult, error)
	// StatsByDay returns per-day aggregates for the given time range
	// (inclusive), grouped by the server-local calendar date of created_at
	// and ordered by date. A positive taskID scopes the aggregation to
	// that task; zero aggregates across all tasks. Days without
	// executions are omitted from the result.
	StatsByDay(ctx context.Context, from, to time.Time, taskID int64) ([]DailyStat, error)
	// Migrate creates or updates the sys_schedule_log table schema.
	Migrate(ctx context.Context) error
}
