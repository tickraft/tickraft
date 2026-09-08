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

// PointSummary aggregates monitor point counts by mode and enabled state.
// The mode counts and the enabled counts are independent dimensions over
// the same dataset (active+passive = enabled+disabled = total).
type PointSummary struct {
	// Active is the number of active probing points (Mode=ModeActive).
	Active int64 `json:"active"`
	// Passive is the number of passive receiving points (Mode=ModePassive).
	Passive int64 `json:"passive"`
	// Enabled is the number of enabled points across both modes.
	Enabled int64 `json:"enabled"`
	// Disabled is the number of disabled points across both modes.
	Disabled int64 `json:"disabled"`
}

// Filter holds optional filtering criteria for listing monitoring
// points. A zero-value Filter matches all points. The Mode field
// filters by monitoring point mode ("active", "passive", or "" for all);
// a positive AssetID narrows results to points bound to that asset.
type Filter struct {
	// Mode filters points by monitoring point mode. An empty string matches
	// all modes. Valid values are "active" and "passive".
	Mode string
	// AssetID filters points bound to the given asset. Zero matches points
	// regardless of asset binding.
	AssetID int64
}

// Service defines the operations for managing monitoring points. The
// concrete implementation (TelemetryService) is injected via the
// WithTelemetryService RouteOption; when omitted, the monitor routes are
// not registered.
type Service interface {
	// ListMonitors returns a page of monitoring points ordered by ascending
	// ID, plus the total count. The filter narrows results by mode when
	// filter.Mode is non-empty.
	ListMonitors(ctx context.Context, page, size int, filter Filter) ([]MonitorPoint, int64, error)
	// GetMonitor returns a single monitoring point by ID.
	GetMonitor(ctx context.Context, id int64) (*MonitorPoint, error)
	// CreateMonitor creates a new monitoring point from the given request.
	CreateMonitor(ctx context.Context, req *MonitorPoint) (*MonitorPoint, error)
	// UpdateMonitor updates an existing monitoring point identified by ID.
	UpdateMonitor(ctx context.Context, id int64, req *MonitorPoint) (*MonitorPoint, error)
	// DeleteMonitor deletes a monitoring point by ID.
	DeleteMonitor(ctx context.Context, id int64) error
	// ProbeNow dispatches an on-demand probe for an active monitoring
	// point. It returns the point (for status rendering) after the probe
	// has been queued; the outcome lands in the probe record store
	// asynchronously. Passive or disabled points are rejected.
	ProbeNow(ctx context.Context, id int64) (*MonitorPoint, error)
	// Summary returns aggregate monitor point counts by mode and enabled
	// state over the full dataset. It backs the monitor list summary chips
	// so the counts do not depend on the current page.
	Summary(ctx context.Context) (PointSummary, error)
}
