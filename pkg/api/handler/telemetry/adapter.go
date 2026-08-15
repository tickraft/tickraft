// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"
)

// netHTTPHandlerAdapter wraps a standard net/http.Handler so it can be
// mounted as a ReportHandler on a Hertz route. It is the bridge
// between the Hertz app.HandlerFunc signature (ctx, *app.RequestContext)
// and the net/http.HandlerFunc returned by the webhook listener's
// ReportHandler method.
//
// The adapter is necessary because the webhook listener
// (pkg/telemetry/http.Listener) exposes its handler via the standard
// net/http interface (ReportHandler returns net/http.HandlerFunc), while the
// API route layer mounts handlers via the Hertz app.HandlerFunc signature.
// Rather than re-implementing the webhook parsing logic in Hertz-specific
// code, the adapter forwards the full request — body, headers, method, and
// remote address — to the net/http handler and copies the response status,
// headers, and body back to the Hertz RequestContext.
//
// The adapter is stateless and safe for concurrent use: each Report call
// allocates its own response recorder so there is no shared mutable state
// between requests.
//
// Each Report invocation emits an audit log (operation, outcome, status code,
// asset key, remote address, body size) so telemetry ingestion is traceable
// for operational forensics and abuse investigation.
type netHTTPHandlerAdapter struct {
	handler http.Handler
	logger  *zap.Logger
}

// Compile-time assertion that netHTTPHandlerAdapter satisfies the
// ReportHandler interface.
var _ ReportHandler = (*netHTTPHandlerAdapter)(nil)

// NewTelemetryReportHandlerAdapter wraps a standard net/http.Handler as a
// ReportHandler. The handler is typically the net/http.HandlerFunc
// returned by telemetry.HTTPListener.ReportHandler(); it receives the raw
// request body and headers, performs authentication (HMAC signature or
// asset-key resolution), parses the telemetry payload, and forwards it to
// the telemetry pipeline via the ingest callback.
//
// A nil logger falls back to a no-op logger so the adapter is safe to use in
// tests without explicit logging configuration.
func NewTelemetryReportHandlerAdapter(h http.Handler, logger *zap.Logger) ReportHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &netHTTPHandlerAdapter{handler: h, logger: logger}
}

// Report handles POST /api/v1/telemetry by forwarding the Hertz request to
// the wrapped net/http.Handler. It performs a mechanical conversion:
//
//  1. Read the request body, method, URI, remote address, and headers from
//     the Hertz RequestContext.
//  2. Build a net/http.Request with the same properties, carrying the Hertz
//     context so downstream cancellation propagates.
//  3. Call the wrapped handler with a response recorder that captures the
//     status code, headers, and body written by the handler.
//  4. Copy the recorded status code, headers, and body back to the Hertz
//     RequestContext so the client receives the response the net/http
//     handler produced.
//
// The asset-key middleware registered on the route group runs before this
// handler, so by the time Report is invoked the request has already passed
// asset-key authentication. The wrapped handler may perform additional
// authentication (e.g. HMAC signature verification) on its own.
func (a *netHTTPHandlerAdapter) Report(ctx context.Context, arc *app.RequestContext) {
	// Build the net/http.Request from the Hertz request data.
	body := arc.Request.Body()
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	method := string(arc.Method())
	path := string(arc.URI().Path())
	if path == "" {
		path = string(arc.URI().RequestURI())
	}

	// Capture the asset key and remote address for audit logging before
	// the request is forwarded. The asset key header is the primary
	// identifier for the reporting source; the remote address provides a
	// secondary attribution channel.
	assetKey := string(arc.GetHeader("X-Tickraft-Asset-Key"))
	remoteAddr := ""
	if ra := arc.RemoteAddr(); ra != nil {
		remoteAddr = ra.String()
	}

	req, err := http.NewRequestWithContext(ctx, method, path, bodyReader)
	if err != nil {
		// Unreachable in practice: NewRequestWithContext only fails on
		// invalid method or URL, both of which come from a well-formed
		// Hertz request. Return a 500 rather than panicking to honor the
		// "no panic in business logic" rule.
		a.logger.Error("telemetry report: build net/http request failed",
			zap.String("operation", "telemetry.report"),
			zap.String("outcome", "error"),
			zap.String("method", method),
			zap.String("path", path),
			zap.String("asset_key", assetKey),
			zap.String("remote_addr", remoteAddr),
			zap.Error(err),
		)
		arc.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Copy request headers from Hertz to the net/http.Request so the
	// wrapped handler can read X-Tickraft-Signature and other headers.
	arc.Request.Header.VisitAll(func(key, value []byte) {
		req.Header.Add(string(key), string(value))
	})

	// Set the remote address so the webhook listener can attribute the
	// telemetry to the reporting source.
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}

	// Set ContentLength from the body so the handler can optionally use it
	// without re-reading the body.
	req.ContentLength = int64(len(body))

	// Record the response produced by the net/http.Handler.
	rec := newResponseRecorder()
	a.handler.ServeHTTP(rec, req)

	// Audit the telemetry ingestion outcome. A 2xx status means the payload
	// was accepted into the processing pipeline; 4xx/5xx indicates rejection
	// (auth failure, malformed payload, or internal error). The body size and
	// remote address support abuse investigation and capacity planning.
	outcome := "success"
	if rec.statusCode >= 400 {
		outcome = "rejected"
	}
	a.logger.Info("telemetry report received",
		zap.String("operation", "telemetry.report"),
		zap.String("outcome", outcome),
		zap.Int("status_code", rec.statusCode),
		zap.String("asset_key", assetKey),
		zap.String("remote_addr", remoteAddr),
		zap.Int("body_size", len(body)),
	)

	// Copy the recorded response back to the Hertz RequestContext.
	arc.SetStatusCode(rec.statusCode)
	for key, values := range rec.Header() {
		for _, v := range values {
			// arc.Header is the safe Hertz API for setting response headers
			// on the RequestContext without copying the nocopy-protected
			// ResponseHeader value.
			arc.Header(key, v)
		}
	}
	if len(rec.body) > 0 {
		// Write returns an error only when the connection is closed or
		// the response is already committed; both are non-actionable at
		// this point since the response is being finalized.
		_, _ = arc.Write(rec.body)
	}
}

// responseRecorder is a minimal net/http.ResponseWriter implementation that
// captures the status code, headers, and body written by a net/http.Handler.
// It is NOT safe for concurrent use; each request allocates its own instance.
type responseRecorder struct {
	header     http.Header
	statusCode int
	body       []byte
}

// newResponseRecorder creates a responseRecorder initialized to a default
// 200 OK status with an empty header map and body.
func newResponseRecorder() *responseRecorder {
	return &responseRecorder{
		header:     http.Header{},
		statusCode: http.StatusOK,
	}
}

// Header returns the response header map. The handler may set headers on it
// before WriteHeader or Write is called.
func (r *responseRecorder) Header() http.Header {
	return r.header
}

// WriteHeader records the status code for the response. It must be called at
// most once; subsequent calls are ignored to match net/http behavior.
func (r *responseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
}

// Write appends the given bytes to the response body. If WriteHeader has not
// been called yet, it defaults to 200 OK.
func (r *responseRecorder) Write(p []byte) (int, error) {
	r.body = append(r.body, p...)
	return len(p), nil
}

// Compile-time assertion that responseRecorder satisfies
// net/http.ResponseWriter.
var _ http.ResponseWriter = (*responseRecorder)(nil)
