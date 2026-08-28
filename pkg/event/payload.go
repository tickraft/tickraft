// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package event

// Payload naming conventions:
//
// Every bus payload type carries the "Payload" suffix: payloads are the
// event-carried representations of domain data and routinely meet their
// same-named domain models at publish/bridge sites (telemetry.TaskReport →
// event.TaskReportPayload), so the suffix is what keeps the two apart.
// Do not rename payloads to bare nouns or to XxxEvent (the package already
// exposes the generic Event[T]).
//
// Two sub-conventions select the prefix:
//   - Entity payloads ({Entity}Payload): one struct serves all lifecycle
//     events of an entity; the Action field discriminates the specific
//     event type. Use when several event types share the entity's data
//     shape (TaskPayload, ExecutionPayload, AlertLifecyclePayload,
//     RemediationPayload).
//   - Fact payloads ({Fact}Payload): one struct for a single occurrence,
//     named after the past-tense action of its Type constant
//     (StatusChangePayload for asset.status_changed, FaultDetectedPayload
//     for asset.fault_detected).

// TaskPayload is the payload for task lifecycle events, applicable to all task.* event types.
// Published by Service when a task definition is created, updated, deleted, paused,
// resumed, scheduled, or retry-scheduled.
type TaskPayload struct {
	// TaskID is the unique identifier of the task definition.
	TaskID string `json:"task_id"`
	// TaskName is the human-readable name of the task.
	TaskName string `json:"task_name"`
	// TaskType is the task type, matching the task definition's type field.
	TaskType string `json:"task_type"`
	// ExecutorType is the executor type (e.g. http, tcp, icmp, local, webhook).
	ExecutorType string `json:"executor_type"`
	// TenantID is the tenant identifier the task belongs to.
	// The runtime is single-tenant: this field is always "".
	// callers may inject the actual tenant ID via the event bus.
	TenantID string `json:"tenant_id"`
	// Action is the lifecycle action: created, updated, deleted, paused, resumed, scheduled, retry_scheduled.
	Action string `json:"action"`
	// TriggerType is the trigger type (e.g. cron, interval, manual, event). Populated for scheduled/retry_scheduled.
	TriggerType string `json:"trigger_type,omitempty"`
	// RetryCount is the current retry attempt number. Populated for retry_scheduled.
	RetryCount int `json:"retry_count,omitempty"`
	// MaxRetries is the maximum number of retries configured for the task. Populated for retry_scheduled.
	MaxRetries int `json:"max_retries,omitempty"`
	// NextRunAt is the next scheduled execution time as Unix nanoseconds. Populated for scheduled/retry_scheduled.
	NextRunAt int64 `json:"next_run_at,omitempty"`
	// TriggerReason is a human-readable reason for the trigger
	// (e.g. "cron schedule", "manual trigger", "retry after failure").
	TriggerReason string `json:"trigger_reason,omitempty"`
	// ChangedBy is the user or system component that initiated the change.
	// Populated for created/updated/deleted/paused/resumed.
	ChangedBy string `json:"changed_by,omitempty"`
}

// ExecutionPayload is the payload for execution lifecycle events, applicable to all execution.* event types.
// Published by Service (triggered) and Runner (started/completed/progressed).
type ExecutionPayload struct {
	// TaskID is the ID of the task definition this execution belongs to.
	TaskID string `json:"task_id"`
	// ExecutionID is the unique identifier of this execution instance.
	ExecutionID string `json:"execution_id"`
	// ExecutorType is the executor type (e.g. http, tcp, icmp, local, webhook).
	ExecutorType string `json:"executor_type"`
	// TenantID is the tenant identifier.
	// The runtime is single-tenant: this field is always "".
	// callers may inject the actual tenant ID via the event bus.
	TenantID string `json:"tenant_id"`
	// AssetID is the target asset ID. Populated when the executor targets a specific asset.
	AssetID string `json:"asset_id,omitempty"`
	// Operation is the operation kind: "probe" (read-only probing) or
	// "execute" (write action). Required on triggered events; the runner
	// drops payloads that do not carry a valid operation.
	Operation string `json:"operation,omitempty"`
	// Action is the lifecycle action: triggered, started, completed, progressed.
	Action string `json:"action"`
	// Status is the execution status: success, failure, timeout, running. Populated for completed.
	Status string `json:"status,omitempty"`
	// TriggerType is the trigger type: schedule, manual, event, retry.
	TriggerType string `json:"trigger_type,omitempty"`
	// Config is the serialized executor configuration (JSON string). Populated for triggered.
	Config string `json:"config,omitempty"`
	// TimeoutSeconds is the execution timeout in seconds.
	TimeoutSeconds int64 `json:"timeout_seconds,omitempty"`
	// MaxRetries is the maximum number of retries configured for the task.
	MaxRetries int `json:"max_retries,omitempty"`
	// RetryIntervalSeconds is the configured delay between retries in seconds.
	RetryIntervalSeconds int64 `json:"retry_interval_seconds,omitempty"`
	// RunID is the run handle generated per trigger fire, before any
	// execution row exists. It reaches the remote executor as the task_ref
	// dispatch credential and is used for idempotency tracking and executor
	// naming.
	RunID string `json:"run_id,omitempty"`
	// ReportStatus marks a Mode A task (remote status reporting): the
	// executor outcome is only the dispatch result; the execution row stays
	// running until the remote reporter closes it via the telemetry report
	// endpoint (or the sweeper reaps it). Populated for triggered.
	ReportStatus bool `json:"report_status,omitempty"`
	// Result is the execution result summary. Populated for completed.
	Result string `json:"result,omitempty"`
	// Output is the raw execution output. Populated for completed.
	Output string `json:"output,omitempty"`
	// StatusCode is the HTTP status code returned by HTTP executors. Populated for completed.
	StatusCode int `json:"status_code,omitempty"`
	// Error is the error message when the execution fails. Populated for completed (failure/timeout).
	Error string `json:"error,omitempty"`
	// Duration is the execution duration in nanoseconds. Populated for completed.
	Duration int64 `json:"duration,omitempty"`
	// Progress is the execution progress from 0.0 to 1.0. Populated for progressed.
	Progress float64 `json:"progress,omitempty"`
	// RetryCount is the current retry attempt number for this execution.
	RetryCount int `json:"retry_count,omitempty"`
	// StartedAt is the execution start time as Unix nanoseconds. Populated for started/completed.
	StartedAt int64 `json:"started_at,omitempty"`
	// CompletedAt is the execution completion time as Unix nanoseconds. Populated for completed.
	CompletedAt int64 `json:"completed_at,omitempty"`
}

// TaskReportPayload is the payload for remote task status report events
// (TypeTaskStatusReported). The assembly layer converts a telemetry.TaskReport
// into this payload; the task domain consumes it to bind the reported state
// to the task's execution rows.
type TaskReportPayload struct {
	// Kind selects the report category: "task_status".
	Kind string `json:"kind"`
	// TaskRef identifies the reported execution: the dispatch credential
	// (the run handle) or the task number as a decimal string.
	TaskRef string `json:"task_ref"`
	// Status is the reported status: running/completed/failed/timeout.
	Status string `json:"status"`
	// StartedAt is the reported execution start as Unix nanoseconds. Zero
	// when the reporter omits it.
	StartedAt int64 `json:"started_at,omitempty"`
	// FinishedAt is the reported execution finish as Unix nanoseconds. Zero
	// while running.
	FinishedAt int64 `json:"finished_at,omitempty"`
	// Output is the reported execution output.
	Output string `json:"output,omitempty"`
	// Error is the reported execution error message.
	Error string `json:"error,omitempty"`
	// Reason describes the status transition.
	Reason string `json:"reason,omitempty"`
	// TenantID is the tenant resolved from the reporter's credential at
	// ingestion time.
	TenantID int64 `json:"tenant_id,omitempty"`
}

// StatusChangePayload is the payload for asset status change events.
// Published by Telemetry Emitter when an asset transitions between status levels
// (healthy -> warning -> critical -> offline -> unknown).
//
// Each event carries at most ONE metric context (MetricName + MetricValue). When
// multiple metrics contribute to a status change, each metric breach is published
// as a separate MetricExceededPayload event; this payload only records the overall
// asset status transition. The metric fields are optional and are populated only
// when the status change is directly attributable to a single metric.
type StatusChangePayload struct {
	// AssetID is the unique identifier of the asset.
	AssetID string `json:"asset_id"`
	// AssetName is the human-readable name of the asset.
	AssetName string `json:"asset_name"`
	// AssetType is the asset type (e.g. host, device, service, network).
	AssetType string `json:"asset_type"`
	// AssetKey is the tenant-unique identifier of the asset, used for deduplication.
	AssetKey string `json:"asset_key,omitempty"`
	// TenantID is the tenant identifier.
	// The runtime is single-tenant: this field is always "".
	// callers may inject the actual tenant ID via the event bus.
	TenantID string `json:"tenant_id"`
	// PrevStatus is the asset status before the transition: healthy, warning, critical, offline, unknown.
	PrevStatus string `json:"prev_status"`
	// CurrStatus is the asset status after the transition: healthy, warning, critical, offline, unknown.
	CurrStatus string `json:"curr_status"`
	// Reason is the human-readable reason for the transition (e.g. probe_failure, timeout, recovery, manual, fault).
	Reason string `json:"reason,omitempty"`
	// Source is the event source: prober, listener, timeout, manual.
	Source string `json:"source,omitempty"`
	// MonitorID is the monitor point ID associated with the status
	// change. Populated when the change is triggered by a specific
	// monitor point.
	//
	// Note: "Monitor" here refers to a monitor point (a configured
	// prober/listener target instance), not the collector module.
	// The standard term for the module is "collector" per the
	// terminology rules.
	MonitorID string `json:"monitor_id,omitempty"`
	// MetricName is the metric name that directly caused the status change.
	// Populated when the change is attributable to a single metric.
	MetricName string `json:"metric_name,omitempty"`
	// MetricValue is the metric value observed at the time of the status change. Populated together with MetricName.
	MetricValue float64 `json:"metric_value,omitempty"`
	// DetectedAt is the time the status change was detected, as Unix nanoseconds.
	DetectedAt int64 `json:"detected_at"`
}

// FaultDetectedPayload is the payload for asset fault detected events
// (TypeAssetFaultDetected), sourced from fault information reported by
// external systems. Published by Telemetry Emitter when a fault is detected
// via MQTT, SNMP, Syslog, or Webhook receivers.
type FaultDetectedPayload struct {
	// AssetID is the unique identifier of the asset.
	AssetID string `json:"asset_id"`
	// AssetName is the human-readable name of the asset.
	AssetName string `json:"asset_name"`
	// AssetType is the asset type (e.g. host, device, service, network).
	AssetType string `json:"asset_type"`
	// TenantID is the tenant identifier.
	// The runtime is single-tenant: this field is always "".
	// callers may inject the actual tenant ID via the event bus.
	TenantID string `json:"tenant_id"`
	// FaultType is the fault type: hardware, software, network, security, communication.
	FaultType string `json:"fault_type"`
	// Severity is the severity level: info, warning, critical.
	Severity string `json:"severity"`
	// Source is the fault source: mqtt, snmp, syslog, webhook, heartbeat.
	Source string `json:"source"`
	// ClientID is the device client identifier, used in MQTT scenarios. Populated for MQTT-sourced faults.
	ClientID string `json:"client_id,omitempty"`
	// FaultCode is the fault code reported by the external system.
	FaultCode string `json:"fault_code,omitempty"`
	// Message is the human-readable fault description.
	Message string `json:"message"`
	// Context is the extended context map, injected into condition_expr evaluation for rule matching.
	Context map[string]any `json:"context,omitempty"`
	// DetectedAt is the time the fault was detected, as Unix nanoseconds.
	DetectedAt int64 `json:"detected_at"`
}

// MetricExceededPayload is the payload for metric threshold breach events, published by Telemetry Emitter.
//
// The primary metric that triggered the breach is recorded in MetricName/MetricValue/Threshold.
// Additional context metrics observed at the same time are recorded in Resources as a
// name-to-value map, allowing subscribers to access the full metric snapshot without
// querying the time-series store. The primary metric is the one evaluated against the
// threshold; Resources metrics are informational only.
type MetricExceededPayload struct {
	// AssetID is the unique identifier of the asset.
	AssetID string `json:"asset_id"`
	// AssetName is the human-readable name of the asset.
	AssetName string `json:"asset_name"`
	// AssetType is the asset type (e.g. host, device, service, network).
	AssetType string `json:"asset_type"`
	// TenantID is the tenant identifier.
	// The runtime is single-tenant: this field is always "".
	// callers may inject the actual tenant ID via the event bus.
	TenantID string `json:"tenant_id"`
	// MonitorID is the monitor point ID that produced this metric.
	MonitorID string `json:"monitor_id"`
	// MetricName is the name of the primary metric that breached the threshold.
	MetricName string `json:"metric_name"`
	// MetricValue is the observed value of the primary metric.
	MetricValue float64 `json:"metric_value"`
	// Threshold is the threshold value the metric was compared against.
	Threshold float64 `json:"threshold"`
	// Operator is the comparison operator: gt, lt, gte, lte, eq.
	Operator string `json:"operator"`
	// Severity is the severity level: warning, critical.
	Severity string `json:"severity"`
	// Window is the aggregation window (e.g. 1m, 5m, 15m). Populated when the metric is evaluated over a time window.
	Window string `json:"window,omitempty"`
	// Resources is the additional metric snapshot at the time of the breach, as a name-to-value map.
	// These metrics are informational context only; the primary trigger metric is in MetricName/MetricValue.
	Resources map[string]float64 `json:"resources,omitempty"`
	// DetectedAt is the time the threshold breach was detected, as Unix nanoseconds.
	DetectedAt int64 `json:"detected_at"`
}

// LogMatchedPayload is the payload for log keyword match events, published by Telemetry Emitter.
type LogMatchedPayload struct {
	// AssetID is the unique identifier of the asset.
	AssetID string `json:"asset_id"`
	// AssetName is the human-readable name of the asset.
	AssetName string `json:"asset_name"`
	// AssetType is the asset type (e.g. host, device, service, network).
	AssetType string `json:"asset_type"`
	// TenantID is the tenant identifier.
	// The runtime is single-tenant: this field is always "".
	// callers may inject the actual tenant ID via the event bus.
	TenantID string `json:"tenant_id"`
	// MonitorID is the monitor point ID that produced this log.
	MonitorID string `json:"monitor_id"`
	// Level is the log level: debug, info, warn, error, fatal.
	Level string `json:"level"`
	// Keyword is the matched keyword that triggered the event.
	Keyword string `json:"keyword"`
	// Content is the raw log content that matched the keyword.
	Content string `json:"content"`
	// Severity is the severity level: warning, critical.
	Severity string `json:"severity"`
	// SourceIP is the source IP address extracted from the log. Populated when available.
	SourceIP string `json:"source_ip,omitempty"`
	// DetectedAt is the time the log match was detected, as Unix nanoseconds.
	DetectedAt int64 `json:"detected_at"`
}

// AlertPayload is the payload for alert triggered events, published by Alerter after a rule matches successfully.
type AlertPayload struct {
	// AlertID is the unique identifier of the generated alert record.
	AlertID string `json:"alert_id"`
	// RuleID is the alert rule ID that matched.
	RuleID string `json:"rule_id"`
	// RuleName is the human-readable name of the alert rule.
	RuleName string `json:"rule_name"`
	// AssetID is the ID of the asset associated with the alert.
	AssetID string `json:"asset_id"`
	// AssetName is the human-readable name of the asset.
	AssetName string `json:"asset_name"`
	// TenantID is the tenant identifier.
	// The runtime is single-tenant: this field is always "".
	// callers may inject the actual tenant ID via the event bus.
	TenantID string `json:"tenant_id"`
	// AlertType is the alert type: metric, log, fault, status.
	AlertType string `json:"alert_type"`
	// Severity is the severity level: info, warning, critical.
	Severity string `json:"severity"`
	// SourceEventID is the ID of the source event that triggered this alert. Populated for traceability.
	SourceEventID string `json:"source_event_id,omitempty"`
	// Title is the alert title, rendered from the alert template.
	Title string `json:"title"`
	// Message is the alert detail message, rendered from the alert template.
	Message string `json:"message"`
	// Value is the trigger value for metric alerts. Populated for alert_type=metric.
	Value float64 `json:"value,omitempty"`
	// Threshold is the threshold value for metric alerts. Populated for alert_type=metric.
	Threshold float64 `json:"threshold,omitempty"`
	// Context is the extended context map, carrying additional data for notification templates.
	Context map[string]any `json:"context,omitempty"`
	// TriggeredAt is the time the alert was triggered, as Unix nanoseconds.
	TriggeredAt int64 `json:"triggered_at"`
}

// AlertLifecyclePayload is the payload for alert lifecycle events,
// applicable to alert.acknowledged / alert.resolved / alert.suppressed / alert.notified.
//
// A single struct serves four lifecycle actions; action-specific fields use omitempty
// and are populated only for the relevant action. See field comments for applicability.
type AlertLifecyclePayload struct {
	// AlertID is the unique identifier of the alert record.
	AlertID string `json:"alert_id"`
	// TenantID is the tenant identifier.
	// The runtime is single-tenant: this field is always "".
	// callers may inject the actual tenant ID via the event bus.
	TenantID string `json:"tenant_id"`
	// Action is the lifecycle action: acknowledged, resolved, suppressed, notified.
	Action string `json:"action"`
	// Channel is the notification channel (e.g. email, webhook, dingtalk, feishu, slack). Populated for notified.
	Channel string `json:"channel,omitempty"`
	// UserID is the user ID who performed the action. Populated for acknowledged/resolved.
	UserID string `json:"user_id,omitempty"`
	// Reason is the reason for the action. Populated for suppressed/resolved.
	Reason string `json:"reason,omitempty"`
	// SuppressType is the suppression type: dependency, storm. Populated for suppressed.
	SuppressType string `json:"suppress_type,omitempty"`
	// NotifiedAt is the notification time as Unix nanoseconds. Populated for notified.
	NotifiedAt int64 `json:"notified_at,omitempty"`
	// Timestamp is the event timestamp as Unix nanoseconds.
	Timestamp int64 `json:"timestamp"`
}

// RemediationPayload is the payload for remediation lifecycle events, applicable to all remediation.* event types.
// Published by RemediationManager when a remediation rule matches and an executor is dispatched.
type RemediationPayload struct {
	// RemediationID is the unique identifier of this remediation execution instance.
	RemediationID string `json:"remediation_id"`
	// RuleID is the remediation rule ID that matched.
	RuleID string `json:"rule_id"`
	// RuleName is the human-readable name of the remediation rule.
	RuleName string `json:"rule_name"`
	// TaskID is the task definition ID used for the remediation execution.
	TaskID string `json:"task_id"`
	// AssetID is the ID of the asset targeted by the remediation action.
	AssetID string `json:"asset_id"`
	// TenantID is the tenant identifier.
	// The runtime is single-tenant: this field is always "".
	// callers may inject the actual tenant ID via the event bus.
	TenantID string `json:"tenant_id"`
	// Action is the lifecycle action: triggered, started, completed, skipped.
	Action string `json:"action"`
	// TriggerType is the trigger type: status_change, metric_exceeded, log_matched, fault_detected, manual.
	TriggerType string `json:"trigger_type,omitempty"`
	// SourceEventID is the ID of the source event that triggered the remediation. Populated for traceability.
	SourceEventID string `json:"source_event_id,omitempty"`
	// Status is the execution status: success, failure, timeout, skipped. Populated for completed.
	Status string `json:"status,omitempty"`
	// Error is the error message when the remediation fails. Populated for completed (failure/timeout).
	Error string `json:"error,omitempty"`
	// SkipReason is the reason for skipping: idempotent, cooldown, circuit_breaker,
	// condition_mismatch. Populated for skipped.
	SkipReason string `json:"skip_reason,omitempty"`
	// ExecutorType is the executor type (e.g. http, local, webhook, ssh).
	ExecutorType string `json:"executor_type,omitempty"`
	// Duration is the execution duration in nanoseconds. Populated for completed.
	Duration int64 `json:"duration,omitempty"`
	// RetryCount is the current retry attempt number.
	RetryCount int `json:"retry_count,omitempty"`
	// StartedAt is the execution start time as Unix nanoseconds. Populated for started/completed.
	StartedAt int64 `json:"started_at,omitempty"`
	// CompletedAt is the execution completion time as Unix nanoseconds. Populated for completed.
	CompletedAt int64 `json:"completed_at,omitempty"`
}

// SystemConfigPayload is the payload for the system configuration change
// event (TypeSystemConfigChanged); license-change events are handled by
// the callers's own payload type.
type SystemConfigPayload struct {
	// Scope is the configuration scope: global, tenant.
	Scope string `json:"scope"`
	// TenantID is the tenant identifier. Populated when Scope is tenant.
	// The runtime is single-tenant: this field is always "".
	// callers may inject the actual tenant ID via the event bus.
	TenantID string `json:"tenant_id,omitempty"`
	// ConfigKey is the configuration key that changed.
	ConfigKey string `json:"config_key,omitempty"`
	// OldValue is the configuration value before the change.
	OldValue string `json:"old_value,omitempty"`
	// NewValue is the configuration value after the change.
	NewValue string `json:"new_value,omitempty"`
	// ChangedBy is the user or system component that initiated the change.
	ChangedBy string `json:"changed_by,omitempty"`
	// ChangedAt is the event timestamp as Unix nanoseconds.
	ChangedAt int64 `json:"changed_at"`
}
