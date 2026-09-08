// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package http

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/types"
)

// expositionContentType is the canonical Content-Type of the Prometheus
// text exposition format.
const expositionContentType = "text/plain; version=0.0.4; charset=utf-8"

// sampleExposition covers every metric family shape the parser flattens:
// gauge and counter with labels, a histogram, and a summary.
const sampleExposition = `# HELP cpu_temp CPU temperature
# TYPE cpu_temp gauge
cpu_temp{chip="cpu0"} 42.5

# HELP http_requests_total Total requests
# TYPE http_requests_total counter
http_requests_total{method="get",code="200"} 1027

# HELP request_duration_seconds Request latency
# TYPE request_duration_seconds histogram
request_duration_seconds_bucket{le="0.1"} 1
request_duration_seconds_bucket{le="0.5"} 2
request_duration_seconds_bucket{le="+Inf"} 3
request_duration_seconds_sum 0.6
request_duration_seconds_count 3

# HELP rpc_duration_seconds RPC duration
# TYPE rpc_duration_seconds summary
rpc_duration_seconds{quantile="0.5"} 0.3
rpc_duration_seconds_sum 1.8
rpc_duration_seconds_count 4
`

// mustPostExposition posts body with the exposition Content-Type and the
// given raw query string.
func mustPostExposition(
	t *testing.T,
	handler nethttp.HandlerFunc,
	body []byte,
	rawQuery string,
	headers ...[2]string,
) *nethttp.Response {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	target := srv.URL
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req, _ := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodPost, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", expositionContentType)
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
	}
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		panic("post exposition: " + err.Error())
	}
	return resp
}

func TestListener_Exposition_PushAndFlatten(t *testing.T) {
	store := newMockStore()
	cb, peek := captureIngest()
	h := New(
		WithStore(store),
		WithIngest(cb),
		WithLogger(zap.NewNop()),
	)

	body := []byte(sampleExposition)
	resp := mustPostExposition(t, h.ReportHandler(), body, "asset_id=1")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}

	got := peek()
	if got == nil {
		t.Fatal("ingest not called")
	}
	if got.AssetID != 1 || got.TenantID != 100 {
		t.Errorf("identity = (%d, %d), want (1, 100)", got.AssetID, got.TenantID)
	}
	if got.AssetType != types.AssetTypeDevice {
		t.Errorf("AssetType = %q, want %q", got.AssetType, types.AssetTypeDevice)
	}
	if got.SourceType != webhookSourceType {
		t.Errorf("SourceType = %q, want %q", got.SourceType, webhookSourceType)
	}
	if !bytes.Equal(got.RawData, body) {
		t.Errorf("RawData not preserved verbatim")
	}

	want := map[string]float64{
		// Labeled gauge, key unchanged (single label).
		`cpu_temp{chip="cpu0"}`: 42.5,
		// Labeled counter, labels re-sorted by name on the wire order
		// (method before code) — the key must render code first.
		`http_requests_total{code="200",method="get"}`: 1027,
		// Histogram expansion.
		"request_duration_seconds_sum":               0.6,
		"request_duration_seconds_count":             3,
		`request_duration_seconds_bucket{le="0.1"}`:  1,
		`request_duration_seconds_bucket{le="0.5"}`:  2,
		`request_duration_seconds_bucket{le="+Inf"}`: 3,
		// Summary expansion.
		`rpc_duration_seconds{quantile="0.5"}`: 0.3,
		"rpc_duration_seconds_sum":             1.8,
		"rpc_duration_seconds_count":           4,
	}
	if len(got.Metrics) != len(want) {
		t.Fatalf("metric count = %d, want %d: %v", len(got.Metrics), len(want), got.Metrics)
	}
	for key, value := range want {
		if gotValue, ok := got.Metrics[key]; !ok {
			t.Errorf("metric %q missing from %v", key, got.Metrics)
		} else if gotValue != value {
			t.Errorf("metric %q = %v, want %v", key, gotValue, value)
		}
	}
}

func TestListener_Exposition_AssetKeyQuery(t *testing.T) {
	store := newMockStore()
	cb, peek := captureIngest()
	h := New(
		WithStore(store),
		WithIngest(cb),
		WithLogger(zap.NewNop()),
	)

	resp := mustPostExposition(
		t, h.ReportHandler(),
		[]byte("# TYPE up gauge\nup 1\n"),
		"asset_key=dev-1&tenant_id=100",
	)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}
	if got := peek(); got == nil || got.AssetID != 1 {
		t.Fatalf("asset not resolved via asset_key: %+v", got)
	}
}

func TestListener_Exposition_UntypedMetric(t *testing.T) {
	cb, peek := captureIngest()
	h := New(WithIngest(cb), WithLogger(zap.NewNop()))

	// No TYPE line: the parser yields an untyped family, which must still
	// flatten to a value. The query carries the asset identity (no store
	// configured: the reporter's identity is trusted, as on the JSON path).
	resp := mustPostExposition(t, h.ReportHandler(), []byte("load_one 0.42\n"), "asset_id=1")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}
	if got := peek(); got == nil || got.Metrics["load_one"] != 0.42 {
		t.Fatalf("untyped metric not flattened: %+v", got)
	}
}

func TestListener_Exposition_HMACAuth(t *testing.T) {
	const secret = "test-secret-at-least-32-bytes-long!!"
	cb, peek := captureIngest()
	h := New(
		WithSecret(secret),
		WithIngest(cb),
		WithLogger(zap.NewNop()),
	)
	body := []byte("# TYPE up gauge\nup 1\n")

	resp := mustPostExposition(t, h.ReportHandler(), body, "asset_id=1")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusUnauthorized {
		t.Fatalf("unsigned status = %d, want %d", resp.StatusCode, nethttp.StatusUnauthorized)
	}
	if peek() != nil {
		t.Fatal("unsigned push reached ingest")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	resp = mustPostExposition(t, h.ReportHandler(), body, "asset_id=1",
		[2]string{headerSignature, hex.EncodeToString(mac.Sum(nil))})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("signed status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}
	if peek() == nil {
		t.Fatal("signed push did not reach ingest")
	}
}

func TestListener_Exposition_InvalidPayload(t *testing.T) {
	h := New(WithLogger(zap.NewNop()))

	resp := mustPostExposition(t, h.ReportHandler(), []byte("this is { not exposition\n"), "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusBadRequest)
	}
}

func TestListener_Exposition_EmptyPayload(t *testing.T) {
	h := New(WithLogger(zap.NewNop()))

	// Only HELP/TYPE metadata lines carry no samples.
	body := []byte("# HELP up helper\n# TYPE up gauge\n")
	resp := mustPostExposition(t, h.ReportHandler(), body, "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusBadRequest)
	}
}

func TestListener_Exposition_BodyTooLarge(t *testing.T) {
	h := New(WithLogger(zap.NewNop()))

	body := append(
		[]byte("# TYPE pad gauge\n"),
		bytes.Repeat([]byte("pad 1\n"), maxMetricsBodySize/6)...,
	)
	if len(body) <= maxMetricsBodySize {
		t.Fatal("fixture not over the metrics limit")
	}
	resp := mustPostExposition(t, h.ReportHandler(), body, "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusRequestEntityTooLarge)
	}
}

func TestParseExposition_SortsLabels(t *testing.T) {
	metrics, err := parseExposition([]byte(
		"# TYPE m gauge\nm{b=\"2\",a=\"1\"} 7\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := metrics[`m{a="1",b="2"}`]; !ok {
		t.Fatalf("labels not sorted into the key: %v", metrics)
	}
}
