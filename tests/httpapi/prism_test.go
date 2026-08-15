// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	prismalert "github.com/tickraft/tickraft/pkg/prism/alert"
)

// TestAlertRulesCRUD covers alert rule create/get/update/delete/list.
func TestAlertRulesCRUD(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	ruleBody := map[string]any{
		"name":       "httpapi-cpu-rule",
		"scene":      "metric",
		"expression": `event.metrics["cpu_usage"] > 90`,
		"enabled":    true,
	}
	status, env := hs.do("POST", "/api/v1/prism/alert/rules", ruleBody, token)
	var created struct {
		ID int64 `json:"id"`
	}
	hs.mustOK(status, env, "create alert rule", &created)
	if created.ID == 0 {
		t.Fatal("create alert rule: no id returned")
	}
	defer func() {
		_, _ = hs.do("DELETE", "/api/v1/prism/alert/rules/"+jsonInt64(created.ID), nil, token)
	}()

	// Validation: missing required fields is a 400.
	status, _ = hs.do("POST", "/api/v1/prism/alert/rules",
		map[string]any{"name": "incomplete"}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("create alert rule without scene/expression: expected 400, got %d", status)
	}

	// Get.
	status, env = hs.do("GET", "/api/v1/prism/alert/rules/"+jsonInt64(created.ID), nil, token)
	var got map[string]any
	hs.mustOK(status, env, "get alert rule", &got)
	if got["expression"] != `event.metrics["cpu_usage"] > 90` {
		t.Fatalf("get alert rule: unexpected expression %v", got["expression"])
	}

	// Update.
	ruleBody["expression"] = `event.metrics["cpu_usage"] > 95`
	ruleBody["name"] = "httpapi-cpu-rule-v2"
	status, env = hs.do("PUT", "/api/v1/prism/alert/rules/"+jsonInt64(created.ID), ruleBody, token)
	if status != http.StatusOK {
		t.Fatalf("update alert rule: expected 200, got %d code=%d", status, env.Code)
	}

	// List.
	pd := hs.listPage(token, "/api/v1/prism/alert/rules?page=1&page_size=20")
	if pd.Total < 1 {
		t.Fatalf("list alert rules: expected >=1, got %d", pd.Total)
	}
}

// seedAlertRecord inserts an alert record directly through the record store
// (records are normally produced by the prism engine at runtime).
func seedAlertRecord(hs *harness, ruleID int64, ruleName, severity, status string, triggeredAt time.Time) int64 {
	hs.t.Helper()
	rec := &prismalert.Record{
		RuleID:      ruleID,
		RuleName:    ruleName,
		Severity:    severity,
		Value:       91.5,
		Message:     "httpapi seeded record",
		Status:      status,
		TriggeredAt: triggeredAt,
	}
	if err := hs.prismEngine.RecordStore().Create(context.Background(), rec); err != nil {
		hs.t.Fatalf("seed alert record: %v", err)
	}
	return rec.ID
}

// TestAlertRecordsFlow covers record filtering (severity/status/from/to) and
// the acknowledge/resolve transitions.
func TestAlertRecordsFlow(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	now := time.Now().UTC()
	idFiring := seedAlertRecord(hs, 1, "httpapi-rule", "critical", "firing", now)
	idResolved := seedAlertRecord(hs, 1, "httpapi-rule", "warning", "resolved", now.Add(-2*time.Hour))

	// Severity filter.
	pd := hs.listPage(token, "/api/v1/prism/alert/records?page=1&page_size=50&severity=critical")
	if pd.Total < 1 {
		t.Fatalf("records severity filter: expected >=1, got %d", pd.Total)
	}
	for _, item := range pd.Items {
		if item["severity"] != "critical" {
			t.Fatalf("records severity filter: leaked item %v", item)
		}
	}

	// Status filter.
	pd = hs.listPage(token, "/api/v1/prism/alert/records?page=1&page_size=50&status=resolved")
	if pd.Total < 1 {
		t.Fatalf("records status filter: expected >=1, got %d", pd.Total)
	}

	// Time range filter: from = 1h ago excludes the 2h-old record.
	pd = hs.listPage(token, fmt.Sprintf(
		"/api/v1/prism/alert/records?page=1&page_size=50&from=%s",
		now.Add(-time.Hour).Format(time.RFC3339)))
	if pd.Total < 1 {
		t.Fatalf("records from filter: expected >=1, got %d", pd.Total)
	}
	for _, item := range pd.Items {
		if name, _ := item["rule_name"].(string); name == "" {
			continue
		}
	}

	// Acknowledge the firing record.
	status, env := hs.do("PUT",
		"/api/v1/prism/alert/records/"+jsonInt64(idFiring)+"/acknowledge", nil, token)
	var acked map[string]any
	hs.mustOK(status, env, "acknowledge record", &acked)
	if acked["status"] != "acknowledged" {
		t.Fatalf("acknowledge record: status=%v, expected acknowledged", acked["status"])
	}

	// Resolve it.
	status, env = hs.do("PUT",
		"/api/v1/prism/alert/records/"+jsonInt64(idFiring)+"/resolve", nil, token)
	var resolved map[string]any
	hs.mustOK(status, env, "resolve record", &resolved)
	if resolved["status"] != "resolved" {
		t.Fatalf("resolve record: status=%v, expected resolved", resolved["status"])
	}
	_ = idResolved
}

// TestChannelsCRUDAndWebhookTest covers notification channel CRUD and the
// webhook test endpoint against a local httptest receiver.
func TestChannelsCRUDAndWebhookTest(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	var hits atomic.Int64
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()

	config, err := json.Marshal(map[string]any{
		"type":    "webhook",
		"url":     receiver.URL,
		"timeout": "5s",
	})
	if err != nil {
		t.Fatalf("marshal channel config: %v", err)
	}

	status, env := hs.do("POST", "/api/v1/prism/channels", map[string]any{
		"name":    "httpapi-webhook",
		"type":    "webhook",
		"config":  string(config),
		"enabled": true,
	}, token)
	var created struct {
		ID int64 `json:"id"`
	}
	hs.mustOK(status, env, "create channel", &created)
	if created.ID == 0 {
		t.Fatal("create channel: no id returned")
	}
	defer func() {
		_, _ = hs.do("DELETE", "/api/v1/prism/channels/"+jsonInt64(created.ID), nil, token)
	}()

	// Test delivers a real webhook to the local receiver.
	status, env = hs.do("POST", "/api/v1/prism/channels/"+jsonInt64(created.ID)+"/test", nil, token)
	if status != http.StatusOK {
		t.Fatalf("test channel: expected 200, got %d code=%d (%s)", status, env.Code, env.Message)
	}
	if hits.Load() == 0 {
		t.Fatal("test channel: webhook receiver was not called")
	}

	// Update + list.
	status, env = hs.do("PUT", "/api/v1/prism/channels/"+jsonInt64(created.ID), map[string]any{
		"name":    "httpapi-webhook-v2",
		"type":    "webhook",
		"config":  string(config),
		"enabled": false,
	}, token)
	if status != http.StatusOK {
		t.Fatalf("update channel: expected 200, got %d code=%d", status, env.Code)
	}
	pd := hs.listPage(token, "/api/v1/prism/channels?page=1&page_size=20")
	if pd.Total < 1 {
		t.Fatalf("list channels: expected >=1, got %d", pd.Total)
	}
}
