// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details

package httpapi

import (
	"encoding/json"
	"testing"
)

// quotaUsageItem mirrors one row of the quota usage aggregate.
type quotaUsageItem struct {
	Type    string  `json:"type"`
	Used    int64   `json:"used"`
	Ceiling int     `json:"ceiling"`
	Ratio   float64 `json:"ratio"`
}

// listQuotaUsage fetches GET /api/v1/quota/usage and decodes the envelope.
// The harness is package-shared (TestMain owns its lifecycle); this test
// must not shut it down.
func listQuotaUsage(t *testing.T, hs *harness, token string) []quotaUsageItem {
	t.Helper()
	status, env := hs.do("GET", "/api/v1/quota/usage", nil, token)
	var items []quotaUsageItem
	hs.mustOK(status, env, "/api/v1/quota/usage", &items)
	return items
}

// TestQuotaUsageRoute verifies the registered aggregate over the real
// stores: rows arrive for the wired count-based types with the CE fixed
// ceilings, and the device row reflects actually seeded assets.
func TestQuotaUsageRoute(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	before := listQuotaUsage(t, hs, token)
	beforeByType := map[string]quotaUsageItem{}
	for _, it := range before {
		beforeByType[it.Type] = it
	}

	// The five wired count-based types must all be present with the CE
	// fixed ceilings from internal/quota. Used counters may be non-zero
	// when other tests of the shared harness already seeded rows; only the
	// ceilings are stable to assert here.
	for ty, ceiling := range map[string]int{
		"device":      20,
		"probe":       20,
		"task":        20,
		"remediation": 5,
		"contact":     2,
	} {
		it, ok := beforeByType[ty]
		if !ok {
			t.Fatalf("quota usage missing type %q: %v", ty, before)
		}
		if it.Ceiling != ceiling {
			t.Errorf("type %q ceiling = %d, want %d", ty, it.Ceiling, ceiling)
		}
	}
	// On a fresh harness the device counter starts at zero.
	if beforeByType["device"].Used != 0 {
		t.Errorf("device used = %d on empty deployment, want 0", beforeByType["device"].Used)
	}

	// Seeding a device asset must move the device row, proving the counter
	// is wired to the real store (used grows by one; ratio follows).
	seed := map[string]any{
		"asset_type": "device",
		"asset_key":  "quota-usage-device",
		"name":       "quota-usage-device",
		"metadata":   `{"endpoint":"192.0.2.1"}`,
	}
	status, env := hs.do("POST", "/api/v1/assets", seed, token)
	var created struct {
		ID int64 `json:"id"`
	}
	hs.mustOK(status, env, "seed device asset", &created)

	after := listQuotaUsage(t, hs, token)
	raw, _ := json.Marshal(after)
	for _, it := range after {
		if it.Type != "device" {
			continue
		}
		if it.Used < 1 || it.Ratio < 0.05 {
			t.Fatalf("device row after seed = %s, want used>=1 ratio>=0.05", raw)
		}
		return
	}
	t.Fatalf("device row disappeared after seed: %s", raw)
}
