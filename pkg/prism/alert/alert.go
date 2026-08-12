// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

// PrimaryViolation returns the most severe violation carried by the Event,
// or nil when no violations have been recorded. This is the recommended
// accessor for summary scenarios (alert titles, channel messages).
func PrimaryViolation(e Event) *Violation {
	if v, ok := e.PrimaryViolation(); ok {
		return &v
	}
	return nil
}

// MetricName returns the metric name from the primary violation's Metric
// context, or "" when no metric context is present.
func MetricName(e Event) string {
	if v := PrimaryViolation(e); v != nil && v.Metric != nil {
		return v.Metric.Name
	}
	return ""
}

// MetricValue returns the observed metric value from the primary violation's
// Metric context, or 0 when no metric context is present.
func MetricValue(e Event) float64 {
	if v := PrimaryViolation(e); v != nil && v.Metric != nil {
		return v.Metric.Value
	}
	return 0
}

// Threshold returns the configured threshold from the primary violation's
// Metric context, or 0 when no metric context is present.
func Threshold(e Event) float64 {
	if v := PrimaryViolation(e); v != nil && v.Metric != nil {
		return v.Metric.Threshold
	}
	return 0
}

// Metrics returns the full metric value map from the primary violation's
// Metric context, or nil when no metric context is present.
func Metrics(e Event) map[string]float64 {
	if v := PrimaryViolation(e); v != nil && v.Metric != nil {
		return v.Metric.Metrics
	}
	return nil
}

// Severity returns the unified severity from the primary violation, or ""
// when no violation carries a severity.
func Severity(e Event) string {
	if v := PrimaryViolation(e); v != nil {
		return v.Severity
	}
	return ""
}

// Keyword returns the matched log keyword from the primary violation's Log
// context, or "" when no log context is present.
func Keyword(e Event) string {
	if v := PrimaryViolation(e); v != nil && v.Log != nil {
		return v.Log.Keyword
	}
	return ""
}

// Content returns the log content that matched from the primary violation's
// Log context, or "" when no log context is present.
func Content(e Event) string {
	if v := PrimaryViolation(e); v != nil && v.Log != nil {
		return v.Log.Content
	}
	return ""
}

// SourceIP returns the origin address from the primary violation's Source
// field. The Violation.Source field carries the IP for log violations and
// the probe ID for heartbeat violations. Returns "" when no violation
// carries a source.
func SourceIP(e Event) string {
	if v := PrimaryViolation(e); v != nil {
		return v.Source
	}
	return ""
}
