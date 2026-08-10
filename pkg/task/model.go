// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ScheduleTask is the GORM model for the sys_schedule_task table.
// It stores persisted task configuration for the scheduling engine.
//
// NOTE: The table name is kept as sys_schedule_task to avoid a database
// migration. A future migration may rename it to sys_task.
type ScheduleTask struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// TenantID is the tenant identifier for multi-tenancy isolation.
	// The runtime is single-tenant: this field is always 0.
	// The runtime injects the actual tenant ID via the store layer.
	TenantID       int64  `gorm:"column:tenant_id;not null;index" json:"tenant_id"`
	AssetID        int64  `gorm:"column:asset_id;not null;index" json:"asset_id"`
	Name           string `gorm:"column:name;type:varchar(255);not null" json:"name"`
	ExecutorType   string `gorm:"column:executor_type;type:varchar(64);not null" json:"executor_type"`
	ExecutorConfig string `gorm:"column:executor_config;type:text;not null" json:"executor_config"`
	ScheduleType   string `gorm:"column:schedule_type;type:varchar(32);not null" json:"schedule_type"`
	CronExpr       string `gorm:"column:cron_expr;type:varchar(128)" json:"cron_expr,omitempty"`
	Interval       int64  `gorm:"column:interval" json:"interval,omitempty"`
	Timeout        int64  `gorm:"column:timeout;not null;default:30" json:"timeout"` // seconds
	Priority       int    `gorm:"column:priority;not null;default:0" json:"priority"`
	DependsOn      int64  `gorm:"column:depends_on;not null;default:0" json:"depends_on"`
	MaxRetries     int    `gorm:"column:max_retries;not null;default:0" json:"max_retries"`
	RetryInterval  int64  `gorm:"column:retry_interval;not null;default:0" json:"retry_interval"` // seconds
	Enabled        bool   `gorm:"column:enabled;not null;default:true" json:"enabled"`
	Metadata       string `gorm:"column:metadata;type:text" json:"metadata,omitempty"`
	Group          string `gorm:"column:group;type:varchar(64);index" json:"group,omitempty"`
	Tags           string `gorm:"column:tags;type:varchar(255)" json:"tags,omitempty"`
	// RunID is an optional idempotency key. It is not unique: tasks are
	// created without a run ID by the handler layer, so a unique index
	// would reject every task after the first.
	RunID string `json:"run_id" gorm:"column:run_id;type:varchar(64)"`
	// RetryPolicy is the retry strategy: "fixed" or "exponential".
	RetryPolicy string `json:"retry_policy" gorm:"column:retry_policy;type:varchar(16);default:fixed"`
	// Concurrency controls per-task concurrent execution (0=unlimited, 1=no concurrent).
	Concurrency int            `json:"concurrency" gorm:"column:concurrency;type:tinyint;default:1"`
	CreatedAt   time.Time      `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at;index" json:"deleted_at,omitempty"`
}

// TableName returns the database table name.
func (ScheduleTask) TableName() string { return "sys_schedule_task" }

// ToTask converts a ScheduleTask to a Task.
func (m *ScheduleTask) ToTask() (Task, error) {
	var metadata map[string]string
	if m.Metadata != "" {
		if err := json.Unmarshal([]byte(m.Metadata), &metadata); err != nil {
			return Task{}, fmt.Errorf("parse task %d metadata: %w", m.ID, err)
		}
	}
	return Task{
		ID:           m.ID,
		TenantID:     m.TenantID,
		AssetID:      m.AssetID,
		ExecutorName: m.ExecutorType,
		Config:       m.ExecutorConfig,
		Timeout:      time.Duration(m.Timeout) * time.Second,
		Priority:     m.Priority,
		DependsOn:    m.DependsOn,
		Metadata:     metadata,
		Group:        m.Group,
		Tags:         parseTags(m.Tags),
		RunID:        m.RunID,
		RetryPolicy:  m.RetryPolicy,
		Concurrency:  m.Concurrency,
	}, nil
}

// parseTags splits a comma-separated tag string into a slice. An empty input
// yields nil so that the JSON omitempty tag drops the field. This avoids the
// strings.Split edge case where "" produces []string{""}.
func parseTags(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// ScheduleLog is the GORM model for the sys_schedule_log table.
// It stores execution history records for scheduled tasks.
//
// NOTE: The table name is kept as sys_schedule_log to avoid a database
// migration. A future migration may rename it to sys_task_execution.
type ScheduleLog struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// TenantID is the tenant identifier for multi-tenancy isolation.
	// The runtime is single-tenant: this field is always 0.
	// The runtime injects the actual tenant ID via the store layer.
	TenantID     int64     `gorm:"column:tenant_id;not null;index" json:"tenant_id"`
	TaskID       int64     `gorm:"column:task_id;not null;index" json:"task_id"`
	AssetID      int64     `gorm:"column:asset_id;not null;index" json:"asset_id"`
	ExecutorType string    `gorm:"column:executor_type;type:varchar(64);not null" json:"executor_type"`
	Status       string    `gorm:"column:status;type:varchar(32);not null" json:"status"`
	StatusCode   int       `gorm:"column:status_code" json:"status_code"`
	Output       string    `gorm:"column:output;type:text" json:"output,omitempty"`
	ErrorMsg     string    `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`
	Duration     int64     `gorm:"column:duration" json:"duration"` // milliseconds
	RetryCount   int       `gorm:"column:retry_count;not null;default:0" json:"retry_count"`
	StartedAt    time.Time `gorm:"column:started_at;not null" json:"started_at"`
	FinishedAt   time.Time `gorm:"column:finished_at" json:"finished_at,omitempty"`
	// RunID links to the task run for idempotency tracking.
	RunID string `json:"run_id" gorm:"column:run_id;type:varchar(64);index"`
	// TriggerType records how the execution was triggered: "schedule", "manual", "event".
	TriggerType string `json:"trigger_type" gorm:"column:trigger_type;type:varchar(16)"`
	// SkipReason records why the execution was skipped.
	SkipReason string `json:"skip_reason" gorm:"column:skip_reason;type:varchar(256)"`
	// Metrics stores execution metrics as JSON.
	Metrics   string    `json:"metrics" gorm:"column:metrics;type:text"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName returns the database table name.
func (ScheduleLog) TableName() string { return "sys_schedule_log" }

// ToExecution converts a ScheduleLog to an Execution domain object.
func (m *ScheduleLog) ToExecution() Execution {
	return Execution{
		ID:           m.ID,
		TaskID:       m.TaskID,
		TenantID:     m.TenantID,
		AssetID:      m.AssetID,
		ExecutorName: m.ExecutorType,
		Status:       m.Status,
		StatusCode:   m.StatusCode,
		Output:       m.Output,
		Error:        m.ErrorMsg,
		Duration:     m.Duration,
		RetryCount:   m.RetryCount,
		StartedAt:    m.StartedAt,
		FinishedAt:   m.FinishedAt,
		RunID:        m.RunID,
		TriggerType:  m.TriggerType,
		SkipReason:   m.SkipReason,
		Metrics:      m.Metrics,
	}
}

// ExecutionToModel converts an Execution domain object to a ScheduleLog for
// persistence. The ID field is preserved so callers can update existing
// records; for new records the caller should leave ID as zero so the
// database assigns an auto-increment value.
func ExecutionToModel(exec *Execution) *ScheduleLog {
	return &ScheduleLog{
		ID:           exec.ID,
		TenantID:     exec.TenantID,
		TaskID:       exec.TaskID,
		AssetID:      exec.AssetID,
		ExecutorType: exec.ExecutorName,
		Status:       exec.Status,
		StatusCode:   exec.StatusCode,
		Output:       exec.Output,
		ErrorMsg:     exec.Error,
		Duration:     exec.Duration,
		RetryCount:   exec.RetryCount,
		StartedAt:    exec.StartedAt,
		FinishedAt:   exec.FinishedAt,
		RunID:        exec.RunID,
		TriggerType:  exec.TriggerType,
		SkipReason:   exec.SkipReason,
		Metrics:      exec.Metrics,
	}
}
