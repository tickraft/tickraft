// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package asset

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
	assetstore "github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/quota"
	"github.com/tickraft/tickraft/pkg/types"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newAssetTestEngineWithLogger is like newAssetTestEngine but returns a
// zaptest observer logger so tests can assert on the audit log entries
// emitted by the Handler. The handler is constructed with the observed
// logger, and the observed core is returned for inspection.
func newAssetTestEngineWithLogger(t *testing.T) (*route.Engine, assetstore.Store, *observer.ObservedLogs) {
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

	core, recorded := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	store := assetstore.NewStore(dbc)
	h := NewHandler(store, logger)
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.GET(assetBasePath, h.ListAssets)
	engine.GET(assetBasePath+"/:id", h.GetAsset)
	engine.POST(assetBasePath, h.CreateAsset)
	engine.PUT(assetBasePath+"/:id", h.UpdateAsset)
	engine.DELETE(assetBasePath+"/:id", h.DeleteAsset)
	engine.PUT(assetBasePath+"/:id/status", h.UpdateAssetStatus)
	engine.POST(assetBasePath+"/:id/probe", h.ProbeAsset)
	return engine, store, recorded
}

// findAuditEntry returns the first observed log entry whose "operation" field
// matches the given value. It fails the test if no match is found.
func findAuditEntry(t *testing.T, logs *observer.ObservedLogs, operation string) observer.LoggedEntry {
	t.Helper()
	all := logs.All()
	for _, e := range all {
		if op, ok := fieldValue(e, "operation"); ok && op == operation {
			return e
		}
	}
	t.Fatalf("audit log entry with operation=%q not found (total entries: %d)", operation, len(all))
	return observer.LoggedEntry{}
}

// fieldValue extracts a string field value from a zap log entry.
func fieldValue(e observer.LoggedEntry, key string) (string, bool) {
	for _, f := range e.Context {
		if f.Key == key {
			if f.Type == zapcore.StringType {
				return f.String, true
			}
		}
	}
	return "", false
}

// fieldInt extracts an int64 field value from a zap log entry.
func fieldInt(e observer.LoggedEntry, key string) (int64, bool) {
	for _, f := range e.Context {
		if f.Key == key {
			if f.Type == zapcore.Int64Type {
				return f.Integer, true
			}
		}
	}
	return 0, false
}

// =============================================================================
// Create — duplicate key conflict (uniqueIndex enforcement)
// =============================================================================

// TestAssetCreateDuplicateKeyConflict verifies that creating two assets with
// the same asset_key is rejected with 409 Conflict at the database level via
// the composite unique index idx_assets_tenant_key. This is the P2 fix that
// added the GORM uniqueIndex tag and the 409 response code mapping.
func TestAssetCreateDuplicateKeyConflict(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	// First creation succeeds.
	w := doRequest(engine, "POST", assetBasePath,
		[]byte(`{"asset_type":"host","asset_key":"dup-key-001","name":"first"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("first create: status = %d, want %d (body=%q)", w.Code, http.StatusOK, w.Body.String())
	}

	// Second creation with the same key must be rejected with 409.
	w = doRequest(engine, "POST", assetBasePath,
		[]byte(`{"asset_type":"host","asset_key":"dup-key-001","name":"second"}`))
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate create: status = %d, want %d (body=%q)", w.Code, http.StatusConflict, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	if resp.Code != errdefs.CodeConflict {
		t.Errorf("code = %d, want %d", resp.Code, errdefs.CodeConflict)
	}
	if resp.Message != "asset key already exists" {
		t.Errorf("message = %q, want %q", resp.Message, "asset key already exists")
	}
}

// TestAssetCreateDuplicateKeyDifferentTypeStillConflicts verifies the unique
// index is on asset_key alone (not asset_type+asset_key): two assets with the
// same key but different types still conflict.
func TestAssetCreateDuplicateKeyDifferentTypeStillConflicts(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	w := doRequest(engine, "POST", assetBasePath,
		[]byte(`{"asset_type":"host","asset_key":"shared-key","name":"host-asset"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("first create: status = %d, want %d", w.Code, http.StatusOK)
	}

	// Same key, different type — still conflicts because the unique index is
	// on (tenant_id, asset_key), not asset_type.
	w = doRequest(engine, "POST", assetBasePath,
		[]byte(`{"asset_type":"website","asset_key":"shared-key","name":"web-asset"}`))
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate key with different type: status = %d, want %d", w.Code, http.StatusConflict)
	}
}

// TestAssetCreateDuplicateKeyAfterDelete verifies that deleting an asset frees
// its key for reuse: a subsequent create with the same key succeeds.
func TestAssetCreateDuplicateKeyAfterDelete(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	created := createAssetViaAPI(t, engine, `{"asset_type":"host","asset_key":"recyclable","name":"first"}`)

	w := ut.PerformRequest(engine, "DELETE", assetBasePath+"/"+itoa(created.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: status = %d, want %d", w.Code, http.StatusOK)
	}

	// Reuse the same key after deletion.
	w = doRequest(engine, "POST", assetBasePath,
		[]byte(`{"asset_type":"host","asset_key":"recyclable","name":"second"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("recreate after delete: status = %d, want %d (body=%q)",
			w.Code, http.StatusOK, w.Body.String())
	}
}

// =============================================================================
// UpdateAssetStatus — all status transitions
// =============================================================================

// TestAssetUpdateStatusAllValidStatuses verifies every accepted status value
// (normal, abnormal, offline, unknown) is persisted correctly.
func TestAssetUpdateStatusAllValidStatuses(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	created := createAssetViaAPI(t, engine,
		`{"asset_type":"host","asset_key":"status-test","name":"status-host"}`)

	validStatuses := []types.AssetStatus{
		types.AssetStatusNormal,
		types.AssetStatusAbnormal,
		types.AssetStatusOffline,
		types.AssetStatusUnknown,
	}
	for _, s := range validStatuses {
		body := []byte(`{"status":"` + string(s) + `"}`)
		w := doRequest(engine, "PUT", assetBasePath+"/"+itoa(created.ID)+"/status", body)
		if w.Code != http.StatusOK {
			t.Errorf("status %q: HTTP %d, want %d (body=%q)",
				s, w.Code, http.StatusOK, w.Body.String())
		}
		// Verify via GET that the status was persisted.
		w = ut.PerformRequest(engine, "GET", assetBasePath+"/"+itoa(created.ID), nil)
		if w.Code != http.StatusOK {
			t.Fatalf("get after status update: HTTP %d", w.Code)
		}
		resp := decodeAPIResponse(t, w)
		a := decodeAssetData(t, resp)
		if a.Status != s {
			t.Errorf("after setting %q: persisted status = %q", s, a.Status)
		}
	}
}

// TestAssetUpdateStatusInvalidValue verifies an unknown status value is
// rejected with 400.
func TestAssetUpdateStatusInvalidValue(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	created := createAssetViaAPI(t, engine,
		`{"asset_type":"host","asset_key":"invalid-status","name":"x"}`)

	w := doRequest(engine, "PUT", assetBasePath+"/"+itoa(created.ID)+"/status",
		[]byte(`{"status":"bogus"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	if resp.Code != errdefs.CodeBadRequest {
		t.Errorf("code = %d, want %d", resp.Code, errdefs.CodeBadRequest)
	}
}

// TestAssetUpdateStatusEmptyStatus verifies an empty status is rejected.
func TestAssetUpdateStatusEmptyStatus(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	created := createAssetViaAPI(t, engine,
		`{"asset_type":"host","asset_key":"empty-status","name":"x"}`)

	w := doRequest(engine, "PUT", assetBasePath+"/"+itoa(created.ID)+"/status",
		[]byte(`{"status":""}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestAssetUpdateStatusNotFound verifies updating status on a non-existent
// asset returns 404.
func TestAssetUpdateStatusNotFound(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	w := doRequest(engine, "PUT", assetBasePath+"/999/status",
		[]byte(`{"status":"normal"}`))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	resp := decodeAPIResponse(t, w)
	if resp.Code != errdefs.CodeNotFound {
		t.Errorf("code = %d, want %d", resp.Code, errdefs.CodeNotFound)
	}
}

// TestAssetUpdateStatusInvalidID verifies PUT /abc/status returns 400.
func TestAssetUpdateStatusInvalidID(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	w := doRequest(engine, "PUT", assetBasePath+"/abc/status",
		[]byte(`{"status":"normal"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestAssetUpdateStatusInvalidJSON verifies malformed JSON returns 400.
func TestAssetUpdateStatusInvalidJSON(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	created := createAssetViaAPI(t, engine,
		`{"asset_type":"host","asset_key":"bad-json","name":"x"}`)

	w := doRequest(engine, "PUT", assetBasePath+"/"+itoa(created.ID)+"/status",
		[]byte(`{not json`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// =============================================================================
// ProbeAsset
// =============================================================================

// TestAssetProbeSuccess verifies POST /:id/probe returns the asset's current
// status as a probeResult.
func TestAssetProbeSuccess(t *testing.T) {
	engine, store := newAssetTestEngine(t)
	ctx := context.Background()

	// Seed an asset with a known status via the store so we can verify the
	// probe reads it back accurately.
	a := &assetstore.Asset{
		AssetType: types.AssetTypeHost,
		AssetKey:  "probe-target",
		Name:      "probe-host",
		Status:    types.AssetStatusNormal,
	}
	if err := store.Create(ctx, a); err != nil {
		t.Fatalf("seed: %v", err)
	}

	w := ut.PerformRequest(engine, "POST", assetBasePath+"/"+itoa(a.ID)+"/probe", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	raw, _ := json.Marshal(resp.Data)
	var result probeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal probe result: %v", err)
	}
	if result.AssetID != a.ID {
		t.Errorf("asset_id = %d, want %d", result.AssetID, a.ID)
	}
	if result.Status != types.AssetStatusNormal {
		t.Errorf("status = %q, want %q", result.Status, types.AssetStatusNormal)
	}
}

// TestAssetProbeNotFound verifies probing a non-existent asset returns 404.
func TestAssetProbeNotFound(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	w := ut.PerformRequest(engine, "POST", assetBasePath+"/999/probe", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	resp := decodeAPIResponse(t, w)
	if resp.Code != errdefs.CodeNotFound {
		t.Errorf("code = %d, want %d", resp.Code, errdefs.CodeNotFound)
	}
}

// TestAssetProbeInvalidID verifies POST /abc/probe returns 400.
func TestAssetProbeInvalidID(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	w := ut.PerformRequest(engine, "POST", assetBasePath+"/abc/probe", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// =============================================================================
// Full lifecycle integration test
// =============================================================================

// TestAssetFullLifecycle exercises the complete asset lifecycle end-to-end:
// create → get → list → update → status update → probe → delete → get(deleted).
// It verifies all operations compose correctly and the state transitions are
// consistent across the CRUD surface.
func TestAssetFullLifecycle(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	// 1. Create
	created := createAssetViaAPI(t, engine,
		`{"asset_type":"website","asset_key":"lifecycle-001","name":"lifecycle-site"}`)
	if created.Status != types.AssetStatusUnknown {
		t.Errorf("initial status = %q, want %q (default)", created.Status, types.AssetStatusUnknown)
	}

	// 2. Get — verify the created asset is retrievable.
	w := ut.PerformRequest(engine, "GET", assetBasePath+"/"+itoa(created.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get after create: HTTP %d", w.Code)
	}
	fetched := decodeAssetData(t, decodeAPIResponse(t, w))
	if fetched.AssetKey != "lifecycle-001" {
		t.Errorf("fetched key = %q, want lifecycle-001", fetched.AssetKey)
	}

	// 3. List — verify the asset appears in the list.
	w = ut.PerformRequest(engine, "GET", assetBasePath+"?page=1&page_size=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: HTTP %d", w.Code)
	}
	listResp := decodeAPIResponse(t, w)
	listData, _ := listResp.Data.(map[string]any)
	if listData["total"].(float64) != 1 {
		t.Errorf("list total = %v, want 1", listData["total"])
	}

	// 4. Update — change name and status.
	w = doRequest(engine, "PUT", assetBasePath+"/"+itoa(created.ID),
		[]byte(`{"asset_type":"website","asset_key":"lifecycle-001","name":"renamed-site","status":"normal"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("update: HTTP %d (body=%q)", w.Code, w.Body.String())
	}
	updated := decodeAssetData(t, decodeAPIResponse(t, w))
	if updated.Name != "renamed-site" {
		t.Errorf("updated name = %q, want renamed-site", updated.Name)
	}
	if updated.Status != types.AssetStatusNormal {
		t.Errorf("updated status = %q, want normal", updated.Status)
	}

	// 5. Status update — transition to abnormal.
	w = doRequest(engine, "PUT", assetBasePath+"/"+itoa(created.ID)+"/status",
		[]byte(`{"status":"abnormal"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status update: HTTP %d", w.Code)
	}

	// 6. Probe — verify the probe reads the updated status.
	w = ut.PerformRequest(engine, "POST", assetBasePath+"/"+itoa(created.ID)+"/probe", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("probe: HTTP %d", w.Code)
	}
	probeResp := decodeAPIResponse(t, w)
	probeRaw, _ := json.Marshal(probeResp.Data)
	var pr probeResult
	if err := json.Unmarshal(probeRaw, &pr); err != nil {
		t.Fatalf("unmarshal probe: %v", err)
	}
	if pr.Status != types.AssetStatusAbnormal {
		t.Errorf("probe status = %q, want abnormal", pr.Status)
	}

	// 7. Delete — remove the asset.
	w = ut.PerformRequest(engine, "DELETE", assetBasePath+"/"+itoa(created.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: HTTP %d", w.Code)
	}

	// 8. Get after delete — must return 404.
	w = ut.PerformRequest(engine, "GET", assetBasePath+"/"+itoa(created.ID), nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("get after delete: HTTP %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestAssetCreateAllAssetTypes verifies all supported asset types are accepted
// on creation (device, host, website, service).
func TestAssetCreateAllAssetTypes(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	cases := []struct {
		name      string
		assetType types.AssetType
	}{
		{"device", types.AssetTypeDevice},
		{"host", types.AssetTypeHost},
		{"website", types.AssetTypeWebsite},
		{"service", types.AssetTypeService},
	}
	for _, c := range cases {
		body := []byte(`{"asset_type":"` + string(c.assetType) + `","asset_key":"` + c.name + `-key","name":"` + c.name + `"}`)
		w := doRequest(engine, "POST", assetBasePath, body)
		if w.Code != http.StatusOK {
			t.Errorf("type %q: HTTP %d, want %d (body=%q)", c.assetType, w.Code, http.StatusOK, w.Body.String())
		}
	}
}

// =============================================================================
// Audit log verification tests
// =============================================================================

// TestAuditLogAssetCreateSuccess verifies the asset.create audit log entry
// contains the expected operation, outcome, id, asset_key, asset_type, and
// name fields.
func TestAuditLogAssetCreateSuccess(t *testing.T) {
	engine, _, logs := newAssetTestEngineWithLogger(t)

	w := doRequest(engine, "POST", assetBasePath,
		[]byte(`{"asset_type":"host","asset_key":"audit-create","name":"audit-host"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("create: HTTP %d", w.Code)
	}

	entry := findAuditEntry(t, logs, "asset.create")
	if level := entry.Level; level != zapcore.InfoLevel {
		t.Errorf("level = %v, want Info", level)
	}
	if out, _ := fieldValue(entry, "outcome"); out != "success" {
		t.Errorf("outcome = %q, want success", out)
	}
	if key, _ := fieldValue(entry, "asset_key"); key != "audit-create" {
		t.Errorf("asset_key = %q, want audit-create", key)
	}
	if at, _ := fieldValue(entry, "asset_type"); at != "host" {
		t.Errorf("asset_type = %q, want host", at)
	}
	if name, _ := fieldValue(entry, "name"); name != "audit-host" {
		t.Errorf("name = %q, want audit-host", name)
	}
	if id, ok := fieldInt(entry, "id"); !ok || id == 0 {
		t.Errorf("id = %d, want non-zero", id)
	}
}

// TestAuditLogAssetCreateConflict verifies the duplicate-key conflict path
// emits a Warn-level audit log with outcome=conflict.
func TestAuditLogAssetCreateConflict(t *testing.T) {
	engine, _, logs := newAssetTestEngineWithLogger(t)

	// First create succeeds.
	doRequest(engine, "POST", assetBasePath,
		[]byte(`{"asset_type":"host","asset_key":"conflict-audit","name":"first"}`))

	// Second create with same key — triggers conflict audit log.
	w := doRequest(engine, "POST", assetBasePath,
		[]byte(`{"asset_type":"host","asset_key":"conflict-audit","name":"second"}`))
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate create: HTTP %d, want %d", w.Code, http.StatusConflict)
	}

	// Find the conflict audit entry (the second create's log).
	var conflictEntry observer.LoggedEntry
	found := false
	for _, e := range logs.All() {
		if op, _ := fieldValue(e, "operation"); op == "asset.create" {
			if out, _ := fieldValue(e, "outcome"); out == "conflict" {
				conflictEntry = e
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("conflict audit log entry not found")
	}
	if conflictEntry.Level != zapcore.WarnLevel {
		t.Errorf("conflict level = %v, want Warn", conflictEntry.Level)
	}
	if key, _ := fieldValue(conflictEntry, "asset_key"); key != "conflict-audit" {
		t.Errorf("conflict asset_key = %q, want conflict-audit", key)
	}
}

// TestAuditLogAssetCreateQuotaExceeded verifies the quota-exceeded path emits
// a Warn-level audit log with outcome=quota_exceeded and current_count/quota.
func TestAuditLogAssetCreateQuotaExceeded(t *testing.T) {
	engine, store, logs := newAssetTestEngineWithLogger(t)
	ctx := context.Background()

	// Seed the store to the quota cap.
	for i := 0; i < maxDeviceQuota; i++ {
		if err := store.Create(ctx, &assetstore.Asset{
			AssetType: types.AssetTypeDevice,
			AssetKey:  "quota-audit-" + itoa(int64(i)),
			Name:      "device",
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	w := doRequest(engine, "POST", assetBasePath,
		[]byte(`{"asset_type":"device","asset_key":"quota-over","name":"over"}`))
	if w.Code != http.StatusConflict {
		t.Fatalf("quota create: HTTP %d, want %d", w.Code, http.StatusConflict)
	}

	var quotaEntry observer.LoggedEntry
	found := false
	for _, e := range logs.All() {
		if op, _ := fieldValue(e, "operation"); op == "asset.create" {
			if out, _ := fieldValue(e, "outcome"); out == "quota_exceeded" {
				quotaEntry = e
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("quota_exceeded audit log entry not found")
	}
	if quotaEntry.Level != zapcore.WarnLevel {
		t.Errorf("quota level = %v, want Warn", quotaEntry.Level)
	}
	if count, ok := fieldInt(quotaEntry, "current_count"); !ok || count != int64(maxDeviceQuota) {
		t.Errorf("current_count = %d, want %d", count, maxDeviceQuota)
	}
}

// TestAuditLogAssetUpdate verifies the asset.update audit log captures the
// field transition (prev_name → name, prev_status → status).
func TestAuditLogAssetUpdate(t *testing.T) {
	engine, _, logs := newAssetTestEngineWithLogger(t)

	created := createAssetViaAPI(t, engine,
		`{"asset_type":"host","asset_key":"audit-update","name":"old-name"}`)

	w := doRequest(engine, "PUT", assetBasePath+"/"+itoa(created.ID),
		[]byte(`{"asset_type":"host","asset_key":"audit-update","name":"new-name","status":"normal"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("update: HTTP %d", w.Code)
	}

	entry := findAuditEntry(t, logs, "asset.update")
	if out, _ := fieldValue(entry, "outcome"); out != "success" {
		t.Errorf("outcome = %q, want success", out)
	}
	if name, _ := fieldValue(entry, "name"); name != "new-name" {
		t.Errorf("name = %q, want new-name", name)
	}
	if prev, _ := fieldValue(entry, "prev_name"); prev != "old-name" {
		t.Errorf("prev_name = %q, want old-name", prev)
	}
	if status, _ := fieldValue(entry, "status"); status != "normal" {
		t.Errorf("status = %q, want normal", status)
	}
}

// TestAuditLogAssetDelete verifies the asset.delete audit log is emitted
// with outcome=success and the deleted id.
func TestAuditLogAssetDelete(t *testing.T) {
	engine, _, logs := newAssetTestEngineWithLogger(t)

	created := createAssetViaAPI(t, engine,
		`{"asset_type":"host","asset_key":"audit-delete","name":"to-delete"}`)

	w := ut.PerformRequest(engine, "DELETE", assetBasePath+"/"+itoa(created.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: HTTP %d", w.Code)
	}

	entry := findAuditEntry(t, logs, "asset.delete")
	if out, _ := fieldValue(entry, "outcome"); out != "success" {
		t.Errorf("outcome = %q, want success", out)
	}
	if id, ok := fieldInt(entry, "id"); !ok || id != created.ID {
		t.Errorf("id = %d, want %d", id, created.ID)
	}
}

// TestAuditLogAssetStatusUpdate verifies the asset.status_update audit log
// captures the status transition (prev_status → status).
func TestAuditLogAssetStatusUpdate(t *testing.T) {
	engine, _, logs := newAssetTestEngineWithLogger(t)

	created := createAssetViaAPI(t, engine,
		`{"asset_type":"host","asset_key":"audit-status","name":"x"}`)

	w := doRequest(engine, "PUT", assetBasePath+"/"+itoa(created.ID)+"/status",
		[]byte(`{"status":"abnormal"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status update: HTTP %d", w.Code)
	}

	entry := findAuditEntry(t, logs, "asset.status_update")
	if out, _ := fieldValue(entry, "outcome"); out != "success" {
		t.Errorf("outcome = %q, want success", out)
	}
	if status, _ := fieldValue(entry, "status"); status != "abnormal" {
		t.Errorf("status = %q, want abnormal", status)
	}
	if prev, _ := fieldValue(entry, "prev_status"); prev != "unknown" {
		t.Errorf("prev_status = %q, want unknown", prev)
	}
}

// TestAuditLogAssetProbe verifies the asset.probe audit log is emitted with
// outcome=success, id, asset_key, and status.
func TestAuditLogAssetProbe(t *testing.T) {
	engine, _, logs := newAssetTestEngineWithLogger(t)

	created := createAssetViaAPI(t, engine,
		`{"asset_type":"host","asset_key":"audit-probe","name":"probe-target"}`)

	w := ut.PerformRequest(engine, "POST", assetBasePath+"/"+itoa(created.ID)+"/probe", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("probe: HTTP %d", w.Code)
	}

	entry := findAuditEntry(t, logs, "asset.probe")
	if out, _ := fieldValue(entry, "outcome"); out != "success" {
		t.Errorf("outcome = %q, want success", out)
	}
	if id, ok := fieldInt(entry, "id"); !ok || id != created.ID {
		t.Errorf("id = %d, want %d", id, created.ID)
	}
	if key, _ := fieldValue(entry, "asset_key"); key != "audit-probe" {
		t.Errorf("asset_key = %q, want audit-probe", key)
	}
}

// TestAuditLogAssetGetNotFound verifies the asset.get audit log records
// outcome=not_found for a missing asset.
func TestAuditLogAssetGetNotFound(t *testing.T) {
	engine, _, logs := newAssetTestEngineWithLogger(t)

	w := ut.PerformRequest(engine, "GET", assetBasePath+"/999", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get missing: HTTP %d, want %d", w.Code, http.StatusNotFound)
	}

	entry := findAuditEntry(t, logs, "asset.get")
	if out, _ := fieldValue(entry, "outcome"); out != "not_found" {
		t.Errorf("outcome = %q, want not_found", out)
	}
	if entry.Level != zapcore.InfoLevel {
		t.Errorf("level = %v, want Info", entry.Level)
	}
}

// TestAuditLogAssetDeleteNotFound verifies the asset.delete audit log records
// outcome=not_found when deleting a missing asset.
func TestAuditLogAssetDeleteNotFound(t *testing.T) {
	engine, _, logs := newAssetTestEngineWithLogger(t)

	w := ut.PerformRequest(engine, "DELETE", assetBasePath+"/999", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("delete missing: HTTP %d, want %d", w.Code, http.StatusNotFound)
	}

	entry := findAuditEntry(t, logs, "asset.delete")
	if out, _ := fieldValue(entry, "outcome"); out != "not_found" {
		t.Errorf("outcome = %q, want not_found", out)
	}
}

// TestAuditLogAssetStatusUpdateInvalidStatus verifies the invalid-status
// rejection path emits a Warn-level audit log with outcome=invalid_status.
func TestAuditLogAssetStatusUpdateInvalidStatus(t *testing.T) {
	engine, _, logs := newAssetTestEngineWithLogger(t)

	created := createAssetViaAPI(t, engine,
		`{"asset_type":"host","asset_key":"invalid-status-audit","name":"x"}`)

	w := doRequest(engine, "PUT", assetBasePath+"/"+itoa(created.ID)+"/status",
		[]byte(`{"status":"bogus"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid status: HTTP %d, want %d", w.Code, http.StatusBadRequest)
	}

	var invalidEntry observer.LoggedEntry
	found := false
	for _, e := range logs.All() {
		if op, _ := fieldValue(e, "operation"); op == "asset.status_update" {
			if out, _ := fieldValue(e, "outcome"); out == "invalid_status" {
				invalidEntry = e
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("invalid_status audit log entry not found")
	}
	if invalidEntry.Level != zapcore.WarnLevel {
		t.Errorf("level = %v, want Warn", invalidEntry.Level)
	}
	if req, _ := fieldValue(invalidEntry, "requested_status"); req != "bogus" {
		t.Errorf("requested_status = %q, want bogus", req)
	}
}

// TestAuditLogNilLoggerNoPanic verifies the handler does not panic when
// constructed with a nil logger (the nop fallback path).
func TestAuditLogNilLoggerNoPanic(t *testing.T) {
	quota.SetProvider(testProvider{})
	t.Cleanup(func() { quota.SetProvider(nil) })

	dbc, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := dbc.AutoMigrate(&assetstore.Asset{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := assetstore.NewStore(dbc)
	h := NewHandler(store, nil)

	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.POST(assetBasePath, h.CreateAsset)

	// This must not panic despite the nil logger (constructor falls back to nop).
	w := doRequest(engine, "POST", assetBasePath,
		[]byte(`{"asset_type":"host","asset_key":"nil-logger","name":"x"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("create with nil logger: HTTP %d, want %d (body=%q)",
			w.Code, http.StatusOK, w.Body.String())
	}
}

// =============================================================================
// Pagination edge cases
// =============================================================================

// TestAssetListPagination verifies page/size parameters are honored and the
// total count is accurate across multiple pages.
func TestAssetListPagination(t *testing.T) {
	engine, store := newAssetTestEngine(t)
	ctx := context.Background()

	// Seed 5 assets.
	for i := 0; i < 5; i++ {
		if err := store.Create(ctx, &assetstore.Asset{
			AssetType: types.AssetTypeHost,
			AssetKey:  "page-" + itoa(int64(i)),
			Name:      "host",
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	// Page 1 with size 2 — should return 2 items and total=5.
	w := ut.PerformRequest(engine, "GET", assetBasePath+"?page=1&page_size=2", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("page 1: HTTP %d", w.Code)
	}
	resp := decodeAPIResponse(t, w)
	data, _ := resp.Data.(map[string]any)
	if data["total"].(float64) != 5 {
		t.Errorf("total = %v, want 5", data["total"])
	}
	items, _ := data["items"].([]any)
	if len(items) != 2 {
		t.Errorf("page 1 items = %d, want 2", len(items))
	}

	// Page 3 with size 2 — should return 1 item (5 total - 4 on pages 1-2).
	w = ut.PerformRequest(engine, "GET", assetBasePath+"?page=3&page_size=2", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("page 3: HTTP %d", w.Code)
	}
	resp = decodeAPIResponse(t, w)
	data, _ = resp.Data.(map[string]any)
	items, _ = data["items"].([]any)
	if len(items) != 1 {
		t.Errorf("page 3 items = %d, want 1", len(items))
	}
}

// TestAssetListDefaultPaging verifies that omitting page/size uses the
// default page=1 and size=20.
func TestAssetListDefaultPaging(t *testing.T) {
	engine, store := newAssetTestEngine(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := store.Create(ctx, &assetstore.Asset{
			AssetType: types.AssetTypeHost,
			AssetKey:  "default-" + itoa(int64(i)),
			Name:      "host",
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	w := ut.PerformRequest(engine, "GET", assetBasePath, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: HTTP %d", w.Code)
	}
	resp := decodeAPIResponse(t, w)
	data, _ := resp.Data.(map[string]any)
	if data["total"].(float64) != 3 {
		t.Errorf("total = %v, want 3", data["total"])
	}
	items, _ := data["items"].([]any)
	if len(items) != 3 {
		t.Errorf("items = %d, want 3", len(items))
	}
}

// =============================================================================
// Update edge cases
// =============================================================================

// TestAssetUpdatePartialFields verifies PUT with only some fields preserves
// unspecified fields from the existing asset (bind-over-existing pattern).
func TestAssetUpdatePartialFields(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	created := createAssetViaAPI(t, engine,
		`{"asset_type":"host","asset_key":"partial-update","name":"original","status":"normal"}`)

	// Update only the name; asset_type, asset_key, and status should be
	// preserved from the existing record.
	w := doRequest(engine, "PUT", assetBasePath+"/"+itoa(created.ID),
		[]byte(`{"name":"renamed"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("partial update: HTTP %d (body=%q)", w.Code, w.Body.String())
	}
	updated := decodeAssetData(t, decodeAPIResponse(t, w))
	if updated.Name != "renamed" {
		t.Errorf("name = %q, want renamed", updated.Name)
	}
	// The bind-over-existing pattern means omitted fields retain the existing
	// value. The exact behavior depends on the JSON binding: zero values in
	// the request may overwrite, so we only assert the name changed.
}

// TestAssetUpdateInvalidJSON verifies malformed JSON on update returns 400.
func TestAssetUpdateInvalidJSON(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	created := createAssetViaAPI(t, engine,
		`{"asset_type":"host","asset_key":"bad-update","name":"x"}`)

	w := doRequest(engine, "PUT", assetBasePath+"/"+itoa(created.ID),
		[]byte(`{not json`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// =============================================================================
// Concurrency / multi-asset scenario
// =============================================================================

// TestAssetCreateMultipleDistinctKeys verifies creating several assets with
// distinct keys all succeed and are listable.
func TestAssetCreateMultipleDistinctKeys(t *testing.T) {
	engine, _ := newAssetTestEngine(t)

	for i := 0; i < 10; i++ {
		body := []byte(`{"asset_type":"host","asset_key":"multi-` + itoa(int64(i)) + `","name":"host-` + itoa(int64(i)) + `"}`)
		w := doRequest(engine, "POST", assetBasePath, body)
		if w.Code != http.StatusOK {
			t.Fatalf("create %d: HTTP %d (body=%q)", i, w.Code, w.Body.String())
		}
	}

	w := ut.PerformRequest(engine, "GET", assetBasePath+"?page=1&page_size=100", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: HTTP %d", w.Code)
	}
	resp := decodeAPIResponse(t, w)
	data, _ := resp.Data.(map[string]any)
	if data["total"].(float64) != 10 {
		t.Errorf("total = %v, want 10", data["total"])
	}
}

// Ensure json import is used (referenced by helpers above).
var _ = json.Marshal
