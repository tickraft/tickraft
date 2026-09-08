// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httpapi

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/task"
	"github.com/tickraft/tickraft/pkg/telemetry"
)

// TestAssetStatusUpdate covers PUT /api/v1/assets/:id/status: valid
// transitions persist and echo through GET, invalid statuses are rejected
// with 400, and unknown ids return 404.
func TestAssetStatusUpdate(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)
	ids := seedAssets(hs, token, 1)
	defer func() {
		for _, id := range ids {
			_, _ = hs.do("DELETE", "/api/v1/assets/"+jsonInt64(id), nil, token)
		}
	}()
	id := jsonInt64(ids[0])

	// Valid transition.
	status, env := hs.do("PUT", "/api/v1/assets/"+id+"/status",
		map[string]any{"status": "abnormal"}, token)
	if status != http.StatusOK || env.Code != 0 {
		t.Fatalf("update status: expected 200 code=0, got %d code=%d (%s)",
			status, env.Code, env.Message)
	}

	// The new status is visible through the read path.
	status, env = hs.do("GET", "/api/v1/assets/"+id, nil, token)
	var got struct {
		Status string `json:"status"`
	}
	hs.mustOK(status, env, "get asset", &got)
	if got.Status != "abnormal" {
		t.Fatalf("status after update = %q, want abnormal", got.Status)
	}

	// Invalid status value.
	status, env = hs.do("PUT", "/api/v1/assets/"+id+"/status",
		map[string]any{"status": "exploded"}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("invalid status: expected 400, got %d code=%d", status, env.Code)
	}

	// Unknown asset.
	status, env = hs.do("PUT", "/api/v1/assets/999999/status",
		map[string]any{"status": "normal"}, token)
	if status != http.StatusNotFound {
		t.Fatalf("unknown asset status update: expected 404, got %d code=%d", status, env.Code)
	}
}

// TestAssetProbe covers POST /api/v1/assets/:id/probe: the default
// implementation echoes the asset id and current status, and unknown ids
// return 404.
func TestAssetProbe(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)
	ids := seedAssets(hs, token, 1)
	defer func() {
		for _, id := range ids {
			_, _ = hs.do("DELETE", "/api/v1/assets/"+jsonInt64(id), nil, token)
		}
	}()

	status, env := hs.do("POST", "/api/v1/assets/"+jsonInt64(ids[0])+"/probe", nil, token)
	var probed struct {
		AssetID int64  `json:"asset_id"`
		Status  string `json:"status"`
	}
	hs.mustOK(status, env, "probe asset", &probed)
	if probed.AssetID != ids[0] {
		t.Fatalf("probe asset_id = %d, want %d", probed.AssetID, ids[0])
	}
	if probed.Status == "" {
		t.Fatal("probe: empty status echoed")
	}

	status, env = hs.do("POST", "/api/v1/assets/999999/probe", nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("probe unknown asset: expected 404, got %d code=%d", status, env.Code)
	}
}

// TestMonitorProbeNow covers POST /api/v1/telemetry/monitors/:id/probe: it
// must answer 202 Accepted and the dispatched probe must land in the point's
// history as a persisted probe record.
func TestMonitorProbeNow(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)
	id := createMonitor(hs, token, "blindspot-probe-now")
	defer func() { _, _ = hs.do("DELETE", "/api/v1/telemetry/monitors/"+jsonInt64(id), nil, token) }()

	// Probing a disabled point is rejected with a client error; enable it
	// first (the 60s schedule cannot fire on its own within the test
	// window, so the history rows below come from the on-demand probe).
	status, env := hs.do("POST", "/api/v1/telemetry/monitors/"+jsonInt64(id)+"/probe", nil, token)
	if status != http.StatusBadRequest {
		t.Fatalf("probe disabled point: expected 400, got %d code=%d", status, env.Code)
	}
	status, env = hs.do("PUT", "/api/v1/telemetry/monitors/"+jsonInt64(id)+"/enable", nil, token)
	hs.mustOK(status, env, "enable monitor", nil)

	status, env = hs.do("POST", "/api/v1/telemetry/monitors/"+jsonInt64(id)+"/probe", nil, token)
	if status != http.StatusAccepted {
		t.Fatalf("probe now: expected 202 Accepted, got %d code=%d (%s)",
			status, env.Code, env.Message)
	}

	// The probe is asynchronous (engine dispatch → runner → probe store);
	// poll the history endpoint until the record shows up.
	deadline := time.Now().Add(5 * time.Second)
	for {
		pd := hs.listPage(token, "/api/v1/telemetry/monitors/"+jsonInt64(id)+"/history?page=1&size=10")
		if pd.Total >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("probe now: no probe record in history within 5s (total=%d)", pd.Total)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestProbeTaskIDsDoNotPoisonAutoincrement pins the probe task ID scheme
// end to end. Enabling an active monitor persists a probe task row with a
// NEGATIVE synthetic ID; under the legacy positive scheme that persisted
// row pushed sys_schedule_task's auto-increment counter above the probe
// offset, so every regular task created afterwards received an ID inside
// the probe range (where a later monitor point could overwrite it). A
// regular task created after the monitor must keep a small positive ID.
func TestProbeTaskIDsDoNotPoisonAutoincrement(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	id := createMonitor(hs, token, "blindspot-id-scheme")
	defer func() { _, _ = hs.do("DELETE", "/api/v1/telemetry/monitors/"+jsonInt64(id), nil, token) }()

	status, env := hs.do("PUT", "/api/v1/telemetry/monitors/"+jsonInt64(id)+"/enable", nil, token)
	hs.mustOK(status, env, "enable monitor", nil)

	var probeRows []task.Task
	if err := hs.dbc.Where("id < 0").Find(&probeRows).Error; err != nil {
		t.Fatalf("query probe task rows: %v", err)
	}
	wantID := -(telemetry.ProbeTaskIDOffset + id)
	found := false
	for _, row := range probeRows {
		if row.ID == wantID && row.Name == fmt.Sprintf("prober-%d", id) {
			found = true
		}
	}
	if !found {
		t.Fatalf("probe task row %d (%s) not found in negative range, rows=%v",
			wantID, "prober-"+jsonInt64(id), probeRows)
	}

	regularID := createTask(hs, token, "blindspot-id-scheme-task")
	defer func() { _, _ = hs.do("DELETE", "/api/v1/tasks/"+strconv.FormatInt(regularID, 10), nil, token) }()
	if regularID <= 0 || regularID >= telemetry.ProbeTaskIDOffset {
		t.Fatalf("regular task ID after monitor = %d, want 0 < id < %d",
			regularID, telemetry.ProbeTaskIDOffset)
	}
}

// wsHandshake performs a raw HTTP upgrade request against the /ws endpoint
// and returns the response status line. Raw TCP keeps the test free of a
// websocket client dependency: 101 for a successful upgrade, 401 for auth
// failures.
func wsHandshake(t *testing.T, baseURL, rawQuery string) string {
	t.Helper()
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse base url: %v", err)
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(t.Context(), "tcp", u.Host)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	req := "GET /ws" + rawQuery + " HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read handshake response: %v", err)
	}
	return strings.TrimSpace(line)
}

// TestWSHandshake covers the /ws query-token auth ladder: no token and an
// invalid token are rejected with 401 before the upgrade, and a valid
// access token completes the 101 upgrade.
func TestWSHandshake(t *testing.T) {
	hs := newHarness(t)

	if line := wsHandshake(t, hs.baseURL, ""); !strings.Contains(line, "401") {
		t.Fatalf("ws without token: expected 401, got %q", line)
	}
	if line := wsHandshake(t, hs.baseURL, "?token=not-a-jwt"); !strings.Contains(line, "401") {
		t.Fatalf("ws with invalid token: expected 401, got %q", line)
	}

	token := hs.login(adminUsername, adminPassword)
	line := wsHandshake(t, hs.baseURL, "?token="+url.QueryEscape(token))
	if !strings.Contains(line, "101") {
		t.Fatalf("ws with valid token: expected 101 Switching Protocols, got %q", line)
	}
}

// TestI18nLocalesPublic covers GET /api/v1/i18n/locales: public (no auth),
// envelope code 0, and the builtin locale set from the embedded bundles.
func TestI18nLocalesPublic(t *testing.T) {
	hs := newHarness(t)

	// No Authorization header on purpose: the login page needs this before
	// authentication.
	status, env := hs.do("GET", "/api/v1/i18n/locales", nil, "")
	var locales []map[string]any
	hs.mustOK(status, env, "i18n locales", &locales)
	if len(locales) == 0 {
		t.Fatal("i18n locales: empty list")
	}
	tags := make(map[string]bool)
	for _, loc := range locales {
		if tag, ok := loc["tag"].(string); ok {
			tags[tag] = true
		} else if tag, ok := loc["locale"].(string); ok {
			tags[tag] = true
		}
	}
	for _, want := range []string{"en-US", "zh-Hans"} {
		if !tags[want] {
			t.Fatalf("i18n locales: %s missing from %v", want, tags)
		}
	}
}

// TestSPAServing covers the embedded single-page app mount: the index and a
// client-side route fall back to index.html, static assets serve real
// bytes, and unknown API paths keep their JSON 404 envelope. Skipped when
// the embedded dist has not been populated (web build artifact absent).
func TestSPAServing(t *testing.T) {
	hs := newHarness(t)
	if !hs.spaRegistered {
		t.Skip("embedded SPA dist not populated; run the frontend build first")
	}

	// Root serves the app shell.
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, hs.baseURL+"/", http.NoBody)
	resp, err := hs.client.Do(req)
	if err != nil {
		t.Fatalf("get /: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get /: expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("get /: content-type = %q, want text/html", ct)
	}

	// Deep client-side route falls back to index.html (same shell).
	req, _ = http.NewRequestWithContext(t.Context(), http.MethodGet,
		hs.baseURL+"/dashboard/overview", http.NoBody)
	resp, err = hs.client.Do(req)
	if err != nil {
		t.Fatalf("get spa deep link: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("spa deep link: expected 200, got %d", resp.StatusCode)
	}

	// A built asset serves with its digest name; discover one from /.
	req, _ = http.NewRequestWithContext(t.Context(), http.MethodGet, hs.baseURL+"/", http.NoBody)
	resp, err = hs.client.Do(req)
	if err != nil {
		t.Fatalf("get / for asset scan: %v", err)
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read index shell: %v", err)
	}
	shell := string(raw)
	asset := ""
	for _, prefix := range []string{`src="`, `href="`} {
		for _, part := range strings.Split(shell, prefix) {
			part = strings.TrimSuffix(strings.SplitN(part, `"`, 2)[0], ">")
			if strings.HasPrefix(part, "/assets/") {
				asset = part
				break
			}
		}
		if asset != "" {
			break
		}
	}
	if asset == "" {
		t.Fatalf("no /assets/ reference found in index.html shell")
	}
	req, _ = http.NewRequestWithContext(t.Context(), http.MethodGet, hs.baseURL+asset, http.NoBody)
	resp, err = hs.client.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", asset, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get %s: expected 200, got %d", asset, resp.StatusCode)
	}

	// Unknown API path keeps the JSON 404 envelope (not the SPA shell).
	status, env := hs.do("GET", "/api/v1/definitely-not-a-route", nil, "")
	if status != http.StatusNotFound {
		t.Fatalf("unknown api path: expected 404, got %d", status)
	}
	if env.Code == 0 {
		t.Fatalf("unknown api path: expected non-zero envelope code, got 0 (%s)", env.Message)
	}
}
