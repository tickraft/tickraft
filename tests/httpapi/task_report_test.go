// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/task"
)

// mockRemote is an HTTP server standing in for the remote executor: it
// captures the dispatch request (headers + body) and answers 200.
type mockRemote struct {
	server  *httptest.Server
	mu      sync.Mutex
	headers http.Header
	body    string
}

func startMockRemote(t *testing.T) *mockRemote {
	t.Helper()
	m := &mockRemote{}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		m.mu.Lock()
		m.headers = r.Header.Clone()
		m.body = string(b)
		m.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockRemote) snapshot() http.Header {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.headers == nil {
		return nil
	}
	return m.headers.Clone()
}

// waitFor polls fn every 100ms until it returns true or the deadline
// expires, failing the test on timeout.
func waitFor(t *testing.T, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s: condition not met within 15s", what)
}

// createReportAsset creates one asset and returns its id and key, used for
// asset-key authenticated telemetry reports.
func createReportAsset(hs *harness, token string) (id int64, key string) {
	hs.t.Helper()
	key = fmt.Sprintf("httpapi-modea-%d", time.Now().UnixNano()%1_000_000)
	status, env := hs.do("POST", "/api/v1/assets", map[string]any{
		"asset_type": "device",
		"asset_key":  key,
		"name":       key,
	}, token)
	var created struct {
		ID int64 `json:"id"`
	}
	hs.mustOK(status, env, "create report asset", &created)
	return created.ID, key
}

// doTelemetryReport POSTs a telemetry report authenticated with the
// X-Tickraft-Asset-Key header (the unified AssetKey middleware path) and
// returns the HTTP status.
func (hs *harness) doTelemetryReport(body map[string]any, assetKey string) int {
	hs.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		hs.t.Fatalf("marshal report body: %v", err)
	}
	req, err := http.NewRequestWithContext(hs.t.Context(), http.MethodPost,
		hs.baseURL+"/api/v1/telemetry", bytes.NewReader(raw))
	if err != nil {
		hs.t.Fatalf("build report request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tickraft-Asset-Key", assetKey)
	resp, err := hs.client.Do(req)
	if err != nil {
		hs.t.Fatalf("perform report request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// createModeATask creates a webhook task dispatching to the mock remote.
// reportStatus selects Mode A (true) or Mode B (false).
func createModeATask(hs *harness, token, name, url string, reportStatus bool) int64 {
	hs.t.Helper()
	status, env := hs.do("POST", "/api/v1/tasks", map[string]any{
		"name":          name,
		"executor_type": "webhook",
		"schedule":      "0 0 1 1 *",
		"enabled":       true,
		"report_status": reportStatus,
		"timeout":       30,
		"config":        map[string]any{"url": url},
	}, token)
	var created struct {
		ID int64 `json:"id"`
	}
	hs.mustOK(status, env, "create webhook task", &created)
	if created.ID == 0 {
		hs.t.Fatal("create webhook task: no id returned")
	}
	return created.ID
}

// executionWithStatus returns the id of the task's first execution row in
// the given status, or 0 when none exists yet.
func (hs *harness) executionWithStatus(token string, taskID int64, status string) int64 {
	pd := hs.listPage(token, fmt.Sprintf("/api/v1/tasks/%d/executions?page=1&size=50", taskID))
	for _, item := range pd.Items {
		if s, _ := item["status"].(string); s == status {
			if raw, ok := item["id"].(float64); ok {
				return int64(raw)
			}
		}
	}
	return 0
}

// loadExecution reads one execution row straight from the store.
func (hs *harness) loadExecution(id int64) task.Execution {
	var row task.Execution
	if err := hs.dbc.First(&row, "id = ?", id).Error; err != nil {
		hs.t.Fatalf("load execution %d: %v", id, err)
	}
	return row
}

// executionCount counts the execution rows of one task straight from the
// store.
func (hs *harness) executionCount(taskID int64) int64 {
	var count int64
	if err := hs.dbc.Model(&task.Execution{}).
		Where("task_id = ?", taskID).Count(&count).Error; err != nil {
		hs.t.Fatalf("count executions of %d: %v", taskID, err)
	}
	return count
}

// TestTaskModeAFullChain covers the Mode A loop end to end: dispatch opens a
// running row and hands the remote its task_ref (the run handle) through the
// X-Tickraft-Task-Ref header, the remote reports completion through
// /api/v1/telemetry with the same credential, the row closes as success, and
// a repeated report is idempotent.
func TestTaskModeAFullChain(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	remote := startMockRemote(t)
	assetID, assetKey := createReportAsset(hs, token)
	taskID := createModeATask(hs, token, "mode-a-full-chain", remote.server.URL, true)
	defer func() { _, _ = hs.do("DELETE", "/api/v1/tasks/"+jsonInt64(taskID), nil, token) }()

	status, env := hs.do("POST", "/api/v1/tasks/"+jsonInt64(taskID)+"/trigger", nil, token)
	if status != http.StatusOK && status != http.StatusAccepted {
		t.Fatalf("trigger task: expected 200/202, got %d code=%d (%s)", status, env.Code, env.Message)
	}

	// The dispatch reached the remote carrying the task_ref header.
	var headers http.Header
	waitFor(t, "dispatch reached remote", func() bool {
		headers = remote.snapshot()
		return headers != nil
	})
	taskRef := headers.Get("X-Tickraft-Task-Ref")
	if taskRef == "" {
		t.Fatal("dispatch header X-Tickraft-Task-Ref is empty")
	}

	// The running dispatch row exists and its run_id is the handed-out
	// credential.
	var execID int64
	waitFor(t, "running dispatch row", func() bool {
		execID = hs.executionWithStatus(token, taskID, task.StatusRunning)
		return execID > 0
	})
	if row := hs.loadExecution(execID); row.RunID != taskRef {
		t.Fatalf("running row run_id = %q, dispatch header task_ref = %q", row.RunID, taskRef)
	}

	// Remote reports completion with the credential.
	if code := hs.doTelemetryReport(map[string]any{
		"kind":     "task_status",
		"asset_id": assetID,
		"task_ref": taskRef,
		"status":   "completed",
		"output":   "remote done",
	}, assetKey); code != http.StatusAccepted {
		t.Fatalf("report completion: expected 202, got %d", code)
	}

	waitFor(t, "row closed as success", func() bool {
		return hs.executionWithStatus(token, taskID, task.StatusSuccess) == execID
	})
	row := hs.loadExecution(execID)
	if row.FinishedAt == nil {
		t.Fatal("closed row has no finished_at")
	}
	if row.Output != "remote done" {
		t.Fatalf("closed row output = %q, want %q", row.Output, "remote done")
	}

	// A repeated terminal report is accepted at the edge but dropped by the
	// consumer (idempotent close).
	if code := hs.doTelemetryReport(map[string]any{
		"kind":     "task_status",
		"asset_id": assetID,
		"task_ref": taskRef,
		"status":   "failed",
		"error":    "late report",
	}, assetKey); code != http.StatusAccepted {
		t.Fatalf("repeat report: expected 202, got %d", code)
	}
	after := hs.loadExecution(execID)
	if after.Status != task.StatusSuccess || after.FinishedAt == nil ||
		!after.FinishedAt.Equal(*row.FinishedAt) {
		t.Fatalf("terminal row mutated by repeat report: %+v", after)
	}
}

// TestTaskModeANumericTaskRef covers the task-number fallback: a reporter
// that only knows the task identity sends the task number as task_ref, and
// the report binds the task's latest running execution row.
func TestTaskModeANumericTaskRef(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	remote := startMockRemote(t)
	assetID, assetKey := createReportAsset(hs, token)
	taskID := createModeATask(hs, token, "mode-a-numeric-ref", remote.server.URL, true)
	defer func() { _, _ = hs.do("DELETE", "/api/v1/tasks/"+jsonInt64(taskID), nil, token) }()

	_, _ = hs.do("POST", "/api/v1/tasks/"+jsonInt64(taskID)+"/trigger", nil, token)

	var execID int64
	waitFor(t, "running dispatch row", func() bool {
		execID = hs.executionWithStatus(token, taskID, task.StatusRunning)
		return execID > 0
	})

	if code := hs.doTelemetryReport(map[string]any{
		"kind":     "task_status",
		"asset_id": assetID,
		"task_ref": jsonInt64(taskID),
		"status":   "failed",
		"error":    "remote failed",
	}, assetKey); code != http.StatusAccepted {
		t.Fatalf("numeric task_ref report: expected 202, got %d", code)
	}

	waitFor(t, "row closed as failed", func() bool {
		return hs.executionWithStatus(token, taskID, task.StatusFailed) == execID
	})
	row := hs.loadExecution(execID)
	if row.Error != "remote failed" {
		t.Fatalf("closed row error = %q, want %q", row.Error, "remote failed")
	}
}

// TestTaskModeAUnknownRefDropped covers the credential-mismatch rule: an
// unknown non-numeric task_ref is dropped by the consumer without inserting
// an execution row.
func TestTaskModeAUnknownRefDropped(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	remote := startMockRemote(t)
	assetID, assetKey := createReportAsset(hs, token)
	taskID := createModeATask(hs, token, "mode-a-unknown-ref", remote.server.URL, true)
	defer func() { _, _ = hs.do("DELETE", "/api/v1/tasks/"+jsonInt64(taskID), nil, token) }()

	_, _ = hs.do("POST", "/api/v1/tasks/"+jsonInt64(taskID)+"/trigger", nil, token)

	var execID int64
	waitFor(t, "running dispatch row", func() bool {
		execID = hs.executionWithStatus(token, taskID, task.StatusRunning)
		return execID > 0
	})

	if code := hs.doTelemetryReport(map[string]any{
		"kind":     "task_status",
		"asset_id": assetID,
		"task_ref": "c0ffee00000000000000000000000000",
		"status":   "completed",
	}, assetKey); code != http.StatusAccepted {
		t.Fatalf("unknown task_ref report: expected 202, got %d", code)
	}

	// The drop is asynchronous (event consumer); give it a moment, then the
	// row count must still be exactly the one dispatch row, still running.
	time.Sleep(500 * time.Millisecond)
	if got := hs.executionCount(taskID); got != 1 {
		t.Fatalf("execution rows = %d, want 1 (unknown credential must not create rows)", got)
	}
	if row := hs.loadExecution(execID); row.Status != task.StatusRunning {
		t.Fatalf("dispatch row status = %q, want running (unknown credential must not touch rows)", row.Status)
	}
}

// TestTaskModeAMissReportSweeper covers the miss-report fallback: a stale
// running row past its reap deadline is flipped to timeout by the engine
// sweeper.
func TestTaskModeAMissReportSweeper(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	remote := startMockRemote(t)
	taskID := createModeATask(hs, token, "mode-a-miss-report", remote.server.URL, true)
	defer func() { _, _ = hs.do("DELETE", "/api/v1/tasks/"+jsonInt64(taskID), nil, token) }()

	// Seed a running row whose trigger lies far past the reap deadline
	// (timeout floor 30s + grace 300s).
	stale := time.Now().Add(-10 * time.Minute)
	row := task.Execution{
		TaskID:      taskID,
		TenantID:    1,
		Status:      task.StatusRunning,
		StartedAt:   stale,
		TriggeredAt: &stale,
		TriggerType: string(task.TriggerTypeSchedule),
		RunID:       "run-sweep-httpapi",
	}
	if err := hs.dbc.Create(&row).Error; err != nil {
		t.Fatalf("seed stale row: %v", err)
	}

	// A fresh engine with the execution store sweeps stale rows on startup;
	// the harness engine's own 30s ticker would catch it eventually.
	sweeper, err := task.NewEngine(
		task.WithEventBus(hs.workerBus),
		task.WithExecutionStore(task.NewExecutionStore(hs.dbc)),
	)
	if err != nil {
		t.Fatalf("create sweeper engine: %v", err)
	}
	t.Cleanup(func() { _ = sweeper.Stop(t.Context()) })

	waitFor(t, "stale row reaped as timeout", func() bool {
		return hs.executionWithStatus(token, taskID, task.StatusTimeout) == row.ID
	})
	closed := hs.loadExecution(row.ID)
	if closed.FinishedAt == nil {
		t.Fatal("reaped row has no finished_at")
	}
}

// TestTaskStatusVocabularyRejected pins the merged contract: the former
// task-level active/paused values were removed together with the remote
// enable-flip semantics — such reports are rejected at the edge with 400 and
// the task's enabled flag stays a control-plane concern.
func TestTaskStatusVocabularyRejected(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	remote := startMockRemote(t)
	assetID, assetKey := createReportAsset(hs, token)
	taskID := createModeATask(hs, token, "mode-a-vocab", remote.server.URL, true)
	defer func() { _, _ = hs.do("DELETE", "/api/v1/tasks/"+jsonInt64(taskID), nil, token) }()

	for _, status := range []string{"active", "paused"} {
		if code := hs.doTelemetryReport(map[string]any{
			"kind":     "task_status",
			"asset_id": assetID,
			"task_ref": jsonInt64(taskID),
			"status":   status,
		}, assetKey); code != http.StatusBadRequest {
			t.Fatalf("%s report: expected 400, got %d", status, code)
		}
	}

	got, env := hs.do("GET", "/api/v1/tasks/"+jsonInt64(taskID), nil, token)
	var body struct {
		Enabled bool `json:"enabled"`
	}
	hs.mustOK(got, env, "get task", &body)
	if !body.Enabled {
		t.Fatal("task enabled flag changed by rejected reports (enable/disable is control-plane only)")
	}
}

// TestTaskModeBNoDispatchHeaders is the Mode B regression: without
// report_status the dispatch carries no task_ref header and the execution
// row closes straight from the schedule outcome.
func TestTaskModeBNoDispatchHeaders(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	remote := startMockRemote(t)
	taskID := createModeATask(hs, token, "mode-b-regression", remote.server.URL, false)
	defer func() { _, _ = hs.do("DELETE", "/api/v1/tasks/"+jsonInt64(taskID), nil, token) }()

	_, _ = hs.do("POST", "/api/v1/tasks/"+jsonInt64(taskID)+"/trigger", nil, token)

	var headers http.Header
	waitFor(t, "dispatch reached remote", func() bool {
		headers = remote.snapshot()
		return headers != nil
	})
	if headers.Get("X-Tickraft-Task-Ref") != "" {
		t.Fatalf("mode B dispatch carried the task_ref header: %v", headers)
	}

	waitFor(t, "mode B row closed as success", func() bool {
		return hs.executionWithStatus(token, taskID, task.StatusSuccess) > 0
	})
}
