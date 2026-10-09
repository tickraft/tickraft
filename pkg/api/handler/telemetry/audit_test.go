// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// newAuditTestEngine builds a Hertz engine with the telemetry report route
// wired to WithReportAudit wrapping the given handler. It returns the engine
// and a zaptest observer so tests can assert on the audit log entries.
func newAuditTestEngine(t *testing.T, wrapped app.HandlerFunc) (*route.Engine, *observer.ObservedLogs) {
	t.Helper()
	core, recorded := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	h := route.NewEngine(config.NewOptions(nil))
	h.POST("/api/v1/telemetry", WithReportAudit(wrapped, logger))
	return h, recorded
}

// findAuditEntry returns the first observed log entry whose "operation" field
// matches the given value. It fails the test if no match is found.
func findAuditEntry(t *testing.T, logs *observer.ObservedLogs, operation string) observer.LoggedEntry {
	t.Helper()
	all := logs.All()
	for i := range all {
		if op, ok := fieldValue(all[i], "operation"); ok && op == operation {
			return all[i]
		}
	}
	t.Fatalf("audit log entry with operation=%q not found (total entries: %d)", operation, len(all))
	return observer.LoggedEntry{}
}

// fieldValue extracts a string field value from a zap log entry.
func fieldValue(e observer.LoggedEntry, key string) (string, bool) {
	for _, f := range e.Context {
		if f.Key == key {
			if f.Type == zapcore.StringType {
				return f.String, true
			}
		}
	}
	return "", false
}

// fieldInt extracts an int64 field value from a zap log entry.
func fieldInt(e observer.LoggedEntry, key string) (int64, bool) {
	for _, f := range e.Context {
		if f.Key == key {
			if f.Type == zapcore.Int64Type {
				return f.Integer, true
			}
		}
	}
	return 0, false
}

// statusHandler returns an app.HandlerFunc that responds with the given
// status code and no body.
func statusHandler(code int) app.HandlerFunc {
	return func(_ context.Context, arc *app.RequestContext) {
		arc.SetStatusCode(code)
	}
}

// TestAuditLogTelemetryReportSuccess verifies the telemetry.report audit log
// is emitted with outcome=success for a 2xx response.
func TestAuditLogTelemetryReportSuccess(t *testing.T) {
	engine, logs := newAuditTestEngine(t, statusHandler(http.StatusAccepted))

	body := []byte(`{"kind":"heartbeat"}`)
	w := ut.PerformRequest(engine, "POST", "/api/v1/telemetry",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "X-Tickraft-Asset-Key", Value: "audit-telemetry"})

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
	entry := findAuditEntry(t, logs, "telemetry.report")
	if out, _ := fieldValue(entry, "outcome"); out != "success" {
		t.Errorf("outcome = %q, want success", out)
	}
	if sc, ok := fieldInt(entry, "status_code"); !ok || sc != http.StatusAccepted {
		t.Errorf("status_code = %d, want %d", sc, http.StatusAccepted)
	}
	if key, _ := fieldValue(entry, "asset_key"); key != "audit-telemetry" {
		t.Errorf("asset_key = %q, want audit-telemetry", key)
	}
	if bs, ok := fieldInt(entry, "body_size"); !ok || bs != int64(len(body)) {
		t.Errorf("body_size = %d, want %d", bs, len(body))
	}
}

// TestAuditLogTelemetryReportRejected verifies the telemetry.report audit log
// records outcome=rejected when the handler returns a 4xx.
func TestAuditLogTelemetryReportRejected(t *testing.T) {
	engine, logs := newAuditTestEngine(t, statusHandler(http.StatusBadRequest))

	ut.PerformRequest(engine, "POST", "/api/v1/telemetry",
		&ut.Body{Body: bytes.NewReader([]byte(`{bad}`)), Len: 5},
		ut.Header{Key: "X-Tickraft-Asset-Key", Value: "rejected-source"})

	entry := findAuditEntry(t, logs, "telemetry.report")
	if out, _ := fieldValue(entry, "outcome"); out != "rejected" {
		t.Errorf("outcome = %q, want rejected", out)
	}
	if sc, ok := fieldInt(entry, "status_code"); !ok || sc != http.StatusBadRequest {
		t.Errorf("status_code = %d, want %d", sc, http.StatusBadRequest)
	}
}

// TestAuditLogTelemetryReportServerErr verifies a 5xx from the handler is
// audited as outcome=rejected.
func TestAuditLogTelemetryReportServerErr(t *testing.T) {
	engine, logs := newAuditTestEngine(t, statusHandler(http.StatusInternalServerError))

	ut.PerformRequest(engine, "POST", "/api/v1/telemetry",
		&ut.Body{Body: bytes.NewReader([]byte(`{}`)), Len: 2})

	entry := findAuditEntry(t, logs, "telemetry.report")
	if out, _ := fieldValue(entry, "outcome"); out != "rejected" {
		t.Errorf("outcome = %q, want rejected (5xx)", out)
	}
	if sc, ok := fieldInt(entry, "status_code"); !ok || sc != http.StatusInternalServerError {
		t.Errorf("status_code = %d, want %d", sc, http.StatusInternalServerError)
	}
}

// TestAuditLogTelemetryReportDefaultStatus verifies a handler that writes no
// status is audited with the implicit 200 default.
func TestAuditLogTelemetryReportDefaultStatus(t *testing.T) {
	engine, logs := newAuditTestEngine(t,
		func(_ context.Context, _ *app.RequestContext) {})

	ut.PerformRequest(engine, "POST", "/api/v1/telemetry",
		&ut.Body{Body: bytes.NewReader([]byte(`{}`)), Len: 2})

	entry := findAuditEntry(t, logs, "telemetry.report")
	if out, _ := fieldValue(entry, "outcome"); out != "success" {
		t.Errorf("outcome = %q, want success", out)
	}
	if sc, ok := fieldInt(entry, "status_code"); !ok || sc != http.StatusOK {
		t.Errorf("status_code = %d, want %d (implicit default)", sc, http.StatusOK)
	}
}

// TestAuditLogTelemetryReportMissingAssetKey verifies the audit log is still
// emitted when the asset key header is missing (empty string in the log).
func TestAuditLogTelemetryReportMissingAssetKey(t *testing.T) {
	engine, logs := newAuditTestEngine(t, statusHandler(http.StatusAccepted))

	ut.PerformRequest(engine, "POST", "/api/v1/telemetry",
		&ut.Body{Body: bytes.NewReader([]byte(`{}`)), Len: 2})

	entry := findAuditEntry(t, logs, "telemetry.report")
	if out, _ := fieldValue(entry, "outcome"); out != "success" {
		t.Errorf("outcome = %q, want success", out)
	}
	// asset_key should be empty (not missing) when the header is absent.
	if key, _ := fieldValue(entry, "asset_key"); key != "" {
		t.Errorf("asset_key = %q, want empty (missing header)", key)
	}
}

// TestWithReportAuditNilLogger verifies the wrapper falls back to a nop
// logger when passed nil, and the response still passes through unchanged.
func TestWithReportAuditNilLogger(t *testing.T) {
	h := route.NewEngine(config.NewOptions(nil))
	h.POST("/api/v1/telemetry",
		WithReportAudit(statusHandler(http.StatusAccepted), nil))

	w := ut.PerformRequest(h, "POST", "/api/v1/telemetry",
		&ut.Body{Body: bytes.NewReader([]byte(`{}`)), Len: 2})
	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
}
