// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db/errmap"
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
