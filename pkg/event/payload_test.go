// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package event

import (
	"encoding/json"
	"testing"
)

func TestTaskPayloadJSON(t *testing.T) {
	p := TaskPayload{
		TaskID:        "task-001",
		TaskName:      "test-task",
		TaskType:      "task",
		ExecutorType:  "http",
		TenantID:      "tenant-001",
		Action:        "created",
		TriggerType:   "cron",
		RetryCount:    2,
		MaxRetries:    3,
		NextRunAt:     1700000000,
		TriggerReason: "schedule",
		ChangedBy:     "user-001",
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got TaskPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.TaskID != p.TaskID {
		t.Errorf("task_id: got %q, want %q", got.TaskID, p.TaskID)
	}
	if got.RetryCount != p.RetryCount {
		t.Errorf("retry_count: got %d, want %d", got.RetryCount, p.RetryCount)
	}
	if got.NextRunAt != p.NextRunAt {
		t.Errorf("next_run_at: got %d, want %d", got.NextRunAt, p.NextRunAt)
	}
}

func TestTaskPayloadOmitEmpty(t *testing.T) {
	p := TaskPayload{
		TaskID:   "task-001",
		TaskName: "test",
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	omitted := []string{"trigger_type", "retry_count", "max_retries", "next_run_at", "trigger_reason", "changed_by"}
	for _, key := range omitted {
		if _, ok := raw[key]; ok {
			t.Errorf("%s should be omitted when zero", key)
		}
	}
}

func TestExecutionPayloadJSON(t *testing.T) {
	p := ExecutionPayload{
		TaskID:       "task-001",
		ExecutionID:  "exec-001",
		ExecutorType: "http",
		TenantID:     "tenant-001",
		Operation:    "probe",
		Action:       "triggered",
		Status:       "success",
		TriggerType:  "schedule",
		Result:       "ok",
		Output:       "200 OK",
		Error:        "",
		Duration:     1500000000,
		Progress:     1.0,
		RetryCount:   0,
		StartedAt:    1700000000,
		CompletedAt:  1700000001,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ExecutionPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ExecutionID != p.ExecutionID {
		t.Errorf("execution_id: got %q, want %q", got.ExecutionID, p.ExecutionID)
	}
	if got.Operation != p.Operation {
		t.Errorf("operation: got %q, want %q", got.Operation, p.Operation)
	}
	if got.Duration != p.Duration {
		t.Errorf("duration: got %d, want %d", got.Duration, p.Duration)
	}
	if got.Progress != p.Progress {
		t.Errorf("progress: got %f, want %f", got.Progress, p.Progress)
	}
}

func TestExecutionPayloadOmitEmpty(t *testing.T) {
	p := ExecutionPayload{
		TaskID:       "task-001",
		ExecutionID:  "exec-001",
		ExecutorType: "http",
		TenantID:     "tenant-001",
		Action:       "triggered",
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	omitted := []string{"operation", "status", "trigger_type", "result", "output", "error", "duration",
		"progress", "retry_count", "started_at", "completed_at"}
	for _, key := range omitted {
		if _, ok := raw[key]; ok {
			t.Errorf("%s should be omitted when zero", key)
		}
	}
}

func TestStatusChangePayloadJSON(t *testing.T) {
	p := StatusChangePayload{
		AssetID:     "asset-001",
		AssetName:   "server-01",
		AssetType:   "host",
		TenantID:    "tenant-001",
		PrevStatus:  "healthy",
		CurrStatus:  "warning",
		Reason:      "probe_failure",
		Source:      "prober",
		MonitorID:   "mon-001",
		MetricName:  "cpu_usage",
		MetricValue: 95.5,
		DetectedAt:  1700000000,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got StatusChangePayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.AssetID != p.AssetID {
		t.Errorf("asset_id: got %q, want %q", got.AssetID, p.AssetID)
	}
	if got.MetricValue != p.MetricValue {
		t.Errorf("metric_value: got %f, want %f", got.MetricValue, p.MetricValue)
	}
}

func TestStatusChangePayloadOmitEmpty(t *testing.T) {
	p := StatusChangePayload{
		AssetID:    "asset-001",
		AssetName:  "server",
		AssetType:  "host",
		TenantID:   "tenant-001",
		PrevStatus: "healthy",
		CurrStatus: "warning",
		DetectedAt: 1700000000,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	omitted := []string{"reason", "source", "monitor_id", "metric_name", "metric_value"}
	for _, key := range omitted {
		if _, ok := raw[key]; ok {
			t.Errorf("%s should be omitted when zero", key)
		}
	}
}

func TestFaultDetectedPayloadJSON(t *testing.T) {
	p := FaultDetectedPayload{
		AssetID:    "asset-001",
		AssetName:  "device-01",
		AssetType:  "device",
		TenantID:   "tenant-001",
		FaultType:  "hardware",
		Severity:   "critical",
		Source:     "mqtt",
		ClientID:   "client-001",
		FaultCode:  "ERR_001",
		Message:    "device offline",
		Context:    map[string]any{"temperature": 90},
		DetectedAt: 1700000000,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got FaultDetectedPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.FaultType != p.FaultType {
		t.Errorf("fault_type: got %q, want %q", got.FaultType, p.FaultType)
	}
	if got.Context["temperature"].(float64) != 90 {
		t.Errorf("context.temperature: got %v, want 90", got.Context["temperature"])
	}
}

func TestFaultDetectedPayloadOmitEmpty(t *testing.T) {
	p := FaultDetectedPayload{
		AssetID:    "asset-001",
		AssetName:  "device",
		AssetType:  "device",
		TenantID:   "tenant-001",
		FaultType:  "hardware",
		Severity:   "critical",
		Source:     "mqtt",
		Message:    "offline",
		DetectedAt: 1700000000,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	omitted := []string{"client_id", "fault_code", "context"}
	for _, key := range omitted {
		if _, ok := raw[key]; ok {
			t.Errorf("%s should be omitted when zero", key)
		}
	}
}

func TestMetricExceededPayloadJSON(t *testing.T) {
	p := MetricExceededPayload{
		AssetID:     "asset-001",
		AssetName:   "server-01",
		AssetType:   "host",
		TenantID:    "tenant-001",
		MonitorID:   "mon-001",
		MetricName:  "cpu_usage",
		MetricValue: 95.5,
		Threshold:   90.0,
		Operator:    "gt",
		Severity:    "warning",
		Window:      "5m",
		DetectedAt:  1700000000,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got MetricExceededPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.MetricValue != p.MetricValue {
		t.Errorf("metric_value: got %f, want %f", got.MetricValue, p.MetricValue)
	}
	if got.Window != p.Window {
		t.Errorf("window: got %q, want %q", got.Window, p.Window)
	}
}

func TestMetricExceededPayloadOmitEmpty(t *testing.T) {
	p := MetricExceededPayload{
		AssetID:     "asset-001",
		AssetName:   "server",
		AssetType:   "host",
		TenantID:    "tenant-001",
		MonitorID:   "mon-001",
		MetricName:  "cpu",
		MetricValue: 95.0,
		Threshold:   90.0,
		Operator:    "gt",
		Severity:    "warning",
		DetectedAt:  1700000000,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["window"]; ok {
		t.Error("window should be omitted when empty")
	}
}

func TestLogMatchedPayloadJSON(t *testing.T) {
	p := LogMatchedPayload{
		AssetID:    "asset-001",
		AssetName:  "server-01",
		AssetType:  "host",
		TenantID:   "tenant-001",
		MonitorID:  "mon-001",
		Level:      "ERROR",
		Keyword:    "OutOfMemory",
		Content:    "Java heap space",
		Severity:   "critical",
		DetectedAt: 1700000000,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got LogMatchedPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Keyword != p.Keyword {
		t.Errorf("keyword: got %q, want %q", got.Keyword, p.Keyword)
	}
}

func TestAlertPayloadJSON(t *testing.T) {
	p := AlertPayload{
		AlertID:       "alert-001",
		RuleID:        "rule-001",
		RuleName:      "cpu-high",
		AssetID:       "asset-001",
		AssetName:     "server-01",
		TenantID:      "tenant-001",
		AlertType:     "metric",
		Severity:      "warning",
		SourceEventID: "evt-001",
		Title:         "CPU usage high",
		Message:       "CPU usage exceeded 90%",
		Value:         95.5,
		Threshold:     90.0,
		Context:       map[string]any{"host": "server-01"},
		TriggeredAt:   1700000000,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got AlertPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.AlertID != p.AlertID {
		t.Errorf("alert_id: got %q, want %q", got.AlertID, p.AlertID)
	}
	if got.Value != p.Value {
		t.Errorf("value: got %f, want %f", got.Value, p.Value)
	}
}

func TestAlertPayloadOmitEmpty(t *testing.T) {
	p := AlertPayload{
		AlertID:     "alert-001",
		RuleID:      "rule-001",
		RuleName:    "cpu-high",
		AssetID:     "asset-001",
		AssetName:   "server",
		TenantID:    "tenant-001",
		AlertType:   "metric",
		Severity:    "warning",
		Title:       "title",
		Message:     "msg",
		TriggeredAt: 1700000000,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	omitted := []string{"source_event_id", "value", "threshold", "context"}
	for _, key := range omitted {
		if _, ok := raw[key]; ok {
			t.Errorf("%s should be omitted when zero", key)
		}
	}
}

func TestAlertLifecyclePayloadJSON(t *testing.T) {
	p := AlertLifecyclePayload{
		AlertID:      "alert-001",
		TenantID:     "tenant-001",
		Action:       "acknowledged",
		Channel:      "email",
		UserID:       "user-001",
		Reason:       "manual",
		SuppressType: "dependency",
		NotifiedAt:   1700000000,
		Timestamp:    1700000001,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got AlertLifecyclePayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Action != p.Action {
		t.Errorf("action: got %q, want %q", got.Action, p.Action)
	}
}

func TestAlertLifecyclePayloadOmitEmpty(t *testing.T) {
	p := AlertLifecyclePayload{
		AlertID:   "alert-001",
		TenantID:  "tenant-001",
		Action:    "resolved",
		Timestamp: 1700000000,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	omitted := []string{"channel", "user_id", "reason", "suppress_type", "notified_at"}
	for _, key := range omitted {
		if _, ok := raw[key]; ok {
			t.Errorf("%s should be omitted when zero", key)
		}
	}
}

func TestRemediationPayloadJSON(t *testing.T) {
	p := RemediationPayload{
		RemediationID: "rem-001",
		RuleID:        "rule-001",
		RuleName:      "auto-restart",
		TaskID:        "task-001",
		AssetID:       "asset-001",
		TenantID:      "tenant-001",
		Action:        "triggered",
		TriggerType:   "status_change",
		SourceEventID: "evt-001",
		Status:        "success",
		Error:         "",
		SkipReason:    "",
		ExecutorType:  "local",
		Duration:      500000000,
		RetryCount:    1,
		StartedAt:     1700000000,
		CompletedAt:   1700000001,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got RemediationPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.RemediationID != p.RemediationID {
		t.Errorf("remediation_id: got %q, want %q", got.RemediationID, p.RemediationID)
	}
	if got.Duration != p.Duration {
		t.Errorf("duration: got %d, want %d", got.Duration, p.Duration)
	}
}

func TestRemediationPayloadOmitEmpty(t *testing.T) {
	p := RemediationPayload{
		RemediationID: "rem-001",
		RuleID:        "rule-001",
		RuleName:      "rule",
		TaskID:        "task-001",
		AssetID:       "asset-001",
		TenantID:      "tenant-001",
		Action:        "triggered",
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	omitted := []string{"trigger_type", "source_event_id", "status", "error", "skip_reason",
		"executor_type", "duration", "retry_count", "started_at", "completed_at"}
	for _, key := range omitted {
		if _, ok := raw[key]; ok {
			t.Errorf("%s should be omitted when zero", key)
		}
	}
}

func TestSystemConfigPayloadJSON(t *testing.T) {
	p := SystemConfigPayload{
		Scope:     "global",
		TenantID:  "tenant-001",
		ConfigKey: "max_connections",
		OldValue:  "100",
		NewValue:  "200",
		ChangedBy: "admin",
		ChangedAt: 1700000001,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got SystemConfigPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Scope != p.Scope {
		t.Errorf("scope: got %q, want %q", got.Scope, p.Scope)
	}
	if got.NewValue != p.NewValue {
		t.Errorf("new_value: got %q, want %q", got.NewValue, p.NewValue)
	}
}

func TestSystemConfigPayloadOmitEmpty(t *testing.T) {
	p := SystemConfigPayload{
		Scope:     "global",
		ChangedAt: 1700000000,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	omitted := []string{"tenant_id", "config_key", "old_value", "new_value", "changed_by"}
	for _, key := range omitted {
		if _, ok := raw[key]; ok {
			t.Errorf("%s should be omitted when zero", key)
		}
	}
}
