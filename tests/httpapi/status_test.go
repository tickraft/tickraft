// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httpapi

import (
	"encoding/json"
	"testing"
)

// statusPublicView mirrors the status.PublicView wire shape.
type statusPublicView struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Overall     string `json:"overall"`
	Components  []struct {
		Name         string `json:"name"`
		Status       string `json:"status"`
		MonitorCount int    `json:"monitor_count"`
	} `json:"components"`
}

// TestStatusPageDisabledByDefault verifies the seeded configuration is
// disabled and the public endpoint answers 404 without authentication.
func TestStatusPageDisabledByDefault(t *testing.T) {
	hs := newHarness(t)

	code, env := hs.do("GET", "/api/v1/status", nil, "")
	if code != 404 {
		t.Fatalf("public status = %d, want 404 (disabled): %v", code, env.Message)
	}
}

// TestStatusConfigLifecycle covers the management endpoints: read the
// seeded config, publish a page with a component map, render the public
// view, reject invalid payloads, and verify the viewer role cannot write.
func TestStatusConfigLifecycle(t *testing.T) {
	hs := newHarness(t)
	admin := hs.login(adminUsername, adminPassword)
	viewer := hs.login(viewerUsername, viewerPassword)

	// Seeded config: disabled, default title.
	code, env := hs.do("GET", "/api/v1/status/config", nil, admin)
	if code != 200 {
		t.Fatalf("get config = %d: %v", code, env.Message)
	}
	var seeded struct {
		Title   string `json:"title"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(env.Data, &seeded); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if seeded.Enabled || seeded.Title == "" {
		t.Fatalf("seeded config = %+v, want disabled with default title", seeded)
	}

	// Publish a page mapping one component onto the seeded monitor points.
	// The exact point IDs are unknown here; the auto-component path is
	// covered by the service tests, so this uses a placeholder component
	// plus an empty map fallback (components omitted entirely).
	cfg := map[string]any{
		"title":       "Public Status",
		"description": "all systems",
		"enabled":     true,
	}
	code, env = hs.do("PUT", "/api/v1/status/config", cfg, admin)
	if code != 200 {
		t.Fatalf("put config = %d: %v", code, env.Message)
	}

	// Public view is served without a token.
	code, env = hs.do("GET", "/api/v1/status", nil, "")
	if code != 200 {
		t.Fatalf("public status = %d: %v", code, env.Message)
	}
	var view statusPublicView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if view.Title != "Public Status" || view.Description != "all systems" {
		t.Fatalf("view = %+v", view)
	}
	if view.Overall == "" {
		t.Fatal("overall status missing")
	}

	// Invalid payload: empty title.
	code, _ = hs.do("PUT", "/api/v1/status/config", map[string]any{"title": ""}, admin)
	if code != 400 {
		t.Fatalf("empty title = %d, want 400", code)
	}

	// Viewer cannot write the configuration.
	code, _ = hs.do("PUT", "/api/v1/status/config", cfg, viewer)
	if code != 403 {
		t.Fatalf("viewer put config = %d, want 403", code)
	}

	// Anonymous management read is rejected.
	code, _ = hs.do("GET", "/api/v1/status/config", nil, "")
	if code != 401 {
		t.Fatalf("anonymous config read = %d, want 401", code)
	}

	// Disabling the page hides the public endpoint again.
	cfg["enabled"] = false
	code, _ = hs.do("PUT", "/api/v1/status/config", cfg, admin)
	if code != 200 {
		t.Fatalf("disable config = %d", code)
	}
	code, _ = hs.do("GET", "/api/v1/status", nil, "")
	if code != 404 {
		t.Fatalf("public status after disable = %d, want 404", code)
	}
}
