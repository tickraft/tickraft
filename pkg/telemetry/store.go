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
func (NoopMetricStore) QueryMetrics(_ context.Context, _ MetricQuery) ([]CollectMetric, error) {
	return nil, nil
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
func (NoopLogStore) QueryLogs(_ context.Context, _ LogQuery) ([]CollectLog, error) {
	return nil, nil
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

// QueryMetrics queries metrics for an asset within a time range.
// If q.MetricName is non-empty, results are filtered by metric name.
// The q.Limit field caps the number of returned entries; a value <= 0
// applies a default limit of 1000.
// Results are ordered by timestamp ascending.
func (s *metricStore) QueryMetrics(ctx context.Context, q MetricQuery) ([]CollectMetric, error) {
	query := s.dbc.WithContext(ctx).
		Where("tenant_id = ? AND asset_id = ? AND timestamp >= ? AND timestamp <= ?", q.TenantID, q.AssetID, q.Start, q.End)
	if q.MetricName != "" {
		query = query.Where("metric_name = ?", q.MetricName)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 1000
	}
	var metrics []CollectMetric
	if err := query.Order("timestamp ASC").Limit(limit).Find(&metrics).Error; err != nil {
		return nil, fmt.Errorf("telemetry: query metrics: %w", errmap.MapError(err))
	}
	return metrics, nil
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

// QueryLogs queries logs for an asset within a time range.
// If q.Level is non-empty, results are filtered by log level.
// The q.Limit field caps the number of returned entries; a value <= 0
// applies a default limit of 1000.
// Results are ordered by timestamp descending (newest first).
func (s *logStore) QueryLogs(ctx context.Context, q LogQuery) ([]CollectLog, error) {
	query := s.dbc.WithContext(ctx).
		Where("tenant_id = ? AND asset_id = ? AND timestamp >= ? AND timestamp <= ?", q.TenantID, q.AssetID, q.Start, q.End)

	if q.Level != "" {
		query = query.Where("level = ?", q.Level)
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 1000
	}

	var logs []CollectLog
	if err := query.Order("timestamp DESC").Limit(limit).Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("telemetry: query logs: %w", errmap.MapError(err))
	}
	return logs, nil
}

// Compile-time assertion that logStore satisfies LogStore.
var _ LogStore = (*logStore)(nil)
