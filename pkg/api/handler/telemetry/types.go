// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"

	"github.com/tickraft/tickraft/pkg/telemetry"
)

// The wire and storage shapes are the same model: this package carries no
// Task DTO. telemetry.MonitorPoint holds both gorm and json tags, and the
// runtime-managed columns (TenantID, Interval, Timeout) serialize to
// nothing, so handlers bind and return the model type directly. Status is
// runtime-maintained too but exposed read-only. See
// docs/model-layering-design.md for the layering contract.

// MetricStore is the interface a metric store must satisfy for
// injection via WithTelemetryDataStores. It mirrors telemetry.MetricStore's
// query method.
type MetricStore interface {
	QueryMetrics(ctx context.Context, q telemetry.MetricQuery) ([]telemetry.CollectMetric, int64, error)
}

// LogStore is the interface a log store must satisfy for injection
// via WithTelemetryDataStores. It mirrors telemetry.LogStore's query method.
type LogStore interface {
	QueryLogs(ctx context.Context, q telemetry.LogQuery) ([]telemetry.CollectLog, int64, error)
}

// ProbeRecordStore is the interface a probe record store must satisfy for
// injection via WithTelemetryProbeRecords. It mirrors the query methods of
// telemetry.ProbeRecordStore used by the monitor endpoints. Active monitor
// points read their status, history, and log views from probe records;
// passive points fall back to the asset-level metric/log stores.
type ProbeRecordStore interface {
	// QueryByPoint returns a page of probe records for a monitor point.
	QueryByPoint(ctx context.Context, q telemetry.ProbeQuery) ([]telemetry.ProbeRecord, int64, error)
	// LatestByPoint returns the most recent probe record for a point.
	LatestByPoint(ctx context.Context, pointID int64) (*telemetry.ProbeRecord, error)
}

// Filter holds optional filtering criteria for listing telemetry
// tasks. A zero-value Filter matches all tasks. The Mode field
// filters by monitoring point mode ("active", "passive", or "" for all).
type Filter struct {
	// Mode filters tasks by monitoring point mode. An empty string matches
	// all modes. Valid values are "active" and "passive".
	Mode string
}

// Service defines the operations for managing telemetry collection
// tasks. The concrete implementation is injected via the WithTelemetryService
// RouteOption; when omitted, the handler package falls back to an in-memory
// implementation suitable for the runtime.
type Service interface {
	// ListTasks returns a page of telemetry tasks ordered by ascending
	// ID, plus the total count. The filter narrows results by mode when
	// filter.Mode is non-empty.
	ListTasks(ctx context.Context, page, size int, filter Filter) ([]telemetry.MonitorPoint, int64, error)
	// GetTask returns a single telemetry task by ID.
	GetTask(ctx context.Context, id int64) (*telemetry.MonitorPoint, error)
	// CreateTask creates a new telemetry task from the given request.
	CreateTask(ctx context.Context, req *telemetry.MonitorPoint) (*telemetry.MonitorPoint, error)
	// UpdateTask updates an existing telemetry task identified by ID.
	UpdateTask(ctx context.Context, id int64, req *telemetry.MonitorPoint) (*telemetry.MonitorPoint, error)
	// DeleteTask deletes a telemetry task by ID.
	DeleteTask(ctx context.Context, id int64) error
	// ProbeNow dispatches an on-demand probe for an active monitoring
	// point. It returns the point (for status rendering) after the probe
	// has been queued; the outcome lands in the probe record store
	// asynchronously. Passive or disabled points are rejected.
	ProbeNow(ctx context.Context, id int64) (*telemetry.MonitorPoint, error)
	// Summary returns aggregate monitor point counts by mode and enabled
	// state over the full dataset. It backs the monitor list summary chips
	// so the counts do not depend on the current page.
	Summary(ctx context.Context) (telemetry.PointSummary, error)
}
