// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db/errmap"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/types"
)

// ProbeRecord is the GORM model for the sys_probe_record table. It is the
// telemetry-domain home for active probe results: one row per probe
// execution, keyed by the monitor point that scheduled it.
//
// It holds a structured record, not a log entry: a probe is an operation the
// runtime executes, and the row preserves its full result envelope (status,
// status code, duration, output, error) so status filters, latency trends and
// success-rate statistics stay SQL-queryable. Passive collection data lives
// separately in sys_collect_log / sys_collect_metric; the active/passive
// distinction is structural — which data source a reader queries — and no
// row-level marker is needed.
type ProbeRecord struct {
	// ID is the unique identifier of the probe record.
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// TenantID is the tenant identifier for multi-tenancy isolation.
	// The runtime is single-tenant: this field is always 0.
	TenantID int64 `gorm:"index;not null" json:"-"`
	// PointID is the monitor point (monitor_points.id) that scheduled the
	// probe. It is recovered from the synthetic prober task ID.
	PointID int64 `gorm:"column:point_id;not null;index" json:"point_id"`
	// AssetID is the target asset, denormalized for asset-level queries.
	AssetID int64 `gorm:"index;not null" json:"asset_id"`
	// ExecutorType is the probe executor type (e.g. icmp, tcp, http, udp).
	ExecutorType string `gorm:"size:32;not null" json:"executor_type"`
	// Status is the probe result in the asset status vocabulary
	// (normal, abnormal, offline, unknown).
	Status types.AssetStatus `gorm:"size:32;not null;index" json:"status"`
	// StatusCode is the numeric status code returned by the executor.
	StatusCode int `gorm:"status_code" json:"status_code,omitempty"`
	// Output is the raw probe output.
	Output string `gorm:"type:text" json:"output,omitempty"`
	// Error is the error message when the probe failed.
	Error string `gorm:"column:error_msg;type:text" json:"error,omitempty"`
	// Duration is the probe duration in milliseconds.
	Duration int64 `gorm:"duration" json:"duration,omitempty"`
	// RetryCount is the number of retries attempted.
	RetryCount int `gorm:"not null;default:0" json:"retry_count,omitempty"`
	// RunID links to the task run for idempotency tracking.
	RunID string `gorm:"size:64;index" json:"run_id,omitempty"`
	// TriggerType records how the probe was triggered.
	TriggerType string `gorm:"size:16" json:"-"`
	// StartedAt is when the probe began.
	StartedAt time.Time `gorm:"column:started_at;not null;index" json:"started_at"`
	// CreatedAt is the row creation timestamp.
	CreatedAt time.Time `gorm:"autoCreateTime" json:"-"`
}

// TableName returns the database table name for ProbeRecord.
func (ProbeRecord) TableName() string { return "sys_probe_record" }

// ProbeQuery filters a probe-record query. PointID is required; the time
// bounds and tenant filter are applied when non-zero.
type ProbeQuery struct {
	TenantID int64
	PointID  int64
	Start    time.Time
	End      time.Time
	Page     int
	Size     int
}

// ProbeRecordStore persists probe execution records into sys_probe_record
// and keeps the scheduling monitor point's runtime status column in sync.
// It implements executor.RecordStore so the runner can persist probe
// results directly; Operation is not consulted here — routing OpProbe
// records to this store (and OpExecute records to the task domain) is a
// decision owned by the assembly layer.
type ProbeRecordStore struct {
	dbc *gorm.DB
}

// NewProbeRecordStore creates a ProbeRecordStore backed by the given GORM
// database. A nil dbc yields a no-op store (Save returns nil), mirroring
// task.NewExecutionRecordStore's nil tolerance.
func NewProbeRecordStore(dbc *gorm.DB) *ProbeRecordStore {
	return &ProbeRecordStore{dbc: dbc}
}

// Save persists one probe execution record and updates the monitor point's
// runtime status. It implements executor.RecordStore; the runner hands down
// a context detached from the execution's own deadline, so a probe that
// timed out can still persist its record.
//
// The synthetic prober task ID encodes the monitor point ID
// (ProbeTaskIDOffset + point ID); records whose TaskID does not carry a
// point ID are rejected instead of silently dropped.
func (s *ProbeRecordStore) Save(ctx context.Context, record executor.ExecutionRecord) error {
	if s == nil || s.dbc == nil {
		return nil
	}
	if record.Operation != executor.OpProbe {
		return fmt.Errorf("telemetry: probe record store received %s operation for task %d",
			record.Operation, record.TaskID)
	}
	if record.TaskID < ProbeTaskIDOffset {
		return fmt.Errorf("telemetry: task id %d does not map to a monitor point", record.TaskID)
	}
	pointID := record.TaskID - ProbeTaskIDOffset

	rec := &ProbeRecord{
		TenantID:     record.TenantID,
		PointID:      pointID,
		AssetID:      record.AssetID,
		ExecutorType: record.ExecutorName,
		Status:       record.Status,
		StatusCode:   record.StatusCode,
		Output:       record.Output,
		Error:        record.ErrorMsg,
		Duration:     int64(record.Duration / 1_000_000),
		RetryCount:   record.RetryCount,
		RunID:        record.RunID,
		TriggerType:  record.TriggerType,
		StartedAt:    record.StartedAt,
	}

	err := s.dbc.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(rec).Error; err != nil {
			return err
		}
		// Best-effort runtime status update: a missing point row (deleted
		// mid-flight) is not an error — the record itself stays valuable.
		// The condition skips the write when the status is unchanged, so
		// a steady stream of identical probe results does not churn the
		// point row (and its updated_at) on every probe.
		newStatus := pointStatusFromProbe(record.Status)
		return tx.Model(&MonitorPoint{}).
			Where("id = ? AND status <> ?", pointID, newStatus).
			Update("status", newStatus).Error
	})
	if err != nil {
		return fmt.Errorf("telemetry: save probe record: %w", errmap.MapError(err))
	}
	return nil
}

// Compile-time assertion that ProbeRecordStore implements executor.RecordStore.
var _ executor.RecordStore = (*ProbeRecordStore)(nil)

// pointStatusFromProbe maps a probe result in the asset status vocabulary
// onto the monitor point runtime status vocabulary: a normal probe leaves
// the point active, anything else marks it as erroring.
func pointStatusFromProbe(status types.AssetStatus) string {
	if status == types.AssetStatusNormal {
		return MonitorStatusActive
	}
	return MonitorStatusError
}

// QueryByPoint returns a page of probe records for a monitor point, plus
// the total count of matching rows. Results are ordered newest first
// (started_at DESC, id DESC), matching the task-domain execution list.
func (s *ProbeRecordStore) QueryByPoint(ctx context.Context, q ProbeQuery) ([]ProbeRecord, int64, error) {
	if s == nil || s.dbc == nil || q.PointID <= 0 {
		return []ProbeRecord{}, 0, nil
	}
	query := s.dbc.WithContext(ctx).Model(&ProbeRecord{}).Where("point_id = ?", q.PointID)
	if q.TenantID > 0 {
		query = query.Where("tenant_id = ?", q.TenantID)
	}
	if !q.Start.IsZero() {
		query = query.Where("started_at >= ?", q.Start)
	}
	if !q.End.IsZero() {
		query = query.Where("started_at <= ?", q.End)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("telemetry: count probe records: %w", errmap.MapError(err))
	}
	limit, offset := queryWindow(q.Page, q.Size)
	var records []ProbeRecord
	if err := query.
		Order("started_at DESC, id DESC").
		Offset(offset).Limit(limit).
		Find(&records).Error; err != nil {
		return nil, 0, fmt.Errorf("telemetry: query probe records: %w", errmap.MapError(err))
	}
	return records, total, nil
}

// LatestByPoint returns the most recent probe record for a monitor point,
// or errdefs.ErrNotFound when the point has never been probed.
func (s *ProbeRecordStore) LatestByPoint(ctx context.Context, pointID int64) (*ProbeRecord, error) {
	if s == nil || s.dbc == nil {
		return nil, fmt.Errorf("telemetry: latest probe: %w", errdefs.ErrNotFound)
	}
	var rec ProbeRecord
	err := s.dbc.WithContext(ctx).
		Where("point_id = ?", pointID).
		Order("started_at DESC, id DESC").
		First(&rec).Error
	if err != nil {
		return nil, fmt.Errorf("telemetry: latest probe for point %d: %w", pointID, errmap.MapError(err))
	}
	return &rec, nil
}

// DeleteOlderThan removes probe records that started before the given time.
// It backs the shared retention sweep alongside the task-domain execution
// log cleanup.
func (s *ProbeRecordStore) DeleteOlderThan(ctx context.Context, before time.Time) error {
	if s == nil || s.dbc == nil {
		return nil
	}
	err := s.dbc.WithContext(ctx).
		Where("started_at < ?", before).
		Delete(&ProbeRecord{}).Error
	if err != nil {
		return fmt.Errorf("telemetry: delete old probe records: %w", errmap.MapError(err))
	}
	return nil
}

// Migrate creates or upgrades the sys_probe_record table.
func (s *ProbeRecordStore) Migrate() error {
	if s == nil || s.dbc == nil {
		return nil
	}
	if err := s.dbc.AutoMigrate(&ProbeRecord{}); err != nil {
		return fmt.Errorf("telemetry: migrate probe records: %w", err)
	}
	return nil
}
