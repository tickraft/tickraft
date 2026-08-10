// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package readyz

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/cache"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newReadyEngine wires a Handler onto a fresh route.Engine at the
// /readyz path, mirroring the production registration in routes.go.
func newReadyEngine(h *Handler) *route.Engine {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.GET("/readyz", h.Ready)
	return engine
}

// newDefaultReadyEngine wires the DefaultReady fallback handler onto a
// fresh route.Engine.
func newDefaultReadyEngine() *route.Engine {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.GET("/readyz", DefaultReady)
	return engine
}

// openTestDB opens an in-memory SQLite database for readyz tests.
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbc, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return dbc
}

// closeUnderlyingDB closes the underlying *sql.DB of a gorm.DB so that
// subsequent queries fail. This simulates an unhealthy database dependency.
func closeUnderlyingDB(t *testing.T, dbc *gorm.DB) {
	t.Helper()
	sqlDB, err := dbc.DB()
	if err != nil {
		t.Fatalf("get underlying sql.DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql.DB: %v", err)
	}
}

// decodeReadyResponse decodes the ut response body into an api.Response
// plus the embedded status/checks payload. The checks map is decoded into
// a map of name -> {status, latency_ms} so assertions can inspect both the
// status and the latency field.
func decodeReadyResponse(t *testing.T, w *ut.ResponseRecorder) (status string, checks map[string]map[string]any) {
	t.Helper()
	var resp api.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%q)", err, w.Body.String())
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	if s, ok := data["status"].(string); ok {
		status = s
	}
	if c, ok := data["checks"].(map[string]any); ok {
		checks = make(map[string]map[string]any, len(c))
		for k, v := range c {
			if vm, ok := v.(map[string]any); ok {
				checks[k] = vm
			}
		}
	}
	return status, checks
}

// TestReadyHandlerAllReady verifies that when both DB and cache are
// available and responsive, the handler returns 200 with status=ready and
// each check reporting status=up.
func TestReadyHandlerAllReady(t *testing.T) {
	dbc := openTestDB(t)
	c := cache.NewLRU(16, time.Minute)
	h := NewHandler(dbc, c)
	engine := newReadyEngine(h)

	w := ut.PerformRequest(engine, "GET", "/readyz", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusOK, w.Body.String())
	}
	status, checks := decodeReadyResponse(t, w)
	if status != "ready" {
		t.Errorf("status = %q, want %q", status, "ready")
	}
	if got := checks["database"]["status"]; got != "up" {
		t.Errorf("checks[database].status = %v, want %q", got, "up")
	}
	if got := checks["cache"]["status"]; got != "up" {
		t.Errorf("checks[cache].status = %v, want %q", got, "up")
	}
}

// TestReadyHandlerNilDeps verifies that when neither DB nor cache is
// injected, the handler skips both checks and returns 200 with status=ready
// and an empty checks map.
func TestReadyHandlerNilDeps(t *testing.T) {
	h := NewHandler(nil, nil)
	engine := newReadyEngine(h)

	w := ut.PerformRequest(engine, "GET", "/readyz", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusOK, w.Body.String())
	}
	status, checks := decodeReadyResponse(t, w)
	if status != "ready" {
		t.Errorf("status = %q, want %q", status, "ready")
	}
	if len(checks) != 0 {
		t.Errorf("checks = %v, want empty map", checks)
	}
}

// TestReadyHandlerOnlyDB verifies that when only the DB is injected, the
// handler probes the DB and skips the cache check.
func TestReadyHandlerOnlyDB(t *testing.T) {
	dbc := openTestDB(t)
	h := NewHandler(dbc, nil)
	engine := newReadyEngine(h)

	w := ut.PerformRequest(engine, "GET", "/readyz", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	status, checks := decodeReadyResponse(t, w)
	if status != "ready" {
		t.Errorf("status = %q, want %q", status, "ready")
	}
	if got := checks["database"]["status"]; got != "up" {
		t.Errorf("checks[database].status = %v, want %q", got, "up")
	}
	if _, hasCache := checks["cache"]; hasCache {
		t.Errorf("checks should not contain cache entry, got %v", checks)
	}
}

// TestReadyHandlerDBDown verifies that a closed database connection causes
// the handler to return 503 with status=not_ready and the database check
// reporting status=down.
func TestReadyHandlerDBDown(t *testing.T) {
	dbc := openTestDB(t)
	closeUnderlyingDB(t, dbc)
	h := NewHandler(dbc, nil)
	engine := newReadyEngine(h)

	w := ut.PerformRequest(engine, "GET", "/readyz", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
	status, checks := decodeReadyResponse(t, w)
	if status != "not_ready" {
		t.Errorf("status = %q, want %q", status, "not_ready")
	}
	if got := checks["database"]["status"]; got != "down" {
		t.Errorf("checks[database].status = %v, want %q", got, "down")
	}
}

// TestReadyHandlerCustomCheckerDown verifies that a failing custom
// DependencyChecker causes the handler to return 503 while a passing custom
// checker still reports up. This covers the parallel-run path with mixed
// results.
func TestReadyHandlerCustomCheckerDown(t *testing.T) {
	up := &stubChecker{name: "upstream", err: nil}
	down := &stubChecker{name: "queue", err: errors.New("connection refused")}
	h := NewHandlerWithCheckers(up, down)
	engine := newReadyEngine(h)

	w := ut.PerformRequest(engine, "GET", "/readyz", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	status, checks := decodeReadyResponse(t, w)
	if status != "not_ready" {
		t.Errorf("status = %q, want %q", status, "not_ready")
	}
	if got := checks["upstream"]["status"]; got != "up" {
		t.Errorf("checks[upstream].status = %v, want %q", got, "up")
	}
	if got := checks["queue"]["status"]; got != "down" {
		t.Errorf("checks[queue].status = %v, want %q", got, "down")
	}
}

// TestReadyHandlerLatencyReported verifies that the latency_ms field is
// present and non-negative for a healthy check.
func TestReadyHandlerLatencyReported(t *testing.T) {
	dbc := openTestDB(t)
	h := NewHandler(dbc, nil)
	engine := newReadyEngine(h)

	w := ut.PerformRequest(engine, "GET", "/readyz", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	_, checks := decodeReadyResponse(t, w)
	lat, ok := checks["database"]["latency_ms"].(float64)
	if !ok {
		t.Fatalf("latency_ms = %v, want float64", checks["database"]["latency_ms"])
	}
	if lat < 0 {
		t.Errorf("latency_ms = %v, want >= 0", lat)
	}
}

// TestDefaultReady verifies the fallback handler returns 200 with
// status=ready and no checks map.
func TestDefaultReady(t *testing.T) {
	engine := newDefaultReadyEngine()

	w := ut.PerformRequest(engine, "GET", "/readyz", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp api.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Code != 0 {
		t.Errorf("code = %d, want 0", resp.Code)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	if data["status"] != "ready" {
		t.Errorf("status = %v, want %q", data["status"], "ready")
	}
}

// TestReadyHandlerSignature verifies Ready has the Hertz handler signature
// so it can be registered directly as an app.HandlerFunc.
func TestReadyHandlerSignature(t *testing.T) {
	var _ = (*Handler)(nil).Ready
	var _ = DefaultReady
}

// stubChecker is a test-only DependencyChecker that returns a preset error.
type stubChecker struct {
	name string
	err  error
}

// Name returns the stub dependency name.
func (s *stubChecker) Name() string { return s.name }

// Check returns the preset error (nil when healthy).
func (s *stubChecker) Check(ctx context.Context) error { return s.err }
