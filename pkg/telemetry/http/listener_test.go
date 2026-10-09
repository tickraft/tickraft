// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package http

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	nethttp "net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/types"
)

// mockStore implements asset.Store for testing.
type mockStore struct {
	mu     sync.RWMutex
	assets map[int64]*asset.Asset
}

func newMockStore() *mockStore {
	store := &mockStore{assets: make(map[int64]*asset.Asset)}
	store.assets[1] = &asset.Asset{
		ID:        1,
		TenantID:  100,
		AssetType: types.AssetTypeDevice,
		AssetKey:  "dev-1",
		Name:      "device-1",
		Status:    types.AssetStatusNormal,
	}
	return store
}

func (s *mockStore) CountByStatus(_ context.Context) (map[string]int64, error) {
	return map[string]int64{}, nil
}

func (s *mockStore) Create(_ context.Context, r *asset.Asset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assets[r.ID] = r
	return nil
}

func (s *mockStore) Update(_ context.Context, r *asset.Asset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assets[r.ID] = r
	return nil
}

func (s *mockStore) GetByID(_ context.Context, id int64) (*asset.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.assets[id]
	if !ok {
		return nil, fmt.Errorf("asset not found: %d", id)
	}
	return r, nil
}

func (s *mockStore) GetByKey(_ context.Context, tenantID int64, key string) (*asset.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.assets {
		if r.TenantID == tenantID && r.AssetKey == key {
			return r, nil
		}
	}
	return nil, fmt.Errorf("asset not found: tenant=%d key=%s", tenantID, key)
}

func (s *mockStore) UpdateStatus(_ context.Context, id int64, status types.AssetStatus, activeAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.assets[id]; ok {
		r.Status = status
		r.LastActiveAt = activeAt
	}
	return nil
}

func (s *mockStore) Migrate(_ context.Context) error { return nil }

func (s *mockStore) List(_ context.Context, page, size int, _ asset.ListFilter) ([]*asset.Asset, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := int64(len(s.assets))
	all := make([]*asset.Asset, 0, total)
	for _, r := range s.assets {
		all = append(all, r)
	}
	page = max(page, 1)
	if size <= 0 {
		size = 20
	}
	offset := (page - 1) * size
	if offset >= int(total) {
		return nil, total, nil
	}
	end := offset + size
	end = min(end, int(total))
	return all[offset:end], total, nil
}

func (s *mockStore) Delete(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.assets, id)
	return nil
}

func (s *mockStore) CountByType(_ context.Context, tenantID int64, assetType types.AssetType) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int64
	for _, r := range s.assets {
		if r.TenantID == tenantID && r.AssetType == assetType {
			count++
		}
	}
	return count, nil
}

func (s *mockStore) ExistsByKey(_ context.Context, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.assets {
		if r.AssetKey == key {
			return true, nil
		}
	}
	return false, nil
}

func (s *mockStore) ListKeyset(_ context.Context,
	_ pagination.PageRequest) (pagination.PageResult[*asset.Asset], error) {
	return pagination.PageResult[*asset.Asset]{}, nil
}

// computeHMAC returns the hex-encoded HMAC-SHA256 of body using secret,
// matching the X-Tickraft-Signature header format.
func computeHMAC(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// postHandler invokes the given net/http handler via httptest and returns
// the response. The handler is wrapped in a httptest.Server so the request
// path does not matter (the listener handler ignores the path).
func postHandler(t *testing.T, handler nethttp.HandlerFunc, body []byte, headers ...[2]string) *nethttp.Response {
	t.Helper()
	return postHandlerTo(t, handler, "", body, headers...)
}

// postHandlerTo is postHandler with an explicit request target appended to
// the server URL (e.g. "?point_id=1").
func postHandlerTo(
	t *testing.T, handler nethttp.HandlerFunc, target string, body []byte, headers ...[2]string,
) *nethttp.Response {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	req, _ := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodPost, srv.URL+target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
	}
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		panic(fmt.Sprintf("post handler: %v", err))
	}
	return resp
}

// mustPost posts to the given handler and returns the response, failing
// the test if the request could not be performed.
func mustPost(t *testing.T, handler nethttp.HandlerFunc, body []byte, headers ...[2]string) *nethttp.Response {
	t.Helper()
	return postHandler(t, handler, body, headers...)
}

// mustPostTo is mustPost with an explicit request target appended to the
// server URL.
func mustPostTo(
	t *testing.T, handler nethttp.HandlerFunc, target string, body []byte, headers ...[2]string,
) *nethttp.Response {
	t.Helper()
	return postHandlerTo(t, handler, target, body, headers...)
}

// captureIngest returns an ingest callback that stores the received telemetry
// in a mutex-guarded variable for later assertion. The returned function
// must be used as the ingest argument to WithIngest.
func captureIngest() (ingest func(context.Context, *telemetry.Telemetry) error, peek func() *telemetry.Telemetry) {
	var (
		mu  sync.Mutex
		got *telemetry.Telemetry
	)
	ingest = func(_ context.Context, r *telemetry.Telemetry) error {
		mu.Lock()
		defer mu.Unlock()
		got = r
		return nil
	}
	peek = func() *telemetry.Telemetry {
		mu.Lock()
		defer mu.Unlock()
		return got
	}
	return ingest, peek
}

func TestListener_PostReport_ByID(t *testing.T) {
	store := newMockStore()
	cb, peek := captureIngest()
	h := New(
		WithStore(store),
		WithIngest(cb),
		WithLogger(zap.NewNop()),
	)

	body := telemetryRequest{Kind: "heartbeat",
		reportRequest: reportRequest{AssetID: 1, LogContent: "hello", LogLevel: "warning"}}
	bodyBytes, _ := json.Marshal(body)

	resp := mustPost(t, h.ReportHandler(), bodyBytes)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}

	if got := peek(); got == nil {
		t.Fatalf("ingest not called")
	} else {
		if got.AssetID != 1 {
			t.Errorf("AssetID = %d, want 1", got.AssetID)
		}
		if got.TenantID != 100 {
			t.Errorf("TenantID = %d, want 100", got.TenantID)
		}
		if got.AssetType != types.AssetTypeDevice {
			t.Errorf("AssetType = %q, want %q", got.AssetType, types.AssetTypeDevice)
		}
		if got.LogContent != "hello" {
			t.Errorf("LogContent = %q, want %q", got.LogContent, "hello")
		}
		if got.LogLevel != "warning" {
			t.Errorf("LogLevel = %q, want %q", got.LogLevel, "warning")
		}
		if got.SourceType != webhookSourceType {
			t.Errorf("SourceType = %q, want %q", got.SourceType, webhookSourceType)
		}
	}
}

func TestListener_PostReport_ByKey(t *testing.T) {
	store := newMockStore()
	cb, peek := captureIngest()
	h := New(
		WithStore(store),
		WithIngest(cb),
		WithLogger(zap.NewNop()),
	)

	body := telemetryRequest{Kind: "heartbeat",
		reportRequest: reportRequest{AssetKey: "dev-1", TenantID: 100, Status: "abnormal"}}
	bodyBytes, _ := json.Marshal(body)

	resp := mustPost(t, h.ReportHandler(), bodyBytes)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}

	if got := peek(); got == nil {
		t.Fatalf("ingest not called")
	} else {
		if got.AssetID != 1 {
			t.Errorf("AssetID = %d, want 1", got.AssetID)
		}
		if got.Status != types.AssetStatusAbnormal {
			t.Errorf("Status = %q, want %q", got.Status, types.AssetStatusAbnormal)
		}
	}
}

func TestListener_MethodNotAllowed(t *testing.T) {
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	srv := httptest.NewServer(h.ReportHandler())
	defer srv.Close()

	req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodGet, srv.URL, nethttp.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusMethodNotAllowed)
	}
}

func TestListener_InvalidJSON(t *testing.T) {
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	resp := mustPost(t, h.ReportHandler(), []byte("{not json"))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusBadRequest)
	}
}

func TestListener_MissingAssetIdentity(t *testing.T) {
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	body, _ := json.Marshal(telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{LogContent: "no asset"}})
	resp := mustPost(t, h.ReportHandler(), body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusBadRequest)
	}
}

func TestListener_AssetNotFound(t *testing.T) {
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	body, _ := json.Marshal(telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{AssetID: 999}})
	resp := mustPost(t, h.ReportHandler(), body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusNotFound)
	}
}

func TestListener_HMAC_ValidSignature(t *testing.T) {
	secret := "test-secret"
	store := newMockStore()
	cb, peek := captureIngest()
	h := New(
		WithSecret(secret),
		WithStore(store),
		WithIngest(cb),
		WithLogger(zap.NewNop()),
	)

	body := telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{AssetID: 1, LogContent: "signed"}}
	bodyBytes, _ := json.Marshal(body)
	sig := computeHMAC(bodyBytes, secret)

	resp := mustPost(t, h.ReportHandler(), bodyBytes, [2]string{"X-Tickraft-Signature", sig})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}

	if got := peek(); got == nil {
		t.Fatalf("ingest not called")
	} else if got.LogContent != "signed" {
		t.Errorf("LogContent = %q, want %q", got.LogContent, "signed")
	}
}

func TestListener_HMAC_InvalidSignature(t *testing.T) {
	h := New(
		WithSecret("test-secret"),
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	body, _ := json.Marshal(telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{AssetID: 1}})
	resp := mustPost(t, h.ReportHandler(), body, [2]string{"X-Tickraft-Signature", "deadbeef"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusUnauthorized)
	}
}

func TestListener_HMAC_MissingSignature(t *testing.T) {
	h := New(
		WithSecret("test-secret"),
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	body, _ := json.Marshal(telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{AssetID: 1}})
	resp := mustPost(t, h.ReportHandler(), body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusUnauthorized)
	}
}

func TestListener_BodyTooLarge(t *testing.T) {
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	// Heartbeat limit is 1 KiB. Build a heartbeat body larger than the
	// Kind limit but within the overall read limit.
	big := make([]byte, maxHeartbeatBodySize+1)
	for i := range big {
		big[i] = 'a'
	}
	// Prepend a valid JSON prefix with kind=heartbeat so the body parses
	// and the per-Kind limit check is reached.
	prefix := []byte(`{"kind":"heartbeat","log_content":"`)
	big = append(prefix, big...)
	big = append(big, []byte(`"}`)...)

	resp := mustPost(t, h.ReportHandler(), big)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusRequestEntityTooLarge)
	}
}

func TestListener_MetricsBodyTooLarge(t *testing.T) {
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	// Metrics limit is 64 KiB. Build a metrics body larger than the Kind
	// limit but within the overall read limit.
	big := make([]byte, maxMetricsBodySize+1)
	for i := range big {
		big[i] = 'a'
	}
	prefix := []byte(`{"kind":"metrics","log_content":"`)
	big = append(prefix, big...)
	big = append(big, []byte(`"}`)...)

	resp := mustPost(t, h.ReportHandler(), big)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusRequestEntityTooLarge)
	}
}

func TestListener_LogsBodyTooLarge(t *testing.T) {
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	// Logs limit is 1 MiB (overall max). Build a body larger than the
	// overall read limit so it is rejected before per-Kind check.
	big := make([]byte, maxLogsBodySize+1)
	for i := range big {
		big[i] = 'a'
	}
	big[0] = '{'

	resp := mustPost(t, h.ReportHandler(), big)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusRequestEntityTooLarge)
	}
}

func TestListener_DefaultLogLevel(t *testing.T) {
	store := newMockStore()
	cb, peek := captureIngest()
	h := New(
		WithStore(store),
		WithIngest(cb),
		WithLogger(zap.NewNop()),
	)
	body, _ := json.Marshal(telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{AssetID: 1}})
	resp := mustPost(t, h.ReportHandler(), body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}

	if got := peek(); got == nil {
		t.Fatalf("ingest not called")
	} else if got.LogLevel != "INFO" {
		t.Errorf("LogLevel = %q, want %q", got.LogLevel, "INFO")
	}
}

func TestListener_NoIngestNoPanic(t *testing.T) {
	// When no ingest callback is set, the handler should still accept the
	// request without panicking.
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	body, _ := json.Marshal(telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{AssetID: 1}})
	resp := mustPost(t, h.ReportHandler(), body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}
}

// --- task_status kind tests ---

// testTaskRef is a run-handle-shaped task_ref (the dispatch credential the
// engine stamps on every fire).
const testTaskRef = "5f0e9d3c1a2b4e6f8d7c9a0b1c2d3e4f"

func TestListener_TaskStatus_Valid(t *testing.T) {
	store := newMockStore()
	cb, peek := captureIngest()
	h := New(
		WithStore(store),
		WithIngest(cb),
		WithLogger(zap.NewNop()),
	)
	body := telemetryRequest{
		Kind:    kindTaskStatus,
		TaskRef: testTaskRef,
		Reason:  "manual trigger",
		reportRequest: reportRequest{
			AssetID: 1,
			Status:  "running",
		},
	}
	bodyBytes, _ := json.Marshal(body)

	resp := mustPost(t, h.ReportHandler(), bodyBytes)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}

	got := peek()
	if got == nil {
		t.Fatalf("ingest not called")
	}
	if got.AssetID != 1 {
		t.Errorf("AssetID = %d, want 1", got.AssetID)
	}
	// The raw body must carry the credential for downstream parsing.
	if !bytes.Contains(got.RawData, []byte(`"task_ref":"`+testTaskRef+`"`)) {
		t.Errorf("RawData does not contain task_ref: %s", got.RawData)
	}
	if !bytes.Contains(got.RawData, []byte(`"status":"running"`)) {
		t.Errorf("RawData does not contain status: %s", got.RawData)
	}
}

func TestListener_TaskStatus_BodyTooLarge(t *testing.T) {
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	// task_status limit is 16 KiB. Build a body larger than the Kind limit
	// but within the overall read limit.
	big := make([]byte, maxTaskStatusBodySize+1)
	for i := range big {
		big[i] = 'a'
	}
	prefix := []byte(`{"kind":"task_status","task_ref":"r-1","status":"running","output":"`)
	big = append(prefix, big...)
	big = append(big, []byte(`"}`)...)

	resp := mustPost(t, h.ReportHandler(), big)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusRequestEntityTooLarge)
	}
}

func TestListener_TaskStatus_MissingTaskRef(t *testing.T) {
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	body, _ := json.Marshal(telemetryRequest{
		Kind: kindTaskStatus,
		reportRequest: reportRequest{
			AssetID: 1,
			Status:  "running",
		},
	})
	resp := mustPost(t, h.ReportHandler(), body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusBadRequest)
	}
}

func TestListener_TaskStatus_MissingStatus(t *testing.T) {
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	body, _ := json.Marshal(telemetryRequest{
		Kind:    kindTaskStatus,
		TaskRef: testTaskRef,
		reportRequest: reportRequest{
			AssetID: 1,
		},
	})
	resp := mustPost(t, h.ReportHandler(), body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusBadRequest)
	}
}

// TestListener_TaskStatus_OffVocabularyStatus pins the closed vocabulary: the
// removed task-level active/paused pair (and any other unknown status) is
// rejected at the edge with 400, not silently accepted.
func TestListener_TaskStatus_OffVocabularyStatus(t *testing.T) {
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	for _, status := range []string{"active", "paused", "succeeded"} {
		body, _ := json.Marshal(telemetryRequest{
			Kind:    kindTaskStatus,
			TaskRef: testTaskRef,
			reportRequest: reportRequest{
				AssetID: 1,
				Status:  status,
			},
		})
		resp := mustPost(t, h.ReportHandler(), body)
		_ = resp.Body.Close()
		if resp.StatusCode != nethttp.StatusBadRequest {
			t.Errorf("status %q: code = %d, want %d", status, resp.StatusCode, nethttp.StatusBadRequest)
		}
	}
}

// captureTaskReport returns a task-report callback that stores the received
// report in a mutex-guarded variable for later assertion.
func captureTaskReport() (cb telemetry.TaskReportCallback, peek func() *telemetry.TaskReport) {
	var (
		mu  sync.Mutex
		got *telemetry.TaskReport
	)
	cb = func(_ context.Context, r *telemetry.TaskReport) {
		mu.Lock()
		defer mu.Unlock()
		got = r
	}
	peek = func() *telemetry.TaskReport {
		mu.Lock()
		defer mu.Unlock()
		return got
	}
	return cb, peek
}

// TestListener_TaskReport_CallbackTakesPrecedence pins the dedicated task
// report path: with a callback configured, task kinds are delivered to it
// and never reach the ingest pipeline.
func TestListener_TaskReport_CallbackTakesPrecedence(t *testing.T) {
	reportCb, reportPeek := captureTaskReport()
	ingestCb, ingestPeek := captureIngest()
	h := New(
		WithStore(newMockStore()),
		WithIngest(ingestCb),
		WithTaskReport(reportCb),
		WithLogger(zap.NewNop()),
	)

	started := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	body, _ := json.Marshal(telemetryRequest{
		Kind:      kindTaskStatus,
		TaskRef:   testTaskRef,
		StartedAt: started,
		Output:    "done",
		reportRequest: reportRequest{
			AssetID: 1,
			Status:  "completed",
		},
	})
	resp := mustPost(t, h.ReportHandler(), body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}

	if ingestPeek() != nil {
		t.Fatalf("ingest called for a task report with a callback configured")
	}
	got := reportPeek()
	if got == nil {
		t.Fatalf("task report callback not called")
	}
	if got.Kind != telemetry.KindTaskStatus {
		t.Errorf("Kind = %q, want %q", got.Kind, telemetry.KindTaskStatus)
	}
	if got.TaskRef != testTaskRef {
		t.Errorf("TaskRef = %q, want %q", got.TaskRef, testTaskRef)
	}
	if got.Status != "completed" {
		t.Errorf("Status = %q, want completed", got.Status)
	}
	if got.Output != "done" {
		t.Errorf("Output = %q, want done", got.Output)
	}
	// Tenant resolution still ran: the mock store's asset 1 is tenant 100.
	if got.TenantID != 100 {
		t.Errorf("TenantID = %d, want 100", got.TenantID)
	}
	if !got.StartedAt.Equal(started) {
		t.Errorf("StartedAt = %v, want %v", got.StartedAt, started)
	}
}

// TestListener_TaskReport_NoCallbackFallsToIngest pins the compatibility
// path: without a callback, task kinds flow through ingest as raw telemetry
// (the distributed collector deployment relies on this).
func TestListener_TaskReport_NoCallbackFallsToIngest(t *testing.T) {
	ingestCb, ingestPeek := captureIngest()
	h := New(
		WithStore(newMockStore()),
		WithIngest(ingestCb),
		WithLogger(zap.NewNop()),
	)

	body, _ := json.Marshal(telemetryRequest{
		Kind:    kindTaskStatus,
		TaskRef: testTaskRef,
		reportRequest: reportRequest{
			AssetID: 1,
			Status:  "running",
		},
	})
	resp := mustPost(t, h.ReportHandler(), body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}
	if ingestPeek() == nil {
		t.Fatalf("ingest not called for a task report without a callback")
	}
}

// TestListener_TaskReport_CallbackIgnoresOtherKinds pins that non-task
// kinds never take the task report path even with a callback configured.
func TestListener_TaskReport_CallbackIgnoresOtherKinds(t *testing.T) {
	reportCb, reportPeek := captureTaskReport()
	ingestCb, ingestPeek := captureIngest()
	h := New(
		WithStore(newMockStore()),
		WithIngest(ingestCb),
		WithTaskReport(reportCb),
		WithLogger(zap.NewNop()),
	)

	body, _ := json.Marshal(telemetryRequest{
		Kind: "heartbeat",
		reportRequest: reportRequest{
			AssetID: 1,
			Status:  "normal",
		},
	})
	resp := mustPost(t, h.ReportHandler(), body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}
	if reportPeek() != nil {
		t.Fatalf("task report callback called for a non-task kind")
	}
	if ingestPeek() == nil {
		t.Fatalf("ingest not called for a non-task kind")
	}
}

// TestListener_IngestErrorMapping pins how the ingest callback's error maps
// onto the response on the JSON path: an error wrapping telemetry.
// ErrIngestRejected answers 429 with Retry-After (the admission-gate
// backpressure channel), any other error is logged as a warning while the
// push stays acknowledged (best-effort ingest), and a nil error (or no
// callback at all) keeps the plain 202.
func TestListener_IngestErrorMapping(t *testing.T) {
	body, _ := json.Marshal(telemetryRequest{
		Kind:          "heartbeat",
		reportRequest: reportRequest{AssetID: 1, LogContent: "hello"},
	})

	cases := []struct {
		name          string
		ingest        func(context.Context, *telemetry.Telemetry) error
		wantStatus    int
		wantRetry     string
		wantWarnEntry bool
	}{
		{
			name: "wrapped sentinel answers 429",
			ingest: func(context.Context, *telemetry.Telemetry) error {
				return fmt.Errorf("tps gate: %w", telemetry.ErrIngestRejected)
			},
			wantStatus: nethttp.StatusTooManyRequests,
			wantRetry:  "1",
		},
		{
			name: "non-sentinel error keeps 202 and warns",
			ingest: func(context.Context, *telemetry.Telemetry) error {
				return errors.New("pipeline hiccup")
			},
			wantStatus:    nethttp.StatusAccepted,
			wantWarnEntry: true,
		},
		{
			name: "nil error keeps 202",
			ingest: func(context.Context, *telemetry.Telemetry) error {
				return nil
			},
			wantStatus: nethttp.StatusAccepted,
		},
		{
			name:       "no callback keeps 202",
			ingest:     nil,
			wantStatus: nethttp.StatusAccepted,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core, recorded := observer.New(zap.WarnLevel)
			opts := []Option{
				WithStore(newMockStore()),
				WithLogger(zap.New(core)),
			}
			if tc.ingest != nil {
				opts = append(opts, WithIngest(tc.ingest))
			}
			h := New(opts...)

			resp := mustPost(t, h.ReportHandler(), body)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if got := resp.Header.Get("Retry-After"); got != tc.wantRetry {
				t.Errorf("Retry-After = %q, want %q", got, tc.wantRetry)
			}
			warns := recorded.FilterMessage("http listener: ingest callback failed").All()
			if tc.wantWarnEntry && len(warns) != 1 {
				t.Errorf("warning log entries = %d, want 1", len(warns))
			}
			if !tc.wantWarnEntry && len(warns) != 0 {
				t.Errorf("warning log entries = %d, want 0", len(warns))
			}
		})
	}
}
