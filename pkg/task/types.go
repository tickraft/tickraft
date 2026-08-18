// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"time"

	"github.com/tickraft/tickraft/pkg/executor"
)

// ScheduleType defines the type of a task schedule.
type ScheduleType string

const (
	// ScheduleTypeCron is a cron-expression-based schedule.
	ScheduleTypeCron ScheduleType = "cron"
	// ScheduleTypeInterval is a fixed-interval schedule.
	ScheduleTypeInterval ScheduleType = "interval"
	// ScheduleTypeOnce is a one-time schedule.
	ScheduleTypeOnce ScheduleType = "once"
	// ScheduleTypeEvent is an event-driven schedule.
	ScheduleTypeEvent ScheduleType = "event"
)

// TriggerType identifies how a task execution was initiated.
type TriggerType string

const (
	// TriggerTypeSchedule indicates the execution was initiated by the
	// scheduling engine (the time wheel fired).
	TriggerTypeSchedule TriggerType = "schedule"
	// TriggerTypeManual indicates the execution was initiated by a manual
	// API call (POST /tasks/:id/trigger).
	TriggerTypeManual TriggerType = "manual"
	// TriggerTypeEvent indicates the execution was initiated by an
	// event-driven status change.
	TriggerTypeEvent TriggerType = "event"
)

// MetadataKeyEnabled is the Task.Metadata key that persists the task's
// enabled flag.
//
// It is a storage compatibility contract: the scheduler writes it on
// Pause/Resume and the API converter layer writes it on every task
// create/update round trip. Renaming the key would orphan the flag on
// tasks already persisted in sys_schedule_task, so the value must never
// change.
const MetadataKeyEnabled = "enabled"

// Persisted execution status values stored in the sys_schedule_log.status
// column and carried on Execution.Status.
//
// These strings are a storage compatibility contract: rows already written
// to the database must keep resolving to the same semantics, so the values
// must never change. The scheduler records StatusTriggered when an
// execution is dispatched; terminal outcomes are recorded as StatusNormal
// (success), StatusAbnormal (failure), or StatusUnknown.
//
// These are the task execution domain's own statuses. They intentionally
// coincide with pkg/types.AssetStatus values for "normal"/"abnormal"/
// "unknown" but are not the same concept and must not be mixed.
const (
	// StatusNormal marks an execution that completed successfully.
	StatusNormal = "normal"
	// StatusAbnormal marks an execution that completed with a failure.
	StatusAbnormal = "abnormal"
	// StatusUnknown marks an execution whose outcome could not be
	// determined.
	StatusUnknown = "unknown"
	// StatusTriggered marks an execution that has been dispatched but has
	// not yet reached a terminal state.
	StatusTriggered = "triggered"
)

// Lifecycle status values exposed by the HTTP API execution contract.
// They are the user-facing counterpart of the persisted Status* values
// above and are translated by ToLifecycleStatus / ToStoredStatus.
const (
	// LifecycleSuccess is the API lifecycle status for executions that
	// completed successfully.
	LifecycleSuccess = "success"
	// LifecycleFailed is the API lifecycle status for executions that
	// completed with a failure.
	LifecycleFailed = "failed"
	// LifecycleRunning is the API lifecycle status for executions that
	// have been triggered but have not yet reached a terminal state.
	LifecycleRunning = "running"
)

// ToLifecycleStatus maps a persisted execution status to the API contract
// lifecycle status. Unknown values pass through unchanged so callers
// surfacing custom stored statuses keep working.
func ToLifecycleStatus(stored string) string {
	switch stored {
	case StatusNormal:
		return LifecycleSuccess
	case StatusAbnormal, StatusUnknown:
		return LifecycleFailed
	case StatusTriggered:
		return LifecycleRunning
	default:
		return stored
	}
}

// ToStoredStatus is the inverse of ToLifecycleStatus: it maps an API
// contract lifecycle status to the persisted execution status value.
// Unknown values pass through so callers filtering by raw stored values
// keep working.
func ToStoredStatus(lifecycle string) string {
	switch lifecycle {
	case LifecycleSuccess:
		return StatusNormal
	case LifecycleFailed:
		return StatusAbnormal
	case LifecycleRunning:
		return StatusTriggered
	default:
		return lifecycle
	}
}

// Task represents the scheduler's view of a task for executor consumption.
type Task struct {
	// ID is the unique task identifier.
	ID int64 `json:"id"`
	// TenantID is the tenant identifier for multi-tenancy isolation.
	// The runtime is single-tenant: this field is always 0.
	// The runtime injects the actual tenant ID via the store layer.
	TenantID int64 `json:"tenant_id"`
	// AssetID is the associated asset identifier.
	AssetID int64 `json:"asset_id"`
	// ExecutorName identifies which executor to use.
	ExecutorName string `json:"executor_name"`
	// Config stores executor-specific configuration as JSON.
	Config string `json:"config"`
	// Operation specifies the operation type (probe or execute).
	// Defaults to OpExecute when not set (zero value).
	Operation executor.Operation `json:"operation,omitempty"`
	// Timeout is the maximum execution duration.
	Timeout time.Duration `json:"timeout"`
	// Priority controls execution order when multiple tasks fire simultaneously.
	Priority int `json:"priority"`
	// DependsOn is the task ID that must succeed before this task can run.
	DependsOn int64 `json:"depends_on"`
	// Metadata holds optional key-value extension data.
	Metadata map[string]string `json:"metadata,omitempty"`
	// Group is an optional logical grouping label for the task, used for
	// filtering and organization. Stored as a single string value.
	Group string `json:"group,omitempty"`
	// Tags is an optional list of arbitrary labels for categorizing the task.
	// Stored as a comma-separated string in the database and split into a
	// slice in the domain type.
	Tags []string `json:"tags,omitempty"`
	// RunID is the unique identifier for idempotency control of the task.
	RunID string `json:"run_id,omitempty"`
	// RetryPolicy is the retry strategy: "fixed" or "exponential".
	RetryPolicy string `json:"retry_policy,omitempty"`
	// Concurrency controls per-task concurrent execution
	// (0=unlimited, 1=no concurrent execution).
	Concurrency int `json:"concurrency,omitempty"`
}

// Execution represents a single execution history record of a scheduled task.
// It is the domain counterpart of ScheduleLog and is used by ExecutionStore to
// persist and retrieve execution history across restarts.
type Execution struct {
	// ID is the unique execution record identifier (auto-increment).
	ID int64 `json:"id"`
	// TaskID is the associated task identifier.
	TaskID int64 `json:"task_id"`
	// TenantID is the tenant identifier for multi-tenancy isolation.
	// The runtime is single-tenant: this field is always 0.
	// The runtime injects the actual tenant ID via the store layer.
	TenantID int64 `json:"tenant_id"`
	// AssetID is the associated asset identifier.
	AssetID int64 `json:"asset_id"`
	// ExecutorName identifies which executor was used.
	ExecutorName string `json:"executor_name"`
	// Operation records the operation type (probe or execute).
	Operation executor.Operation `json:"operation,omitempty"`
	// Status is the persisted execution outcome; see the Status*
	// constants for the recognized values.
	Status string `json:"status"`
	// StatusCode is the numeric status code returned by the executor.
	StatusCode int `json:"status_code"`
	// Output is the raw executor output.
	Output string `json:"output,omitempty"`
	// Error is the error message if the execution failed.
	Error string `json:"error,omitempty"`
	// Duration is the execution duration in milliseconds.
	Duration int64 `json:"duration"`
	// RetryCount is the number of retries attempted.
	RetryCount int `json:"retry_count"`
	// StartedAt is when the execution began.
	StartedAt time.Time `json:"started_at"`
	// FinishedAt is when the execution completed (zero if not finished).
	FinishedAt time.Time `json:"finished_at,omitempty"`
	// RunID links to the task run for idempotency tracking.
	RunID string `json:"run_id,omitempty"`
	// TriggerType records how the execution was triggered:
	// "schedule", "manual", or "event".
	TriggerType string `json:"trigger_type,omitempty"`
	// SkipReason records why the execution was skipped.
	SkipReason string `json:"skip_reason,omitempty"`
	// Metrics stores execution metrics as JSON.
	Metrics string `json:"metrics,omitempty"`
}
