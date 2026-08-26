// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"

	"github.com/tickraft/tickraft/pkg/api/httputil"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"
)

// WithReportAudit wraps the unified telemetry report handler
// (POST /api/v1/telemetry) with an ingestion audit log. Each invocation
// emits one log entry (operation, outcome, status code, asset key, remote
// address, body size) so telemetry ingestion is traceable for operational
// forensics and abuse investigation. A 2xx/3xx status is audited as
// success; 4xx/5xx as rejected.
//
// A nil logger falls back to a no-op logger so the wrapper is safe to use
// in tests without explicit logging configuration.
func WithReportAudit(next app.HandlerFunc, logger *zap.Logger) app.HandlerFunc {
	if logger == nil {
		logger = zap.NewNop()
	}
	return func(ctx context.Context, arc *app.RequestContext) {
		// Capture the asset key and remote address before the handler runs.
		// The asset key header is the primary identifier for the reporting
		// source; the remote address provides a secondary attribution channel.
		assetKey := string(arc.GetHeader(httputil.HeaderAssetKey))
		remoteAddr := ""
		if ra := arc.RemoteAddr(); ra != nil {
			remoteAddr = ra.String()
		}
		bodySize := len(arc.Request.Body())

		next(ctx, arc)

		logger.Info("telemetry report received",
			zap.String("operation", "telemetry.report"),
			zap.String("outcome", auditOutcome(arc.Response.StatusCode())),
			zap.Int("status_code", arc.Response.StatusCode()),
			zap.String("asset_key", assetKey),
			zap.String("remote_addr", remoteAddr),
			zap.Int("body_size", bodySize),
		)
	}
}

// auditOutcome maps an HTTP status code to the audit outcome label:
// "rejected" for 4xx/5xx, "success" otherwise.
func auditOutcome(statusCode int) string {
	if statusCode >= 400 {
		return "rejected"
	}
	return "success"
}
