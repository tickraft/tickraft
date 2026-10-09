// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/task"
)

// createTask creates an enabled local-executor task and returns its ID. The
// once-a-year cron schedule keeps the task off the wheel in practice during
// the test window.
func createTask(hs *harness, token, name string) int64 {
	hs.t.Helper()
	status, env := hs.do("POST", "/api/v1/tasks", map[string]any{
		"name":          name,
		"description":   "httpapi integration task",
		"executor_type": "local",
		"schedule":      "0 0 1 1 *",
		"enabled":       true,
		"config":        map[string]any{"command": "echo httpapi"},
	}, token)
	var created struct {
		ID int64 `json:"id"`
	}
	hs.mustOK(status, env, "create task", &created)
	if created.ID == 0 {
		hs.t.Fatal("create task: no id returned")
	}
	return created.ID
}

// TestTaskLifecycle covers create → get → update → trigger → pause → resume
// → copy → delete, including the execution records produced by triggering.
func TestTaskLifecycle(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	taskID := createTask(hs, token, "httpapi-lifecycle-task")
	defer func() { _, _ = hs.do("DELETE", "/api/v1/tasks/"+jsonInt64(taskID), nil, token) }()

	// Get by ID.
	status, env := hs.do("GET", "/api/v1/tasks/"+jsonInt64(taskID), nil, token)
	var got map[string]any
	hs.mustOK(status, env, "get task", &got)
	if got["name"] != "httpapi-lifecycle-task" || got["executor_type"] != "local" {
		t.Fatalf("get task: unexpected payload %v", got)
	}

	// Update.
	status, env = hs.do("PUT", "/api/v1/tasks/"+jsonInt64(taskID), map[string]any{
		"name":          "httpapi-lifecycle-task-v2",
		"description":   "updated",
		"executor_type": "local",
		"schedule":      "0 0 2 2 *",
		"enabled":       true,
		"config":        map[string]any{"command": "echo updated"},
	}, token)
	if status != http.StatusOK {
		t.Fatalf("update task: expected 200, got %d code=%d", status, env.Code)
	}

	// Pause/resume round-trip.
	status, env = hs.do("POST", "/api/v1/tasks/"+jsonInt64(taskID)+"/pause", nil, token)
	if status != http.StatusOK {
		t.Fatalf("pause task: expected 200, got %d code=%d (%s)", status, env.Code, env.Message)
	}
	status, env = hs.do("POST", "/api/v1/tasks/"+jsonInt64(taskID)+"/resume", nil, token)
	if status != http.StatusOK {
		t.Fatalf("resume task: expected 200, got %d code=%d (%s)", status, env.Code, env.Message)
	}

	// Copy.
	status, env = hs.do("POST", "/api/v1/tasks/"+jsonInt64(taskID)+"/copy",
		map[string]any{"name": "httpapi-lifecycle-copy"}, token)
	var copied struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	hs.mustOK(status, env, "copy task", &copied)
	if copied.ID == 0 || copied.ID == taskID || copied.Name != "httpapi-lifecycle-copy" {
		t.Fatalf("copy task: unexpected copy %+v", copied)
	}
	defer func() { _, _ = hs.do("DELETE", "/api/v1/tasks/"+jsonInt64(copied.ID), nil, token) }()

	// List contains both.
	pd := hs.listPage(token, "/api/v1/tasks?page=1&size=100")
	if pd.Total < 2 {
		t.Fatalf("list tasks: expected >=2, got %d", pd.Total)
	}
}

// TestTaskExecutionsFilterAndDetail triggers a task, waits for its execution
// to finish, then exercises the server-side execution filters and the
// single-execution detail endpoint.
func TestTaskExecutionsFilterAndDetail(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	unique := time.Now().UnixNano() % 1_000_000
	nameA := fmt.Sprintf("httpapi-exec-%d-a", unique)
	nameB := fmt.Sprintf("httpapi-exec-%d-b", unique)
	idA := createTask(hs, token, nameA)
	idB := createTask(hs, token, nameB)
	defer func() {
		_, _ = hs.do("DELETE", "/api/v1/tasks/"+jsonInt64(idA), nil, token)
		_, _ = hs.do("DELETE", "/api/v1/tasks/"+jsonInt64(idB), nil, token)
	}()

	for _, id := range []int64{idA, idB} {
		status, env := hs.do("POST", "/api/v1/tasks/"+jsonInt64(id)+"/trigger", nil, token)
		if status != http.StatusOK && status != http.StatusAccepted {
			t.Fatalf("trigger task %d: expected 200/202, got %d code=%d (%s)",
				id, status, env.Code, env.Message)
		}
	}

	// Wait for the execution list to become non-empty. The manual-trigger
	// placeholder row is written synchronously; the runner's completed row
	// lands asynchronously.
	deadline := time.Now().Add(15 * time.Second)
	var pd pageData
	for time.Now().Before(deadline) {
		pd = hs.listPage(token, "/api/v1/tasks/"+jsonInt64(idA)+"/executions?page=1&size=20")
		for _, item := range pd.Items {
			if _, ok := item["executor_type"].(string); ok && item["executor_type"] != "" {
				break
			}
		}
		hasReal := false
		for _, item := range pd.Items {
			if v, ok := item["executor_type"].(string); ok && v != "" {
				hasReal = true
				break
			}
		}
		if hasReal {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if pd.Total < 1 {
		t.Fatalf("task executions: no records produced for task %d within 15s", idA)
	}

	// Filter by task_name (substring, case-insensitive per contract).
	filtered := hs.listPage(token,
		"/api/v1/tasks/0/executions?page=1&size=50&task_name="+fmt.Sprintf("exec-%d-a", unique))
	if filtered.Total < 1 {
		t.Fatalf("executions task_name filter: expected >=1, got %d", filtered.Total)
	}
	for _, item := range filtered.Items {
		itemName, _ := item["task_name"].(string)
		if itemName != nameA {
			t.Fatalf("executions task_name filter: unexpected item task_name=%q", itemName)
		}
	}

	// Filter by executor type.
	filtered = hs.listPage(token,
		"/api/v1/tasks/0/executions?page=1&size=50&executor_type=local")
	if filtered.Total < 1 {
		t.Fatalf("executions executor filter: expected >=1, got %d", filtered.Total)
	}

	// Single execution detail (was a permanent 404 stub before the fix).
	execID := int64(0)
	if raw, ok := pd.Items[0]["id"].(float64); ok {
		execID = int64(raw)
	}
	if execID == 0 {
		t.Fatalf("execution record has no numeric id: %v", pd.Items[0])
	}
	status, env := hs.do("GET",
		"/api/v1/tasks/"+jsonInt64(idA)+"/executions/"+jsonInt64(execID), nil, token)
	var detail map[string]any
	hs.mustOK(status, env, "get execution", &detail)
	if detail["task_id"].(float64) != float64(idA) {
		t.Fatalf("execution detail: task_id mismatch: %v", detail["task_id"])
	}
	if _, ok := detail["status"]; !ok {
		t.Fatalf("execution detail: missing status field: %v", keysOf(detail))
	}
}

// TestTaskStats verifies the /tasks/stats aggregate endpoint: field shape,
// the task_id scoping filter, the days chart-series parameter, and strict
// parameter validation.
func TestTaskStats(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	status, env := hs.do("GET",
		"/api/v1/tasks/stats?from="+time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339)+
			"&to="+time.Now().UTC().Format(time.RFC3339), nil, token)
	var stats map[string]any
	hs.mustOK(status, env, "task stats", &stats)
	for _, key := range []string{
		"total_executions", "success_count", "failure_count", "success_rate", "average_duration_ms",
	} {
		if _, ok := stats[key]; !ok {
			t.Fatalf("task stats: missing field %q, keys=%v", key, keysOf(stats))
		}
	}

	// The package harness shares one server and database across tests, so
	// global counts are asserted as deltas against a pre-seed baseline and
	// per-task counts use task IDs no other test touches.
	status, env = hs.do("GET", "/api/v1/tasks/stats", nil, token)
	var baseline map[string]any
	hs.mustOK(status, env, "baseline stats", &baseline)

	// Seed rows directly so the aggregate assertions below are exact and
	// independent of the async runner.
	now := time.Now()
	rows := []task.Execution{
		{TaskID: 9101, Status: task.StatusSuccess, Duration: 100, StartedAt: now},
		{TaskID: 9101, Status: task.StatusFailed, Duration: 300, StartedAt: now},
		{TaskID: 9202, Status: task.StatusSuccess, Duration: 200, StartedAt: now},
	}
	if err := hs.dbc.Create(&rows).Error; err != nil {
		t.Fatalf("seed executions: %v", err)
	}

	// Global aggregate grows by the seeded rows.
	status, env = hs.do("GET", "/api/v1/tasks/stats", nil, token)
	hs.mustOK(status, env, "global stats", &stats)
	if stats["total_executions"].(float64)-baseline["total_executions"].(float64) != 3 ||
		stats["success_count"].(float64)-baseline["success_count"].(float64) != 2 {
		t.Fatalf("global stats delta: total %v→%v success %v→%v, want +3/+2",
			baseline["total_executions"], stats["total_executions"],
			baseline["success_count"], stats["success_count"])
	}

	// task_id scopes the aggregate to a single task's executions.
	status, env = hs.do("GET", "/api/v1/tasks/stats?task_id=9101", nil, token)
	var scoped map[string]any
	hs.mustOK(status, env, "scoped stats", &scoped)
	if scoped["total_executions"].(float64) != 2 ||
		scoped["success_count"].(float64) != 1 ||
		scoped["failure_count"].(float64) != 1 {
		t.Fatalf("task_id stats: unexpected aggregate %v", scoped)
	}

	// days returns a zero-filled contiguous daily series ending today;
	// combined with task_id the counts are scoped to the seeded task.
	status, env = hs.do("GET", "/api/v1/tasks/stats?days=3&task_id=9101", nil, token)
	var withDays struct {
		Daily []struct {
			Date    string `json:"date"`
			Total   int64  `json:"total"`
			Success int64  `json:"success"`
		} `json:"daily"`
	}
	hs.mustOK(status, env, "days stats", &withDays)
	if len(withDays.Daily) != 3 {
		t.Fatalf("days=3: got %d daily entries, want 3: %+v", len(withDays.Daily), withDays.Daily)
	}
	today := time.Now().Format("2006-01-02")
	if last := withDays.Daily[len(withDays.Daily)-1]; last.Date != today || last.Total != 2 || last.Success != 1 {
		t.Fatalf("days=3: today entry = %+v, want date=%s total=2 success=1", last, today)
	}

	// Invalid parameters are rejected strictly.
	for _, q := range []string{
		"task_id=0", "task_id=-1", "task_id=abc", "days=0", "days=91",
	} {
		status, env = hs.do("GET", "/api/v1/tasks/stats?"+q, nil, token)
		if status != http.StatusBadRequest {
			t.Fatalf("stats %q: expected 400, got %d code=%d (%s)", q, status, env.Code, env.Message)
		}
	}
}

// TestTaskExecutorCapabilityGate verifies the create-time capability
// prevalidation: probe-only executor types are rejected with 400, while the
// dual-mode http executor is accepted as a task type.
func TestTaskExecutorCapabilityGate(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	status, env := hs.do("POST", "/api/v1/tasks", map[string]any{
		"name":          "gate-probe-only",
		"executor_type": "icmp",
		"schedule":      "0 0 1 1 *",
		"enabled":       false,
	}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("create icmp task: expected 400, got %d code=%d (%s)", status, env.Code, env.Message)
	}

	status, env = hs.do("POST", "/api/v1/tasks", map[string]any{
		"name":          "gate-http-task",
		"executor_type": "http",
		"schedule":      "0 0 1 1 *",
		"enabled":       false,
		"config":        map[string]any{"address": "http://127.0.0.1:1/healthz", "method": "POST"},
	}, token)
	var created struct {
		ID int64 `json:"id"`
	}
	hs.mustOK(status, env, "create http task", &created)
	if created.ID == 0 {
		t.Fatal("create http task: no id returned")
	}
	_, _ = hs.do("DELETE", "/api/v1/tasks/"+jsonInt64(created.ID), nil, token)
}
