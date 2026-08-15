// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/tickraft/tickraft/pkg/telemetry"
)

// MetricStoreInjector is the interface a metric store must satisfy for
// injection via WithTelemetryDataStores. It mirrors telemetry.MetricStore's
// query method.
type MetricStoreInjector interface {
	QueryMetrics(ctx context.Context, tenantID, assetID int64, metricName string, start, end time.Time, limit int) ([]telemetry.CollectMetric, error)
}

// LogStoreInjector is the interface a log store must satisfy for injection
// via WithTelemetryDataStores. It mirrors telemetry.LogStore's query method.
type LogStoreInjector interface {
	QueryLogs(ctx context.Context, tenantID, assetID int64, level string, start, end time.Time, limit int) ([]telemetry.CollectLog, error)
}

// Task represents a telemetry collection task definition. Each task
// describes how a specific asset is observed (probe schedule, target
// configuration, enabled state). The concrete persistence is provided by the
// injected Service implementation; the runtime falls
// back to an in-memory stub when no service is injected.
//
// The Mode and Type fields align with the unified telemetry.MonitorPoint
// model: Mode is "active" (probed by ProberService) or "passive" (receives
// data via a listener); Type identifies the prober executor (icmp, tcp,
// http) or listener type (webhook).
type Task struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	AssetType   string         `json:"asset_type"`
	AssetID     int64          `json:"asset_id,omitempty"`
	Mode        string         `json:"mode"`
	Type        string         `json:"type"`
	Schedule    string         `json:"schedule"`
	Enabled     bool           `json:"enabled"`
	Config      map[string]any `json:"config,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
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
	ListTasks(ctx context.Context, page, size int, filter Filter) ([]Task, int64, error)
	// GetTask returns a single telemetry task by ID.
	GetTask(ctx context.Context, id int64) (*Task, error)
	// CreateTask creates a new telemetry task from the given request.
	CreateTask(ctx context.Context, req *Task) (*Task, error)
	// UpdateTask updates an existing telemetry task identified by ID.
	UpdateTask(ctx context.Context, id int64, req *Task) (*Task, error)
	// DeleteTask deletes a telemetry task by ID.
	DeleteTask(ctx context.Context, id int64) error
}

// Telemetry is the unified payload accepted by POST /api/v1/telemetry. The
// Kind field routes the payload to the appropriate internal handler with a
// differentiated body size limit. This structure replaces the former
// distributed endpoints /api/v1/telemetry/heartbeat, /metrics, and /logs.
type Telemetry struct {
	// Kind identifies the telemetry data category and decides the
	// processing pipeline and payload size limit.
	Kind telemetry.Kind `json:"kind" binding:"required"`
	// AssetID identifies the reporting asset. It must be consistent with
	// the asset resolved by the AssetKey middleware.
	AssetID string `json:"asset_id" binding:"required"`
	// Payload is the kind-specific raw payload, interpreted by the
	// internal handler according to Kind.
	Payload json.RawMessage `json:"payload"`
	// Ts is the report time in Unix milliseconds.
	Ts int64 `json:"timestamp"`
}

// ReportHandler exposes the unified telemetry report endpoint. The
// concrete implementation is injected via the WithTelemetryReportHandler
// RouteOption and registered on POST /api/v1/telemetry with the asset-key
// middleware. The handler reads a Telemetry body, applies a differentiated
// payload size limit based on Kind, and forwards the parsed report to the
// telemetry pipeline.
type ReportHandler interface {
	// Report handles POST /api/v1/telemetry. The request body is a
	// Telemetry struct; the payload size limit is determined by Kind
	// (heartbeat 1 KiB, metrics 64 KiB, logs 1 MiB). Asset-key
	// authentication is enforced by the middleware registered on the
	// route group.
	Report(ctx context.Context, arc *app.RequestContext)
}
