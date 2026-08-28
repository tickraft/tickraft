// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package event

import "testing"

func TestEventTypeConstants(t *testing.T) {
	tests := []struct {
		name string
		val  Type
		want string
	}{
		// task domain
		{"TypeTaskCreated", TypeTaskCreated, "task.created"},
		{"TypeTaskUpdated", TypeTaskUpdated, "task.updated"},
		{"TypeTaskDeleted", TypeTaskDeleted, "task.deleted"},
		{"TypeTaskPaused", TypeTaskPaused, "task.paused"},
		{"TypeTaskResumed", TypeTaskResumed, "task.resumed"},
		{"TypeTaskScheduled", TypeTaskScheduled, "task.scheduled"},
		{"TypeTaskRetryScheduled", TypeTaskRetryScheduled, "task.retry_scheduled"},
		// execution domain
		{"TypeExecutionTriggered", TypeExecutionTriggered, "execution.triggered"},
		{"TypeExecutionStarted", TypeExecutionStarted, "execution.started"},
		{"TypeExecutionCompleted", TypeExecutionCompleted, "execution.completed"},
		{"TypeExecutionProgressed", TypeExecutionProgressed, "execution.progressed"},
		// asset domain
		{"TypeAssetStatusChanged", TypeAssetStatusChanged, "asset.status_changed"},
		{"TypeAssetFaultDetected", TypeAssetFaultDetected, "asset.fault_detected"},
		// telemetry domain
		{"TypeTelemetryMetricExceeded", TypeTelemetryMetricExceeded, "telemetry.metric_exceeded"},
		{"TypeTelemetryLogMatched", TypeTelemetryLogMatched, "telemetry.log_matched"},
		// alert domain
		{"TypeAlertTriggered", TypeAlertTriggered, "alert.triggered"},
		{"TypeAlertAcknowledged", TypeAlertAcknowledged, "alert.acknowledged"},
		{"TypeAlertResolved", TypeAlertResolved, "alert.resolved"},
		{"TypeAlertSuppressed", TypeAlertSuppressed, "alert.suppressed"},
		{"TypeAlertNotified", TypeAlertNotified, "alert.notified"},
		// remediation domain
		{"TypeRemediationTriggered", TypeRemediationTriggered, "remediation.triggered"},
		{"TypeRemediationStarted", TypeRemediationStarted, "remediation.started"},
		{"TypeRemediationCompleted", TypeRemediationCompleted, "remediation.completed"},
		{"TypeRemediationSkipped", TypeRemediationSkipped, "remediation.skipped"},
		// system domain
		{"TypeSystemConfigChanged", TypeSystemConfigChanged, "system.config_changed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.val) != tt.want {
				t.Errorf("got %q, want %q", tt.val, tt.want)
			}
		})
	}
}

func TestEventTypeCount(t *testing.T) {
	// Ensure exactly 25 event type constants are defined.
	allTypes := []Type{
		TypeTaskCreated, TypeTaskUpdated, TypeTaskDeleted, TypeTaskPaused,
		TypeTaskResumed, TypeTaskScheduled, TypeTaskRetryScheduled,
		TypeExecutionTriggered, TypeExecutionStarted, TypeExecutionCompleted,
		TypeExecutionProgressed,
		TypeAssetStatusChanged, TypeAssetFaultDetected,
		TypeTelemetryMetricExceeded, TypeTelemetryLogMatched,
		TypeAlertTriggered, TypeAlertAcknowledged, TypeAlertResolved,
		TypeAlertSuppressed, TypeAlertNotified,
		TypeRemediationTriggered, TypeRemediationStarted,
		TypeRemediationCompleted, TypeRemediationSkipped,
		TypeSystemConfigChanged,
	}
	if len(allTypes) != 25 {
		t.Fatalf("expected 25 event types, got %d", len(allTypes))
	}

	// Ensure all values are unique.
	seen := make(map[string]bool)
	for _, typ := range allTypes {
		val := string(typ)
		if seen[val] {
			t.Fatalf("duplicate event type value: %s", val)
		}
		seen[val] = true
	}
}

func TestTypeStringer(t *testing.T) {
	var t1 Type = "task.created"
	if t1 != TypeTaskCreated {
		t.Errorf("Type string comparison failed: got %q, want %q", t1, TypeTaskCreated)
	}
}
