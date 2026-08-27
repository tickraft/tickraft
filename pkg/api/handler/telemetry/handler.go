// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package telemetry exposes telemetry monitor CRUD, the unified report
// endpoint, and history/log query endpoints.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/telemetry"
)

// MetricStore is the interface a metric store must satisfy for injection
// via WithTelemetryDataStores. It narrows telemetry.MetricStore to the
// query method the monitor endpoints consume.
type MetricStore interface {
	QueryMetrics(ctx context.Context, q telemetry.MetricQuery) ([]telemetry.CollectMetric, int64, error)
}

// LogStore is the interface a log store must satisfy for injection
// via WithTelemetryDataStores. It narrows telemetry.LogStore to the
// query method the monitor endpoints consume.
type LogStore interface {
	QueryLogs(ctx context.Context, q telemetry.LogQuery) ([]telemetry.CollectLog, int64, error)
}

// ProbeRecordStore is the interface a probe record store must satisfy for
// injection via WithTelemetryProbeRecords. It narrows the query methods of
// telemetry.ProbeRecordStore used by the monitor endpoints. Active monitor
// points read their status, history, and log views from probe records;
// passive points fall back to the asset-level metric/log stores.
type ProbeRecordStore interface {
	// QueryByPoint returns a page of probe records for a monitor point.
	QueryByPoint(ctx context.Context, q telemetry.ProbeQuery) ([]telemetry.ProbeRecord, int64, error)
	// LatestByPoint returns the most recent probe record for a point.
	LatestByPoint(ctx context.Context, pointID int64) (*telemetry.ProbeRecord, error)
}

// Handler implements the telemetry monitoring point CRUD endpoints
// (registered under /api/v1/telemetry/monitors) for the runtime. The CRUD
// methods delegate to an injected telemetry.Service. The monitoring points
// are unified via the Mode field (active/passive), aligning with the
// telemetry.MonitorPoint model. The unified report endpoint
// (POST /api/v1/telemetry) is registered separately via
// WithTelemetryReportHandler.
type Handler struct {
	svc          telemetry.Service
	metricStore  MetricStore
	logStore     LogStore
	probeRecords ProbeRecordStore
}

// NewHandler creates a Handler backed by the given service. The service must
// be non-nil; callers must inject a concrete database-backed implementation.
func NewHandler(svc telemetry.Service) *Handler {
	return &Handler{svc: svc}
}

// SetDataStores injects the metric and log stores used by the history and
// logs endpoints of passive monitor points. Either store may be nil to
// disable the corresponding query path.
func (h *Handler) SetDataStores(metricStore MetricStore, logStore LogStore) {
	h.metricStore = metricStore
	h.logStore = logStore
}

// SetProbeRecordStore injects the probe record store used by the status,
// history, and logs endpoints of active monitor points. A nil store
// disables the probe-backed query paths and the endpoints fall back to the
// enabled-derived defaults.
func (h *Handler) SetProbeRecordStore(store ProbeRecordStore) {
	h.probeRecords = store
}

// ListTelemetry handles GET /api/v1/telemetry/monitors. It returns a page
// of telemetry monitoring points ordered by ascending ID. The optional mode
// query parameter filters by monitoring mode: "active" (probed by
// ProberService), "passive" (receives via listener), or omitted/empty for
// all modes.
func (h *Handler) ListTelemetry(ctx context.Context, arc *app.RequestContext) {
	page, size, ok := httputil.ParsePaging(arc)
	if !ok {
		return
	}
	filter := telemetry.Filter{Mode: arc.Query("mode")}
	items, total, err := h.svc.ListTasks(ctx, page, size, filter)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.SuccessPage(arc, items, total, page, size)
}

// GetTelemetry handles GET /api/v1/telemetry/:id.
func (h *Handler) GetTelemetry(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	task, err := h.svc.GetTask(ctx, id)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, task)
}

// CreateTelemetry handles POST /api/v1/telemetry.
func (h *Handler) CreateTelemetry(ctx context.Context, arc *app.RequestContext) {
	var req telemetry.MonitorPoint
	if !httputil.BindAndValidate(arc, &req) {
		return
	}
	if req.Name == "" {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "name is required")
		return
	}
	if len(req.Name) > httputil.MaxNameLength {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"name exceeds maximum length of 255 characters")
		return
	}
	if len(req.Description) > httputil.MaxDescriptionLength {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"description exceeds maximum length of 1024 characters")
		return
	}
	created, err := h.svc.CreateTask(ctx, &req)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, created)
}

// UpdateTelemetry handles PUT /api/v1/telemetry/:id.
func (h *Handler) UpdateTelemetry(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	var req telemetry.MonitorPoint
	if !httputil.BindAndValidate(arc, &req) {
		return
	}
	if len(req.Name) > httputil.MaxNameLength {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"name exceeds maximum length of 255 characters")
		return
	}
	if len(req.Description) > httputil.MaxDescriptionLength {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"description exceeds maximum length of 1024 characters")
		return
	}
	req.ID = id
	updated, err := h.svc.UpdateTask(ctx, id, &req)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, updated)
}

// DeleteTelemetry handles DELETE /api/v1/telemetry/:id.
func (h *Handler) DeleteTelemetry(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	if err := h.svc.DeleteTask(ctx, id); err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, nil)
}

// monitorStatus is the response for the monitoring point status endpoint. It
// reports the task's enabled state, a derived health status, and — for
// active points with probe history — the latest probe timing and latency.
type monitorStatus struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Enabled     bool       `json:"enabled"`
	Status      string     `json:"status"`
	LastProbeAt *time.Time `json:"last_probe_at,omitempty"`
	LatencyMs   int64      `json:"latency_ms,omitempty"`
}

// monitorStatusResponse builds the status response for a monitoring point.
// Passive points and disabled points derive their status from Enabled.
// Active enabled points report the runtime-maintained Status column
// ("active" after a normal probe, "error" after a failed one, "inactive"
// before the first probe), enriched with the latest probe's timing and
// latency when a probe record store is injected.
func (h *Handler) monitorStatusResponse(ctx context.Context, point *telemetry.MonitorPoint) (monitorStatus, error) {
	status := telemetry.MonitorStatusInactive
	if point.Enabled {
		status = telemetry.MonitorStatusActive
		if point.IsActive() && point.Status != "" {
			status = point.Status
		}
	}
	resp := monitorStatus{
		ID:      point.ID,
		Name:    point.Name,
		Enabled: point.Enabled,
		Status:  status,
	}
	if !point.IsActive() || h.probeRecords == nil {
		return resp, nil
	}
	latest, err := h.probeRecords.LatestByPoint(ctx, point.ID)
	if err != nil {
		if errors.Is(err, errdefs.ErrNotFound) {
			return resp, nil
		}
		return resp, fmt.Errorf("query latest probe: %w", err)
	}
	resp.LastProbeAt = &latest.StartedAt
	resp.LatencyMs = latest.Duration
	return resp, nil
}

// GetMonitorStatus handles GET /api/v1/telemetry/monitors/:id/status. It
// loads the telemetry task and returns its enabled state and status. Active
// points report the probe-maintained runtime status plus the latest probe
// timing and latency.
func (h *Handler) GetMonitorStatus(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	task, err := h.svc.GetTask(ctx, id)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	resp, err := h.monitorStatusResponse(ctx, task)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, resp)
}

// monitorHistoryEntry represents a single historical data point for a
// monitoring task.
type monitorHistoryEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Value     any       `json:"value"`
	Status    string    `json:"status"`
	// Metric names the quantity carried by Value: "latency_ms" for active
	// probe rows, the collected metric name for passive rows. Passive rows
	// leave Status empty — the metric name is not a status.
	Metric string `json:"metric,omitempty"`
}

// GetMonitorHistory handles GET /api/v1/telemetry/monitors/:id/history.
// Active points return their probe history from the probe record store:
// one entry per probe with its latency (milliseconds) and result status.
// Passive points with an AssetID query the asset's collected metrics from
// the persistent metric store. Without a matching store an empty list is
// returned.
func (h *Handler) GetMonitorHistory(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	task, err := h.svc.GetTask(ctx, id)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	page, size, ok := httputil.ParsePaging(arc)
	if !ok {
		return
	}
	history := make([]monitorHistoryEntry, 0)
	var total int64

	end := time.Now()
	start := end.AddDate(0, 0, -7) // last 7 days

	switch {
	case task.IsActive() && h.probeRecords != nil:
		records, count, qErr := h.probeRecords.QueryByPoint(ctx, telemetry.ProbeQuery{
			PointID: task.ID,
			Start:   start,
			End:     end,
			Page:    page,
			Size:    size,
		})
		if qErr != nil {
			httputil.Fail(arc, fmt.Errorf("query monitor history: %w", qErr))
			return
		}
		total = count
		for i := range records {
			history = append(history, monitorHistoryEntry{
				Timestamp: records[i].StartedAt,
				Value:     records[i].Duration,
				Status:    string(records[i].Status),
				Metric:    "latency_ms",
			})
		}
	case h.metricStore != nil && task.AssetID > 0:
		metrics, count, qErr := h.metricStore.QueryMetrics(ctx, telemetry.MetricQuery{
			AssetID: task.AssetID,
			Start:   start,
			End:     end,
			Page:    page,
			Size:    size,
		})
		if qErr != nil {
			httputil.Fail(arc, fmt.Errorf("query monitor history: %w", qErr))
			return
		}
		total = count
		for i := range metrics {
			history = append(history, monitorHistoryEntry{
				Timestamp: metrics[i].Timestamp,
				Value:     metrics[i].MetricValue,
				Metric:    metrics[i].MetricName,
			})
		}
	}

	httputil.SuccessPage(arc, history, total, page, size)
}

// ProbeMonitor handles POST /api/v1/telemetry/monitors/:id/probe. It
// dispatches a real on-demand probe through the prober scheduling engine
// and responds 202 Accepted with the point's current status; the probe
// outcome is recorded asynchronously and clients poll the status/history
// endpoints for the refreshed result.
func (h *Handler) ProbeMonitor(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	point, err := h.svc.ProbeNow(ctx, id)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	resp, err := h.monitorStatusResponse(ctx, point)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.SuccessAccepted(arc, resp)
}

// monitorLogEntry represents a single log line for a monitoring task.
type monitorLogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// GetMonitorLogs handles GET /api/v1/telemetry/monitors/:id/logs. Active
// points return their probe records rendered as log entries (level = probe
// status, message = error or output). Passive points with an AssetID query
// the asset's collected logs from the persistent log store. Without a
// matching store an empty list is returned.
func (h *Handler) GetMonitorLogs(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	task, err := h.svc.GetTask(ctx, id)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	page, size, ok := httputil.ParsePaging(arc)
	if !ok {
		return
	}
	logs := make([]monitorLogEntry, 0)
	var total int64

	end := time.Now()
	start := end.AddDate(0, 0, -7) // last 7 days

	switch {
	case task.IsActive() && h.probeRecords != nil:
		records, count, qErr := h.probeRecords.QueryByPoint(ctx, telemetry.ProbeQuery{
			PointID: task.ID,
			Start:   start,
			End:     end,
			Page:    page,
			Size:    size,
		})
		if qErr != nil {
			httputil.Fail(arc, fmt.Errorf("query monitor logs: %w", qErr))
			return
		}
		total = count
		for i := range records {
			message := records[i].Error
			if message == "" {
				message = records[i].Output
			}
			logs = append(logs, monitorLogEntry{
				Timestamp: records[i].StartedAt,
				Level:     string(records[i].Status),
				Message:   message,
			})
		}
	case h.logStore != nil && task.AssetID > 0:
		entries, count, qErr := h.logStore.QueryLogs(ctx, telemetry.LogQuery{
			AssetID: task.AssetID,
			Start:   start,
			End:     end,
			Page:    page,
			Size:    size,
		})
		if qErr != nil {
			httputil.Fail(arc, fmt.Errorf("query monitor logs: %w", qErr))
			return
		}
		total = count
		for i := range entries {
			logs = append(logs, monitorLogEntry{
				Timestamp: entries[i].Timestamp,
				Level:     entries[i].Level,
				Message:   entries[i].Content,
			})
		}
	}

	httputil.SuccessPage(arc, logs, total, page, size)
}

// EnableMonitor handles PUT /api/v1/telemetry/monitors/:id/enable. It
// loads the telemetry task, sets Enabled=true, and persists the update.
func (h *Handler) EnableMonitor(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	task, err := h.svc.GetTask(ctx, id)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	task.Enabled = true
	updated, err := h.svc.UpdateTask(ctx, id, task)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, updated)
}

// DisableMonitor handles PUT /api/v1/telemetry/monitors/:id/disable. It
// loads the telemetry task, sets Enabled=false, and persists the update.
func (h *Handler) DisableMonitor(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	task, err := h.svc.GetTask(ctx, id)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	task.Enabled = false
	updated, err := h.svc.UpdateTask(ctx, id, task)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, updated)
}

// --- Monitor point type metadata ---
//
// The listeners endpoint returns the passive collection types supported by
// the current runtime. These correspond to the Type field of the unified
// MonitorPoint model: passive points (Mode=passive) use listener types
// (webhook). The callers may extend this list with Syslog/SNMP/MQTT
// listeners via the Plugin SPI. Active point (prober) types are enumerated
// from the executor registry by the executor handler package.

// ListenerType describes a supported passive monitoring point type.
type ListenerType struct {
	// Type is the listener identifier (webhook). This value populates the
	// Type field of a MonitorPoint with Mode=passive.
	Type string `json:"type"`
	// Name is the human-readable display name.
	Name string `json:"name"`
	// Description is a short summary of the listener capability.
	Description string `json:"description,omitempty"`
}

// ceListenerTypes returns the listener types supported by the default
// runtime. The callers may extend this list with Syslog, SNMP, and MQTT
// listeners via the Plugin SPI.
var ceListenerTypes = []ListenerType{
	{Type: "webhook", Name: "HTTP Webhook", Description: "Receive events via HTTP POST webhook"},
}

// ListListeners handles GET /api/v1/telemetry/listeners. It returns the list
// of passive monitoring point types (listeners) supported by the current
// runtime. The default runtime supports the HTTP Webhook listener; the
// callers may add Syslog, SNMP, and MQTT via the Plugin SPI.
func (h *Handler) ListListeners(ctx context.Context, arc *app.RequestContext) {
	_ = ctx
	httputil.Success(arc, ceListenerTypes)
}

// GetMonitorSummary handles GET /api/v1/telemetry/monitors/summary. It
// returns aggregate monitor point counts (active/passive/enabled/disabled)
// computed over the full dataset, so the list summary chips stay correct
// regardless of pagination.
func (h *Handler) GetMonitorSummary(ctx context.Context, arc *app.RequestContext) {
	summary, err := h.svc.Summary(ctx)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, summary)
}
