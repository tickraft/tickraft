// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httpapi

import (
	"net/http"
	"testing"
)

// TestSystemConfig covers GET/PUT /api/v1/system/config with the snake_case
// contract the frontend settings page binds to.
func TestSystemConfig(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	status, env := hs.do("GET", "/api/v1/system/config", nil, token)
	var cfg map[string]any
	hs.mustOK(status, env, "get system config", &cfg)
	for _, key := range []string{"log_level", "default_lang", "retention_days"} {
		if _, ok := cfg[key]; !ok {
			t.Fatalf("system config: missing field %q, keys=%v", key, keysOf(cfg))
		}
	}

	origLogLevel, _ := cfg["log_level"].(string)

	status, env = hs.do("PUT", "/api/v1/system/config", map[string]any{
		"log_level":      "debug",
		"default_lang":   "zh-Hans",
		"retention_days": 14,
	}, token)
	var updated map[string]any
	hs.mustOK(status, env, "update system config", &updated)
	if updated["log_level"] != "debug" || updated["retention_days"] != float64(14) {
		t.Fatalf("update system config: unexpected payload %v", updated)
	}

	// Viewer cannot write system config.
	status, _ = hs.do("PUT", "/api/v1/system/config", map[string]any{
		"log_level": "error",
	}, hs.login(viewerUsername, viewerPassword))
	if status != http.StatusForbidden {
		t.Fatalf("viewer update system config: expected 403, got %d", status)
	}

	// Restore so other tests see the original value.
	_, _ = hs.do("PUT", "/api/v1/system/config", map[string]any{
		"log_level":      origLogLevel,
		"default_lang":   cfg["default_lang"],
		"retention_days": cfg["retention_days"],
	}, token)
}

// TestSystemInfoAndStats covers /system/info and /system/stats including the
// asset_status_counts breakdown used by the dashboard ring chart.
func TestSystemInfoAndStats(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	status, env := hs.do("GET", "/api/v1/system/info", nil, token)
	var info map[string]any
	hs.mustOK(status, env, "get system info", &info)
	for _, key := range []string{"version", "build_tags", "start_time", "uptime"} {
		if _, ok := info[key]; !ok {
			t.Fatalf("system info: missing field %q, keys=%v", key, keysOf(info))
		}
	}

	// Seed assets in known statuses to verify the status breakdown.
	seedStatus := func(name, status string) {
		code, env := hs.do("POST", "/api/v1/assets", map[string]any{
			"name": name, "asset_type": "device", "asset_key": name, "status": status,
		}, token)
		if code != http.StatusOK && code != http.StatusCreated {
			t.Fatalf("seed asset %s: got %d (code=%d, %s)", name, code, env.Code, env.Message)
		}
	}
	seedStatus("httpapi-stats-normal-1", "normal")
	seedStatus("httpapi-stats-normal-2", "normal")
	seedStatus("httpapi-stats-abnormal-1", "abnormal")

	status, env = hs.do("GET", "/api/v1/system/stats", nil, token)
	var stats struct {
		TotalTasks        int64            `json:"total_tasks"`
		TotalDevices      int64            `json:"total_devices"`
		TodayExecutions   int64            `json:"today_executions"`
		TodaySuccessRate  float64          `json:"today_success_rate"`
		AssetStatusCounts map[string]int64 `json:"asset_status_counts"`
	}
	hs.mustOK(status, env, "get system stats", &stats)
	if stats.TotalDevices < 3 {
		t.Fatalf("system stats total_devices: expected >=3, got %d", stats.TotalDevices)
	}
	if stats.AssetStatusCounts == nil {
		t.Fatal("system stats: asset_status_counts missing")
	}
	// The contract is a GROUP BY over present statuses; absent segments
	// are rendered as zero by the frontend.
	if stats.AssetStatusCounts["normal"] < 2 || stats.AssetStatusCounts["abnormal"] < 1 {
		t.Fatalf("system stats asset_status_counts: unexpected counts %v", stats.AssetStatusCounts)
	}
}

// TestSystemProfile covers GET/PUT /api/v1/system/profile.
func TestSystemProfile(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	status, env := hs.do("GET", "/api/v1/system/profile", nil, token)
	var profile map[string]any
	hs.mustOK(status, env, "get profile", &profile)
	for _, key := range []string{"id", "username", "role", "language", "alert_format_style"} {
		if _, ok := profile[key]; !ok {
			t.Fatalf("profile: missing field %q, keys=%v", key, keysOf(profile))
		}
	}
	if profile["username"] != adminUsername {
		t.Fatalf("profile: expected username %q, got %v", adminUsername, profile["username"])
	}

	status, env = hs.do("PUT", "/api/v1/system/profile", map[string]any{
		"nickname": "httpapi-admin",
		"language": "en-US",
	}, token)
	var updated map[string]any
	hs.mustOK(status, env, "update profile", &updated)
	if updated["nickname"] != "httpapi-admin" || updated["language"] != "en-US" {
		t.Fatalf("update profile: unexpected payload %v", updated)
	}

	// Restore language so locale-dependent assertions elsewhere stay stable.
	_, _ = hs.do("PUT", "/api/v1/system/profile", map[string]any{
		"nickname": profile["nickname"],
		"language": profile["language"],
	}, token)
}
