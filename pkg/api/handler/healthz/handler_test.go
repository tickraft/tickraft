// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package healthz

import (
	"context"
	"encoding/json"
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

// newHealthzEngine wires a Handler onto a fresh route.Engine at the
// /healthz path, mirroring the production registration in routes.go.
func newHealthzEngine(h *Handler) *route.Engine {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.GET("/healthz", h.Healthz)
	return engine
}

// newDefaultHealthzEngine wires the DefaultHealthz fallback handler onto a
// fresh route.Engine, mirroring the no-injection branch of routes.go.
func newDefaultHealthzEngine() *route.Engine {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.GET("/healthz", DefaultHealthz)
	return engine
}

// openTestDB opens an in-memory SQLite database for healthz tests.
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

// decodeHealthzResponse decodes the ut response body into an api.Response
// plus the embedded status/checks payload. The data field is re-marshaled
// and decoded into a map to allow assertions on the checks map without a
// dedicated response struct.
func decodeHealthzResponse(t *testing.T, w *ut.ResponseRecorder) (status string, checks map[string]string) {
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
		checks = make(map[string]string, len(c))
		for k, v := range c {
			if sv, ok := v.(string); ok {
				checks[k] = sv
			}
		}
	}
	return status, checks
}

// TestHealthzHandlerAllHealthy verifies that when both DB and cache are
// available and responsive, the handler returns 200 with status=ok and each
// check reporting ok.
func TestHealthzHandlerAllHealthy(t *testing.T) {
	dbc := openTestDB(t)
	c := cache.NewLRU(16, time.Minute)
	h := NewHandler(dbc, c)
	engine := newHealthzEngine(h)

	w := ut.PerformRequest(engine, "GET", "/healthz", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusOK, w.Body.String())
	}
	status, checks := decodeHealthzResponse(t, w)
	if status != "ok" {
		t.Errorf("status = %q, want %q", status, "ok")
	}
	if checks["db"] != "ok" {
		t.Errorf("checks[db] = %q, want %q", checks["db"], "ok")
	}
	if checks["cache"] != "ok" {
		t.Errorf("checks[cache] = %q, want %q", checks["cache"], "ok")
	}
}

// TestHealthzHandlerNilDeps verifies that when neither DB nor cache is
// injected, the handler skips both checks and returns 200 with status=ok
// and an empty checks map.
func TestHealthzHandlerNilDeps(t *testing.T) {
	h := NewHandler(nil, nil)
	engine := newHealthzEngine(h)

	w := ut.PerformRequest(engine, "GET", "/healthz", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusOK, w.Body.String())
	}
	status, checks := decodeHealthzResponse(t, w)
	if status != "ok" {
		t.Errorf("status = %q, want %q", status, "ok")
	}
	if len(checks) != 0 {
		t.Errorf("checks = %v, want empty map", checks)
	}
}

// TestHealthzHandlerOnlyDB verifies that when only the DB is injected, the
// handler probes the DB and skips the cache check.
func TestHealthzHandlerOnlyDB(t *testing.T) {
	dbc := openTestDB(t)
	h := NewHandler(dbc, nil)
	engine := newHealthzEngine(h)

	w := ut.PerformRequest(engine, "GET", "/healthz", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	status, checks := decodeHealthzResponse(t, w)
	if status != "ok" {
		t.Errorf("status = %q, want %q", status, "ok")
	}
	if checks["db"] != "ok" {
		t.Errorf("checks[db] = %q, want %q", checks["db"], "ok")
	}
	if _, hasCache := checks["cache"]; hasCache {
		t.Errorf("checks should not contain cache entry, got %v", checks)
	}
}

// TestHealthzHandlerOnlyCache verifies that when only the cache is injected,
// the handler probes the cache and skips the DB check.
func TestHealthzHandlerOnlyCache(t *testing.T) {
	c := cache.NewLRU(16, time.Minute)
	h := NewHandler(nil, c)
	engine := newHealthzEngine(h)

	w := ut.PerformRequest(engine, "GET", "/healthz", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	status, checks := decodeHealthzResponse(t, w)
	if status != "ok" {
		t.Errorf("status = %q, want %q", status, "ok")
	}
	if checks["cache"] != "ok" {
		t.Errorf("checks[cache] = %q, want %q", checks["cache"], "ok")
	}
	if _, hasDB := checks["db"]; hasDB {
		t.Errorf("checks should not contain db entry, got %v", checks)
	}
}

// TestHealthzHandlerUnhealthyDB verifies that a closed database connection
// causes the handler to return 503 with status=unhealthy and the db check
// reporting failed.
func TestHealthzHandlerUnhealthyDB(t *testing.T) {
	dbc := openTestDB(t)
	closeUnderlyingDB(t, dbc)
	h := NewHandler(dbc, nil)
	engine := newHealthzEngine(h)

	w := ut.PerformRequest(engine, "GET", "/healthz", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
	status, checks := decodeHealthzResponse(t, w)
	if status != "unhealthy" {
		t.Errorf("status = %q, want %q", status, "unhealthy")
	}
	if v := checks["db"]; v == "" || v == "ok" {
		t.Errorf("checks[db] = %q, want a failed message", v)
	}
}

// TestDefaultHealthz verifies the fallback handler returns 200 with
// status=ok and no checks map (preserving the default behavior
// for deployments that do not wire a concrete Handler).
func TestDefaultHealthz(t *testing.T) {
	engine := newDefaultHealthzEngine()

	w := ut.PerformRequest(engine, "GET", "/healthz", nil)
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
	if data["status"] != "ok" {
		t.Errorf("status = %v, want %q", data["status"], "ok")
	}
}

// TestProbeCacheHealthy verifies probeCache returns true for a functional
// cache implementation.
func TestProbeCacheHealthy(t *testing.T) {
	c := cache.NewLRU(4, time.Minute)
	if !probeCache(context.Background(), c) {
		t.Error("probeCache(healthy) = false, want true")
	}
}

// TestNewHandlerConstructsFields verifies the constructor wires both
// dependencies onto the struct so subsequent Healthz calls can probe them.
func TestNewHandlerConstructsFields(t *testing.T) {
	dbc := openTestDB(t)
	c := cache.NewLRU(4, time.Minute)
	h := NewHandler(dbc, c)
	if h.dbc == nil {
		t.Error("db field is nil, want non-nil")
	}
	if h.cache == nil {
		t.Error("cache field is nil, want non-nil")
	}
}

// TestHealthzHandlerSignature verifies Healthz has the Hertz handler
// signature so it can be registered directly as an app.HandlerFunc.
func TestHealthzHandlerSignature(t *testing.T) {
	var _ = (*Handler)(nil).Healthz
	var _ = DefaultHealthz
}
