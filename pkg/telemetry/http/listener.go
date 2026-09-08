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
// that authentication is enforced before the handler runs. Three
// authentication modes are supported:
//
//   - Per-point HMAC signature: each passive webhook point may carry its own
//     secret (config "secret" with auth_type "hmac"). A request carrying an
//     X-Tickraft-Signature header — the hex-encoded HMAC-SHA256 of the raw
//     request body — is verified against every registered per-point secret
//     (SecretRegistry, WithSecretRegistry). A match binds the report to the
//     owning point's asset: the request may omit asset identity entirely, and
//     an explicit asset_id naming a different asset is rejected.
//   - Global HMAC signature: failing a per-point match, the signature is
//     verified against the global secret configured via WithSecret. When a
//     global secret is configured, unsigned requests are rejected.
//   - Asset key: when no signature credential matches and no global secret
//     is configured, the telemetry is authenticated by resolving the asset
//     via asset_id (or asset_key + tenant_id) against the asset store.
//
// A presented signature that matches no credential is always rejected.
//
// Implementations must be safe for concurrent use.
package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	nethttp "net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/bytedance/sonic"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/quota"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/types"
)

// headerSignature is the report authentication header carrying the HMAC
// signature, mirroring the transport-side constant in pkg/api.
const headerSignature = "X-Tickraft-Signature"

const (
	// webhookSourceType is the SourceType identifier stamped on telemetry
	// received through the webhook listener.
	webhookSourceType = "webhook"
	// kindTaskStatus is the Kind discriminator for task execution status
	// telemetry reported by remote executors.
	kindTaskStatus = "task_status"
	// maxHeartbeatBodySize limits Telemetry{Kind:"heartbeat"} payloads to 1 KiB.
	maxHeartbeatBodySize = 1 << 10
	// maxMetricsBodySize limits Telemetry{Kind:"metrics"} payloads to 64 KiB.
	maxMetricsBodySize = 64 << 10
	// maxLogsBodySize limits Telemetry{Kind:"logs"} payloads to 1 MiB.
	maxLogsBodySize = 1 << 20
	// maxTaskStatusBodySize limits Telemetry{Kind:"task_status"} payloads to
	// 16 KiB (the kind carries the execution output).
	maxTaskStatusBodySize = 16 << 10
)

// taskStatusVocabulary is the closed status vocabulary of the task_status
// kind. The former task-level active/paused values were removed together
// with the remote enable-flip semantics; a report carrying a status outside
// this set is rejected at the edge so the reporter learns immediately.
var taskStatusVocabulary = map[string]bool{
	"running":   true,
	"completed": true,
	"failed":    true,
	"timeout":   true,
}

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
// payload size limit and the internal processing pipeline. Three
// authentication modes are supported (see the package comment for the full
// model): per-point HMAC signatures verified against the SecretRegistry,
// the global HMAC secret via WithSecret, and asset-key resolution against
// the asset store.
//
// Task status reports (kind task_status) take a dedicated path when a
// task-report callback is configured via WithTaskReport: after
// authentication the parsed report goes straight to the callback, bypassing
// the ingest pipeline. Without the callback the reports flow through ingest
// as raw telemetry, which is how the distributed collector deployment
// consumes them.
//
// Implementations must be safe for concurrent use.
type Listener struct {
	secret     string
	registry   *SecretRegistry
	store      asset.Store
	ingest     func(context.Context, *telemetry.Telemetry)
	taskReport telemetry.TaskReportCallback
	logger     *zap.Logger
	counter    *DailyEventCounter
}

// Option configures a Listener.
type Option interface {
	apply(*Listener)
}

// secretOption sets the HMAC secret for signature verification.
type secretOption string

func (o secretOption) apply(h *Listener) { h.secret = string(o) }

// WithSecret sets the HMAC secret for signature verification. When empty,
// signature verification falls back to per-point secrets (if a registry is
// configured) and asset-key authentication.
func WithSecret(secret string) Option { return secretOption(secret) }

// secretRegistryOption sets the per-point secret registry used for
// signature verification.
type secretRegistryOption struct {
	registry *SecretRegistry
}

func (o secretRegistryOption) apply(h *Listener) { h.registry = o.registry }

// WithSecretRegistry sets the per-point secret registry. A signature is
// first verified against the registry's per-point secrets; the global
// secret (WithSecret) remains the fallback credential.
func WithSecretRegistry(registry *SecretRegistry) Option {
	return secretRegistryOption{registry: registry}
}

// storeOption sets the asset store used for asset-key authentication and
// asset type/tenant resolution.
type storeOption struct {
	store asset.Store
}

func (o storeOption) apply(h *Listener) { h.store = o.store }

// WithStore sets the asset store used for asset-key authentication
// and asset type/tenant resolution.
func WithStore(store asset.Store) Option { return storeOption{store: store} }

// ingestOption sets the ingest callback that forwards parsed Telemetry
// values to the telemetry pipeline.
type ingestOption struct {
	ingest func(context.Context, *telemetry.Telemetry)
}

func (o ingestOption) apply(h *Listener) { h.ingest = o.ingest }

// WithIngest sets the ingest callback that forwards parsed Telemetry values to
// the telemetry pipeline. It must be called before the handler methods are
// invoked; the API router typically sets it during route registration.
func WithIngest(ingest func(context.Context, *telemetry.Telemetry)) Option {
	return ingestOption{ingest}
}

// taskReportOption sets the callback receiving parsed task status reports.
type taskReportOption struct {
	cb telemetry.TaskReportCallback
}

func (o taskReportOption) apply(h *Listener) { h.taskReport = o.cb }

// WithTaskReport sets the callback that receives task status reports (kind
// task_status) after authentication. When set, that kind bypasses the ingest
// pipeline entirely; when unset, it flows through ingest as raw telemetry so
// external wrappers (the distributed collector) can consume it.
func WithTaskReport(cb telemetry.TaskReportCallback) Option {
	return taskReportOption{cb: cb}
}

// loggerOption sets the structured logger.
type loggerOption struct {
	logger *zap.Logger
}

func (o loggerOption) apply(h *Listener) { h.logger = o.logger }

// WithLogger sets the structured logger.
func WithLogger(logger *zap.Logger) Option { return loggerOption{logger: logger} }

// New creates a new Listener with the given options.
func New(options ...Option) *Listener {
	h := &Listener{
		logger:  zap.NewNop(),
		counter: &DailyEventCounter{},
	}
	for _, o := range options {
		o.apply(h)
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
// The API router mounts this handler directly on the telemetry endpoint;
// the ReportHandler method delegates here with the WithIngest callback.
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

		// Authentication. A presented X-Tickraft-Signature must verify
		// against a registered per-point secret or, failing that, the
		// global secret. When the header is absent, a configured global
		// secret rejects the request; otherwise asset-key authentication
		// applies in resolveTelemetry.
		sigOwner, authed := h.authenticate(w, r, body)
		if !authed {
			return
		}

		// Prometheus text exposition pushes branch here: the body is the
		// exposition text format rather than JSON, and asset identity
		// arrives via the query string.
		if isExpositionContentType(r.Header.Get("Content-Type")) {
			h.handleExposition(w, r, body, sigOwner, ingest)
			return
		}

		req, errMsg, status := decodeTelemetry(body)
		if req == nil {
			nethttp.Error(w, errMsg, status)
			return
		}

		report, status, ok := h.resolveTelemetry(r.Context(), &req.reportRequest, body, r.RemoteAddr, sigOwner)
		if !ok {
			nethttp.Error(w, "asset not found", status)
			return
		}

		ceiling := quota.Ceiling(quota.TypeDailyEvents)
		if !h.counter.Allow(ceiling) {
			nethttp.Error(w, "daily event quota exceeded", nethttp.StatusTooManyRequests)
			return
		}

		// Task status reports with a configured callback are delivered to it
		// directly instead of the ingest pipeline; without a callback they
		// flow through ingest as raw telemetry, which is how the distributed
		// collector deployment consumes them.
		if isTaskKind(req.Kind) && h.taskReport != nil {
			h.taskReport(r.Context(), &telemetry.TaskReport{
				Kind:       telemetry.Kind(req.Kind),
				TaskRef:    req.TaskRef,
				Status:     req.Status,
				StartedAt:  req.StartedAt,
				FinishedAt: req.FinishedAt,
				Output:     req.Output,
				Error:      req.Error,
				Reason:     req.Reason,
				TenantID:   report.TenantID,
			})
			w.WriteHeader(nethttp.StatusAccepted)
			return
		}

		h.accept(r.Context(), w, report, ingest)
	}
}

// isTaskKind reports whether the kind discriminator selects a task status
// report category.
func isTaskKind(kind string) bool {
	return kind == kindTaskStatus
}

// authenticate verifies the request's signature credential (see the package
// comment for the authentication model) and writes the error response itself
// when authentication fails. It returns the per-point secret owner when the
// signature matched a per-point secret, and false when the request was
// rejected.
func (h *Listener) authenticate(w nethttp.ResponseWriter, r *nethttp.Request, body []byte) (*SecretOwner, bool) {
	var sigOwner *SecretOwner
	sig := r.Header.Get(headerSignature)
	if sig != "" {
		if h.registry != nil {
			if owner, ok := h.registry.Match(body, sig); ok {
				sigOwner = &owner
			}
		}
		if sigOwner == nil && !h.verifySignature(body, sig) {
			nethttp.Error(w, "invalid signature", nethttp.StatusUnauthorized)
			return nil, false
		}
	} else if h.secret != "" {
		nethttp.Error(w, "invalid signature", nethttp.StatusUnauthorized)
		return nil, false
	}
	return sigOwner, true
}

// decodeTelemetry unmarshals the request body, enforces the per-kind size
// limit and validates kind-specific required fields. On failure it returns a
// nil request together with the error message and HTTP status the caller
// should write.
func decodeTelemetry(body []byte) (req *telemetryRequest, errMsg string, status int) {
	var parsed telemetryRequest
	if err := sonic.Unmarshal(body, &parsed); err != nil {
		return nil, "invalid JSON body", nethttp.StatusBadRequest
	}
	maxSize, ok := kindLimit(parsed.Kind)
	if !ok {
		return nil, "unknown telemetry kind: " + parsed.Kind, nethttp.StatusBadRequest
	}
	if len(body) > maxSize {
		return nil, "request body too large", nethttp.StatusRequestEntityTooLarge
	}
	if msg := validateRequest(&parsed); msg != "" {
		return nil, msg, nethttp.StatusBadRequest
	}
	return &parsed, "", 0
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
	case kindTaskStatus:
		return maxTaskStatusBodySize, true
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
	if req.Kind == kindTaskStatus {
		// task_ref and status are required. task_ref is the dispatch
		// credential handed to the remote (the X-Tickraft-Task-Ref header /
		// {{task_ref}} variable value); a reporter that only knows the task
		// number sends it as a decimal string instead.
		if req.TaskRef == "" {
			return "invalid request: missing required field: task_ref"
		}
		if req.Status == "" {
			return "invalid request: missing required field: status"
		}
		if !taskStatusVocabulary[req.Status] {
			return "invalid request: unknown status for task_status: " + req.Status
		}
	}
	return ""
}

// telemetryRequest is the JSON body schema for the unified telemetry
// endpoint. It embeds reportRequest with the
// former distributed endpoints while adding the Kind discriminator.
type telemetryRequest struct {
	// Kind identifies the telemetry data category (heartbeat, metrics,
	// logs, task_status) and selects the payload size limit.
	Kind string `json:"kind"`
	// TaskRef identifies the reported execution for the task_status kind:
	// the dispatch credential (X-Tickraft-Task-Ref / {{task_ref}} value) or,
	// for reporters that only know the task identity, the task number as a
	// decimal string.
	TaskRef string `json:"task_ref,omitempty"`
	// StartedAt is when the task execution started. Optional, used by the
	// task_status kind.
	StartedAt time.Time `json:"started_at,omitempty"`
	// FinishedAt is when the task execution finished. Optional, used by
	// the task_status kind.
	FinishedAt time.Time `json:"finished_at,omitempty"`
	// Output holds the task execution output. Optional, used by the
	// task_status kind.
	Output string `json:"output,omitempty"`
	// Error holds the task execution error message. Optional, used by the
	// task_status kind.
	Error string `json:"error,omitempty"`
	// Reason describes the task status transition reason. Optional, used
	// by the task_status kind.
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

// assetResolution carries the resolved asset identity of a report request.
type assetResolution struct {
	assetID   int64
	tenantID  int64
	assetType types.AssetType
}

// fallbackResolution is the identity used when no store lookup refines the
// request: the reporter's own asset identity (possibly zero) with device
// type and the request's tenant.
func fallbackResolution(req *reportRequest) assetResolution {
	return assetResolution{assetID: req.AssetID, tenantID: req.TenantID, assetType: types.AssetTypeDevice}
}

// resolveAsset resolves the report's asset identity: per-point signature
// binding first, then explicit asset_id lookup, then asset_key lookup. When
// no usable identity exists the report is rejected unless an HMAC credential
// authenticated the reporter. It returns (resolution, httpStatus, true) on
// success, or (zero resolution, httpStatus, false) when rejected.
func (h *Listener) resolveAsset(
	ctx context.Context, req *reportRequest, sigOwner *SecretOwner,
) (assetResolution, int, bool) {
	switch {
	case sigOwner != nil && sigOwner.AssetID > 0:
		return h.resolveOwnedAsset(ctx, req, sigOwner)
	case req.AssetID > 0 && h.store != nil:
		return h.resolveAssetByID(ctx, req.AssetID)
	case req.AssetID <= 0 && req.AssetKey != "" && h.store != nil:
		return h.resolveAssetByKey(ctx, req)
	case req.AssetID <= 0:
		// No usable asset identity: reject unless an HMAC credential
		// (global secret or per-point secret from an unbound point)
		// authenticated the reporter, in which case the reporter is
		// trusted.
		if h.secret == "" && sigOwner == nil {
			return assetResolution{}, nethttp.StatusBadRequest, false
		}
	}
	return fallbackResolution(req), nethttp.StatusOK, true
}

// resolveOwnedAsset binds the report to the per-point credential's asset: an
// explicit asset_id naming a different asset is a cross-asset escalation
// attempt and is rejected. Store lookup refines type and tenant when
// available.
func (h *Listener) resolveOwnedAsset(
	ctx context.Context, req *reportRequest, sigOwner *SecretOwner,
) (assetResolution, int, bool) {
	if req.AssetID > 0 && req.AssetID != sigOwner.AssetID {
		h.logger.Warn("http listener: signature asset mismatch",
			zap.Int64("point_id", sigOwner.PointID),
			zap.Int64("owner_asset_id", sigOwner.AssetID),
			zap.Int64("request_asset_id", req.AssetID),
		)
		return assetResolution{}, nethttp.StatusForbidden, false
	}
	res := assetResolution{assetID: sigOwner.AssetID, tenantID: req.TenantID, assetType: types.AssetTypeDevice}
	if h.store != nil {
		if a, lookupErr := h.store.GetByID(ctx, res.assetID); lookupErr == nil && a != nil {
			res.assetType = a.AssetType
			res.tenantID = a.TenantID
		}
	}
	return res, nethttp.StatusOK, true
}

// resolveAssetByID resolves an explicit asset_id against the store. An
// unknown id is rejected.
func (h *Listener) resolveAssetByID(ctx context.Context, assetID int64) (assetResolution, int, bool) {
	a, lookupErr := h.store.GetByID(ctx, assetID)
	if lookupErr != nil || a == nil {
		h.logger.Warn("http listener: asset id not found",
			zap.Int64("asset_id", assetID),
			zap.Error(lookupErr),
		)
		return assetResolution{}, nethttp.StatusNotFound, false
	}
	return assetResolution{assetID: a.ID, tenantID: a.TenantID, assetType: a.AssetType}, nethttp.StatusOK, true
}

// resolveAssetByKey resolves the report via asset_key + tenant_id lookup. An
// unknown key is rejected.
func (h *Listener) resolveAssetByKey(ctx context.Context, req *reportRequest) (assetResolution, int, bool) {
	a, lookupErr := h.store.GetByKey(ctx, req.TenantID, req.AssetKey)
	if lookupErr != nil || a == nil {
		h.logger.Warn("http listener: asset key not found",
			zap.String("asset_key", req.AssetKey),
			zap.Int64("tenant_id", req.TenantID),
			zap.Error(lookupErr),
		)
		return assetResolution{}, nethttp.StatusNotFound, false
	}
	return assetResolution{assetID: a.ID, tenantID: a.TenantID, assetType: a.AssetType}, nethttp.StatusOK, true
}

// resolveTelemetry resolves the asset identity and builds a Telemetry.
// It returns (telemetry, httpStatus, true) on success, or (nil, httpStatus, false)
// when the asset cannot be resolved. sigOwner is the per-point secret owner
// when the request was authenticated with a per-point signature; when the
// owning point is bound to an asset, the report is locked to that asset.
func (h *Listener) resolveTelemetry(
	ctx context.Context,
	req *reportRequest,
	body []byte,
	remoteAddr string,
	sigOwner *SecretOwner,
) (*telemetry.Telemetry, int, bool) {
	res, status, ok := h.resolveAsset(ctx, req, sigOwner)
	if !ok {
		return nil, status, false
	}

	statusValue := types.AssetStatusNormal
	if req.Status != "" {
		statusValue = types.AssetStatus(req.Status)
	}

	logLevel := req.LogLevel
	if logLevel == "" {
		logLevel = "INFO"
	}

	report := &telemetry.Telemetry{
		AssetID:     res.assetID,
		TenantID:    res.tenantID,
		AssetType:   res.assetType,
		SourceType:  webhookSourceType,
		RemoteAddr:  remoteAddr,
		CollectedAt: time.Now(),
		RawData:     body,
		Metrics:     req.Metrics,
		LogContent:  req.LogContent,
		LogLevel:    logLevel,
		Status:      statusValue,
	}
	return report, nethttp.StatusOK, true
}

// verifySignature checks that the provided hex-encoded signature matches the
// HMAC-SHA256 of the body using the configured secret. An empty secret never
// verifies: an HMAC over the empty key is computable by anyone and is not a
// credential.
func (h *Listener) verifySignature(body []byte, signature string) bool {
	if h.secret == "" || signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expected))
}
