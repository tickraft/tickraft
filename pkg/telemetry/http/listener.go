// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package http provides the webhook listener for the telemetry
// engine. It receives telemetry via the unified endpoint registered by the
// API router layer.
//
// The Listener exposes a ReportHandler method returning a net/http.HandlerFunc
// registered on POST /api/v1/telemetry. The request body is a Telemetry
// struct whose Kind field selects the payload size limit and internal
// processing pipeline. The router applies the unified AssetKey middleware so
// that authentication is enforced before the handler runs. Two authentication
// modes are supported when a secret is configured via WithSecret:
//
//   - HMAC signature: the request must carry an X-Tickraft-Signature header
//     containing the hex-encoded HMAC-SHA256 of the raw request body.
//   - Asset key: when no secret is configured, the telemetry is
//     authenticated by resolving the asset via asset_id (or
//     asset_key + tenant_id) against the asset store.
//
// Implementations must be safe for concurrent use.
package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	nethttp "net/http"
	"sync"
	"time"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/quota"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/types"
	"go.uber.org/zap"
)

const (
	// webhookSourceType is the SourceType identifier stamped on telemetry
	// received through the webhook listener.
	webhookSourceType = "webhook"
	// maxHeartbeatBodySize limits Telemetry{Kind:"heartbeat"} payloads to 1 KiB.
	maxHeartbeatBodySize = 1 << 10
	// maxMetricsBodySize limits Telemetry{Kind:"metrics"} payloads to 64 KiB.
	maxMetricsBodySize = 64 << 10
	// maxLogsBodySize limits Telemetry{Kind:"logs"} payloads to 1 MiB.
	maxLogsBodySize = 1 << 20
	// maxTaskStatusBodySize limits Telemetry{Kind:"task_status"} payloads to 4 KiB.
	maxTaskStatusBodySize = 4 << 10
	// maxTaskExecStatusBodySize limits Telemetry{Kind:"task_execution_status"}
	// payloads to 16 KiB.
	maxTaskExecStatusBodySize = 16 << 10
)

// webhookListenerType is the Type() identifier for the webhook HTTPListener.
const webhookListenerType = "webhook"

// DailyEventCounter tracks telemetry event ingestion per UTC day for quota
// enforcement. When the UTC day changes the counter resets so the daily
// ceiling applies to a rolling UTC calendar day.
type DailyEventCounter struct {
	mu    sync.Mutex
	count int
	day   int
	year  int
}

// Allow increments the counter and returns true when the event is within the
// daily ceiling. When ceiling is 0 or negative the quota is treated as
// unlimited and Allow always returns true. On a new UTC day the counter
// resets before evaluating the ceiling.
func (c *DailyEventCounter) Allow(ceiling int) bool {
	if ceiling <= 0 {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	if now.Year() != c.year || now.YearDay() != c.day {
		c.year = now.Year()
		c.day = now.YearDay()
		c.count = 0
	}
	if c.count >= ceiling {
		return false
	}
	c.count++
	return true
}

// Listener is the webhook listener. It parses HTTP POST requests
// on the unified telemetry endpoint and forwards the resulting Telemetry to
// the ingest callback, which is typically wired to telemetry.Collector.Submit
// during API router setup.
//
// Listener implements telemetry.HTTPListener. It does not bind its own HTTP
// server; instead it exposes a net/http.HandlerFunc (via Handler or
// ReportHandler) that the API router registers on POST /api/v1/telemetry.
// The request body is a Telemetry struct; the Kind field selects the
// payload size limit and the internal processing pipeline. Two
// authentication modes are supported:
//
//   - HMAC signature: when a secret is configured, the request must carry an
//     X-Tickraft-Signature header containing the hex-encoded HMAC-SHA256 of
//     the raw request body.
//   - Asset key: when no secret is configured, the telemetry is authenticated
//     by resolving the asset via asset_id (or asset_key + tenant_id)
//     against the asset store; a non-existent asset is rejected.
//
// Implementations must be safe for concurrent use.
type Listener struct {
	secret  string
	store   asset.Store
	ingest  func(context.Context, *telemetry.Telemetry)
	logger  *zap.Logger
	counter *DailyEventCounter
}

// Option configures a Listener.
type Option func(*Listener)

// WithSecret sets the HMAC secret for signature verification. When empty,
// signature verification is disabled and asset-key authentication is used.
func WithSecret(secret string) Option {
	return func(h *Listener) { h.secret = secret }
}

// WithStore sets the asset store used for asset-key authentication
// and asset type/tenant resolution.
func WithStore(store asset.Store) Option {
	return func(h *Listener) { h.store = store }
}

// WithIngest sets the ingest callback that forwards parsed Telemetry values to
// the telemetry pipeline. It must be called before the handler methods are
// invoked; the API router typically sets it during route registration.
func WithIngest(ingest func(context.Context, *telemetry.Telemetry)) Option {
	return func(h *Listener) { h.ingest = ingest }
}

// WithLogger sets the structured logger.
func WithLogger(logger *zap.Logger) Option {
	return func(h *Listener) { h.logger = logger }
}

// New creates a new Listener with the given options.
func New(opts ...Option) *Listener {
	h := &Listener{
		logger:  zap.NewNop(),
		counter: &DailyEventCounter{},
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Compile-time assertion that Listener satisfies telemetry.HTTPListener.
var _ telemetry.HTTPListener = (*Listener)(nil)

// Type implements telemetry.HTTPListener. It returns the listener type
// identifier "webhook".
func (h *Listener) Type() string { return webhookListenerType }

// Handler implements telemetry.HTTPListener. It returns an
// http.HandlerFunc that parses the request body and forwards the
// resulting Telemetry to the supplied ingest callback. The ingest
// callback overrides any callback previously set via WithIngest.
//
// This is the SPI entry point used by telemetry.ListenerRegistry; the
// ReportHandler method delegates here with the WithIngest callback.
func (h *Listener) Handler(ingest func(context.Context, *telemetry.Telemetry)) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
			return
		}

		// Read up to the largest limit so we can parse Kind first; the
		// per-Kind limit is enforced after parsing.
		body, err := io.ReadAll(io.LimitReader(r.Body, int64(maxLogsBodySize)+1))
		if err != nil {
			nethttp.Error(w, "read body failed", nethttp.StatusBadRequest)
			return
		}
		// ignored because: body already fully read via io.ReadAll; Close just
		// releases the underlying connection and has no actionable error path.
		_ = r.Body.Close()

		if len(body) > maxLogsBodySize {
			nethttp.Error(w, "request body too large", nethttp.StatusRequestEntityTooLarge)
			return
		}

		// Authentication: HMAC signature when a secret is configured.
		if h.secret != "" {
			sig := r.Header.Get("X-Tickraft-Signature")
			if !h.verifySignature(body, sig) {
				nethttp.Error(w, "invalid signature", nethttp.StatusUnauthorized)
				return
			}
		}

		var req telemetryRequest
		if err := json.Unmarshal(body, &req); err != nil {
			nethttp.Error(w, "invalid JSON body", nethttp.StatusBadRequest)
			return
		}

		maxSize, ok := kindLimit(req.Kind)
		if !ok {
			nethttp.Error(w, "unknown telemetry kind: "+string(req.Kind), nethttp.StatusBadRequest)
			return
		}
		if len(body) > maxSize {
			nethttp.Error(w, "request body too large", nethttp.StatusRequestEntityTooLarge)
			return
		}

		if msg := validateRequest(&req); msg != "" {
			nethttp.Error(w, msg, nethttp.StatusBadRequest)
			return
		}

		report, status, ok := h.resolveTelemetry(r.Context(), &req.reportRequest, body, r.RemoteAddr)
		if !ok {
			nethttp.Error(w, "asset not found", status)
			return
		}

		ceiling := quota.Ceiling(quota.TypeDailyEvents)
		if !h.counter.Allow(ceiling) {
			nethttp.Error(w, "daily event quota exceeded", nethttp.StatusTooManyRequests)
			return
		}

		if ingest != nil {
			ingest(r.Context(), report)
		}
		w.WriteHeader(nethttp.StatusAccepted)
	}
}

// ReportHandler returns a net/http.HandlerFunc for the unified telemetry
// endpoint POST /api/v1/telemetry. It is the entry point that uses
// the ingest callback previously set via WithIngest. New callers should
// prefer Handler, the telemetry.HTTPListener SPI method.
func (h *Listener) ReportHandler() nethttp.HandlerFunc {
	return h.Handler(h.ingest)
}

// kindLimit returns the payload size limit for the given telemetry Kind.
// The second return value is false when kind is not recognized.
func kindLimit(kind string) (int, bool) {
	switch kind {
	case string(telemetry.KindHeartbeat):
		return maxHeartbeatBodySize, true
	case string(telemetry.KindMetrics):
		return maxMetricsBodySize, true
	case string(telemetry.KindLogs):
		return maxLogsBodySize, true
	case "task_status":
		return maxTaskStatusBodySize, true
	case "task_execution_status":
		return maxTaskExecStatusBodySize, true
	default:
		return 0, false
	}
}

// validateRequest validates kind-specific required fields. It returns an
// empty string when the request passes validation; otherwise it returns a
// human-readable message naming the missing field, which the caller writes
// as the 400 Bad Request body.
//
// The listener validates only the presence of required fields so
// that downstream consumers (e.g. extended task handlers) can trust the
// request shape. Task status processing logic itself is an extended concern
// and is not implemented here.
func validateRequest(req *telemetryRequest) string {
	switch req.Kind {
	case "task_status":
		// task_id and status are required.
		if req.TaskID <= 0 {
			return "invalid request: missing required field: task_id"
		}
		if req.Status == "" {
			return "invalid request: missing required field: status"
		}
	case "task_execution_status":
		// task_id, execution_id and status are required.
		if req.TaskID <= 0 {
			return "invalid request: missing required field: task_id"
		}
		if req.ExecutionID <= 0 {
			return "invalid request: missing required field: execution_id"
		}
		if req.Status == "" {
			return "invalid request: missing required field: status"
		}
	}
	return ""
}

// telemetryRequest is the JSON body schema for the unified telemetry
// endpoint. It embeds reportRequest with the
// former distributed endpoints while adding the Kind discriminator.
type telemetryRequest struct {
	// Kind identifies the telemetry data category (heartbeat, metrics,
	// logs, task_status, task_execution_status) and selects the payload
	// size limit.
	Kind string `json:"kind"`
	// TaskID identifies the task for task_status and task_execution_status
	// kinds. Required for those kinds (must be > 0).
	TaskID int64 `json:"task_id,omitempty"`
	// ExecutionID identifies a single task execution for the
	// task_execution_status kind. Required for that kind (must be > 0).
	ExecutionID int64 `json:"execution_id,omitempty"`
	// StartedAt is when the task execution started. Optional, used by the
	// task_execution_status kind.
	StartedAt time.Time `json:"started_at,omitempty"`
	// FinishedAt is when the task execution finished. Optional, used by
	// the task_execution_status kind.
	FinishedAt time.Time `json:"finished_at,omitempty"`
	// Output holds the task execution output. Optional, used by the
	// task_execution_status kind.
	Output string `json:"output,omitempty"`
	// Error holds the task execution error message. Optional, used by the
	// task_execution_status kind.
	Error string `json:"error,omitempty"`
	// Reason describes the task status transition reason. Optional, used
	// by task_status and task_execution_status kinds.
	Reason string `json:"reason,omitempty"`
	reportRequest
}

// reportRequest is the JSON body schema for the telemetry payload fields. It
// is embedded by telemetryRequest so the unified endpoint accepts the same
// asset identity and content fields as the former distributed endpoints.
type reportRequest struct {
	// AssetID identifies the asset the telemetry was collected from. Takes precedence over
	// AssetKey when both are set.
	AssetID int64 `json:"asset_id"`
	// AssetKey is the unique asset key, resolved via the store when
	// AssetID is zero. Requires TenantID to be set for lookup.
	AssetKey string `json:"asset_key,omitempty"`
	// TenantID is used together with AssetKey for store lookup.
	TenantID int64 `json:"tenant_id,omitempty"`
	// Metrics holds optional numerical metrics.
	Metrics map[string]float64 `json:"metrics,omitempty"`
	// LogContent holds optional log content.
	LogContent string `json:"log_content,omitempty"`
	// LogLevel is the severity of LogContent (defaults to "INFO").
	LogLevel string `json:"log_level,omitempty"`
	// Status is a pre-judged status string (e.g., "normal", "abnormal").
	Status string `json:"status,omitempty"`
}

// resolveTelemetry resolves the asset identity and builds a Telemetry.
// It returns (telemetry, httpStatus, true) on success, or (nil, httpStatus, false)
// when the asset cannot be resolved.
func (h *Listener) resolveTelemetry(ctx context.Context, req *reportRequest, body []byte, remoteAddr string) (*telemetry.Telemetry, int, bool) {
	assetID := req.AssetID
	assetType := types.AssetTypeDevice
	tenantID := req.TenantID

	// Resolve via explicit asset_id first, then asset_key lookup.
	if assetID > 0 && h.store != nil {
		a, lookupErr := h.store.GetByID(ctx, assetID)
		if lookupErr != nil || a == nil {
			h.logger.Warn("http listener: asset id not found",
				zap.Int64("asset_id", assetID),
				zap.Error(lookupErr),
			)
			return nil, nethttp.StatusNotFound, false
		}
		assetType = a.AssetType
		tenantID = a.TenantID
	} else if assetID <= 0 && req.AssetKey != "" && h.store != nil {
		a, lookupErr := h.store.GetByKey(ctx, req.TenantID, req.AssetKey)
		if lookupErr != nil || a == nil {
			h.logger.Warn("http listener: asset key not found",
				zap.String("asset_key", req.AssetKey),
				zap.Int64("tenant_id", req.TenantID),
				zap.Error(lookupErr),
			)
			return nil, nethttp.StatusNotFound, false
		}
		assetID = a.ID
		assetType = a.AssetType
		tenantID = a.TenantID
	} else if assetID <= 0 {
		// No secret and no usable asset identity: reject.
		if h.secret == "" {
			return nil, nethttp.StatusBadRequest, false
		}
		// With HMAC auth the reporter is trusted; allow asset_id-only
		// without a store for type resolution.
	}

	status := types.AssetStatusNormal
	if req.Status != "" {
		status = types.AssetStatus(req.Status)
	}

	logLevel := req.LogLevel
	if logLevel == "" {
		logLevel = "INFO"
	}

	report := &telemetry.Telemetry{
		AssetID:     assetID,
		TenantID:    tenantID,
		AssetType:   assetType,
		SourceType:  webhookSourceType,
		RemoteAddr:  remoteAddr,
		CollectedAt: time.Now(),
		RawData:     body,
		Metrics:     req.Metrics,
		LogContent:  req.LogContent,
		LogLevel:    logLevel,
		Status:      status,
	}
	return report, nethttp.StatusOK, true
}

// verifySignature checks that the provided hex-encoded signature matches the
// HMAC-SHA256 of the body using the configured secret.
func (h *Listener) verifySignature(body []byte, signature string) bool {
	if signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expected))
}
