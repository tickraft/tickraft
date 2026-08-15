// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httpapi

import (
	"net/http"
	"testing"
	"time"
)

func createMonitor(hs *harness, token, name string) int64 {
	hs.t.Helper()
	status, env := hs.do("POST", "/api/v1/telemetry/monitors", map[string]any{
		"name":        name,
		"description": "httpapi monitor",
		"asset_type":  "device",
		"mode":        "active",
		"type":        "icmp",
		"schedule":    "60s",
		"enabled":     false,
		"config":      map[string]any{"host": "127.0.0.1", "count": 1},
	}, token)
	var created struct {
		ID int64 `json:"id"`
	}
	hs.mustOK(status, env, "create monitor", &created)
	if created.ID == 0 {
		hs.t.Fatal("create monitor: no id returned")
	}
	return created.ID
}

// TestTelemetryMonitors covers monitor CRUD plus enable/disable, status,
// probe, history and logs endpoints.
func TestTelemetryMonitors(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	id := createMonitor(hs, token, "httpapi-monitor")
	defer func() { _, _ = hs.do("DELETE", "/api/v1/telemetry/monitors/"+jsonInt64(id), nil, token) }()

	// Get.
	status, env := hs.do("GET", "/api/v1/telemetry/monitors/"+jsonInt64(id), nil, token)
	var got map[string]any
	hs.mustOK(status, env, "get monitor", &got)
	if got["name"] != "httpapi-monitor" {
		t.Fatalf("get monitor: unexpected payload %v", got["name"])
	}

	// Update.
	status, env = hs.do("PUT", "/api/v1/telemetry/monitors/"+jsonInt64(id), map[string]any{
		"name":       "httpapi-monitor-v2",
		"asset_type": "device",
		"mode":       "active",
		"type":       "icmp",
		"schedule":   "120s",
		"enabled":    false,
		"config":     map[string]any{"host": "127.0.0.1", "count": 1},
	}, token)
	if status != http.StatusOK {
		t.Fatalf("update monitor: expected 200, got %d code=%d", status, env.Code)
	}

	// Enable / disable round-trip.
	status, env = hs.do("PUT", "/api/v1/telemetry/monitors/"+jsonInt64(id)+"/enable", nil, token)
	if status != http.StatusOK {
		t.Fatalf("enable monitor: expected 200, got %d code=%d (%s)", status, env.Code, env.Message)
	}
	status, env = hs.do("PUT", "/api/v1/telemetry/monitors/"+jsonInt64(id)+"/disable", nil, token)
	if status != http.StatusOK {
		t.Fatalf("disable monitor: expected 200, got %d code=%d (%s)", status, env.Code, env.Message)
	}

	// Status.
	status, env = hs.do("GET", "/api/v1/telemetry/monitors/"+jsonInt64(id)+"/status", nil, token)
	var st map[string]any
	hs.mustOK(status, env, "monitor status", &st)
	if _, ok := st["status"]; !ok {
		t.Fatalf("monitor status: missing status field, keys=%v", keysOf(st))
	}

	// History and logs (paginated PageData envelope; empty is acceptable).
	history := hs.listPage(token, "/api/v1/telemetry/monitors/"+jsonInt64(id)+"/history?page=1&page_size=10")
	if history.Page != 1 || history.PageSize != 10 {
		t.Fatalf("monitor history: pagination echo mismatch: %+v", history)
	}
	logs := hs.listPage(token, "/api/v1/telemetry/monitors/"+jsonInt64(id)+"/logs?page=1&page_size=10")
	if logs.Page != 1 || logs.PageSize != 10 {
		t.Fatalf("monitor logs: pagination echo mismatch: %+v", logs)
	}

	// List (mode filter accepted).
	pd := hs.listPage(token, "/api/v1/telemetry/monitors?page=1&page_size=100&mode=active")
	if pd.Total < 1 {
		t.Fatalf("list monitors: expected >=1, got %d", pd.Total)
	}
}

// TestTelemetryProbeMetadata verifies the prober/listener type metadata
// endpoints the frontend uses to populate type selectors.
func TestTelemetryProbeMetadata(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	status, env := hs.do("GET", "/api/v1/telemetry/probers", nil, token)
	var probers []map[string]any
	hs.mustOK(status, env, "probers", &probers)
	if len(probers) == 0 {
		t.Fatal("probers: empty list")
	}
	for _, p := range probers {
		if _, ok := p["type"]; !ok {
			t.Fatalf("probers: missing type field: %v", p)
		}
	}

	status, env = hs.do("GET", "/api/v1/telemetry/listeners", nil, token)
	var listeners []map[string]any
	hs.mustOK(status, env, "listeners", &listeners)
	if len(listeners) == 0 {
		t.Fatal("listeners: empty list")
	}
}

// TestTelemetryTemplates covers template listing, builtin seeding, custom
// CRUD, builtin write protection, and apply.
func TestTelemetryTemplates(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	// List returns a plain array containing the CE builtin set.
	status, env := hs.do("GET", "/api/v1/telemetry/templates", nil, token)
	var all []map[string]any
	hs.mustOK(status, env, "list templates", &all)
	if len(all) == 0 {
		t.Fatal("list templates: empty")
	}

	// Builtin list is a subset and all entries are builtin.
	status, env = hs.do("GET", "/api/v1/telemetry/templates/builtin", nil, token)
	var builtins []map[string]any
	hs.mustOK(status, env, "builtin templates", &builtins)
	if len(builtins) == 0 {
		t.Fatal("builtin templates: empty (builtin seed missing)")
	}
	for _, b := range builtins {
		if isTrue, ok := b["is_builtin"].(bool); !ok || !isTrue {
			t.Fatalf("builtin templates: non-builtin entry %v", b)
		}
	}

	// Builtin entries are read-only: update must fail with 403.
	builtinID := int64(0)
	if raw, ok := builtins[0]["id"].(float64); ok {
		builtinID = int64(raw)
	}
	if builtinID == 0 {
		t.Fatalf("builtin template has no numeric id: %v", builtins[0])
	}
	status, _ = hs.do("PUT", "/api/v1/telemetry/templates/"+jsonInt64(builtinID),
		map[string]any{"name": "hijack"}, token)
	if status != http.StatusForbidden {
		t.Fatalf("update builtin template: expected 403, got %d", status)
	}

	// Custom template CRUD.
	status, env = hs.do("POST", "/api/v1/telemetry/templates", map[string]any{
		"name":          "httpapi-custom-template",
		"description":   "custom",
		"category":      "network",
		"executor_type": "icmp",
		"config":        map[string]any{"count": 2, "timeout": 3},
	}, token)
	var created struct {
		ID        int64 `json:"id"`
		IsBuiltin bool  `json:"is_builtin"`
	}
	hs.mustOK(status, env, "create template", &created)
	if created.ID == 0 || created.IsBuiltin {
		t.Fatalf("create template: unexpected result %+v", created)
	}
	defer func() {
		_, _ = hs.do("DELETE", "/api/v1/telemetry/templates/"+jsonInt64(created.ID), nil, token)
	}()

	status, env = hs.do("PUT", "/api/v1/telemetry/templates/"+jsonInt64(created.ID),
		map[string]any{
			"name":          "httpapi-custom-template-v2",
			"description":   "custom v2",
			"category":      "network",
			"executor_type": "icmp",
			"config":        map[string]any{"count": 4},
		}, token)
	if status != http.StatusOK {
		t.Fatalf("update template: expected 200, got %d code=%d", status, env.Code)
	}

	// Apply creates a monitoring point from the template.
	status, env = hs.do("POST", "/api/v1/telemetry/templates/"+jsonInt64(created.ID)+"/apply",
		map[string]any{"name": "httpapi-applied-monitor"}, token)
	var applied struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	hs.mustOK(status, env, "apply template", &applied)
	if applied.ID == 0 || applied.Name != "httpapi-applied-monitor" {
		t.Fatalf("apply template: unexpected monitor %+v", applied)
	}
	defer func() {
		_, _ = hs.do("DELETE", "/api/v1/telemetry/monitors/"+jsonInt64(applied.ID), nil, token)
	}()
}

// TestTelemetryReportAuth asserts the telemetry report endpoint rejects
// requests without a valid asset key (fail-closed getter is NOT used here;
// the harness wires the real asset store).
func TestTelemetryReportAuth(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	// Without an asset key header the report endpoint must reject.
	status, _ := hs.do("POST", "/api/v1/telemetry", map[string]any{
		"kind":      "metric",
		"asset_key": "httpapi-report-asset",
		"payload":   map[string]any{"cpu": 1.0},
		"timestamp": time.Now().Unix(),
	}, token)
	if status != http.StatusUnauthorized {
		t.Fatalf("telemetry report without asset key: expected 401, got %d", status)
	}
}
