// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
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

// newAdapterTestEngineWithLogger builds a Hertz engine with the telemetry
// report route wired to an adapter wrapping the given net/http.Handler. It
// returns the engine and a zaptest observer so tests can assert on the audit
// log entries emitted by the adapter.
func newAdapterTestEngineWithLogger(t *testing.T, netHandler http.Handler) (*route.Engine, *observer.ObservedLogs) {
	t.Helper()
	core, recorded := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	adapter := NewTelemetryReportHandlerAdapter(netHandler, logger)
	h := route.NewEngine(config.NewOptions(nil))
	h.POST("/api/v1/telemetry", func(ctx context.Context, arc *app.RequestContext) {
		adapter.Report(ctx, arc)
	})
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

// =============================================================================
// Header propagation tests (both directions)
// =============================================================================

// TestAdapterPropagatesAllRequestHeaders verifies all request headers sent
// by the Hertz client are forwarded to the wrapped net/http.Handler.
func TestAdapterPropagatesAllRequestHeaders(t *testing.T) {
	var receivedHeaders http.Header
	netHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	})

	engine, _ := newAdapterTestEngineWithLogger(t, netHandler)

	body := []byte(`{"kind":"heartbeat"}`)
	ut.PerformRequest(engine, "POST", "/api/v1/telemetry",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "X-Tickraft-Asset-Key", Value: "asset-123"},
		ut.Header{Key: "X-Tickraft-Signature", Value: "sha256=abc"},
		ut.Header{Key: "X-Custom-Header", Value: "custom-value"},
	)

	if got := receivedHeaders.Get("X-Tickraft-Asset-Key"); got != "asset-123" {
		t.Errorf("asset key header = %q, want asset-123", got)
	}
	if got := receivedHeaders.Get("X-Tickraft-Signature"); got != "sha256=abc" {
		t.Errorf("signature header = %q, want sha256=abc", got)
	}
	if got := receivedHeaders.Get("X-Custom-Header"); got != "custom-value" {
		t.Errorf("custom header = %q, want custom-value", got)
	}
}

// TestAdapterPropagatesResponseHeaders verifies response headers set by the
// net/http.Handler are copied back to the Hertz client.
func TestAdapterPropagatesResponseHeaders(t *testing.T) {
	netHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Response-Custom", "resp-value")
		w.Header().Set("X-Trace-Id", "trace-456")
		w.WriteHeader(http.StatusAccepted)
	})

	engine, _ := newAdapterTestEngineWithLogger(t, netHandler)

	w := ut.PerformRequest(engine, "POST", "/api/v1/telemetry",
		&ut.Body{Body: bytes.NewReader([]byte(`{}`)), Len: 2})

	if got := w.Header().Get("X-Response-Custom"); got != "resp-value" {
		t.Errorf("X-Response-Custom = %q, want resp-value", got)
	}
	if got := w.Header().Get("X-Trace-Id"); got != "trace-456" {
		t.Errorf("X-Trace-Id = %q, want trace-456", got)
	}
}

// =============================================================================
// Empty body handling
// =============================================================================

// TestAdapterEmptyBody verifies the adapter handles an empty request body
// without panicking and forwards it correctly.
func TestAdapterEmptyBody(t *testing.T) {
	var bodyReceived []byte
	netHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			bodyReceived = make([]byte, r.ContentLength)
			_, _ = r.Body.Read(bodyReceived)
		}
		w.WriteHeader(http.StatusAccepted)
	})

	engine, _ := newAdapterTestEngineWithLogger(t, netHandler)

	w := ut.PerformRequest(engine, "POST", "/api/v1/telemetry", nil)
	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
	if len(bodyReceived) != 0 {
		t.Errorf("body received = %q, want empty", string(bodyReceived))
	}
}

// TestAdapterLargeBody verifies the adapter forwards a large request body
// intact.
func TestAdapterLargeBody(t *testing.T) {
	var receivedSize int64
	netHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSize = r.ContentLength
		w.WriteHeader(http.StatusAccepted)
	})

	engine, _ := newAdapterTestEngineWithLogger(t, netHandler)

	// Build a 10KB body.
	largeBody := bytes.Repeat([]byte("a"), 10240)
	w := ut.PerformRequest(engine, "POST", "/api/v1/telemetry",
		&ut.Body{Body: bytes.NewReader(largeBody), Len: len(largeBody)})
	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
	if receivedSize != int64(len(largeBody)) {
		t.Errorf("received size = %d, want %d", receivedSize, len(largeBody))
	}
}

// =============================================================================
// Status code propagation
// =============================================================================

// TestAdapterStatusCodes verifies a range of HTTP status codes are propagated
// from the net/http.Handler to the Hertz response.
func TestAdapterStatusCodes(t *testing.T) {
	cases := []int{
		http.StatusOK,
		http.StatusCreated,
		http.StatusAccepted,
		http.StatusNoContent,
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusConflict,
		http.StatusRequestEntityTooLarge,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
	}
	for _, code := range cases {
		netHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		})
		engine, _ := newAdapterTestEngineWithLogger(t, netHandler)

		w := ut.PerformRequest(engine, "POST", "/api/v1/telemetry",
			&ut.Body{Body: bytes.NewReader([]byte(`{}`)), Len: 2})
		if w.Code != code {
			t.Errorf("code %d: got %d, want %d", code, w.Code, code)
		}
	}
}

// =============================================================================
// Response body propagation
// =============================================================================

// TestAdapterResponseBody verifies the response body written by the
// net/http.Handler is copied back to the Hertz client intact.
func TestAdapterResponseBody(t *testing.T) {
	netHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"accepted","queued":true}`))
	})

	engine, _ := newAdapterTestEngineWithLogger(t, netHandler)

	w := ut.PerformRequest(engine, "POST", "/api/v1/telemetry",
		&ut.Body{Body: bytes.NewReader([]byte(`{}`)), Len: 2})

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response: %v (body=%q)", err, w.Body.String())
	}
	if result["status"] != "accepted" {
		t.Errorf("status = %v, want accepted", result["status"])
	}
	if result["queued"] != true {
		t.Errorf("queued = %v, want true", result["queued"])
	}
}

// TestAdapterEmptyResponseBody verifies a handler that writes no body produces
// an empty Hertz response body.
func TestAdapterEmptyResponseBody(t *testing.T) {
	netHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	engine, _ := newAdapterTestEngineWithLogger(t, netHandler)

	w := ut.PerformRequest(engine, "POST", "/api/v1/telemetry",
		&ut.Body{Body: bytes.NewReader([]byte(`{}`)), Len: 2})
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", w.Body.String())
	}
}

// =============================================================================
// Multiple Write calls (streamed response)
// =============================================================================

// TestAdapterMultipleWrites verifies the adapter correctly concatenates
// multiple Write calls from the net/http.Handler.
func TestAdapterMultipleWrites(t *testing.T) {
	netHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("chunk1-"))
		_, _ = w.Write([]byte("chunk2-"))
		_, _ = w.Write([]byte("chunk3"))
	})

	engine, _ := newAdapterTestEngineWithLogger(t, netHandler)

	w := ut.PerformRequest(engine, "POST", "/api/v1/telemetry",
		&ut.Body{Body: bytes.NewReader([]byte(`{}`)), Len: 2})
	if w.Body.String() != "chunk1-chunk2-chunk3" {
		t.Errorf("body = %q, want chunk1-chunk2-chunk3", w.Body.String())
	}
}

// =============================================================================
// Audit log verification
// =============================================================================

// TestAuditLogTelemetryReportSuccess verifies the telemetry.report audit log
// is emitted with outcome=success for a 2xx response.
func TestAuditLogTelemetryReportSuccess(t *testing.T) {
	netHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	engine, logs := newAdapterTestEngineWithLogger(t, netHandler)

	body := []byte(`{"kind":"heartbeat"}`)
	ut.PerformRequest(engine, "POST", "/api/v1/telemetry",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "X-Tickraft-Asset-Key", Value: "audit-telemetry"})

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
// records outcome=rejected when the wrapped handler returns a 4xx/5xx.
func TestAuditLogTelemetryReportRejected(t *testing.T) {
	netHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad payload", http.StatusBadRequest)
	})

	engine, logs := newAdapterTestEngineWithLogger(t, netHandler)

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

// TestAuditLogTelemetryReportServerErr verifies a 5xx from the wrapped
// handler is audited as outcome=rejected.
func TestAuditLogTelemetryReportServerErr(t *testing.T) {
	netHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	engine, logs := newAdapterTestEngineWithLogger(t, netHandler)

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

// TestAuditLogTelemetryReportMissingAssetKey verifies the audit log is still
// emitted when the asset key header is missing (empty string in the log).
func TestAuditLogTelemetryReportMissingAssetKey(t *testing.T) {
	netHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	engine, logs := newAdapterTestEngineWithLogger(t, netHandler)

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

// =============================================================================
// Constructor tests
// =============================================================================

// TestNewTelemetryReportHandlerAdapterNilLogger verifies the constructor
// falls back to a nop logger when passed nil, and the adapter still
// functions without panicking.
func TestNewTelemetryReportHandlerAdapterNilLogger(t *testing.T) {
	netHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	adapter := NewTelemetryReportHandlerAdapter(netHandler, nil)

	h := route.NewEngine(config.NewOptions(nil))
	h.POST("/api/v1/telemetry", func(ctx context.Context, arc *app.RequestContext) {
		adapter.Report(ctx, arc)
	})

	w := ut.PerformRequest(h, "POST", "/api/v1/telemetry",
		&ut.Body{Body: bytes.NewReader([]byte(`{}`)), Len: 2})
	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
}

// =============================================================================
// Method forwarding
// =============================================================================

// TestAdapterMethodForwarding verifies the HTTP method is forwarded to the
// net/http.Handler. Although the route is registered as POST, the adapter
// should forward whatever method the Hertz engine delivers.
func TestAdapterMethodForwarding(t *testing.T) {
	var receivedMethod string
	netHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	})

	engine, _ := newAdapterTestEngineWithLogger(t, netHandler)

	ut.PerformRequest(engine, "POST", "/api/v1/telemetry",
		&ut.Body{Body: bytes.NewReader([]byte(`{}`)), Len: 2})

	if receivedMethod != http.MethodPost {
		t.Errorf("method = %q, want %q", receivedMethod, http.MethodPost)
	}
}
