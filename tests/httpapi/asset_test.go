// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httpapi

import (
	"fmt"
	"net/http"
	"testing"
)

// seedAssets creates n assets with predictable names/types/statuses.
func seedAssets(hs *harness, token string, n int) []int64 {
	hs.t.Helper()
	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		assetType := "device"
		if i%2 == 1 {
			assetType = "service"
		}
		status, env := hs.do("POST", "/api/v1/assets", map[string]any{
			"asset_type": assetType,
			"asset_key":  fmt.Sprintf("httpapi-asset-%02d", i),
			"name":       fmt.Sprintf("httpapi-asset-%02d", i),
			"metadata":   fmt.Sprintf(`{"endpoint":"10.0.0.%d"}`, i),
		}, token)
		var created struct {
			ID int64 `json:"id"`
		}
		hs.mustOK(status, env, "seed asset", &created)
		ids = append(ids, created.ID)
	}
	return ids
}

// TestAssetCRUDAndFilters covers asset CRUD, server-side keyword/type/status
// filtering, and the pagination contract (page/page_size in and out).
func TestAssetCRUDAndFilters(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)
	ids := seedAssets(hs, token, 6)
	defer func() {
		for _, id := range ids {
			_, _ = hs.do("DELETE", "/api/v1/assets/"+jsonInt64(id), nil, token)
		}
	}()

	// Get by ID: snake_case field names.
	status, env := hs.do("GET", "/api/v1/assets/"+jsonInt64(ids[0]), nil, token)
	var got map[string]any
	hs.mustOK(status, env, "get asset", &got)
	for _, key := range []string{"id", "asset_type", "asset_key", "status", "created_at", "updated_at"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("get asset: expected snake_case field %q in response, keys=%v", key, keysOf(got))
		}
	}

	// Update name.
	status, env = hs.do("PUT", "/api/v1/assets/"+jsonInt64(ids[0]), map[string]any{
		"asset_type": "host",
		"asset_key":  "httpapi-asset-00",
		"name":       "httpapi-asset-00-renamed",
	}, token)
	if status != http.StatusOK {
		t.Fatalf("update asset: expected 200, got %d code=%d", status, env.Code)
	}

	// Keyword filter matches the renamed asset only.
	pd := hs.listPage(token, "/api/v1/assets?page=1&page_size=20&keyword=renamed")
	if pd.Total != 1 || len(pd.Items) != 1 {
		t.Fatalf("keyword filter: expected exactly 1 result, got total=%d items=%d", pd.Total, len(pd.Items))
	}
	if pd.Items[0]["name"] != "httpapi-asset-00-renamed" {
		t.Fatalf("keyword filter: unexpected item %v", pd.Items[0]["name"])
	}

	// Type filter: half of the six seeded assets are services.
	pd = hs.listPage(token, "/api/v1/assets?page=1&page_size=50&asset_type=service")
	if pd.Total < 3 {
		t.Fatalf("asset_type filter: expected >=3 services, got %d", pd.Total)
	}
	for _, item := range pd.Items {
		if item["asset_type"] != "service" {
			t.Fatalf("asset_type filter: non-service item returned: %v", item)
		}
	}

	// Status filter via explicit status update first.
	status, env = hs.do("PUT", "/api/v1/assets/"+jsonInt64(ids[1])+"/status",
		map[string]any{"status": "abnormal"}, token)
	if status != http.StatusOK {
		t.Fatalf("update asset status: expected 200, got %d code=%d", status, env.Code)
	}
	pd = hs.listPage(token, "/api/v1/assets?page=1&page_size=50&status=abnormal")
	if pd.Total < 1 {
		t.Fatalf("status filter: expected >=1 abnormal asset, got %d", pd.Total)
	}

	// Pagination clamp: page_size above the max is clamped to 100 and echoed
	// back in the envelope.
	pd = hs.listPage(token, "/api/v1/assets?page=1&page_size=500")
	if pd.PageSize != 100 {
		t.Fatalf("pagination clamp: expected page_size=100, got %d", pd.PageSize)
	}

	// Delete one asset and confirm the total shrinks.
	before := hs.listPage(token, "/api/v1/assets?page=1&page_size=100").Total
	status, env = hs.do("DELETE", "/api/v1/assets/"+jsonInt64(ids[2]), nil, token)
	if status != http.StatusOK {
		t.Fatalf("delete asset: expected 200, got %d code=%d", status, env.Code)
	}
	ids[2] = 0 // skip in cleanup
	after := hs.listPage(token, "/api/v1/assets?page=1&page_size=100").Total
	if after != before-1 {
		t.Fatalf("delete asset: total %d -> %d, expected decrement by 1", before, after)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
