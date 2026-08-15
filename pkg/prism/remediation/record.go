// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package remediation

import (
	"context"
	"time"
)

// Record lifecycle status values. They describe the state of a single
// remediation dispatch persisted in sys_prism_remediation_record and are
// part of the public API contract of GET /api/v1/prism/remediation/records.
const (
	// RecordStatusTriggered indicates a rule matched and a dispatch is
	// about to start.
	RecordStatusTriggered = "triggered"
	// RecordStatusStarted indicates the operator has accepted and started
	// the dispatch.
	RecordStatusStarted = "started"
	// RecordStatusCompleted indicates the dispatch finished successfully.
	RecordStatusCompleted = "completed"
	// RecordStatusSkipped indicates the dispatch was skipped (cooldown,
	// circuit breaker, idempotency, or missing operator).
	RecordStatusSkipped = "skipped"
	// RecordStatusFailed indicates the operator ran but reported a
	// failure. The record's Error field is populated.
	RecordStatusFailed = "failed"
)

// Record is the GORM model for the sys_prism_remediation_record table. It
// persists the lifecycle of a single remediation dispatch: which rule fired,
// which asset triggered it, and the final operator outcome. Rows are
// upserted by RunID as the dispatch progresses through the triggered ->
// started -> completed/failed lifecycle.
type Record struct {
	// ID is the auto-incremented primary key.
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// RuleID references the remediation rule that produced this record.
	RuleID int64 `gorm:"column:rule_id;not null;index" json:"rule_id"`
	// RuleName is a denormalized snapshot of the rule name at trigger
	// time, retained so historical records remain readable even if the
	// rule is later renamed or deleted.
	RuleName string `gorm:"column:rule_name;type:varchar(255);not null" json:"rule_name"`
	// AssetID is the numeric asset identifier of the triggering event.
	// A value of 0 means the event carried no asset scope.
	AssetID int64 `gorm:"column:asset_id;not null;default:0" json:"asset_id"`
	// AssetKey is the tenant-unique asset key of the triggering event,
	// when the source payload carried one. It may be empty for metric and
	// log triggers, which only carry the numeric asset ID.
	AssetKey string `gorm:"column:asset_key;type:varchar(255)" json:"asset_key,omitempty"`
	// RunID is the unique identifier of this remediation run. Records are
	// upserted by RunID as the dispatch progresses.
	RunID string `gorm:"column:run_id;type:varchar(64);not null;uniqueIndex" json:"run_id"`
	// Trigger is the trigger type that activated the rule: metric, log,
	// or status_change.
	Trigger string `gorm:"column:trigger;type:varchar(32);not null" json:"trigger"`
	// Status is the dispatch lifecycle state. See the RecordStatus*
	// constants.
	Status string `gorm:"column:status;type:varchar(16);not null;default:'triggered'" json:"status"`
	// Error captures the failure or skip message when Status is "failed"
	// or "skipped".
	Error string `gorm:"column:error;type:varchar(2048)" json:"error,omitempty"`
	// StartedAt is the time the operator started working on the dispatch.
	// Nullable.
	StartedAt *time.Time `gorm:"column:started_at" json:"started_at,omitempty"`
	// FinishedAt is the time the dispatch finished (success or failure).
	// Nullable.
	FinishedAt *time.Time `gorm:"column:finished_at" json:"finished_at,omitempty"`
	// CreatedAt records when this record was inserted, populated by the
	// database.
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	// UpdatedAt records the last lifecycle update time, populated by the
	// database.
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName returns the database table name for Record.
func (Record) TableName() string { return "sys_prism_remediation_record" }

// RecordStore defines the persistence operations for remediation dispatch
// records. The Manager upserts one row per run as the dispatch progresses;
// the records API reads rows through ListRecords.
type RecordStore interface {
	// UpsertRecord inserts the record when no row with the same RunID
	// exists, or updates the existing row's lifecycle fields (status,
	// error, started_at, finished_at) otherwise.
	UpsertRecord(ctx context.Context, record *Record) error
	// ListRecords returns a page of dispatch records ordered by descending
	// ID, plus the total count. A non-empty status filters by exact
	// lifecycle status match. limit and offset control the page; callers
	// should clamp them before calling.
	ListRecords(ctx context.Context, limit, offset int, status string) ([]*Record, int64, error)
}
