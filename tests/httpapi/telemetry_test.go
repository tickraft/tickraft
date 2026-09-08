// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/sonic"

	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/task"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/types"
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
		"config":      map[string]any{"address": "127.0.0.1", "count": 1},
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
		"config":     map[string]any{"address": "127.0.0.1", "count": 1},
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
	history := hs.listPage(token, "/api/v1/telemetry/monitors/"+jsonInt64(id)+"/history?page=1&size=10")
	if history.Page != 1 || history.Size != 10 {
		t.Fatalf("monitor history: pagination echo mismatch: %+v", history)
	}
	logs := hs.listPage(token, "/api/v1/telemetry/monitors/"+jsonInt64(id)+"/logs?page=1&size=10")
	if logs.Page != 1 || logs.Size != 10 {
		t.Fatalf("monitor logs: pagination echo mismatch: %+v", logs)
	}

	// List (mode filter accepted).
	pd := hs.listPage(token, "/api/v1/telemetry/monitors?page=1&size=100&mode=active")
	if pd.Total < 1 {
		t.Fatalf("list monitors: expected >=1, got %d", pd.Total)
	}
}

// TestTelemetryProbeMetadata verifies the prober/listener type metadata
// endpoints the frontend uses to populate type selectors. The prober list
// is derived from the executor registry, so the CE deployment must not
// advertise the udp ghost type.
func TestTelemetryProbeMetadata(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	status, env := hs.do("GET", "/api/v1/telemetry/probers", nil, token)
	var probers []map[string]any
	hs.mustOK(status, env, "probers", &probers)
	if len(probers) == 0 {
		t.Fatal("probers: empty list")
	}
	proberTypes := map[string]bool{}
	for _, p := range probers {
		ty, _ := p["type"].(string)
		if ty == "" {
			t.Fatalf("probers: missing type field: %v", p)
		}
		proberTypes[ty] = true
	}
	for _, want := range []string{"icmp", "tcp", "http"} {
		if !proberTypes[want] {
			t.Errorf("probers: missing %q: %v", want, proberTypes)
		}
	}
	if proberTypes["udp"] {
		t.Error("probers: udp advertised but no udp executor is registered")
	}

	status, env = hs.do("GET", "/api/v1/telemetry/listeners", nil, token)
	var listeners []map[string]any
	hs.mustOK(status, env, "listeners", &listeners)
	if len(listeners) == 0 {
		t.Fatal("listeners: empty list")
	}
}

// TestExecutorEnumeration verifies GET /api/v1/executors lists exactly the
// types the task CRUD prevalidation accepts (OpExecute semantics): local,
// webhook, and the dual-mode http executor — not the pure probers.
func TestExecutorEnumeration(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	status, env := hs.do("GET", "/api/v1/executors", nil, token)
	var executors []map[string]any
	hs.mustOK(status, env, "executors", &executors)
	typeSet := map[string]bool{}
	for _, e := range executors {
		ty, _ := e["type"].(string)
		if ty == "" {
			t.Fatalf("executors: missing type field: %v", e)
		}
		typeSet[ty] = true
	}
	for _, want := range []string{"http", "local", "webhook"} {
		if !typeSet[want] {
			t.Errorf("executors: missing %q: %v", want, typeSet)
		}
	}
	for _, absent := range []string{"icmp", "tcp", "udp"} {
		if typeSet[absent] {
			t.Errorf("executors: must not contain %q: %v", absent, typeSet)
		}
	}
}

// TestTelemetryMonitorSummary verifies the monitor summary endpoint returns
// full-dataset counts consistent with the rows created through the CRUD
// API, independent of list pagination.
func TestTelemetryMonitorSummary(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	// Baseline summary before any monitor exists in this harness.
	status, env := hs.do("GET", "/api/v1/telemetry/monitors/summary", nil, token)
	var before struct {
		Active   int64 `json:"active"`
		Passive  int64 `json:"passive"`
		Enabled  int64 `json:"enabled"`
		Disabled int64 `json:"disabled"`
	}
	hs.mustOK(status, env, "monitor summary", &before)

	// Create one enabled active point and one disabled passive point.
	status, env = hs.do("POST", "/api/v1/telemetry/monitors", map[string]any{
		"name":       "summary-active",
		"asset_type": "device",
		"mode":       "active",
		"type":       "icmp",
		"schedule":   "300s",
		"enabled":    true,
		"config":     map[string]any{"address": "127.0.0.1", "count": 1},
	}, token)
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("create active monitor: expected 200/201, got %d code=%d (%s)", status, env.Code, env.Message)
	}
	status, env = hs.do("POST", "/api/v1/telemetry/monitors", map[string]any{
		"name":       "summary-passive",
		"asset_type": "device",
		"mode":       "passive",
		"type":       "webhook",
		"schedule":   "",
		"enabled":    false,
		"config":     map[string]any{},
	}, token)
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("create passive monitor: expected 200/201, got %d code=%d (%s)", status, env.Code, env.Message)
	}

	status, env = hs.do("GET", "/api/v1/telemetry/monitors/summary", nil, token)
	var after struct {
		Active   int64 `json:"active"`
		Passive  int64 `json:"passive"`
		Enabled  int64 `json:"enabled"`
		Disabled int64 `json:"disabled"`
	}
	hs.mustOK(status, env, "monitor summary", &after)

	if after.Active != before.Active+1 {
		t.Errorf("active = %d, want %d", after.Active, before.Active+1)
	}
	if after.Passive != before.Passive+1 {
		t.Errorf("passive = %d, want %d", after.Passive, before.Passive+1)
	}
	if after.Enabled != before.Enabled+1 {
		t.Errorf("enabled = %d, want %d", after.Enabled, before.Enabled+1)
	}
	if after.Disabled != before.Disabled+1 {
		t.Errorf("disabled = %d, want %d", after.Disabled, before.Disabled+1)
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

	// Apply creates a monitoring point from the template. The applied point
	// must be an enabled active-mode prober of the template's executor
	// type (previously apply produced a mode-less point with the executor
	// type mis-stored as AssetType).
	status, env = hs.do("POST", "/api/v1/telemetry/templates/"+jsonInt64(created.ID)+"/apply",
		map[string]any{"name": "httpapi-applied-monitor"}, token)
	var applied struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Mode string `json:"mode"`
		Type string `json:"type"`
	}
	hs.mustOK(status, env, "apply template", &applied)
	if applied.ID == 0 || applied.Name != "httpapi-applied-monitor" {
		t.Fatalf("apply template: unexpected monitor %+v", applied)
	}
	if applied.Mode != "active" {
		t.Fatalf("apply template: mode = %q, want active", applied.Mode)
	}
	if applied.Type != "icmp" {
		t.Fatalf("apply template: type = %q, want icmp (template executor type)", applied.Type)
	}
	_, _ = hs.do("DELETE", "/api/v1/telemetry/monitors/"+jsonInt64(applied.ID), nil, token)
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

// TestTelemetryMonitorCapabilityGate verifies the create-time capability
// prevalidation for active monitoring points: a write-only executor type is
// rejected with 400, while probe types pass and passive points are exempt
// (their type names a listener, not an executor).
func TestTelemetryMonitorCapabilityGate(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	status, env := hs.do("POST", "/api/v1/telemetry/monitors", map[string]any{
		"name":       "gate-write-only",
		"asset_type": "device",
		"mode":       "active",
		"type":       "local",
		"schedule":   "60s",
		"enabled":    false,
	}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("create local monitor: expected 400, got %d code=%d (%s)", status, env.Code, env.Message)
	}

	status, _ = hs.do("POST", "/api/v1/telemetry/monitors", map[string]any{
		"name":       "gate-passive",
		"asset_type": "device",
		"mode":       "passive",
		"type":       "webhook",
		"enabled":    false,
	}, token)
	if status != http.StatusOK {
		t.Fatalf("create passive webhook monitor: expected 200, got %d", status)
	}
}

// routingRecordStore mirrors the production worker assembly's record
// routing: probe records go to the telemetry store, task executions to the
// task execution log. (The production type is unexported in
// internal/service; the harness wires its own copy.)
type routingRecordStore struct {
	tasks  executor.RecordStore
	probes executor.RecordStore
}

func (s routingRecordStore) Save(ctx context.Context, record executor.ExecutionRecord) error {
	if record.Operation == executor.OpProbe {
		return s.probes.Save(ctx, record)
	}
	return s.tasks.Save(ctx, record)
}

// TestTelemetryProbeRecordFlow pins the end-to-end probe persistence
// contract: an active point's probe execution lands in sys_probe_record
// (never in the task execution log), refreshes the point's runtime status
// column, and is surfaced by the status/history/logs endpoints.
func TestTelemetryProbeRecordFlow(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	// Probe the harness's own HTTP listener: a TCP dial that always
	// succeeds, keeping the flow deterministic.
	port, err := strconv.Atoi(hs.baseURL[strings.LastIndex(hs.baseURL, ":")+1:])
	if err != nil {
		t.Fatalf("parse harness port: %v", err)
	}
	probeCfg := map[string]any{"address": "127.0.0.1", "port": port}
	cfg, err := sonic.Marshal(probeCfg)
	if err != nil {
		t.Fatalf("marshal probe config: %v", err)
	}

	status, env := hs.do("POST", "/api/v1/telemetry/monitors", map[string]any{
		"name":     "httpapi-probe-flow",
		"mode":     "active",
		"type":     "tcp",
		"schedule": "60s",
		"enabled":  true,
		"config":   probeCfg,
	}, token)
	var created struct {
		ID int64 `json:"id"`
	}
	hs.mustOK(status, env, "create tcp monitor", &created)
	defer func() { _, _ = hs.do("DELETE", "/api/v1/telemetry/monitors/"+jsonInt64(created.ID), nil, token) }()

	// Simulate the prober service's trigger: publish an ExecutionTriggered
	// event for the synthetic probe task. The runner executes it and the
	// routing record store persists the result in the telemetry domain.
	payload := event.ExecutionPayload{
		ExecutionID:  strconv.FormatInt(-(telemetry.ProbeTaskIDOffset + created.ID), 10),
		ExecutorType: "tcp",
		Operation:    "probe",
		Action:       "triggered",
		RunID:        "httpapi-probe-flow",
		TriggerType:  "manual",
		Config:       string(cfg),
	}
	if err := event.Publish(context.Background(), hs.workerBus, event.TypeExecutionTriggered, payload); err != nil {
		t.Fatalf("publish trigger: %v", err)
	}

	// Wait for the async runner to persist the probe record.
	var records []telemetry.ProbeRecord
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := hs.dbc.Where("point_id = ?", created.ID).Find(&records).Error; err != nil {
			t.Fatalf("query probe records: %v", err)
		}
		if len(records) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(records) == 0 {
		t.Fatal("probe record was not persisted within 10s")
	}
	if records[0].Status != types.AssetStatusNormal {
		t.Fatalf("probe status = %q, want normal (loopback TCP dial)", records[0].Status)
	}

	// The point's runtime status column was refreshed by the same save.
	var point telemetry.MonitorPoint
	if err := hs.dbc.First(&point, "id = ?", created.ID).Error; err != nil {
		t.Fatalf("load monitor point: %v", err)
	}
	if point.Status != telemetry.MonitorStatusActive {
		t.Fatalf("point status = %q, want active after normal probe", point.Status)
	}

	// Probe rows must never appear in the task execution log.
	var legacy int64
	if err := hs.dbc.Model(&task.Execution{}).
		Where("task_id <= ?", -telemetry.ProbeTaskIDOffset).
		Count(&legacy).Error; err != nil {
		t.Fatalf("count probe rows: %v", err)
	}
	if legacy != 0 {
		t.Fatalf("sys_schedule_log probe rows = %d, want 0", legacy)
	}

	// Status endpoint: active and enriched with the latest probe timing.
	status, env = hs.do("GET", "/api/v1/telemetry/monitors/"+jsonInt64(created.ID)+"/status", nil, token)
	var st struct {
		Status      string `json:"status"`
		LastProbeAt string `json:"last_probe_at"`
	}
	hs.mustOK(status, env, "monitor status", &st)
	if st.Status != "active" {
		t.Fatalf("status endpoint: status = %q, want active", st.Status)
	}
	if st.LastProbeAt == "" {
		t.Fatal("status endpoint: last_probe_at missing")
	}

	// History and logs render the probe records.
	history := hs.listPage(token, "/api/v1/telemetry/monitors/"+jsonInt64(created.ID)+"/history?page=1&size=10")
	if history.Total != 1 || len(history.Items) != 1 {
		t.Fatalf("history: total=%d items=%d, want 1/1", history.Total, len(history.Items))
	}
	if history.Items[0]["status"] != "normal" {
		t.Fatalf("history entry status = %v, want normal", history.Items[0]["status"])
	}
	logs := hs.listPage(token, "/api/v1/telemetry/monitors/"+jsonInt64(created.ID)+"/logs?page=1&size=10")
	if logs.Total != 1 || len(logs.Items) != 1 {
		t.Fatalf("logs: total=%d items=%d, want 1/1", logs.Total, len(logs.Items))
	}
	if logs.Items[0]["level"] != "normal" {
		t.Fatalf("log entry level = %v, want normal", logs.Items[0]["level"])
	}
}

// TestTelemetryProbeNowAccepted covers the on-demand probe REST endpoint:
// POST /monitors/:id/probe dispatches a real probe through the prober
// scheduling engine and responds 202 Accepted with the point's current
// status; the probe record lands asynchronously. Disabled points are
// rejected with 400.
func TestTelemetryProbeNowAccepted(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	// Probe the harness's own HTTP listener: a TCP dial that always
	// succeeds, keeping the dispatched probe deterministic.
	port, err := strconv.Atoi(hs.baseURL[strings.LastIndex(hs.baseURL, ":")+1:])
	if err != nil {
		t.Fatalf("parse harness port: %v", err)
	}
	status, env := hs.do("POST", "/api/v1/telemetry/monitors", map[string]any{
		"name":     "httpapi-probe-now",
		"mode":     "active",
		"type":     "tcp",
		"schedule": "60s",
		"enabled":  true,
		"config":   map[string]any{"address": "127.0.0.1", "port": port},
	}, token)
	var created struct {
		ID int64 `json:"id"`
	}
	hs.mustOK(status, env, "create tcp monitor", &created)
	defer func() { _, _ = hs.do("DELETE", "/api/v1/telemetry/monitors/"+jsonInt64(created.ID), nil, token) }()

	status, env = hs.do("POST", "/api/v1/telemetry/monitors/"+jsonInt64(created.ID)+"/probe", nil, token)
	if status != http.StatusAccepted {
		t.Fatalf("probe now: expected 202, got %d code=%d (%s)", status, env.Code, env.Message)
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := sonic.Unmarshal(env.Data, &resp); err != nil {
		t.Fatalf("decode probe response: %v", err)
	}
	if resp.Status == "" {
		t.Fatalf("probe now: response carries no status: %s", env.Data)
	}

	// The dispatched probe is real: a record lands in the telemetry domain.
	var records []telemetry.ProbeRecord
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := hs.dbc.Where("point_id = ?", created.ID).Find(&records).Error; err != nil {
			t.Fatalf("query probe records: %v", err)
		}
		if len(records) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(records) == 0 {
		t.Fatal("on-demand probe did not persist a record within 10s")
	}

	// On-demand probes must never land in the task execution log.
	var legacy int64
	if err := hs.dbc.Model(&task.Execution{}).
		Where("task_id <= ?", -telemetry.ProbeTaskIDOffset).
		Count(&legacy).Error; err != nil {
		t.Fatalf("count probe rows: %v", err)
	}
	if legacy != 0 {
		t.Fatalf("sys_schedule_log probe rows = %d, want 0", legacy)
	}

	// Disabled points cannot be probed on demand.
	status, env = hs.do("PUT", "/api/v1/telemetry/monitors/"+jsonInt64(created.ID)+"/disable", nil, token)
	if status != http.StatusOK {
		t.Fatalf("disable monitor: expected 200, got %d code=%d (%s)", status, env.Code, env.Message)
	}
	status, env = hs.do("POST", "/api/v1/telemetry/monitors/"+jsonInt64(created.ID)+"/probe", nil, token)
	if status != http.StatusBadRequest {
		t.Fatalf("probe disabled point: expected 400, got %d code=%d (%s)", status, env.Code, env.Message)
	}
}
