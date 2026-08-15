// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package asset

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/tickraft/tickraft/pkg/api"
	assetstore "github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/quota"
	"github.com/tickraft/tickraft/pkg/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// assetBasePath is the route prefix registered by routes.go for the
// asset management API.
const assetBasePath = "/api/v1/assets"

// testProvider is a quota.Provider that returns generous ceilings so tests
// are not constrained by the open-source defaults (e.g. DefaultHost = 0).
type testProvider struct{}

func (testProvider) Ceiling(t quota.Type) int {
	if t == quota.TypeHost {
		return 1000
	}
	// Return CE-equivalent values for known types so the test provider
	// behaves like the real DefaultProvider without importing internal/quota.
	switch t {
	case quota.TypeDevice, quota.TypeProber, quota.TypeScheduledTask:
		return 20
	case quota.TypeRemediation:
		return 5
	case quota.TypeProbeInterval, quota.TypeScheduledTaskInterval:
		return 60
	case quota.TypeDailyEvents:
		return 100000
	default:
		return 0
	}
}

// newAssetTestEngine creates a fresh route.Engine with the asset CRUD
// routes wired to a Handler backed by an in-memory SQLite database.
// It returns the engine and the underlying Store for direct seeding.
func newAssetTestEngine(t *testing.T) (*route.Engine, assetstore.Store) {
	t.Helper()
	quota.SetProvider(testProvider{})
	t.Cleanup(func() { quota.SetProvider(nil) })
	dbc, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := dbc.AutoMigrate(&assetstore.Asset{}); err != nil {
		t.Fatalf("migrate asset: %v", err)
	}
	store := assetstore.NewStore(dbc)
	h := NewHandler(store, nil)
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.GET(assetBasePath, h.ListAssets)
	engine.GET(assetBasePath+"/:id", h.GetAsset)
	engine.POST(assetBasePath, h.CreateAsset)
	engine.PUT(assetBasePath+"/:id", h.UpdateAsset)
	engine.DELETE(assetBasePath+"/:id", h.DeleteAsset)
	engine.PUT(assetBasePath+"/:id/status", h.UpdateAssetStatus)
	engine.POST(assetBasePath+"/:id/probe", h.ProbeAsset)
	return engine, store
}

// jsonHeader is the Content-Type header for JSON request bodies.
var jsonHeader = ut.Header{Key: "Content-Type", Value: "application/json"}

// doRequest is a thin wrapper around ut.PerformRequest that sends a JSON
// body for non-GET/DELETE methods and returns the response recorder.
func doRequest(engine *route.Engine, method, path string, body []byte) *ut.ResponseRecorder {
	var utBody *ut.Body
	if body != nil {
		utBody = &ut.Body{Body: bytes.NewReader(body), Len: len(body)}
	}
	return ut.PerformRequest(engine, method, path, utBody, jsonHeader)
}

// decodeAPIResponse decodes a ut response body into an api.Response.
func decodeAPIResponse(t *testing.T, w *ut.ResponseRecorder) api.Response {
	t.Helper()
	var resp api.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%q)", err, w.Body.String())
	}
	return resp
}

// decodeAssetData re-marshals the api.Response Data field and decodes it
// into a asset.Asset, enabling field assertions on create/get/update
// responses.
func decodeAssetData(t *testing.T, resp api.Response) assetstore.Asset {
	t.Helper()
	raw, err := json.Marshal(resp.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var r assetstore.Asset
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unmarshal asset: %v", err)
	}
	return r
}

// createAssetViaAPI issues a POST to create a asset and returns the
// decoded response asset. It fails the test on a non-200 response.
func createAssetViaAPI(t *testing.T, engine *route.Engine, body string) assetstore.Asset {
	t.Helper()
	w := doRequest(engine, "POST", assetBasePath, []byte(body))
	if w.Code != http.StatusOK {
		t.Fatalf("create: status = %d, want %d (body=%q)", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	return decodeAssetData(t, resp)
}

// --- Create tests ---

// TestAssetHandlerCreateSuccess verifies a valid POST creates a asset
// and returns it with a populated ID.
func TestAssetHandlerCreateSuccess(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	r := createAssetViaAPI(t, engine, `{"asset_type":"host","asset_key":"host-001","name":"web-server-1"}`)
	if r.ID == 0 {
		t.Error("ID = 0, want non-zero")
	}
	if r.Name != "web-server-1" {
		t.Errorf("Name = %q, want %q", r.Name, "web-server-1")
	}
	if r.AssetKey != "host-001" {
		t.Errorf("AssetKey = %q, want %q", r.AssetKey, "host-001")
	}
	if r.AssetType != types.AssetTypeHost {
		t.Errorf("AssetType = %q, want %q", r.AssetType, types.AssetTypeHost)
	}
	if r.Status != types.AssetStatusUnknown {
		t.Errorf("Status = %q, want %q (default)", r.Status, types.AssetStatusUnknown)
	}
}

// TestAssetHandlerCreateMissingName verifies a missing name returns 400.
func TestAssetHandlerCreateMissingName(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	w := doRequest(engine, "POST", assetBasePath, []byte(`{"asset_type":"host","asset_key":"host-001"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	if resp.Code != errdefs.CodeBadRequest {
		t.Errorf("code = %d, want %d", resp.Code, errdefs.CodeBadRequest)
	}
}

// TestAssetHandlerCreateMissingKey verifies a missing asset_key returns 400.
func TestAssetHandlerCreateMissingKey(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	w := doRequest(engine, "POST", assetBasePath, []byte(`{"asset_type":"host","name":"x"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestAssetHandlerCreateMissingType verifies a missing asset_type returns 400.
func TestAssetHandlerCreateMissingType(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	w := doRequest(engine, "POST", assetBasePath, []byte(`{"asset_key":"k","name":"x"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestAssetHandlerCreateInvalidJSON verifies malformed JSON returns 400.
func TestAssetHandlerCreateInvalidJSON(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	w := doRequest(engine, "POST", assetBasePath, []byte(`{not json`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// --- Device quota tests ---

// TestAssetHandlerCreateDeviceQuotaExceeded verifies that creating more
// than maxDeviceQuota device resources is rejected with 409 Conflict and the
// "quota_exceeded" error code.
func TestAssetHandlerCreateDeviceQuotaExceeded(t *testing.T) {
	engine, store := newAssetTestEngine(t)
	ctx := context.Background()

	// Seed the store with the maximum allowed number of device resources.
	for i := 0; i < maxDeviceQuota; i++ {
		if err := store.Create(ctx, &assetstore.Asset{
			AssetType: types.AssetTypeDevice,
			AssetKey:  "dev-" + itoa(int64(i)),
			Name:      "device",
		}); err != nil {
			t.Fatalf("seed device %d: %v", i, err)
		}
	}

	// The next device creation must be rejected with 409 Conflict.
	w := doRequest(engine, "POST", assetBasePath,
		[]byte(`{"asset_type":"device","asset_key":"dev-extra","name":"extra"}`))
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusConflict, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	if resp.Code != errdefs.CodeConflict {
		t.Errorf("code = %d, want %d", resp.Code, errdefs.CodeConflict)
	}
	if resp.Message != "quota_exceeded" {
		t.Errorf("message = %q, want %q", resp.Message, "quota_exceeded")
	}
}

// TestAssetHandlerCreateDeviceAtQuotaBoundary verifies that creating
// exactly maxDeviceQuota device resources succeeds (no off-by-one error).
func TestAssetHandlerCreateDeviceAtQuotaBoundary(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	// Create devices up to the cap via the API; all must succeed.
	for i := 0; i < maxDeviceQuota; i++ {
		body := fmt.Sprintf(`{"asset_type":"device","asset_key":"dev-%d","name":"device-%d"}`, i, i)
		w := doRequest(engine, "POST", assetBasePath, []byte(body))
		if w.Code != http.StatusOK {
			t.Fatalf("create device %d: status = %d, want %d (body=%q)",
				i, w.Code, http.StatusOK, w.Body.String())
		}
	}

	// The next device must be rejected.
	w := doRequest(engine, "POST", assetBasePath,
		[]byte(`{"asset_type":"device","asset_key":"dev-over","name":"over"}`))
	if w.Code != http.StatusConflict {
		t.Fatalf("over-cap create: status = %d, want %d (body=%q)",
			w.Code, http.StatusConflict, w.Body.String())
	}
}

// TestAssetHandlerCreateNonDeviceNotQuotaLimited verifies that non-device
// asset types are not subject to the device quota.
func TestAssetHandlerCreateNonDeviceNotQuotaLimited(t *testing.T) {
	engine, store := newAssetTestEngine(t)
	ctx := context.Background()

	// Seed the store with more than maxDeviceQuota host resources.
	for i := 0; i < maxDeviceQuota+5; i++ {
		if err := store.Create(ctx, &assetstore.Asset{
			AssetType: types.AssetTypeHost,
			AssetKey:  "host-" + itoa(int64(i)),
			Name:      "host",
		}); err != nil {
			t.Fatalf("seed host %d: %v", i, err)
		}
	}

	// Creating another host should still succeed.
	w := doRequest(engine, "POST", assetBasePath,
		[]byte(`{"asset_type":"host","asset_key":"host-extra","name":"extra"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusOK, w.Body.String())
	}
}

// --- List tests ---

// TestAssetHandlerListEmpty verifies a fresh store returns an empty page.
func TestAssetHandlerListEmpty(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	w := ut.PerformRequest(engine, "GET", assetBasePath, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	resp := decodeAPIResponse(t, w)
	if resp.Code != 0 {
		t.Errorf("code = %d, want 0", resp.Code)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	if data["total"].(float64) != 0 {
		t.Errorf("total = %v, want 0", data["total"])
	}
}

// TestAssetHandlerListWithItems verifies list returns seeded resources
// with the correct total count and page metadata.
func TestAssetHandlerListWithItems(t *testing.T) {
	engine, store := newAssetTestEngine(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := store.Create(ctx, &assetstore.Asset{
			AssetType: types.AssetTypeHost,
			AssetKey:  "host-" + string(rune('A'+i)),
			Name:      "host",
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	w := ut.PerformRequest(engine, "GET", assetBasePath+"?page=1&page_size=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	resp := decodeAPIResponse(t, w)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	if data["total"].(float64) != 3 {
		t.Errorf("total = %v, want 3", data["total"])
	}
	items, ok := data["items"].([]any)
	if !ok {
		t.Fatalf("expected items to be a slice, got %T", data["items"])
	}
	if len(items) != 3 {
		t.Errorf("len(items) = %d, want 3", len(items))
	}
}

// --- Get tests ---

// TestAssetHandlerGetSuccess verifies GET /:id returns the asset.
func TestAssetHandlerGetSuccess(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	created := createAssetViaAPI(t, engine, `{"asset_type":"website","asset_key":"site-1","name":"blog"}`)

	w := ut.PerformRequest(engine, "GET", assetBasePath+"/"+itoa(created.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	r := decodeAssetData(t, resp)
	if r.ID != created.ID {
		t.Errorf("ID = %d, want %d", r.ID, created.ID)
	}
	if r.Name != "blog" {
		t.Errorf("Name = %q, want %q", r.Name, "blog")
	}
}

// TestAssetHandlerGetNotFound verifies GET on a non-existent ID returns 404.
func TestAssetHandlerGetNotFound(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	w := ut.PerformRequest(engine, "GET", assetBasePath+"/999", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusNotFound, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	if resp.Code != errdefs.CodeNotFound {
		t.Errorf("code = %d, want %d", resp.Code, errdefs.CodeNotFound)
	}
}

// TestAssetHandlerGetInvalidID verifies GET /abc returns 400.
func TestAssetHandlerGetInvalidID(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	w := ut.PerformRequest(engine, "GET", assetBasePath+"/abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// --- Update tests ---

// TestAssetHandlerUpdateSuccess verifies PUT /:id updates the asset
// fields and returns the updated entity.
func TestAssetHandlerUpdateSuccess(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	created := createAssetViaAPI(t, engine, `{"asset_type":"host","asset_key":"host-1","name":"old"}`)

	w := doRequest(engine, "PUT", assetBasePath+"/"+itoa(created.ID), []byte(`{"asset_type":"host","asset_key":"host-1","name":"new","status":"normal"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	r := decodeAssetData(t, resp)
	if r.ID != created.ID {
		t.Errorf("ID = %d, want %d", r.ID, created.ID)
	}
	if r.Name != "new" {
		t.Errorf("Name = %q, want %q", r.Name, "new")
	}
	if r.Status != types.AssetStatusNormal {
		t.Errorf("Status = %q, want %q", r.Status, types.AssetStatusNormal)
	}
}

// TestAssetHandlerUpdateNotFound verifies PUT on a non-existent ID returns 404.
func TestAssetHandlerUpdateNotFound(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	w := doRequest(engine, "PUT", assetBasePath+"/999", []byte(`{"asset_type":"host","asset_key":"x","name":"y"}`))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusNotFound, w.Body.String())
	}
}

// TestAssetHandlerUpdateInvalidID verifies PUT /abc returns 400.
func TestAssetHandlerUpdateInvalidID(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	w := doRequest(engine, "PUT", assetBasePath+"/abc", []byte(`{"name":"x"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// --- Delete tests ---

// TestAssetHandlerDeleteSuccess verifies DELETE /:id removes the asset.
func TestAssetHandlerDeleteSuccess(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	created := createAssetViaAPI(t, engine, `{"asset_type":"service","asset_key":"svc-1","name":"api"}`)

	w := ut.PerformRequest(engine, "DELETE", assetBasePath+"/"+itoa(created.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: status = %d, want %d (body=%q)", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify the asset is gone via a subsequent GET.
	w = ut.PerformRequest(engine, "GET", assetBasePath+"/"+itoa(created.ID), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get after delete: status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestAssetHandlerDeleteNotFound verifies DELETE on a non-existent ID returns 404.
func TestAssetHandlerDeleteNotFound(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	w := ut.PerformRequest(engine, "DELETE", assetBasePath+"/999", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusNotFound, w.Body.String())
	}
}

// TestAssetHandlerDeleteInvalidID verifies DELETE /abc returns 400.
func TestAssetHandlerDeleteInvalidID(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	w := ut.PerformRequest(engine, "DELETE", assetBasePath+"/abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// --- Constructor & wiring tests ---

// TestNewHandlerConstructsField verifies the constructor wires the
// store onto the handler so subsequent method calls can use it.
func TestNewHandlerConstructsField(t *testing.T) {
	dbc, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	store := assetstore.NewStore(dbc)
	h := NewHandler(store, nil)
	if h.store == nil {
		t.Error("store field is nil, want non-nil")
	}
	if h.logger == nil {
		t.Error("logger field is nil, want non-nil (nop fallback)")
	}
}

// itoa converts an int64 to its decimal string representation without
// pulling in strconv (which is already imported by task.go in the same
// package, but this keeps the test self-contained for readability).
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
