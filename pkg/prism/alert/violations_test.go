// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"testing"
	"time"
)

// TestExtractPredicateOnlyRuleYieldsNoViolations verifies that scalar
// predicates (type == "status") do not fabricate metric violations, so
// the event's payload violations survive dispatch.
func TestExtractPredicateOnlyRuleYieldsNoViolations(t *testing.T) {
	x := NewViolationExtractor(nil)
	rule := Rule{ID: 7, Name: "status-rule", Expression: `type == "status" && severity in ["error", "critical"]`}
	env := testEnv(nil)
	env.Type = "status"
	env.Severity = "error"

	if got := x.Extract(context.Background(), rule, env, true); got != nil {
		t.Fatalf("Extract = %v, want nil for predicate-only rule", got)
	}
}

// TestExtractMetricFactRuleStampsRule verifies metric-fact comparisons
// yield violations carrying the producing rule's id and name.
func TestExtractMetricFactRuleStampsRule(t *testing.T) {
	x := NewViolationExtractor(nil)
	rule := Rule{ID: 7, Name: "cpu-high", Expression: `metrics["cpu"] > 90`}
	env := testEnv(map[string]float64{"cpu": 95})

	violations := x.Extract(context.Background(), rule, env, true)
	if len(violations) != 1 {
		t.Fatalf("Extract produced %d violations, want 1", len(violations))
	}
	v := violations[0]
	if v.RuleID != 7 {
		t.Errorf("RuleID = %d, want 7", v.RuleID)
	}
	if v.RuleName != "cpu-high" {
		t.Errorf("RuleName = %q, want %q", v.RuleName, "cpu-high")
	}
	if v.Metric == nil || v.Metric.Name != "cpu" {
		t.Errorf("Metric = %+v, want name cpu", v.Metric)
	}
}

// TestExtractMixedRuleKeepsOnlyMetricFacts verifies a rule combining
// scalar predicates with metric facts yields violations only for the
// metric facts.
func TestExtractMixedRuleKeepsOnlyMetricFacts(t *testing.T) {
	x := NewViolationExtractor(nil)
	rule := Rule{ID: 9, Name: "mixed", Expression: `severity == "critical" && metrics["cpu"] > 90`}
	env := testEnv(map[string]float64{"cpu": 95})
	env.Severity = "critical"

	violations := x.Extract(context.Background(), rule, env, true)
	if len(violations) != 1 {
		t.Fatalf("Extract produced %d violations, want 1", len(violations))
	}
	if violations[0].Metric == nil || violations[0].Metric.Name != "cpu" {
		t.Errorf("Metric = %+v, want name cpu", violations[0].Metric)
	}
}

// TestExtractDotFormMetricAccess verifies `metrics.cpu` member access is
// recognized as a metric fact with the bare key as the metric name.
func TestExtractDotFormMetricAccess(t *testing.T) {
	x := NewViolationExtractor(nil)
	rule := Rule{ID: 3, Name: "dot", Expression: `metrics.cpu >= 85`}
	env := testEnv(map[string]float64{"cpu": 90})

	violations := x.Extract(context.Background(), rule, env, true)
	if len(violations) != 1 {
		t.Fatalf("Extract produced %d violations, want 1", len(violations))
	}
	if violations[0].Metric == nil || violations[0].Metric.Name != "cpu" {
		t.Errorf("Metric = %+v, want name cpu", violations[0].Metric)
	}
}

// TestViolationToRecordRuleAttribution verifies the record carries the
// violation's rule attribution and keeps the payload message.
func TestViolationToRecordRuleAttribution(t *testing.T) {
	v := Violation{
		Kind:     ViolationKindStatus,
		RuleID:   7,
		RuleName: "status-rule",
		Severity: "error",
		Source:   "127.0.0.1",
		Message:  "asset 2 transitioned normal -> abnormal",
	}
	rec := ViolationToRecord(v, time.Unix(1700000000, 0).UTC())
	if rec.RuleID != 7 {
		t.Errorf("RuleID = %d, want 7", rec.RuleID)
	}
	if rec.RuleName != "status-rule" {
		t.Errorf("RuleName = %q, want status-rule", rec.RuleName)
	}
	if rec.Message != "asset 2 transitioned normal -> abnormal" {
		t.Errorf("Message = %q, want the payload message", rec.Message)
	}
}
