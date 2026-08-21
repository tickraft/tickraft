// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"time"
)

// MetricQuery specifies the filtering criteria for QueryMetrics.
// A zero-value Size applies a default cap of 1000 returned entries.
type MetricQuery struct {
	// TenantID filters results to metrics belonging to this tenant.
	TenantID int64
	// AssetID filters results to metrics reported by this asset.
	AssetID int64
	// MetricName filters results by metric name when non-empty.
	MetricName string
	// Start is the inclusive lower bound of the query time range.
	Start time.Time
	// End is the inclusive upper bound of the query time range.
	End time.Time
	// Page is the 1-based page number used with Size for offset paging.
	Page int
	// Size caps the number of returned entries; a value <= 0 applies
	// a default limit of 1000.
	Size int
}

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
	// QueryMetrics returns a page of metrics for an asset within a time
	// range, plus the total count of matching rows. If q.MetricName is
	// non-empty, results are filtered by metric name. The q.Size field
	// caps the number of returned entries; a value <= 0 applies a default
	// limit of 1000.
	QueryMetrics(ctx context.Context, q MetricQuery) ([]CollectMetric, int64, error)
}

// LogQuery specifies the filtering criteria for QueryLogs.
// A zero-value Size applies a default cap of 1000 returned entries.
type LogQuery struct {
	// TenantID filters results to logs belonging to this tenant.
	TenantID int64
	// AssetID filters results to logs reported by this asset.
	AssetID int64
	// Level filters results by log level when non-empty.
	Level string
	// Start is the inclusive lower bound of the query time range.
	Start time.Time
	// End is the inclusive upper bound of the query time range.
	End time.Time
	// Page is the 1-based page number used with Size for offset paging.
	Page int
	// Size caps the number of returned entries; a value <= 0 applies
	// a default limit of 1000.
	Size int
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
	// QueryLogs returns a page of logs for an asset within a time range,
	// plus the total count of matching rows. If q.Level is non-empty,
	// results are filtered by log level. The q.Size field caps the number
	// of returned entries; a value <= 0 applies a default limit of 1000.
	QueryLogs(ctx context.Context, q LogQuery) ([]CollectLog, int64, error)
}
