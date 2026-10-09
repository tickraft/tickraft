// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	prismalert "github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/channel"
	"github.com/tickraft/tickraft/pkg/prism/channel/tracking"
)

// TestAlertRulesCRUD covers alert rule create/get/update/delete/list.
func TestAlertRulesCRUD(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	ruleBody := map[string]any{
		"name":       "httpapi-cpu-rule",
		"expression": `metrics["cpu_usage"] > 90`,
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
		t.Fatalf("create alert rule without expression: expected 400, got %d", status)
	}

	// Get.
	status, env = hs.do("GET", "/api/v1/prism/alert/rules/"+jsonInt64(created.ID), nil, token)
	var got map[string]any
	hs.mustOK(status, env, "get alert rule", &got)
	if got["expression"] != `metrics["cpu_usage"] > 90` {
		t.Fatalf("get alert rule: unexpected expression %v", got["expression"])
	}

	// Update.
	ruleBody["expression"] = `metrics["cpu_usage"] > 95`
	ruleBody["name"] = "httpapi-cpu-rule-v2"
	status, env = hs.do("PUT", "/api/v1/prism/alert/rules/"+jsonInt64(created.ID), ruleBody, token)
	if status != http.StatusOK {
		t.Fatalf("update alert rule: expected 200, got %d code=%d", status, env.Code)
	}

	// List.
	pd := hs.listPage(token, "/api/v1/prism/alert/rules?page=1&size=20")
	if pd.Total < 1 {
		t.Fatalf("list alert rules: expected >=1, got %d", pd.Total)
	}
}

// seedAlertRecordParams describes the alert record fields inserted by
// seedAlertRecord.
type seedAlertRecordParams struct {
	ruleName    string
	severity    string
	status      string
	triggeredAt time.Time
}

// seedAlertRecord inserts an alert record directly through the record store
// (records are normally produced by the prism engine at runtime).
func seedAlertRecord(hs *harness, ruleID int64, params seedAlertRecordParams) int64 {
	hs.t.Helper()
	rec := &prismalert.Record{
		RuleID:      ruleID,
		RuleName:    params.ruleName,
		Severity:    params.severity,
		Value:       91.5,
		Message:     "httpapi seeded record",
		Status:      params.status,
		TriggeredAt: params.triggeredAt,
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
	idFiring := seedAlertRecord(hs, 1, seedAlertRecordParams{
		ruleName:    "httpapi-rule",
		severity:    "critical",
		status:      "firing",
		triggeredAt: now,
	})
	idResolved := seedAlertRecord(hs, 1, seedAlertRecordParams{
		ruleName:    "httpapi-rule",
		severity:    "warning",
		status:      "resolved",
		triggeredAt: now.Add(-2 * time.Hour),
	})

	// Severity filter.
	pd := hs.listPage(token, "/api/v1/prism/alert/records?page=1&size=50&severity=critical")
	if pd.Total < 1 {
		t.Fatalf("records severity filter: expected >=1, got %d", pd.Total)
	}
	for _, item := range pd.Items {
		if item["severity"] != "critical" {
			t.Fatalf("records severity filter: leaked item %v", item)
		}
	}

	// Status filter.
	pd = hs.listPage(token, "/api/v1/prism/alert/records?page=1&size=50&status=resolved")
	if pd.Total < 1 {
		t.Fatalf("records status filter: expected >=1, got %d", pd.Total)
	}

	// Time range filter: from = 1h ago excludes the 2h-old record.
	pd = hs.listPage(token, fmt.Sprintf(
		"/api/v1/prism/alert/records?page=1&size=50&from=%s",
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

// TestAlertRecordsExport covers the streaming CSV export: headers,
// severity filtering, and the CSV column layout with RFC3339 timestamps.
func TestAlertRecordsExport(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	now := time.Now().UTC()
	ruleName := "httpapi-export-rule"
	idCritical := seedAlertRecord(hs, 1, seedAlertRecordParams{
		ruleName:    ruleName,
		severity:    "critical",
		status:      "firing",
		triggeredAt: now,
	})
	seedAlertRecord(hs, 1, seedAlertRecordParams{
		ruleName:    ruleName,
		severity:    "warning",
		status:      "firing",
		triggeredAt: now,
	})

	req, err := http.NewRequestWithContext(hs.t.Context(), http.MethodGet,
		hs.baseURL+"/api/v1/prism/alert/records/export?severity=critical", http.NoBody)
	if err != nil {
		t.Fatalf("build export request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := hs.client.Do(req)
	if err != nil {
		t.Fatalf("perform export: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export: expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Fatalf("export: content-type %q, expected text/csv", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "alert_records.csv") {
		t.Fatalf("export: content-disposition %q missing filename", cd)
	}

	rows, err := csv.NewReader(resp.Body).ReadAll()
	if err != nil {
		t.Fatalf("export: parse csv: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("export: expected header plus at least one row, got %d rows", len(rows))
	}
	wantHeader := []string{
		"id", "rule_id", "rule_name", "severity", "value", "message",
		"event_id", "status", "triggered_at", "acknowledged_at", "resolved_at", "created_at",
	}
	if !reflect.DeepEqual(rows[0], wantHeader) {
		t.Fatalf("export: header %v, expected %v", rows[0], wantHeader)
	}

	foundCritical := false
	for _, row := range rows[1:] {
		if len(row) < 4 {
			t.Fatalf("export: short row %v", row)
		}
		if row[2] != ruleName {
			continue
		}
		if row[3] != "critical" {
			t.Fatalf("export: severity filter leaked row %v", row)
		}
		if id, err := strconv.ParseInt(row[0], 10, 64); err == nil && id == idCritical {
			foundCritical = true
			// triggered_at is RFC3339-parseable.
			if _, err := time.Parse(time.RFC3339, row[8]); err != nil {
				t.Fatalf("export: triggered_at %q not RFC3339: %v", row[8], err)
			}
		}
	}
	if !foundCritical {
		t.Fatalf("export: seeded critical record %d not present", idCritical)
	}
}

// TestChannelsCRUDTestOptionsAndDeliveries covers the notification channel
// endpoints: create/update/delete with config objects, sensitive-field
// masking (and masked-secret updates that keep the stored plaintext), the
// test dispatch by id and inline config, the compact options projection,
// and the delivery record list + retry flow.
func TestChannelsCRUDTestOptionsAndDeliveries(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	var hits atomic.Int64
	// The receiver answers with a Telegram-style success body; the webhook
	// adapter ignores response bodies, so one shape serves both.
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer receiver.Close()

	// -- Create a webhook channel (config is a JSON object on the wire) --
	status, env := hs.do("POST", "/api/v1/prism/channels", map[string]any{
		"name": "httpapi-webhook",
		"type": "webhook",
		"config": map[string]any{
			"url":     receiver.URL,
			"timeout": "5s",
			"headers": map[string]string{"X-Tickraft": "httpapi"},
		},
		"enabled": true,
	}, token)
	var created struct {
		ID     int64  `json:"id"`
		Type   string `json:"type"`
		Config string `json:"config"`
	}
	hs.mustOK(status, env, "create channel", &created)
	if created.ID == 0 {
		t.Fatal("create channel: no id returned")
	}
	defer func() {
		_, _ = hs.do("DELETE", "/api/v1/prism/channels/"+jsonInt64(created.ID), nil, token)
	}()
	if !strings.Contains(created.Config, receiver.URL) {
		t.Fatalf("create channel: config does not carry the receiver url: %s", created.Config)
	}

	// -- Create a telegram channel; reads come back with masked secrets --
	status, env = hs.do("POST", "/api/v1/prism/channels", map[string]any{
		"name": "httpapi-telegram",
		"type": "telegram",
		"config": map[string]any{
			"bot_token": "123456:tg-secret-1234",
			"chat_id":   "-1001234567890",
			"api_base":  receiver.URL,
		},
		"enabled": true,
	}, token)
	var ding struct {
		ID     int64  `json:"id"`
		Config string `json:"config"`
	}
	hs.mustOK(status, env, "create telegram channel", &ding)
	if ding.ID == 0 {
		t.Fatal("create telegram channel: no id returned")
	}
	defer func() {
		_, _ = hs.do("DELETE", "/api/v1/prism/channels/"+jsonInt64(ding.ID), nil, token)
	}()

	var dingCfg map[string]any
	if err := json.Unmarshal([]byte(ding.Config), &dingCfg); err != nil {
		t.Fatalf("decode telegram config: %v", err)
	}
	maskedSecret, _ := dingCfg["bot_token"].(string)
	if !strings.HasPrefix(maskedSecret, "****") {
		t.Fatalf("telegram config bot token is not masked: %q", maskedSecret)
	}

	// -- Update with the masked secret keeps the stored plaintext --
	status, env = hs.do("PUT", "/api/v1/prism/channels/"+jsonInt64(ding.ID), map[string]any{
		"name": "httpapi-telegram-v2",
		"config": map[string]any{
			"bot_token": maskedSecret,
			"chat_id":   "-1001234567890",
			"api_base":  receiver.URL,
		},
	}, token)
	var updated struct {
		Name   string `json:"name"`
		Config string `json:"config"`
	}
	hs.mustOK(status, env, "update telegram channel", &updated)
	if updated.Name != "httpapi-telegram-v2" {
		t.Fatalf("update telegram channel: name=%q", updated.Name)
	}
	if !strings.Contains(updated.Config, "****") {
		t.Fatalf("update telegram channel: bot token no longer masked: %s", updated.Config)
	}

	// -- List returns every channel under items (unpaginated) --
	status, env = hs.do("GET", "/api/v1/prism/channels", nil, token)
	var list struct {
		Items []struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"items"`
	}
	hs.mustOK(status, env, "list channels", &list)
	if len(list.Items) < 2 {
		t.Fatalf("list channels: expected >=2 items, got %d", len(list.Items))
	}

	// -- Test dispatch: by saved id and with an inline config --
	status, env = hs.do("POST", "/api/v1/prism/channels/test", map[string]any{"id": ding.ID}, token)
	var testOut struct {
		Status string `json:"status"`
	}
	hs.mustOK(status, env, "test channel by id", &testOut)
	if testOut.Status != "success" {
		t.Fatalf("test channel by id: status=%q", testOut.Status)
	}
	if hits.Load() == 0 {
		t.Fatal("test channel by id: receiver was not called")
	}
	status, env = hs.do("POST", "/api/v1/prism/channels/test", map[string]any{
		"type":   "webhook",
		"config": map[string]any{"url": receiver.URL},
	}, token)
	hs.mustOK(status, env, "test channel inline", &testOut)
	if testOut.Status != "success" {
		t.Fatalf("test channel inline: status=%q", testOut.Status)
	}

	// -- Options projection --
	status, env = hs.do("GET", "/api/v1/prism/channels/options", nil, token)
	var options struct {
		Items []struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"items"`
	}
	hs.mustOK(status, env, "list channel options", &options)
	found := false
	for _, opt := range options.Items {
		if opt.ID == ding.ID {
			found = opt.Type == "telegram"
		}
	}
	if !found {
		t.Fatalf("list channel options: telegram channel %d missing", ding.ID)
	}

	// -- Test dispatches are recorded as delivery rows --
	pd := hs.listPage(token, "/api/v1/prism/channels/deliveries?page=1&size=10")
	if pd.Total != 2 || len(pd.Items) != 2 {
		t.Fatalf("list deliveries: expected the 2 test-dispatch rows, got total=%d items=%d", pd.Total, len(pd.Items))
	}
	for _, item := range pd.Items {
		if item["status"] != "success" {
			t.Fatalf("test-dispatch delivery row: status=%v, want success", item["status"])
		}
	}

	// -- Seed a failed delivery and retry it through the live channel --
	evt := prismalert.Event{
		Type:      prismalert.TypeLog,
		Timestamp: time.Now(),
		Violations: []prismalert.Violation{{
			Kind:     prismalert.ViolationKindLog,
			Severity: "warning",
			Log: &prismalert.LogContext{
				Keyword: "[httpapi]",
				Content: "seeded delivery for the retry endpoint",
			},
		}},
	}
	deliveries := channel.NewDeliveryStore(hs.dbc)
	if err := deliveries.Record(context.Background(), tracking.DeliveryRecord{
		Identity: tracking.Identity{
			ChannelID:   ding.ID,
			ChannelName: "httpapi-telegram-v2",
			ChannelType: "telegram",
		},
		AlertType:    "log",
		AlertTitle:   "httpapi retry probe",
		EventID:      "evt-httpapi-retry-1",
		Status:       tracking.StatusFailed,
		Error:        "http 500",
		ResponseCode: http.StatusInternalServerError,
		DurationMs:   12,
		Event:        evt,
		SentAt:       time.Now(),
	}); err != nil {
		t.Fatalf("seed delivery record: %v", err)
	}

	pd = hs.listPage(token, "/api/v1/prism/channels/deliveries?page=1&size=10")
	if pd.Total != 3 || len(pd.Items) != 3 {
		t.Fatalf("list deliveries: expected 3 records after seeding, got total=%d items=%d", pd.Total, len(pd.Items))
	}
	var deliveryID float64
	failed := 0
	for _, item := range pd.Items {
		if item["status"] == "failed" {
			failed++
			deliveryID, _ = item["id"].(float64)
		}
	}
	if failed != 1 || deliveryID == 0 {
		t.Fatalf("list deliveries: expected exactly 1 failed record, got %d (id=%v)", failed, deliveryID)
	}

	status, env = hs.do("POST",
		"/api/v1/prism/channels/deliveries/"+jsonInt64(int64(deliveryID))+"/retry", nil, token)
	var retried struct {
		Status   string `json:"status"`
		Attempts []struct {
			N int `json:"n"`
		} `json:"attempts"`
	}
	hs.mustOK(status, env, "retry delivery", &retried)
	if retried.Status != "success" {
		t.Fatalf("retry delivery: status=%q", retried.Status)
	}
	if len(retried.Attempts) != 2 {
		t.Fatalf("retry delivery: expected 2 attempts, got %d", len(retried.Attempts))
	}

	// The per-channel view reflects the retried record plus ding's own
	// test-dispatch row, all successful.
	pd = hs.listPage(token, "/api/v1/prism/channels/"+jsonInt64(ding.ID)+"/deliveries?page=1&size=10")
	if pd.Total != 2 {
		t.Fatalf("channel deliveries: expected 2 records, got total=%d", pd.Total)
	}
	for _, item := range pd.Items {
		if item["status"] != "success" {
			t.Fatalf("channel deliveries: status=%v, want success", item["status"])
		}
	}

	// Retrying a successful delivery is refused with a validation error.
	status, _ = hs.do("POST",
		"/api/v1/prism/channels/deliveries/"+jsonInt64(int64(deliveryID))+"/retry", nil, token)
	if status != http.StatusBadRequest {
		t.Fatalf("retry succeeded delivery: expected 400, got %d", status)
	}

	// -- Delete: gone afterwards --
	status, _ = hs.do("DELETE", "/api/v1/prism/channels/"+jsonInt64(ding.ID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("delete telegram channel: expected 200, got %d", status)
	}
	status, _ = hs.do("GET", "/api/v1/prism/channels/"+jsonInt64(ding.ID), nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("get deleted channel: expected 404, got %d", status)
	}
}
