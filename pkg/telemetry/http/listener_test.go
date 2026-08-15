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
	"fmt"
	nethttp "net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/types"
	"go.uber.org/zap"
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
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	offset := (page - 1) * size
	if offset >= int(total) {
		return nil, total, nil
	}
	end := offset + size
	if end > int(total) {
		end = int(total)
	}
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

func (s *mockStore) ListKeyset(_ context.Context, _ pagination.PageRequest) (pagination.PageResult[*asset.Asset], error) {
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
func postHandler(handler nethttp.HandlerFunc, body []byte, headers ...[2]string) *nethttp.Response {
	srv := httptest.NewServer(nethttp.HandlerFunc(handler))
	defer srv.Close()
	req, _ := nethttp.NewRequest(nethttp.MethodPost, srv.URL, bytes.NewReader(body))
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
	resp := postHandler(handler, body, headers...)
	return resp
}

// captureIngest returns an ingest callback that stores the received telemetry
// in a mutex-guarded variable for later assertion. The returned function
// must be used as the ingest argument to WithIngest.
func captureIngest() (func(context.Context, *telemetry.Telemetry), func() *telemetry.Telemetry) {
	var (
		mu  sync.Mutex
		got *telemetry.Telemetry
	)
	cb := func(_ context.Context, r *telemetry.Telemetry) {
		mu.Lock()
		defer mu.Unlock()
		got = r
	}
	peek := func() *telemetry.Telemetry {
		mu.Lock()
		defer mu.Unlock()
		return got
	}
	return cb, peek
}

func TestListener_PostReport_ByID(t *testing.T) {
	store := newMockStore()
	cb, peek := captureIngest()
	h := New(
		WithStore(store),
		WithIngest(cb),
		WithLogger(zap.NewNop()),
	)

	body := telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{AssetID: 1, LogContent: "hello", LogLevel: "warning"}}
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

	body := telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{AssetKey: "dev-1", TenantID: 100, Status: "abnormal"}}
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
	srv := httptest.NewServer(nethttp.HandlerFunc(h.ReportHandler()))
	defer srv.Close()

	resp, err := nethttp.Get(srv.URL)
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

func TestListener_TaskStatus_Valid(t *testing.T) {
	store := newMockStore()
	cb, peek := captureIngest()
	h := New(
		WithStore(store),
		WithIngest(cb),
		WithLogger(zap.NewNop()),
	)
	body := telemetryRequest{
		Kind:   "task_status",
		TaskID: 42,
		Reason: "manual trigger",
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
	// The raw body must carry the task fields for downstream parsing.
	if !bytes.Contains(got.RawData, []byte(`"task_id":42`)) {
		t.Errorf("RawData does not contain task_id: %s", got.RawData)
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
	// task_status limit is 4 KiB. Build a body larger than the Kind
	// limit but within the overall read limit.
	big := make([]byte, maxTaskStatusBodySize+1)
	for i := range big {
		big[i] = 'a'
	}
	prefix := []byte(`{"kind":"task_status","task_id":42,"status":"running","reason":"`)
	big = append(prefix, big...)
	big = append(big, []byte(`"}`)...)

	resp := mustPost(t, h.ReportHandler(), big)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusRequestEntityTooLarge)
	}
}

func TestListener_TaskStatus_MissingTaskID(t *testing.T) {
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	body, _ := json.Marshal(telemetryRequest{
		Kind: "task_status",
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
		Kind:   "task_status",
		TaskID: 42,
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

// --- task_execution_status kind tests ---

func TestListener_TaskExecStatus_Valid(t *testing.T) {
	store := newMockStore()
	cb, peek := captureIngest()
	h := New(
		WithStore(store),
		WithIngest(cb),
		WithLogger(zap.NewNop()),
	)
	body := telemetryRequest{
		Kind:        "task_execution_status",
		TaskID:      42,
		ExecutionID: 1024,
		Output:      "task completed",
		Reason:      "executor finished",
		reportRequest: reportRequest{
			AssetID: 1,
			Status:  "succeeded",
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
	if !bytes.Contains(got.RawData, []byte(`"task_id":42`)) {
		t.Errorf("RawData does not contain task_id: %s", got.RawData)
	}
	if !bytes.Contains(got.RawData, []byte(`"execution_id":1024`)) {
		t.Errorf("RawData does not contain execution_id: %s", got.RawData)
	}
	if !bytes.Contains(got.RawData, []byte(`"status":"succeeded"`)) {
		t.Errorf("RawData does not contain status: %s", got.RawData)
	}
}

func TestListener_TaskExecStatus_BodyTooLarge(t *testing.T) {
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	// task_execution_status limit is 16 KiB. Build a body larger than
	// the Kind limit but within the overall read limit.
	big := make([]byte, maxTaskExecStatusBodySize+1)
	for i := range big {
		big[i] = 'a'
	}
	prefix := []byte(`{"kind":"task_execution_status","task_id":42,"execution_id":1024,"status":"running","output":"`)
	big = append(prefix, big...)
	big = append(big, []byte(`"}`)...)

	resp := mustPost(t, h.ReportHandler(), big)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusRequestEntityTooLarge)
	}
}

func TestListener_TaskExecStatus_MissingTaskID(t *testing.T) {
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	body, _ := json.Marshal(telemetryRequest{
		Kind:        "task_execution_status",
		ExecutionID: 1024,
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

func TestListener_TaskExecStatus_MissingExecutionID(t *testing.T) {
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	body, _ := json.Marshal(telemetryRequest{
		Kind:   "task_execution_status",
		TaskID: 42,
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

func TestListener_TaskExecStatus_MissingStatus(t *testing.T) {
	h := New(
		WithStore(newMockStore()),
		WithLogger(zap.NewNop()),
	)
	body, _ := json.Marshal(telemetryRequest{
		Kind:        "task_execution_status",
		TaskID:      42,
		ExecutionID: 1024,
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
