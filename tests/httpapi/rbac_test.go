// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httpapi

import (
	"net/http"
	"testing"
)

// TestRBACRoleMatrix verifies the role-based permission policy end to end:
// visitor is read-only, developer writes tasks but cannot delete devices,
// admin has full access.
func TestRBACRoleMatrix(t *testing.T) {
	hs := newHarness(t)

	viewerToken := hs.login(viewerUsername, viewerPassword)
	developerToken := hs.login(developerName, developerPasswd)
	adminToken := hs.login(adminUsername, adminPassword)

	assetBody := map[string]any{
		"asset_type": "device",
		"asset_key":  "rbac-probe-host",
		"name":       "rbac-probe-host",
	}

	// Visitor: read allowed, write denied.
	status, _ := hs.do("GET", "/api/v1/assets?page=1&page_size=5", nil, viewerToken)
	if status != http.StatusOK {
		t.Fatalf("viewer list assets: expected 200, got %d", status)
	}
	status, env := hs.do("POST", "/api/v1/assets", assetBody, viewerToken)
	if status != http.StatusForbidden {
		t.Fatalf("viewer create asset: expected 403, got %d (code=%d)", status, env.Code)
	}

	// Developer: write on device allowed, delete on device denied.
	status, env = hs.do("POST", "/api/v1/assets", assetBody, developerToken)
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("developer create asset: expected 2xx, got %d (code=%d, %s)",
			status, env.Code, env.Message)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	hs.mustOK(status, env, "developer create asset", &created)
	if created.ID == 0 {
		t.Fatal("developer create asset: no id returned")
	}

	status, env = hs.do("DELETE", "/api/v1/assets/"+jsonInt64(created.ID), nil, developerToken)
	if status != http.StatusForbidden {
		t.Fatalf("developer delete asset: expected 403, got %d (code=%d)", status, env.Code)
	}

	// Developer: task writes allowed (create + delete own task).
	taskBody := map[string]any{
		"name":        "rbac-dev-task",
		"description": "created by developer",
		"executor":    "local",
		"schedule":    "0 0 1 1 *",
		"enabled":     false,
		"config":      map[string]any{"command": "echo rbac"},
	}
	status, env = hs.do("POST", "/api/v1/tasks", taskBody, developerToken)
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("developer create task: expected 2xx, got %d (code=%d, %s)",
			status, env.Code, env.Message)
	}
	var task struct {
		ID int64 `json:"id"`
	}
	hs.mustOK(status, env, "developer create task", &task)
	if task.ID == 0 {
		t.Fatal("developer create task: no id returned")
	}
	status, env = hs.do("DELETE", "/api/v1/tasks/"+jsonInt64(task.ID), nil, developerToken)
	if status != http.StatusOK {
		t.Fatalf("developer delete task: expected 200, got %d (code=%d)", status, env.Code)
	}

	// Visitor: task write denied.
	status, _ = hs.do("POST", "/api/v1/tasks", taskBody, viewerToken)
	if status != http.StatusForbidden {
		t.Fatalf("viewer create task: expected 403, got %d", status)
	}

	// Admin: device delete allowed (cleanup of the developer-created asset).
	status, env = hs.do("DELETE", "/api/v1/assets/"+jsonInt64(created.ID), nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("admin delete asset: expected 200, got %d (code=%d)", status, env.Code)
	}
}

// TestNoTokenRejected asserts protected endpoints reject anonymous requests.
func TestNoTokenRejected(t *testing.T) {
	hs := newHarness(t)

	status, _ := hs.do("GET", "/api/v1/assets?page=1&page_size=5", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous list assets: expected 401, got %d", status)
	}
}
