// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import "time"

// Type enumerates the normalized alert categories consumed by channels.
type Type string

const (
	// TypeMetric is emitted by metric threshold violations.
	TypeMetric Type = "metric"
	// TypeLog is emitted by log keyword matches.
	TypeLog Type = "log"
	// TypeHeartbeat is emitted when a probe stops reporting heartbeats.
	TypeHeartbeat Type = "heartbeat"
	// TypeStatus is emitted when an asset transitions to an abnormal state.
	TypeStatus Type = "status"
)

// Violation kind constants identify the category of a single rule violation.
// Use these instead of raw string literals to avoid magic strings.
const (
	// ViolationKindMetric identifies a metric threshold violation.
	ViolationKindMetric = "metric"
	// ViolationKindLog identifies a log keyword match violation.
	ViolationKindLog = "log"
	// ViolationKindHeartbeat identifies a heartbeat loss violation.
	ViolationKindHeartbeat = "heartbeat"
	// ViolationKindStatus identifies an asset status transition violation.
	ViolationKindStatus = "status"
)

// Violation describes a single rule violation detected during alert
// dispatch. An Event may carry multiple Violations when a compound rule
// (e.g. `alert.metrics["cpu"] > 90 && alert.metrics["mem"] > 85`) matches
// multiple conditions simultaneously.
//
// The Violation uses a Kind-discriminated layout: the common fields
// (Kind, Severity, Source, Message) are always present, while the
// kind-specific context is carried in the Metric, Log, or Status pointer.
// Only the context matching the Kind is populated; the others remain nil.
type Violation struct {
	// Kind identifies the violation category: "metric", "log", "heartbeat",
	// or "status".
	Kind string `json:"kind"`
	// Severity is the unified severity for ranking across all kinds:
	// critical > error > warning > info > debug. Metric violations
	// inherit it from the rule; log violations map from the log level;
	// heartbeat and status violations default to "critical" or "error".
	Severity string `json:"severity,omitempty"`
	// Source is the origin: IP for log, probe_id for heartbeat, asset key
	// for status.
	Source string `json:"source,omitempty"`
	// Message is a human-readable violation description.
	Message string `json:"message,omitempty"`
	// Metric carries metric-specific context. Populated only when
	// Kind == "metric".
	Metric *MetricContext `json:"metric,omitempty"`
	// Log carries log-specific context. Populated only when
	// Kind == "log".
	Log *LogContext `json:"log,omitempty"`
	// Status carries status-specific context. Populated only when
	// Kind == "status" or Kind == "heartbeat".
	Status *StatusContext `json:"status,omitempty"`
}

// MetricContext carries the metric-specific fields of a metric violation.
type MetricContext struct {
	// Name is the metric name that breached its threshold.
	Name string `json:"name"`
	// Value is the observed metric value.
	Value float64 `json:"value"`
	// Threshold is the configured threshold.
	Threshold float64 `json:"threshold"`
	// Metrics holds the full metric value map for the originating alert,
	// providing multi-metric context for rendering. The triggering metric
	// is always present as Metrics[Name].
	Metrics map[string]float64 `json:"metrics,omitempty"`
}

// LogContext carries the log-specific fields of a log violation.
type LogContext struct {
	// Keyword is the matched log keyword.
	Keyword string `json:"keyword,omitempty"`
	// Content is the log line that matched.
	Content string `json:"content,omitempty"`
}

// StatusContext carries the status-specific fields of a status or
// heartbeat violation.
type StatusContext struct {
	// PrevStatus is the asset status before the transition.
	PrevStatus string `json:"prev_status,omitempty"`
	// CurrStatus is the asset status after the transition.
	CurrStatus string `json:"curr_status,omitempty"`
}

// Event is the normalized alert representation dispatched to channels.
// It is derived from the typed event payloads published on the event bus
// (event.MetricExceededPayload, event.LogMatchedPayload) so that channels do not
// need to depend on the event package.
type Event struct {
	// Type is the alert category.
	Type Type `json:"type"`
	// EventID is the unique tracking identifier assigned by the engine when
	// the alert is dispatched. It is stable for a single Dispatch call and
	// suitable for correlating with delivery records in channels and stores.
	// Empty when the alert has not passed through Dispatch.
	EventID string `json:"event_id,omitempty"`
	// AssetID is the asset that triggered the alert.
	AssetID int64 `json:"asset_id"`
	// TenantID is the owning tenant for multi-tenant isolation.
	// The runtime is single-tenant: this field is always 0.
	// The runtime injects the actual tenant ID via the alert pipeline.
	TenantID int64 `json:"tenant_id"`
	// Timestamp is when the alert was generated.
	Timestamp time.Time `json:"timestamp"`
	// Violations carries all rule violations detected during dispatch.
	// A single Event may carry multiple Violations when a compound rule
	// matches multiple conditions (e.g. "cpu > 90 && mem > 85").
	Violations []Violation `json:"violations,omitempty"`
	// Locale is the recipient's preferred locale (BCP 47 tag, e.g. "zh-Hans").
	// When non-empty, channels use it to render the alert in the recipient's
	// language. When empty, channels fall back to the recipient's persisted
	// preference or the system default locale (see i18n.DefaultLocale).
	Locale string `json:"locale,omitempty"`
	// TemplateID identifies the alert template used to render this event.
	// When non-empty, channels that have a template.Library should call
	// Library.Render with this ID instead of the default Formatter. When
	// empty, channels use the default Formatter.
	TemplateID string `json:"template_id,omitempty"`
}

// PrimaryViolation returns the most severe violation in the event, or a
// zero-value Violation when the event has no violations. The boolean
// indicates whether a violation was found.
//
// Severity ordering (highest to lowest):
//
//	critical > error > warning > info > debug
//
// When multiple violations share the same severity, the first one in the
// slice is returned. This method is suitable for summary scenarios (email
// subjects, alert titles) where a single representative violation is
// needed, but the full Violations list should be used for complete rendering.
func (e Event) PrimaryViolation() (Violation, bool) {
	if len(e.Violations) == 0 {
		return Violation{}, false
	}
	primary := e.Violations[0]
	primaryRank := severityRank(primary.Severity)
	for _, v := range e.Violations[1:] {
		if rank := severityRank(v.Severity); rank > primaryRank {
			primary = v
			primaryRank = rank
		}
	}
	return primary, true
}

// severityRank returns a numeric rank for severity levels. Higher numbers
// indicate higher severity. Unknown levels default to 0 (lowest).
func severityRank(level string) int {
	switch level {
	case "critical", "fatal":
		return 5
	case "error":
		return 4
	case "warning", "warn":
		return 3
	case "info", "notice":
		return 2
	case "debug":
		return 1
	default:
		return 0
	}
}

// HasViolations returns true when the event carries at least one violation.
func (e Event) HasViolations() bool {
	return len(e.Violations) > 0
}
