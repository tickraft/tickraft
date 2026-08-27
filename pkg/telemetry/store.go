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
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/types"
)

// NoopMetricStore is a no-op MetricStore that discards all writes and
// returns empty results for queries. It is the SPI default used when no
// concrete store is injected, allowing the telemetry pipeline to run
// without a metric persistence backend.
type NoopMetricStore struct{}

// SaveMetric discards the metric.
func (NoopMetricStore) SaveMetric(_ context.Context, _ *CollectMetric) error { return nil }

// SaveMetricsBatch discards the metrics.
func (NoopMetricStore) SaveMetricsBatch(_ context.Context, _ []*CollectMetric) error { return nil }

// QueryMetrics returns an empty slice.
func (NoopMetricStore) QueryMetrics(_ context.Context, _ MetricQuery) ([]CollectMetric, int64, error) {
	return nil, 0, nil
}

// Compile-time assertion that NoopMetricStore satisfies MetricStore.
var _ MetricStore = NoopMetricStore{}

// NoopLogStore is a no-op LogStore that discards all writes and returns
// empty results for queries. It is the SPI default used when no concrete
// store is injected.
type NoopLogStore struct{}

// SaveLog discards the log entry.
func (NoopLogStore) SaveLog(_ context.Context, _ *CollectLog) error { return nil }

// SaveLogsBatch discards the log entries.
func (NoopLogStore) SaveLogsBatch(_ context.Context, _ []*CollectLog) error { return nil }

// QueryLogs returns an empty slice.
func (NoopLogStore) QueryLogs(_ context.Context, _ LogQuery) ([]CollectLog, int64, error) {
	return nil, 0, nil
}

// Compile-time assertion that NoopLogStore satisfies LogStore.
var _ LogStore = NoopLogStore{}

// metricStore implements MetricStore using GORM.
type metricStore struct {
	dbc *gorm.DB
}

// NewMetricStore creates a new MetricStore backed by the given GORM database.
func NewMetricStore(dbc *gorm.DB) MetricStore {
	return &metricStore{dbc: dbc}
}

// SaveMetric persists a single metric data point.
func (s *metricStore) SaveMetric(ctx context.Context, metric *CollectMetric) error {
	if err := s.dbc.WithContext(ctx).Create(metric).Error; err != nil {
		return fmt.Errorf("telemetry: save metric: %w", errmap.MapError(err))
	}
	return nil
}

// SaveMetricsBatch persists multiple metric data points in a single database
// round-trip. An empty slice is a no-op.
func (s *metricStore) SaveMetricsBatch(ctx context.Context, metrics []*CollectMetric) error {
	if len(metrics) == 0 {
		return nil
	}
	if err := s.dbc.WithContext(ctx).Create(&metrics).Error; err != nil {
		return fmt.Errorf("telemetry: save metrics batch: %w", errmap.MapError(err))
	}
	return nil
}

// QueryMetrics returns a page of metrics for an asset within a time range,
// plus the total count of matching rows. If q.MetricName is non-empty,
// results are filtered by metric name. The q.Size field caps the number of
// returned entries; a value <= 0 applies a default limit of 1000.
// Results are ordered by timestamp ascending.
//
//nolint:dupl // metric and log stores differ in model, filter and ordering
func (s *metricStore) QueryMetrics(ctx context.Context, q MetricQuery) ([]CollectMetric, int64, error) {
	// Model is set explicitly: Count cannot infer the table from a plain
	// Where chain, and the mock-based unit tests never exercise SQL.
	query := s.dbc.WithContext(ctx).Model(&CollectMetric{}).
		Where("tenant_id = ? AND asset_id = ? AND timestamp >= ? AND timestamp <= ?",
			q.TenantID, q.AssetID, q.Start, q.End)
	if q.MetricName != "" {
		query = query.Where("metric_name = ?", q.MetricName)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("telemetry: count metrics: %w", errmap.MapError(err))
	}
	limit, offset := queryWindow(q.Page, q.Size)
	var metrics []CollectMetric
	if err := query.Order("timestamp ASC").Offset(offset).Limit(limit).Find(&metrics).Error; err != nil {
		return nil, 0, fmt.Errorf("telemetry: query metrics: %w", errmap.MapError(err))
	}
	return metrics, total, nil
}

// queryWindow resolves the effective row limit and offset for a paged
// telemetry range query; a size <= 0 applies the default limit of 1000 rows.
func queryWindow(page, size int) (limit, offset int) {
	if size <= 0 {
		size = 1000
	}
	if page > 1 {
		return size, (page - 1) * size
	}
	return size, 0
}

// Compile-time assertion that metricStore satisfies MetricStore.
var _ MetricStore = (*metricStore)(nil)

// logStore implements LogStore using GORM.
type logStore struct {
	dbc *gorm.DB
}

// NewLogStore creates a new LogStore backed by the given GORM database.
func NewLogStore(dbc *gorm.DB) LogStore {
	return &logStore{dbc: dbc}
}

// SaveLog persists a single log entry.
func (s *logStore) SaveLog(ctx context.Context, log *CollectLog) error {
	if err := s.dbc.WithContext(ctx).Create(log).Error; err != nil {
		return fmt.Errorf("telemetry: save log: %w", errmap.MapError(err))
	}
	return nil
}

// SaveLogsBatch persists multiple log entries in a single database round-trip.
// An empty slice is a no-op.
func (s *logStore) SaveLogsBatch(ctx context.Context, logs []*CollectLog) error {
	if len(logs) == 0 {
		return nil
	}
	if err := s.dbc.WithContext(ctx).Create(&logs).Error; err != nil {
		return fmt.Errorf("telemetry: save logs batch: %w", errmap.MapError(err))
	}
	return nil
}

// QueryLogs returns a page of logs for an asset within a time range, plus
// the total count of matching rows. If q.Level is non-empty, results are
// filtered by log level. The q.Size field caps the number of returned
// entries; a value <= 0 applies a default limit of 1000.
// Results are ordered by timestamp descending (newest first).
//
//nolint:dupl // metric and log stores differ in model, filter and ordering
func (s *logStore) QueryLogs(ctx context.Context, q LogQuery) ([]CollectLog, int64, error) {
	query := s.dbc.WithContext(ctx).Model(&CollectLog{}).
		Where("tenant_id = ? AND asset_id = ? AND timestamp >= ? AND timestamp <= ?",
			q.TenantID, q.AssetID, q.Start, q.End)

	if q.Level != "" {
		query = query.Where("level = ?", q.Level)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("telemetry: count logs: %w", errmap.MapError(err))
	}

	limit, offset := queryWindow(q.Page, q.Size)

	var logs []CollectLog
	if err := query.Order("timestamp DESC").Offset(offset).Limit(limit).Find(&logs).Error; err != nil {
		return nil, 0, fmt.Errorf("telemetry: query logs: %w", errmap.MapError(err))
	}
	return logs, total, nil
}

// Compile-time assertion that logStore satisfies LogStore.
var _ LogStore = (*logStore)(nil)

// MonitorStore provides CRUD operations for unified monitoring points backed
// by a GORM database. All methods map low-level driver errors to the shared
// sentinel errors (errdefs.ErrNotFound, errdefs.ErrConflict) via errmap.MapError
// so callers can use errors.Is for consistent handling.
//
// The store is the persistence layer for the MonitorPoint model. The
// ProberService uses it to list active points (Mode=ModeActive); the listener
// pipeline and API handlers use it to list passive points (Mode=ModePassive)
// or all points.
type MonitorStore struct {
	dbc *gorm.DB
}

// NewMonitorStore creates a new MonitorStore backed by the given GORM database.
func NewMonitorStore(dbc *gorm.DB) *MonitorStore {
	return &MonitorStore{dbc: dbc}
}

// List returns monitoring points filtered by an optional mode. When mode is
// empty, all points are returned ordered by ascending ID.
func (s *MonitorStore) List(ctx context.Context, mode Mode) ([]MonitorPoint, error) {
	query := s.dbc.WithContext(ctx).Model(&MonitorPoint{})
	if mode != "" {
		query = query.Where("mode = ?", mode)
	}
	var points []MonitorPoint
	if err := query.Order("id ASC").Find(&points).Error; err != nil {
		return nil, fmt.Errorf("telemetry: list monitor points: %w", errmap.MapError(err))
	}
	return points, nil
}

// ListPaged returns a page of monitoring points filtered by an optional mode,
// together with the total count. When mode is empty, all points are included.
// page is 1-based; size is normalized by pagination.Clamp.
func (s *MonitorStore) ListPaged(ctx context.Context, mode Mode, page, size int) ([]MonitorPoint, int64, error) {
	query := s.dbc.WithContext(ctx).Model(&MonitorPoint{})
	if mode != "" {
		query = query.Where("mode = ?", mode)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("telemetry: count monitor points: %w", errmap.MapError(err))
	}
	page, size = pagination.Clamp(page, size)
	offset := (page - 1) * size
	var points []MonitorPoint
	if err := query.Order("id ASC").Offset(offset).Limit(size).Find(&points).Error; err != nil {
		return nil, 0, fmt.Errorf("telemetry: list monitor points paged: %w", errmap.MapError(err))
	}
	return points, total, nil
}

// GetByID retrieves a single monitoring point by its ID. It returns
// errdefs.ErrNotFound when no point exists with the given ID.
func (s *MonitorStore) GetByID(ctx context.Context, id int64) (*MonitorPoint, error) {
	var p MonitorPoint
	if err := s.dbc.WithContext(ctx).First(&p, id).Error; err != nil {
		return nil, fmt.Errorf("telemetry: get monitor point %d: %w", id, errmap.MapError(err))
	}
	return &p, nil
}

// Create inserts a new monitoring point. The caller is responsible for
// populating all required fields (Name, Mode, Type). A nil point yields
// errdefs.ErrInvalidArgument.
func (s *MonitorStore) Create(ctx context.Context, p *MonitorPoint) error {
	if p == nil {
		return fmt.Errorf("telemetry: create monitor point: %w", errdefs.ErrInvalidArgument)
	}
	if err := s.dbc.WithContext(ctx).Create(p).Error; err != nil {
		return fmt.Errorf("telemetry: create monitor point: %w", errmap.MapError(err))
	}
	return nil
}

// pointUpdateColumns lists the user-editable columns touched by Update.
// Runtime-managed state (tenant_id, status, interval, timeout) and
// lifecycle fields (created_at) are deliberately absent so a stale or
// partial API payload can never clear them.
var pointUpdateColumns = []string{
	"name", "description", "asset_type", "asset_id", "mode", "type",
	"schedule", "enabled", "config", "updated_at",
}

// Update applies a column-level update limited to the user-editable
// columns. Unlike a full Save, the runtime-managed fields (status,
// interval, timeout) and lifecycle fields are never touched. The ID
// field identifies the row to update. It returns errdefs.ErrNotFound
// when the ID does not exist.
func (s *MonitorStore) Update(ctx context.Context, p *MonitorPoint) error {
	if p == nil {
		return fmt.Errorf("telemetry: update monitor point: %w", errdefs.ErrInvalidArgument)
	}
	result := s.dbc.WithContext(ctx).
		Model(&MonitorPoint{}).
		Where("id = ?", p.ID).
		Select(pointUpdateColumns).
		Updates(p)
	if result.Error != nil {
		return fmt.Errorf("telemetry: update monitor point %d: %w", p.ID, errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("telemetry: update monitor point %d: %w", p.ID, errdefs.ErrNotFound)
	}
	return nil
}

// Delete permanently removes a monitoring point by ID. It returns
// errdefs.ErrNotFound when the ID does not exist.
func (s *MonitorStore) Delete(ctx context.Context, id int64) error {
	result := s.dbc.WithContext(ctx).Delete(&MonitorPoint{}, id)
	if result.Error != nil {
		return fmt.Errorf("telemetry: delete monitor point %d: %w", id, errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("telemetry: delete monitor point %d: %w", id, errdefs.ErrNotFound)
	}
	return nil
}

// ListActive returns all monitoring points in active probing mode
// (Mode=ModeActive), ordered by ascending ID. It is a convenience wrapper
// around List for the ProberService.
func (s *MonitorStore) ListActive(ctx context.Context) ([]MonitorPoint, error) {
	return s.List(ctx, ModeActive)
}

// ListPassive returns all monitoring points in passive receiving mode
// (Mode=ModePassive), ordered by ascending ID. It is a convenience wrapper
// around List for the listener pipeline.
func (s *MonitorStore) ListPassive(ctx context.Context) ([]MonitorPoint, error) {
	return s.List(ctx, ModePassive)
}

// Summary returns aggregate monitor point counts grouped by mode and
// enabled state, computed in a single grouped query. It backs the monitor
// list summary chips so the counts reflect the full dataset instead of the
// current page.
func (s *MonitorStore) Summary(ctx context.Context) (PointSummary, error) {
	var rows []struct {
		Mode    Mode
		Enabled bool
		Count   int64 `gorm:"column:count"`
	}
	if err := s.dbc.WithContext(ctx).Model(&MonitorPoint{}).
		Select("mode, enabled, COUNT(*) AS count").
		Group("mode, enabled").
		Find(&rows).Error; err != nil {
		return PointSummary{}, fmt.Errorf("telemetry: summarize monitor points: %w", errmap.MapError(err))
	}
	var summary PointSummary
	for _, row := range rows {
		if row.Mode == ModeActive {
			summary.Active += row.Count
		}
		if row.Mode == ModePassive {
			summary.Passive += row.Count
		}
		if row.Enabled {
			summary.Enabled += row.Count
		} else {
			summary.Disabled += row.Count
		}
	}
	return summary, nil
}

// TemplateStore provides CRUD operations for telemetry templates backed by
// a GORM database. All methods map low-level driver errors to the db
// sentinel errors (ErrNotFound, ErrDuplicateKey) so callers can use
// errors.Is for consistent handling.
type TemplateStore struct {
	dbc *gorm.DB
}

// NewTemplateStore creates a new TemplateStore backed by the given GORM
// database.
func NewTemplateStore(dbc *gorm.DB) *TemplateStore {
	return &TemplateStore{dbc: dbc}
}

// List returns templates filtered by an optional category. When category
// is empty, all templates are returned ordered by ascending ID.
func (s *TemplateStore) List(ctx context.Context, category string) ([]Template, error) {
	query := s.dbc.WithContext(ctx).Model(&Template{})
	if category != "" {
		query = query.Where("category = ?", category)
	}
	var templates []Template
	if err := query.Order("id ASC").Find(&templates).Error; err != nil {
		return nil, fmt.Errorf("telemetry: list templates: %w", errmap.MapError(err))
	}
	return templates, nil
}

// GetByID retrieves a single template by its ID. It returns
// errdefs.ErrNotFound when no template exists with the given ID.
func (s *TemplateStore) GetByID(ctx context.Context, id int64) (*Template, error) {
	var t Template
	if err := s.dbc.WithContext(ctx).First(&t, id).Error; err != nil {
		return nil, fmt.Errorf("telemetry: get template %d: %w", id, errmap.MapError(err))
	}
	return &t, nil
}

// Create inserts a new template. The caller is responsible for populating
// all required fields. A duplicate name surfaces as errdefs.ErrConflict.
func (s *TemplateStore) Create(ctx context.Context, t *Template) error {
	if err := s.dbc.WithContext(ctx).Create(t).Error; err != nil {
		return fmt.Errorf("telemetry: create template: %w", errmap.MapError(err))
	}
	return nil
}

// Update saves all fields of an existing template. The ID field identifies
// the row to update. It returns errdefs.ErrNotFound when the ID does not
// exist.
func (s *TemplateStore) Update(ctx context.Context, t *Template) error {
	result := s.dbc.WithContext(ctx).Save(t)
	if result.Error != nil {
		return fmt.Errorf("telemetry: update template %d: %w", t.ID, errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("telemetry: update template %d: %w", t.ID, errdefs.ErrNotFound)
	}
	return nil
}

// Delete permanently removes a template by ID. It returns
// errdefs.ErrNotFound when the ID does not exist.
func (s *TemplateStore) Delete(ctx context.Context, id int64) error {
	result := s.dbc.WithContext(ctx).Delete(&Template{}, id)
	if result.Error != nil {
		return fmt.Errorf("telemetry: delete template %d: %w", id, errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("telemetry: delete template %d: %w", id, errdefs.ErrNotFound)
	}
	return nil
}

// ListBuiltin returns only built-in templates ordered by ascending ID.
func (s *TemplateStore) ListBuiltin(ctx context.Context) ([]Template, error) {
	var templates []Template
	if err := s.dbc.WithContext(ctx).
		Where("is_builtin = ?", true).
		Order("id ASC").
		Find(&templates).Error; err != nil {
		return nil, fmt.Errorf("telemetry: list builtin templates: %w", errmap.MapError(err))
	}
	return templates, nil
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
