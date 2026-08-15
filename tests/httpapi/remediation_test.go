// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/prism/remediation"
)

func remediationBody(name, trigger, executor string) map[string]any {
	return map[string]any{
		"name":                      name,
		"description":               "httpapi remediation rule",
		"trigger_event_type":        trigger,
		"condition_expr":            "value > 10",
		"executor_type":             executor,
		"executor_config":           `{"url":"http://127.0.0.1:1/noop","method":"POST","timeout":"1s"}`,
		"cooldown":                  300,
		"circuit_breaker_threshold": 3,
		"enabled":                   true,
	}
}

// TestRemediationRulesCRUDAndValidation covers rule CRUD plus the trigger /
// executor enum validation the frontend relies on.
func TestRemediationRulesCRUDAndValidation(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	status, env := hs.do("POST", "/api/v1/prism/remediation/rules",
		remediationBody("httpapi-remediation-rule", "metric", "webhook"), token)
	var created struct {
		ID int64 `json:"id"`
	}
	hs.mustOK(status, env, "create remediation rule", &created)
	if created.ID == 0 {
		t.Fatal("create remediation rule: no id returned")
	}
	defer func() {
		_, _ = hs.do("DELETE", "/api/v1/prism/remediation/rules/"+jsonInt64(created.ID), nil, token)
	}()

	// Invalid trigger event type is rejected.
	status, _ = hs.do("POST", "/api/v1/prism/remediation/rules",
		remediationBody("bad-trigger", "alert.firing", "webhook"), token)
	if status != http.StatusBadRequest {
		t.Fatalf("create remediation rule with bad trigger: expected 400, got %d", status)
	}

	// Invalid executor type is rejected.
	status, _ = hs.do("POST", "/api/v1/prism/remediation/rules",
		remediationBody("bad-executor", "metric", "ssh"), token)
	if status != http.StatusBadRequest {
		t.Fatalf("create remediation rule with bad executor: expected 400, got %d", status)
	}

	// All three contract trigger types are accepted.
	for _, trigger := range []string{"metric", "log", "status_change"} {
		body := remediationBody("httpapi-trigger-"+trigger, trigger, "webhook")
		status, env := hs.do("POST", "/api/v1/prism/remediation/rules", body, token)
		if status != http.StatusOK && status != http.StatusCreated {
			t.Fatalf("create remediation rule trigger=%s: expected 2xx, got %d (code=%d, %s)",
				trigger, status, env.Code, env.Message)
		}
		var rule struct {
			ID int64 `json:"id"`
		}
		hs.mustOK(status, env, "create remediation rule "+trigger, &rule)
		id := rule.ID
		defer func() {
			_, _ = hs.do("DELETE", "/api/v1/prism/remediation/rules/"+jsonInt64(id), nil, token)
		}()
	}

	// Update + get + list.
	status, env = hs.do("PUT", "/api/v1/prism/remediation/rules/"+jsonInt64(created.ID),
		remediationBody("httpapi-remediation-rule-v2", "status_change", "local"), token)
	if status != http.StatusOK {
		t.Fatalf("update remediation rule: expected 200, got %d code=%d (%s)", status, env.Code, env.Message)
	}
	status, env = hs.do("GET", "/api/v1/prism/remediation/rules/"+jsonInt64(created.ID), nil, token)
	var got map[string]any
	hs.mustOK(status, env, "get remediation rule", &got)
	if got["trigger_event_type"] != "status_change" || got["executor_type"] != "local" {
		t.Fatalf("get remediation rule: unexpected payload %v/%v",
			got["trigger_event_type"], got["executor_type"])
	}
	pd := hs.listPage(token, "/api/v1/prism/remediation/rules?page=1&page_size=20")
	if pd.Total < 1 {
		t.Fatalf("list remediation rules: expected >=1, got %d", pd.Total)
	}
}

// TestRemediationQuota asserts the rule quota is enforced with HTTP 409.
func TestRemediationQuota(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	// How many rules already exist (other tests may have left some).
	pd := hs.listPage(token, "/api/v1/prism/remediation/rules?page=1&page_size=1")
	existing := pd.Total

	const quotaCeiling = 5
	created := make([]int64, 0, quotaCeiling)
	defer func() {
		for _, id := range created {
			_, _ = hs.do("DELETE", "/api/v1/prism/remediation/rules/"+jsonInt64(id), nil, token)
		}
	}()

	for i := int64(0); i < quotaCeiling-existing; i++ {
		status, env := hs.do("POST", "/api/v1/prism/remediation/rules",
			remediationBody("httpapi-quota-rule", "metric", "webhook"), token)
		var rule struct {
			ID int64 `json:"id"`
		}
		if status != http.StatusOK && status != http.StatusCreated {
			t.Fatalf("create rule %d under quota: expected 2xx, got %d (code=%d, %s)",
				i, status, env.Code, env.Message)
		}
		hs.mustOK(status, env, "create rule under quota", &rule)
		created = append(created, rule.ID)
	}

	// The next create must hit the quota conflict.
	status, env := hs.do("POST", "/api/v1/prism/remediation/rules",
		remediationBody("httpapi-quota-overflow", "metric", "webhook"), token)
	if status != http.StatusConflict {
		t.Fatalf("create rule over quota: expected 409, got %d (code=%d, %s)",
			status, env.Code, env.Message)
	}
	if env.Code != 40900 {
		t.Fatalf("create rule over quota: expected code 40900, got %d", env.Code)
	}
}

// TestRemediationRecordsList seeds records through the remediation record
// store and verifies the records API contract.
func TestRemediationRecordsList(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	store := hs.prismEngine.RemediationStore()
	now := time.Now().UTC()
	records := []*remediation.Record{
		{
			RuleID: 1, RuleName: "httpapi-rule", AssetID: 4, AssetKey: "httpapi-asset",
			RunID: "httpapi-run-1", Trigger: "metric",
			Status: "completed", StartedAt: &now,
		},
		{
			RuleID: 2, RuleName: "httpapi-rule-fail", AssetID: 5, AssetKey: "httpapi-asset-2",
			RunID: "httpapi-run-2", Trigger: "status_change",
			Status: "failed", Error: "executor timeout", StartedAt: &now,
		},
	}
	for _, rec := range records {
		if err := store.UpsertRecord(context.Background(), rec); err != nil {
			t.Fatalf("seed remediation record: %v", err)
		}
	}

	pd := hs.listPage(token, "/api/v1/prism/remediation/records?page=1&page_size=50")
	if pd.Total < 2 {
		t.Fatalf("remediation records: expected >=2, got %d", pd.Total)
	}

	// Status filter.
	pd = hs.listPage(token, "/api/v1/prism/remediation/records?page=1&page_size=50&status=failed")
	if pd.Total != 1 {
		t.Fatalf("remediation records status filter: expected exactly 1, got %d", pd.Total)
	}
	item := pd.Items[0]
	for _, key := range []string{
		"id", "rule_id", "rule_name", "asset_id", "asset_key", "run_id",
		"trigger", "status", "error", "started_at", "created_at",
	} {
		if _, ok := item[key]; !ok {
			t.Fatalf("remediation record: missing field %q, keys=%v", key, keysOf(item))
		}
	}
	if item["status"] != "failed" {
		t.Fatalf("remediation records status filter: leaked item %v", item)
	}
}
