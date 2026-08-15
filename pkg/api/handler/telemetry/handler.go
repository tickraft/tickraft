// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/telemetry"
)

// Sentinel service errors used by Service implementations. Each wraps
// the transport-agnostic errdefs sentinel so the response layer can map it to
// the correct HTTP status and business code automatically (see
// pkg/api/httputil/errors.go).
var (
	// ErrTelemetryTaskNotFound is returned when a telemetry task ID does not
	// exist. Maps to 404 NotFound.
	ErrTelemetryTaskNotFound = fmt.Errorf("telemetry task not found: %w", errdefs.ErrNotFound)
	// ErrInvalidRequest is returned when a service request is nil or otherwise
	// malformed. Maps to 400 BadRequest.
	ErrInvalidRequest = fmt.Errorf("invalid request: %w", errdefs.ErrInvalidArgument)
)

// Payload size limits for the unified telemetry report endpoint. The limit
// is selected based on Telemetry.Kind: heartbeats are tiny status pings,
// metrics carry moderate batches of numerical samples, and logs may contain
// large blocks of text.
const (
	// maxHeartbeatBodySize limits Telemetry{Kind:"heartbeat"} payloads to 1 KiB.
	maxHeartbeatBodySize = 1 << 10
	// maxMetricsBodySize limits Telemetry{Kind:"metrics"} payloads to 64 KiB.
	maxMetricsBodySize = 64 << 10
	// maxLogsBodySize limits Telemetry{Kind:"logs"} payloads to 1 MiB.
	maxLogsBodySize = 1 << 20
)

// Handler implements the telemetry monitoring point CRUD endpoints
// (registered under /api/v1/telemetry/monitors) and the unified report
// endpoint (POST /api/v1/telemetry) for the runtime. The CRUD
// methods delegate to an injected Service. The monitoring points are
// unified via the Mode field (active/passive), aligning with the
// telemetry.MonitorPoint model. The Report method reads a Telemetry body,
// applies a differentiated payload size limit based on Kind, and returns an
// Accepted response; concrete report processing is provided by an injected
// ReportHandler implementation (e.g. wrapping the collector HTTP listener).
type Handler struct {
	svc         Service
	metricStore MetricStoreInjector
	logStore    LogStoreInjector
}

// Compile-time assertion that Handler satisfies the ReportHandler interface.
var _ ReportHandler = (*Handler)(nil)

// NewHandler creates a Handler backed by the given service. The service must
// be non-nil; callers must inject a concrete database-backed implementation.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

// SetDataStores injects the metric and log stores used by the history and
// logs endpoints. Either store may be nil to disable the corresponding query
// path.
func (h *Handler) SetDataStores(metricStore MetricStoreInjector, logStore LogStoreInjector) {
	h.metricStore = metricStore
	h.logStore = logStore
}

// ListTelemetry handles GET /api/v1/telemetry/monitors. It returns a page
// of telemetry monitoring points ordered by ascending ID. The optional mode
// query parameter filters by monitoring mode: "active" (probed by
// ProberService), "passive" (receives via listener), or omitted/empty for
// all modes.
func (h *Handler) ListTelemetry(ctx context.Context, arc *app.RequestContext) {
	page, size := httputil.ParsePaging(arc)
	filter := Filter{Mode: string(arc.Query("mode"))}
	items, total, err := h.svc.ListTasks(ctx, page, size, filter)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.SuccessPage(arc, items, total, page, size)
}

// GetTelemetry handles GET /api/v1/telemetry/:id.
func (h *Handler) GetTelemetry(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	task, err := h.svc.GetTask(ctx, id)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, task)
}

// CreateTelemetry handles POST /api/v1/telemetry.
func (h *Handler) CreateTelemetry(ctx context.Context, arc *app.RequestContext) {
	var req Task
	if !api.BindAndValidate(arc, &req) {
		return
	}
	if req.Name == "" {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "name is required")
		return
	}
	if len(req.Name) > httputil.MaxNameLength {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "name exceeds maximum length of 255 characters")
		return
	}
	if len(req.Description) > httputil.MaxDescriptionLength {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "description exceeds maximum length of 1024 characters")
		return
	}
	created, err := h.svc.CreateTask(ctx, &req)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, created)
}

// UpdateTelemetry handles PUT /api/v1/telemetry/:id.
func (h *Handler) UpdateTelemetry(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	var req Task
	if !api.BindAndValidate(arc, &req) {
		return
	}
	if len(req.Name) > httputil.MaxNameLength {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "name exceeds maximum length of 255 characters")
		return
	}
	if len(req.Description) > httputil.MaxDescriptionLength {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "description exceeds maximum length of 1024 characters")
		return
	}
	req.ID = id
	updated, err := h.svc.UpdateTask(ctx, id, &req)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, updated)
}

// DeleteTelemetry handles DELETE /api/v1/telemetry/:id.
func (h *Handler) DeleteTelemetry(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	if err := h.svc.DeleteTask(ctx, id); err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, nil)
}

// monitorStatus is the response for the monitoring point status endpoint. It
// reports the task's enabled state and a derived health status.
type monitorStatus struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"`
}

// GetMonitorStatus handles GET /api/v1/telemetry/monitors/:id/status. It
// loads the telemetry task and returns its enabled state and a derived
// status string. The default implementation derives status from the task's
// Enabled field; the callers enriches this with real-time probe
// results.
func (h *Handler) GetMonitorStatus(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	task, err := h.svc.GetTask(ctx, id)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	status := "inactive"
	if task.Enabled {
		status = "active"
	}
	api.Success(arc, monitorStatus{
		ID:      task.ID,
		Name:    task.Name,
		Enabled: task.Enabled,
		Status:  status,
	})
}

// monitorHistoryEntry represents a single historical data point for a
// monitoring task.
type monitorHistoryEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Value     any       `json:"value"`
	Status    string    `json:"status"`
}

// GetMonitorHistory handles GET /api/v1/telemetry/monitors/:id/history. It
// returns historical metric data points for the monitoring task. When a
// MetricStore is injected and the task has an AssetID, metrics are queried
// from the persistent store; otherwise an empty list is returned.
func (h *Handler) GetMonitorHistory(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	task, err := h.svc.GetTask(ctx, id)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	page, size := httputil.ParsePaging(arc)
	history := make([]monitorHistoryEntry, 0)

	if h.metricStore != nil && task.AssetID > 0 {
		end := time.Now()
		start := end.AddDate(0, 0, -7) // last 7 days
		metrics, qErr := h.metricStore.QueryMetrics(ctx, 0, task.AssetID, "", start, end, size)
		if qErr != nil {
			api.Fail(arc, fmt.Errorf("query monitor history: %w", qErr))
			return
		}
		for i := range metrics {
			history = append(history, monitorHistoryEntry{
				Timestamp: metrics[i].Timestamp,
				Value:     metrics[i].MetricValue,
				Status:    metrics[i].MetricName,
			})
		}
	}

	api.SuccessPage(arc, history, int64(len(history)), page, size)
}

// ProbeMonitor handles POST /api/v1/telemetry/monitors/:id/probe. It
// returns the monitoring task's current runtime status. A full on-demand
// probe dispatch requires the executor pipeline and is provided by the
// extended edition; the open-source runtime returns the persisted status.
func (h *Handler) ProbeMonitor(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	task, err := h.svc.GetTask(ctx, id)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	status := "inactive"
	if task.Enabled {
		status = "active"
	}
	api.Success(arc, monitorStatus{
		ID:      task.ID,
		Name:    task.Name,
		Enabled: task.Enabled,
		Status:  status,
	})
}

// monitorLogEntry represents a single log line for a monitoring task.
type monitorLogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// GetMonitorLogs handles GET /api/v1/telemetry/monitors/:id/logs. It
// returns log entries for the monitoring task. When a LogStore is injected
// and the task has an AssetID, logs are queried from the persistent store;
// otherwise an empty list is returned.
func (h *Handler) GetMonitorLogs(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	task, err := h.svc.GetTask(ctx, id)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	page, size := httputil.ParsePaging(arc)
	logs := make([]monitorLogEntry, 0)

	if h.logStore != nil && task.AssetID > 0 {
		end := time.Now()
		start := end.AddDate(0, 0, -7) // last 7 days
		entries, qErr := h.logStore.QueryLogs(ctx, 0, task.AssetID, "", start, end, size)
		if qErr != nil {
			api.Fail(arc, fmt.Errorf("query monitor logs: %w", qErr))
			return
		}
		for i := range entries {
			logs = append(logs, monitorLogEntry{
				Timestamp: entries[i].Timestamp,
				Level:     entries[i].Level,
				Message:   entries[i].Content,
			})
		}
	}

	api.SuccessPage(arc, logs, int64(len(logs)), page, size)
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
		api.Fail(arc, err)
		return
	}
	task.Enabled = true
	updated, err := h.svc.UpdateTask(ctx, id, task)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, updated)
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
		api.Fail(arc, err)
		return
	}
	task.Enabled = false
	updated, err := h.svc.UpdateTask(ctx, id, task)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, updated)
}

// Report handles POST /api/v1/telemetry. It reads a Telemetry body,
// selects a payload size limit based on Kind (heartbeat 1 KiB, metrics
// 64 KiB, logs 1 MiB), enforces that limit, and returns a success
// response. Asset-key authentication is enforced by the middleware
// registered on the route group; the handler itself does not repeat that
// check. An unknown Kind results in a 400 Bad Request.
func (h *Handler) Report(ctx context.Context, arc *app.RequestContext) {
	_ = ctx
	var req Telemetry
	if !api.BindAndValidate(arc, &req) {
		return
	}
	maxSize, ok := kindBodyLimit(req.Kind)
	if !ok {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "unknown telemetry kind: "+string(req.Kind))
		return
	}
	if !readLimitedBody(arc, maxSize) {
		return
	}
	api.Success(arc, nil)
}

// kindBodyLimit returns the payload size limit for the given Kind.
// The second return value is false when kind is not recognized.
func kindBodyLimit(kind telemetry.Kind) (int, bool) {
	switch kind {
	case telemetry.KindHeartbeat:
		return maxHeartbeatBodySize, true
	case telemetry.KindMetrics:
		return maxMetricsBodySize, true
	case telemetry.KindLogs:
		return maxLogsBodySize, true
	default:
		return 0, false
	}
}

// readLimitedBody reads and discards the request body, enforcing a maximum
// size limit. It writes a 413 response when the body exceeds the limit and
// returns false so the caller can return early. The body is consumed and
// discarded because the stub does not process the payload; a
// concrete ReportHandler implementation injected via
// WithTelemetryReportHandler would read the body itself before this stub
// runs.
func readLimitedBody(arc *app.RequestContext, maxSize int) bool {
	body := arc.Request.Body()
	if len(body) > maxSize {
		api.FailWithCode(arc, http.StatusRequestEntityTooLarge, errdefs.CodeBadRequest, "request body too large")
		return false
	}
	return true
}

// --- Monitor point type metadata ---
//
// The probers and listeners endpoints return the collection types supported
// by the current runtime. These correspond to the Type field of the unified
// MonitorPoint model: active points (Mode=active) use prober types (icmp,
// tcp, http, udp); passive points (Mode=passive) use listener types
// (webhook). The callers may extend these lists with DNS/SSL probers
// and Syslog/SNMP/MQTT listeners via the Plugin SPI.

// ProberType describes a supported active monitoring point type.
type ProberType struct {
	// Type is the prober identifier (icmp, tcp, http, udp). This value
	// populates the Type field of a MonitorPoint with Mode=active.
	Type string `json:"type"`
	// Name is the human-readable display name.
	Name string `json:"name"`
	// Description is a short summary of the prober capability.
	Description string `json:"description,omitempty"`
}

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

// ceProberTypes returns the prober types supported by the default
// runtime. The callers may extend this list with DNS and SSL probers
// via the Plugin SPI.
var ceProberTypes = []ProberType{
	{Type: "icmp", Name: "ICMP Ping", Description: "Probe host reachability via ICMP echo requests"},
	{Type: "tcp", Name: "TCP Port", Description: "Probe TCP port connectivity and response time"},
	{Type: "http", Name: "HTTP", Description: "Probe HTTP endpoint availability and status code"},
	{Type: "udp", Name: "UDP Port", Description: "Probe UDP port connectivity"},
}

// ceListenerTypes returns the listener types supported by the default
// runtime. The callers may extend this list with Syslog, SNMP, and MQTT
// listeners via the Plugin SPI.
var ceListenerTypes = []ListenerType{
	{Type: "webhook", Name: "HTTP Webhook", Description: "Receive events via HTTP POST webhook"},
}

// ListProbers handles GET /api/v1/telemetry/probers. It returns the list of
// active monitoring point types (probers) supported by the current runtime.
// The default runtime supports ICMP, TCP, HTTP, and UDP; the callers
// may add DNS and SSL via the Plugin SPI.
func (h *Handler) ListProbers(ctx context.Context, arc *app.RequestContext) {
	_ = ctx
	api.Success(arc, ceProberTypes)
}

// ListListeners handles GET /api/v1/telemetry/listeners. It returns the list
// of passive monitoring point types (listeners) supported by the current
// runtime. The default runtime supports the HTTP Webhook listener; the
// callers may add Syslog, SNMP, and MQTT via the Plugin SPI.
func (h *Handler) ListListeners(ctx context.Context, arc *app.RequestContext) {
	_ = ctx
	api.Success(arc, ceListenerTypes)
}
