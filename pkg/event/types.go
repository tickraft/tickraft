// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package event provides in-process event bus capabilities, implementing
// type-safe publish/subsccribe based on Go generics.
package event

// Type identifies an event type in the system.
// All event types follow the {domain}.{action_past_tense} naming convention:
// the domain aligns with system modules and the action uses the past tense to express an accomplished fact.
type Type string

// The constants below define the event types grouped into 7
// domains, serving as the authoritative contract directory for cross-module
// communication.
//
// callers publish additional event types (e.g. anomaly detection,
// storm governance, probe health, license lifecycle). Those extended types
// are defined in the callers's own package and registered on the
// same event bus; they are intentionally NOT declared here so the
// kernel stays free of optional concerns.

// task domain: task lifecycle events, published by Service.
const (
	// TypeTaskCreated: task definition created.
	TypeTaskCreated Type = "task.created"
	// TypeTaskUpdated: task definition updated.
	TypeTaskUpdated Type = "task.updated"
	// TypeTaskDeleted: task definition deleted.
	TypeTaskDeleted Type = "task.deleted"
	// TypeTaskPaused: task paused.
	TypeTaskPaused Type = "task.paused"
	// TypeTaskResumed: task resumed.
	TypeTaskResumed Type = "task.resumed"
	// TypeTaskScheduled: task is scheduled and ready, triggered on due.
	TypeTaskScheduled Type = "task.scheduled"
	// TypeTaskRetryScheduled: retry scheduling after a task failure.
	TypeTaskRetryScheduled Type = "task.retry_scheduled"
)

// execution domain: execution lifecycle events, published by Service / Runner.
const (
	// TypeExecutionTriggered: execution has been dispatched to the executor.
	TypeExecutionTriggered Type = "execution.triggered"
	// TypeExecutionStarted: the executor has started execution.
	TypeExecutionStarted Type = "execution.started"
	// TypeExecutionCompleted: execution finished (success/failure/timeout).
	TypeExecutionCompleted Type = "execution.completed"
	// TypeExecutionProgressed: progress update for a long-running task.
	TypeExecutionProgressed Type = "execution.progressed"
)

// asset domain: asset state events, published by Telemetry Emitter.
const (
	// TypeAssetStatusChanged: asset state transition (healthy -> warning -> critical -> offline).
	TypeAssetStatusChanged Type = "asset.status_changed"
	// TypeAssetFaultDetected: external fault event detected (MQTT/SNMP/Syslog/Webhook).
	TypeAssetFaultDetected Type = "asset.fault_detected"
)

// telemetry domain: observation result events, published by Telemetry Emitter.
const (
	// TypeTelemetryMetricExceeded: metric threshold breached, triggering alert evaluation.
	TypeTelemetryMetricExceeded Type = "telemetry.metric_exceeded"
	// TypeTelemetryLogMatched: log keyword matched, triggering alert evaluation.
	TypeTelemetryLogMatched Type = "telemetry.log_matched"
)

// alert domain: alert lifecycle events, published by Alerter.
const (
	// TypeAlertTriggered: alert rule matched, alert record created.
	TypeAlertTriggered Type = "alert.triggered"
	// TypeAlertAcknowledged: alert acknowledged by a user.
	TypeAlertAcknowledged Type = "alert.acknowledged"
	// TypeAlertResolved: alert resolved (automatic/manual).
	TypeAlertResolved Type = "alert.resolved"
	// TypeAlertSuppressed: alert suppressed (dependency suppression / storm aggregation).
	TypeAlertSuppressed Type = "alert.suppressed"
	// TypeAlertNotified: alert notification sent to a channel.
	TypeAlertNotified Type = "alert.notified"
)

// remediation domain: remediation lifecycle events, published by RemediationManager.
const (
	// TypeRemediationTriggered: remediation rule matched successfully, remediation triggered.
	TypeRemediationTriggered Type = "remediation.triggered"
	// TypeRemediationStarted: remediation executor started execution.
	TypeRemediationStarted Type = "remediation.started"
	// TypeRemediationCompleted: remediation execution finished (success/failure/timeout).
	TypeRemediationCompleted Type = "remediation.completed"
	// TypeRemediationSkipped: remediation skipped (idempotency / cooldown / circuit breaker / condition mismatch).
	TypeRemediationSkipped Type = "remediation.skipped"
)

// system domain: system-level events.
const (
	// TypeSystemConfigChanged: system configuration changed.
	TypeSystemConfigChanged Type = "system.config_changed"
)
