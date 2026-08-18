// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"time"
)

// MetricStore persists metric data points.
//
// This is the persistence port for the metric sub-domain. The GORM-backed
// implementation lives in store.go (NewMetricStore); the no-op default
// (NoopMetricStore) is used when no concrete store is injected, allowing
// the telemetry pipeline to run without a metric persistence backend.
//
// Implementations must be safe for concurrent use.
type MetricStore interface {
	// SaveMetric persists a single metric data point.
	SaveMetric(ctx context.Context, metric *CollectMetric) error
	// SaveMetricsBatch persists multiple metric data points in a single
	// database round-trip. An empty slice is a no-op.
	SaveMetricsBatch(ctx context.Context, metrics []*CollectMetric) error
	// QueryMetrics queries metrics for an asset within a time range.
	// If metricName is non-empty, results are filtered by metric name.
	// The limit parameter caps the number of returned entries; a value of 0
	// applies a default limit of 1000.
	QueryMetrics(ctx context.Context, tenantID, assetID int64, metricName string,
		start, end time.Time, limit int) ([]CollectMetric, error)
}

// LogStore persists log entries.
//
// This is the persistence port for the log sub-domain. The GORM-backed
// implementation lives in store.go (NewLogStore); the no-op default
// (NoopLogStore) is used when no concrete store is injected.
//
// Implementations must be safe for concurrent use.
type LogStore interface {
	// SaveLog persists a single log entry.
	SaveLog(ctx context.Context, log *CollectLog) error
	// SaveLogsBatch persists multiple log entries in a single database
	// round-trip. An empty slice is a no-op.
	SaveLogsBatch(ctx context.Context, logs []*CollectLog) error
	// QueryLogs queries logs for an asset within a time range.
	// If level is non-empty, results are filtered by log level.
	// The limit parameter caps the number of returned entries; a value of 0
	// applies a default limit of 1000.
	QueryLogs(ctx context.Context, tenantID, assetID int64, level string,
		start, end time.Time, limit int) ([]CollectLog, error)
}
