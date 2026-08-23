// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"time"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/types"

	// Register the GORM serializers (tolerantjson, commalist) used by the
	// model tags in this package.
	_ "github.com/tickraft/tickraft/pkg/db"
)

// ScheduleType defines the type of a task schedule.
type ScheduleType string

const (
	// ScheduleTypeCron is a cron-expression-based schedule.
	ScheduleTypeCron ScheduleType = "cron"
	// ScheduleTypeInterval is a fixed-interval schedule.
	ScheduleTypeInterval ScheduleType = "interval"
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

// Persisted execution status values, stored in the sys_schedule_log.status
// column and carried on Execution.Status. The storage vocabulary is the API
// vocabulary: success, failed, timeout, running, unknown.
const (
	// StatusSuccess marks an execution that completed successfully.
	StatusSuccess = "success"
	// StatusFailed marks an execution that completed with a failure.
	StatusFailed = "failed"
	// StatusTimeout marks an execution that ended because its deadline
	// expired. Distinct from failed so UIs can surface it separately.
	StatusTimeout = "timeout"
	// StatusRunning marks an execution that has been dispatched but has
	// not yet reached a terminal state.
	StatusRunning = "running"
	// StatusUnknown marks an execution whose outcome could not be
	// determined.
	StatusUnknown = "unknown"
)

// ExecutionStatusFromAsset maps an asset status — the vocabulary used by
// executor results and completion events — to the persisted execution
// status. This is the single bridge between the two vocabularies; the
// execution domain never stores asset status values directly.
func ExecutionStatusFromAsset(status types.AssetStatus) string {
	switch status {
	case types.AssetStatusNormal:
		return StatusSuccess
	case types.AssetStatusAbnormal:
		return StatusFailed
	default:
		return StatusUnknown
	}
}

// Task is the single model for a scheduled task: it is the GORM model for
// the sys_schedule_task table, the scheduling engine's runtime view, and
// the HTTP wire representation at once. JSON tags define the API contract;
// fields internal to the engine carry json:"-" so they can never appear in
// (or be set from) request bodies.
type Task struct {
	// ID is the unique task identifier, assigned by the task service at
	// creation time.
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// TenantID is the tenant identifier for multi-tenancy isolation.
	// The runtime is single-tenant: this field is always 0.
	TenantID int64 `gorm:"column:tenant_id;not null;index" json:"-"`
	// AssetID is the associated asset identifier.
	AssetID int64 `gorm:"column:asset_id;not null;index" json:"-"`
	// Name is the human-readable task name.
	Name string `gorm:"column:name;type:varchar(255);not null" json:"name"`
	// Description is an optional human-readable description.
	Description string `gorm:"column:description;type:varchar(255)" json:"description,omitempty"`
	// ExecutorType identifies which executor to use (http, tcp, icmp,
	// local, webhook).
	ExecutorType string `gorm:"column:executor_type;type:varchar(64);not null" json:"executor_type"`
	// Schedule is the single source of truth for when the task runs: a
	// cron expression, a Go duration string ("5m"), or "" for event-driven
	// tasks. See ClassifySchedule.
	Schedule string `gorm:"column:schedule;type:varchar(128)" json:"schedule,omitempty"`
	// Enabled reports whether the scheduler is currently driving the task.
	// Pause clears it and Resume sets it back; both persist the flag. No
	// column default: GORM substitutes the default for zero-valued fields
	// on insert, which would store disabled (false) tasks as enabled.
	Enabled bool `gorm:"column:enabled;not null" json:"enabled"`
	// Config stores executor-specific configuration.
	Config map[string]any `gorm:"column:executor_config;type:text;serializer:tolerantjson" json:"config,omitempty"`
	// TimeoutSeconds bounds a single execution attempt. Values <= 0 let
	// the executor apply its own default; the task service applies the
	// column default (30) on create.
	TimeoutSeconds int64 `gorm:"column:timeout;not null;default:30" json:"timeout,omitempty"`
	// MaxRetries is the number of retry attempts after a failed
	// execution; 0 disables retries.
	MaxRetries int `gorm:"column:max_retries;not null;default:0" json:"max_retries,omitempty"`
	// RetryIntervalSeconds is the delay between retry attempts.
	RetryIntervalSeconds int64 `gorm:"column:retry_interval;not null;default:0" json:"retry_interval,omitempty"`
	// Priority controls execution order when multiple tasks fire
	// simultaneously.
	Priority int `gorm:"column:priority;not null;default:0" json:"-"`
	// DependsOn is the task ID that must succeed before this task can run.
	DependsOn int64 `gorm:"column:depends_on;not null;default:0" json:"-"`
	// Metadata is the extension key bag for engine-internal and
	// integration-specific keys (monitor_point_id, expression, ...).
	Metadata map[string]string `gorm:"column:metadata;type:text;serializer:tolerantjson" json:"-"`
	// Group is an optional logical grouping label, used for filtering.
	Group string `gorm:"column:group;type:varchar(64);index" json:"group,omitempty"`
	// Tags is an optional list of labels, stored comma-separated.
	Tags []string `gorm:"column:tags;type:varchar(255);serializer:commalist" json:"tags,omitempty"`
	// RunID is an optional idempotency key. It is not unique: tasks are
	// created without a run ID by the handler layer.
	RunID string `gorm:"column:run_id;type:varchar(64)" json:"run_id,omitempty"`
	// RetryPolicy is the retry strategy: "fixed" or "exponential".
	RetryPolicy string `gorm:"column:retry_policy;type:varchar(16);default:fixed" json:"retry_policy,omitempty"`
	// Concurrency controls per-task concurrent execution
	// (0=unlimited, 1=no concurrent execution). No column default: the
	// GORM zero-value substitution would turn 0 (unlimited) into 1.
	Concurrency int `gorm:"column:concurrency;type:tinyint" json:"concurrency,omitempty"`
	// Operation specifies the operation type (probe or execute) for the
	// executor. Runtime-only; never persisted or exposed on the wire.
	Operation executor.Operation `gorm:"-" json:"-"`
	// CreatedAt is the row creation timestamp.
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	// UpdatedAt is the row last-update timestamp.
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	// DeletedAt is the soft-delete marker.
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

// TableName returns the database table name.
func (Task) TableName() string { return "sys_schedule_task" }

// Timeout returns the execution timeout as a duration. Non-positive
// configured values yield 0, letting the executor apply its own default.
func (t *Task) Timeout() time.Duration {
	if t.TimeoutSeconds <= 0 {
		return 0
	}
	return time.Duration(t.TimeoutSeconds) * time.Second
}

// RetryInterval returns the delay between retry attempts as a duration.
func (t *Task) RetryInterval() time.Duration {
	if t.RetryIntervalSeconds <= 0 {
		return 0
	}
	return time.Duration(t.RetryIntervalSeconds) * time.Second
}

// Execution is the single model for a task execution history record: the
// GORM model for the sys_schedule_log table and its HTTP wire
// representation at once.
type Execution struct {
	// ID is the unique execution record identifier (auto-increment).
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// TenantID is the tenant identifier for multi-tenancy isolation.
	// The runtime is single-tenant: this field is always 0.
	TenantID int64 `gorm:"column:tenant_id;not null;index" json:"-"`
	// TaskID is the associated task identifier.
	TaskID int64 `gorm:"column:task_id;not null;index" json:"task_id"`
	// AssetID is the associated asset identifier.
	AssetID int64 `gorm:"column:asset_id;not null;index" json:"-"`
	// TaskName is the owning task's name, resolved at read time; it is
	// not persisted on this table.
	TaskName string `gorm:"-" json:"task_name,omitempty"`
	// ExecutorType identifies which executor produced the record.
	ExecutorType string `gorm:"column:executor_type;type:varchar(64);not null" json:"executor_type,omitempty"`
	// Status is the execution outcome: success, failed, running or
	// unknown.
	Status string `gorm:"column:status;type:varchar(32);not null" json:"status"`
	// StatusCode is the numeric status code returned by the executor.
	StatusCode int `gorm:"column:status_code" json:"status_code,omitempty"`
	// Output is the raw executor output.
	Output string `gorm:"column:output;type:text" json:"output,omitempty"`
	// Error is the error message if the execution failed.
	Error string `gorm:"column:error_msg;type:text" json:"error,omitempty"`
	// Duration is the execution duration in milliseconds.
	Duration int64 `gorm:"column:duration" json:"duration,omitempty"`
	// RetryCount is the number of retries attempted.
	RetryCount int `gorm:"column:retry_count;not null;default:0" json:"retry_count,omitempty"`
	// StartedAt is when the execution began.
	StartedAt time.Time `gorm:"column:started_at;not null" json:"started_at"`
	// FinishedAt is when the execution completed; nil while running.
	FinishedAt *time.Time `gorm:"column:finished_at" json:"finished_at,omitempty"`
	// RunID links to the task run for idempotency tracking.
	RunID string `gorm:"column:run_id;type:varchar(64);index" json:"run_id,omitempty"`
	// TriggerType records how the execution was triggered: "schedule",
	// "manual" or "event".
	TriggerType string `gorm:"column:trigger_type;type:varchar(16)" json:"trigger_type,omitempty"`
	// TriggeredAt is when the runner received the trigger event. The gap
	// to StartedAt is the worker pool pickup delay. Nullable: rows written
	// before the field existed have no value.
	TriggeredAt *time.Time `gorm:"column:triggered_at" json:"triggered_at,omitempty"`
	// Node is the hostname of the worker that executed the task, for
	// locating executions in multi-node deployments. Empty on legacy rows.
	Node string `gorm:"column:node;type:varchar(255)" json:"node,omitempty"`
	// ExitCode is the process exit code for command executors (local);
	// 0 for protocol executors and successful runs.
	ExitCode int `gorm:"column:exit_code;not null;default:0" json:"exit_code,omitempty"`
	// SkipReason records why the execution was skipped.
	SkipReason string `gorm:"column:skip_reason;type:varchar(256)" json:"-"`
	// Metrics stores execution metrics as JSON.
	Metrics string `gorm:"column:metrics;type:text" json:"-"`
	// CreatedAt is the row creation timestamp.
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"-"`
}

// TableName returns the database table name.
func (Execution) TableName() string { return "sys_schedule_log" }
