// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"encoding/json"
	"testing"
	"time"
)

// TestViolationKindConstants verifies that the ViolationKind constants
// match their documented string values. This guards against accidental
// renames that would break JSON consumers.
func TestViolationKindConstants(t *testing.T) {
	cases := map[string]string{
		"ViolationKindMetric":    ViolationKindMetric,
		"ViolationKindLog":       ViolationKindLog,
		"ViolationKindHeartbeat": ViolationKindHeartbeat,
		"ViolationKindStatus":    ViolationKindStatus,
	}
	want := map[string]string{
		"ViolationKindMetric":    "metric",
		"ViolationKindLog":       "log",
		"ViolationKindHeartbeat": "heartbeat",
		"ViolationKindStatus":    "status",
	}
	for name, got := range cases {
		if got != want[name] {
			t.Errorf("%s: got %q, want %q", name, got, want[name])
		}
	}
}

// TestTypeConstants verifies the Type enum string values, including the
// newly added TypeHeartbeat and TypeStatus.
func TestTypeConstants(t *testing.T) {
	cases := map[Type]string{
		TypeMetric:    "metric",
		TypeLog:       "log",
		TypeHeartbeat: "heartbeat",
		TypeStatus:    "status",
	}
	for typ, want := range cases {
		if string(typ) != want {
			t.Errorf("Type %v: got %q, want %q", typ, string(typ), want)
		}
	}
}

// TestViolationRoundTrip verifies that a Violation marshals to JSON and
// unmarshals back to an equal value, including optional fields.
func TestViolationRoundTrip(t *testing.T) {
	v := Violation{
		Kind:     ViolationKindMetric,
		Severity: "critical",
		Message:  "cpu usage exceeded threshold",
		Metric: &MetricContext{
			Name:      "cpu_usage",
			Value:     95.5,
			Threshold: 90.0,
		},
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Violation
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !violationEqual(v, got) {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, v)
	}
}

// TestViolationOmitsEmptyFields verifies that zero-value optional fields
// are omitted from the JSON output, while Kind is always present.
func TestViolationOmitsEmptyFields(t *testing.T) {
	v := Violation{Kind: ViolationKindLog}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if _, ok := raw["kind"]; !ok {
		t.Errorf("kind should always be present, got: %s", data)
	}
	for _, key := range []string{
		"severity", "metric", "log", "status", "source", "message",
	} {
		if _, ok := raw[key]; ok {
			t.Errorf("optional field %q should be omitted, got: %s", key, data)
		}
	}
}

// TestEventRoundTrip verifies that an Event with Violations serializes
// and deserializes correctly through JSON.
func TestEventRoundTrip(t *testing.T) {
	ts := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	orig := Event{
		Type:      TypeMetric,
		EventID:   "evt-123",
		AssetID:   5,
		TenantID:  50,
		Timestamp: ts,
		Violations: []Violation{{
			Kind: ViolationKindMetric,
			Metric: &MetricContext{
				Name:      "cpu",
				Value:     95.0,
				Threshold: 90.0,
			},
		}},
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != TypeMetric {
		t.Errorf("type: got %q, want %q", got.Type, TypeMetric)
	}
	if got.EventID != orig.EventID {
		t.Errorf("event_id: got %q, want %q", got.EventID, orig.EventID)
	}
	if got.AssetID != orig.AssetID {
		t.Errorf("asset_id: got %d, want %d", got.AssetID, orig.AssetID)
	}
	if len(got.Violations) != 1 {
		t.Fatalf("violations: got %d items, want 1", len(got.Violations))
	}
	if got.Violations[0].Metric == nil || got.Violations[0].Metric.Name != "cpu" {
		t.Errorf("metric: got %+v, want name %q", got.Violations[0].Metric, "cpu")
	}
	if got.Violations[0].Metric.Value != 95.0 {
		t.Errorf("value: got %f, want %f", got.Violations[0].Metric.Value, 95.0)
	}
	if got.Violations[0].Metric.Threshold != 90.0 {
		t.Errorf("threshold: got %f, want %f", got.Violations[0].Metric.Threshold, 90.0)
	}
}

// TestEventDecodesWithViolationsField verifies that JSON payloads
// with violations correctly decode into an Event.
func TestEventDecodesWithViolationsField(t *testing.T) {
	jsonWithViolations := `{
		"type": "log",
		"event_id": "evt-abc",
		"asset_id": 7,
		"tenant_id": 0,
		"timestamp": "2026-07-25T10:00:00Z",
		"violations": [
			{
				"kind": "log",
				"severity": "error",
				"log": {
					"keyword": "panic",
					"content": "goroutine panic"
				},
				"source": "10.0.0.1"
			}
		]
	}`
	var got Event
	if err := json.Unmarshal([]byte(jsonWithViolations), &got); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}
	if got.Type != TypeLog {
		t.Errorf("type: got %q, want %q", got.Type, TypeLog)
	}
	if len(got.Violations) != 1 {
		t.Fatalf("violations: got %d items, want 1", len(got.Violations))
	}
	if got.Violations[0].Severity != "error" {
		t.Errorf("severity: got %q, want %q", got.Violations[0].Severity, "error")
	}
	if got.Violations[0].Log == nil || got.Violations[0].Log.Keyword != "panic" {
		t.Errorf("keyword: got %+v, want %q", got.Violations[0].Log, "panic")
	}
	if got.Violations[0].Source != "10.0.0.1" {
		t.Errorf("source: got %q, want %q", got.Violations[0].Source, "10.0.0.1")
	}
}

// TestEventMultipleViolations verifies that an Event carrying multiple
// Violations (a compound rule matching several conditions) round-trips
// through JSON preserving all entries and their order.
func TestEventMultipleViolations(t *testing.T) {
	ts := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	orig := Event{
		Type:      TypeMetric,
		EventID:   "evt-multi",
		AssetID:   9,
		TenantID:  1,
		Timestamp: ts,
		Violations: []Violation{
			{
				Kind:     ViolationKindMetric,
				Severity: "critical",
				Message:  "cpu usage exceeded threshold",
				Metric: &MetricContext{
					Name:      "cpu_usage",
					Value:     95.0,
					Threshold: 90.0,
				},
			},
			{
				Kind:     ViolationKindMetric,
				Severity: "warning",
				Message:  "memory usage exceeded threshold",
				Metric: &MetricContext{
					Name:      "mem_usage",
					Value:     88.0,
					Threshold: 85.0,
				},
			},
			{
				Kind:     ViolationKindLog,
				Severity: "error",
				Source:   "10.0.0.2",
				Log: &LogContext{
					Keyword: "oom",
					Content: "out of memory killer invoked",
				},
			},
		},
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Violations) != len(orig.Violations) {
		t.Fatalf("violations length: got %d, want %d", len(got.Violations), len(orig.Violations))
	}
	for i, want := range orig.Violations {
		if !violationEqual(want, got.Violations[i]) {
			t.Errorf("violations[%d] mismatch:\n got  %+v\n want %+v", i, got.Violations[i], want)
		}
	}
}

// TestEventViolationsOmittedWhenEmpty verifies that an Event with no
// Violations omits the "violations" key from its JSON output, keeping
// the wire format identical to the pre-change layout.
func TestEventViolationsOmittedWhenEmpty(t *testing.T) {
	evt := Event{
		Type:       TypeStatus,
		EventID:    "evt-status",
		AssetID:    1,
		TenantID:   0,
		Timestamp:  time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC),
		TemplateID: "tpl-1",
	}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if _, ok := raw["violations"]; ok {
		t.Errorf("violations should be omitted when empty, got: %s", data)
	}
}

// TestPrimaryViolation_PicksHighestSeverity verifies that PrimaryViolation
// returns the violation with the highest severity, not the first one in
// the slice. This is the core contract used by summary renderers (email
// subjects, alert titles) that need a single representative violation.
func TestPrimaryViolation_PicksHighestSeverity(t *testing.T) {
	evt := Event{
		Type: TypeMetric,
		Violations: []Violation{
			{Kind: ViolationKindMetric, Severity: "info", Metric: &MetricContext{Name: "disk"}},
			{Kind: ViolationKindMetric, Severity: "warning", Metric: &MetricContext{Name: "mem"}},
			{Kind: ViolationKindMetric, Severity: "critical", Metric: &MetricContext{Name: "cpu"}},
			{Kind: ViolationKindMetric, Severity: "error", Metric: &MetricContext{Name: "net"}},
		},
	}
	primary, ok := evt.PrimaryViolation()
	if !ok {
		t.Fatal("expected ok=true, got false")
	}
	if primary.Metric == nil || primary.Metric.Name != "cpu" {
		t.Errorf("Metric.Name = %+v, want %q (highest severity)", primary.Metric, "cpu")
	}
	if primary.Severity != "critical" {
		t.Errorf("Severity = %q, want %q", primary.Severity, "critical")
	}
}

// TestPrimaryViolation_FirstWinsOnTie verifies that when multiple
// violations share the same severity, the first one in the slice is
// returned. This keeps the selection deterministic for summary scenarios.
func TestPrimaryViolation_FirstWinsOnTie(t *testing.T) {
	evt := Event{
		Type: TypeMetric,
		Violations: []Violation{
			{Kind: ViolationKindMetric, Severity: "critical", Metric: &MetricContext{Name: "first"}},
			{Kind: ViolationKindMetric, Severity: "critical", Metric: &MetricContext{Name: "second"}},
		},
	}
	primary, ok := evt.PrimaryViolation()
	if !ok {
		t.Fatal("expected ok=true, got false")
	}
	if primary.Metric == nil || primary.Metric.Name != "first" {
		t.Errorf("Metric.Name = %+v, want %q (first wins on tie)", primary.Metric, "first")
	}
}

// TestPrimaryViolation_EmptyViolations verifies that an event with no
// violations returns a zero-value Violation and ok=false, so callers
// can detect the "no violations" case without inspecting the returned
// Violation fields.
func TestPrimaryViolation_EmptyViolations(t *testing.T) {
	cases := []struct {
		name string
		evt  Event
	}{
		{"nil violations", Event{Violations: nil}},
		{"empty violations", Event{Violations: []Violation{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			primary, ok := tc.evt.PrimaryViolation()
			if ok {
				t.Error("expected ok=false for empty violations, got true")
			}
			// Violation contains pointers so check the scalar fields that a
			// zero-value Violation would have.
			if primary.Kind != "" || primary.Metric != nil || primary.Severity != "" {
				t.Errorf("expected zero-value Violation, got %+v", primary)
			}
		})
	}
}

// TestPrimaryViolation_SeverityOrdering verifies the documented severity
// ranking (critical > error > warning > info > debug) end-to-end by
// constructing events where the target severity is the highest-ranked
// violation, ensuring it is selected as the primary.
func TestPrimaryViolation_SeverityOrdering(t *testing.T) {
	cases := []struct {
		name     string
		severity string
		// lowers lists severities with strictly lower rank than the
		// target, used to populate the non-target violations.
		lowers []string
	}{
		{"critical", "critical", []string{"error", "warning", "info", "debug"}},
		{"error", "error", []string{"warning", "info", "debug"}},
		{"warning", "warning", []string{"info", "debug"}},
		{"info", "info", []string{"debug"}},
		{"debug", "debug", []string{"unknown-level"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Build a violation list with lower-severity entries, then
			// append the target at the end. PrimaryViolation must return
			// the target because it has the highest rank.
			violations := make([]Violation, 0, len(tc.lowers)+1)
			for _, s := range tc.lowers {
				violations = append(violations, Violation{Severity: s, Metric: &MetricContext{Name: s + "-v"}})
			}
			violations = append(violations, Violation{Severity: tc.severity, Metric: &MetricContext{Name: "target"}})
			evt := Event{Violations: violations}
			primary, ok := evt.PrimaryViolation()
			if !ok {
				t.Fatal("expected ok=true")
			}
			if primary.Severity != tc.severity {
				t.Errorf("Severity = %q, want %q", primary.Severity, tc.severity)
			}
			if primary.Metric == nil || primary.Metric.Name != "target" {
				t.Errorf("Metric.Name = %+v, want %q", primary.Metric, "target")
			}
		})
	}
}

// TestPrimaryViolation_SeverityAliases verifies that severity aliases
// ("fatal" -> critical, "warn" -> warning, "notice" -> info) are ranked
// alongside their canonical forms, matching the severityRank mapping.
func TestPrimaryViolation_SeverityAliases(t *testing.T) {
	cases := []struct {
		name     string
		severity string
		wantRank int
	}{
		{"fatal ranks as critical", "fatal", 5},
		{"warn ranks as warning", "warn", 3},
		{"notice ranks as info", "notice", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evt := Event{
				Violations: []Violation{
					{Severity: tc.severity, Metric: &MetricContext{Name: "alias"}},
					{Severity: "debug", Metric: &MetricContext{Name: "lower"}},
				},
			}
			primary, ok := evt.PrimaryViolation()
			if !ok {
				t.Fatal("expected ok=true")
			}
			if primary.Metric == nil || primary.Metric.Name != "alias" {
				t.Errorf("expected alias violation to win (rank %d), got %+v", tc.wantRank, primary.Metric)
			}
		})
	}
}

// TestPrimaryViolation_UnknownSeverityRanksZero verifies that an unknown
// severity level (not in the severityRank map) ranks lowest (0), so it
// loses to any recognized level including debug (rank 1).
func TestPrimaryViolation_UnknownSeverityRanksZero(t *testing.T) {
	evt := Event{
		Violations: []Violation{
			{Severity: "unknown-level", Metric: &MetricContext{Name: "unknown"}},
			{Severity: "debug", Metric: &MetricContext{Name: "debug"}},
		},
	}
	primary, ok := evt.PrimaryViolation()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if primary.Metric == nil || primary.Metric.Name != "debug" {
		t.Errorf("expected debug (rank 1) to beat unknown (rank 0), got %+v", primary.Metric)
	}
}

// TestPrimaryViolation_SingleViolation verifies that an event with one
// violation returns that violation as the primary, regardless of its
// severity.
func TestPrimaryViolation_SingleViolation(t *testing.T) {
	evt := Event{
		Violations: []Violation{
			{Kind: ViolationKindLog, Severity: "info", Log: &LogContext{Keyword: "slow-query"}},
		},
	}
	primary, ok := evt.PrimaryViolation()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if primary.Log == nil || primary.Log.Keyword != "slow-query" {
		t.Errorf("Keyword = %+v, want %q", primary.Log, "slow-query")
	}
}

// TestHasViolations verifies that HasViolations returns true when the
// event carries at least one violation and false otherwise.
func TestHasViolations(t *testing.T) {
	cases := []struct {
		name string
		evt  Event
		want bool
	}{
		{"nil violations", Event{Violations: nil}, false},
		{"empty violations", Event{Violations: []Violation{}}, false},
		{"one violation", Event{Violations: []Violation{{Kind: ViolationKindMetric}}}, true},
		{"two violations", Event{Violations: []Violation{{Kind: ViolationKindMetric}, {Kind: ViolationKindLog}}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.evt.HasViolations(); got != tc.want {
				t.Errorf("HasViolations = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEventLocaleAndTemplateIDRoundTrip verifies that the Locale and
// TemplateID fields round-trip through JSON, supporting the i18n and
// template-rendering dispatch paths.
func TestEventLocaleAndTemplateIDRoundTrip(t *testing.T) {
	orig := Event{
		Type:       TypeMetric,
		AssetID:    1,
		Timestamp:  time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC),
		Locale:     "zh-Hans",
		TemplateID: "tpl-email-1",
		Violations: []Violation{{Kind: ViolationKindMetric, Metric: &MetricContext{Name: "cpu"}}},
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Locale != orig.Locale {
		t.Errorf("Locale: got %q, want %q", got.Locale, orig.Locale)
	}
	if got.TemplateID != orig.TemplateID {
		t.Errorf("TemplateID: got %q, want %q", got.TemplateID, orig.TemplateID)
	}
}

// TestEventLocaleOmittedWhenEmpty verifies that an empty Locale is omitted
// from the JSON output, so the wire format stays compact for deployments
// that do not use locale-aware rendering.
func TestEventLocaleOmittedWhenEmpty(t *testing.T) {
	evt := Event{
		Type:      TypeMetric,
		AssetID:   1,
		Timestamp: time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if _, ok := raw["locale"]; ok {
		t.Errorf("locale should be omitted when empty, got: %s", data)
	}
	if _, ok := raw["template_id"]; ok {
		t.Errorf("template_id should be omitted when empty, got: %s", data)
	}
}

// violationEqual reports whether two Violations are deeply equal. It is
// used by the round-trip tests because map equality with reflect.DeepEqual
// is sufficient here and keeps the call sites readable.
func violationEqual(a, b Violation) bool {
	if a.Kind != b.Kind || a.Severity != b.Severity {
		return false
	}
	if a.Source != b.Source || a.Message != b.Message {
		return false
	}
	if !metricContextEqual(a.Metric, b.Metric) {
		return false
	}
	if !logContextEqual(a.Log, b.Log) {
		return false
	}
	if !statusContextEqual(a.Status, b.Status) {
		return false
	}
	return true
}

// metricContextEqual reports whether two *MetricContext values are deeply
// equal, treating nil and non-nil as different (matching JSON semantics).
func metricContextEqual(a, b *MetricContext) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Name != b.Name || a.Value != b.Value || a.Threshold != b.Threshold {
		return false
	}
	if len(a.Metrics) != len(b.Metrics) {
		return false
	}
	for k, v := range a.Metrics {
		if bv, ok := b.Metrics[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// logContextEqual reports whether two *LogContext values are deeply equal.
func logContextEqual(a, b *LogContext) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Keyword == b.Keyword && a.Content == b.Content
}

// statusContextEqual reports whether two *StatusContext values are deeply equal.
func statusContextEqual(a, b *StatusContext) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.PrevStatus == b.PrevStatus && a.CurrStatus == b.CurrStatus
}
