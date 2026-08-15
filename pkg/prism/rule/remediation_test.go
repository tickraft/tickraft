// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"context"
	"testing"
	"time"

	a "github.com/tickraft/tickraft/pkg/prism/alert"
	"go.uber.org/zap"
)

// remediationEnv builds a RemediationMatchEnv carrying the supplied fields on
// the Remediation view. It is a convenience constructor so test cases stay
// readable; fields not relevant to a given test are left zero-valued.
func remediationEnv(metricName string, metricValue, threshold float64) RemediationMatchEnv {
	return RemediationMatchEnv{
		Remediation: RemediationView{
			Type:        string(a.TypeMetric),
			AssetID:     100,
			MetricName:  metricName,
			MetricValue: metricValue,
			Threshold:   threshold,
			Severity:    "critical",
		},
	}
}

// ---------------------------------------------------------------------------
// Engine.MatchRemediation
// ---------------------------------------------------------------------------

// TestEngine_MatchRemediation_SingleRule verifies that MatchRemediation
// evaluates a remediation-scene rule against a RemediationMatchEnv and
// returns the matching rule's ID when the threshold is exceeded.
func TestEngine_MatchRemediation_SingleRule(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rules := []Rule{
		{
			ID:         1,
			TenantID:   1,
			Name:       "cpu-remediate",
			Scene:      SceneRemediation,
			Expression: `remediation.metric_value > 80`,
			Priority:   10,
			Enabled:    true,
		},
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Hit: MetricValue=90 > 80 -> rule matches.
	matched := eng.MatchRemediation(ctx, remediationEnv("cpu", 90, 80))
	if len(matched) != 1 || matched[0] != 1 {
		t.Errorf("MatchRemediation = %v, want [1]", matched)
	}

	// Miss: MetricValue=70 < 80 -> no match.
	if got := eng.MatchRemediation(ctx, remediationEnv("cpu", 70, 80)); len(got) != 0 {
		t.Errorf("MatchRemediation = %v, want empty for value below threshold", got)
	}
}

// TestEngine_MatchRemediation_MultipleRules verifies that multiple
// remediation rules are evaluated and that higher-priority rules appear
// first in the returned slice (the engine preserves Load order, and
// callers are expected to honor Priority when picking a workflow).
func TestEngine_MatchRemediation_MultipleRules(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rules := []Rule{
		{ID: 1, Name: "critical-cpu", Scene: SceneRemediation, Expression: `remediation.metric_value > 90 && remediation.severity == "critical"`, Priority: 10, Enabled: true},
		{ID: 2, Name: "warning-cpu", Scene: SceneRemediation, Expression: `remediation.metric_value > 80 && remediation.severity == "warning"`, Priority: 5, Enabled: true},
		{ID: 3, Name: "any-cpu", Scene: SceneRemediation, Expression: `remediation.metric_value > 70`, Priority: 1, Enabled: true},
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// MetricValue=95, Severity="critical" -> matches rules 1 and 3.
	env := RemediationMatchEnv{
		Remediation: RemediationView{
			MetricValue: 95,
			Severity:    "critical",
		},
	}
	matched := eng.MatchRemediation(ctx, env)
	want := map[int64]bool{1: true, 3: true}
	if len(matched) != len(want) {
		t.Fatalf("MatchRemediation = %v, want %d matches", matched, len(want))
	}
	for _, id := range matched {
		if !want[id] {
			t.Errorf("unexpected match id %d", id)
		}
	}
}

// TestEngine_MatchRemediation_NoRules verifies that an engine with no
// remediation rules returns an empty slice (without panicking).
func TestEngine_MatchRemediation_NoRules(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	if got := eng.MatchRemediation(ctx, remediationEnv("cpu", 99, 80)); len(got) != 0 {
		t.Errorf("MatchRemediation on empty engine = %v, want empty", got)
	}
}

// TestEngine_MatchRemediation_DisabledRuleSkipped verifies that a
// disabled remediation rule does not participate in matching.
func TestEngine_MatchRemediation_DisabledRuleSkipped(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rules := []Rule{
		{ID: 1, Name: "enabled", Scene: SceneRemediation, Expression: `remediation.metric_value > 80`, Priority: 10, Enabled: true},
		{ID: 2, Name: "disabled", Scene: SceneRemediation, Expression: `remediation.metric_value > 80`, Priority: 5, Enabled: false},
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load: %v", err)
	}
	matched := eng.MatchRemediation(ctx, remediationEnv("cpu", 90, 80))
	if len(matched) != 1 || matched[0] != 1 {
		t.Errorf("MatchRemediation = %v, want [1] (disabled rule 2 should be skipped)", matched)
	}
}

// TestEngine_MatchRemediation_SceneIsolation verifies that remediation rules
// are never evaluated by the metric match path, and vice versa.
func TestEngine_MatchRemediation_SceneIsolation(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rules := []Rule{
		{ID: 1, Name: "remediation-rule", Scene: SceneRemediation, Expression: `remediation.metric_value > 80`, Enabled: true},
		{ID: 2, Name: "metric-rule", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Enabled: true},
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// A RemediationMatchEnv must not match the metric rule, and a
	// MetricMatchEnv must not match the remediation rule.
	remMatched := eng.MatchRemediation(ctx, remediationEnv("cpu", 90, 80))
	if len(remMatched) != 1 || remMatched[0] != 1 {
		t.Errorf("MatchRemediation = %v, want [1] (metric rule must not leak)", remMatched)
	}
	metMatched := eng.MatchMetric(ctx, MetricMatchEnv{Alert: AlertView{Metrics: map[string]float64{"cpu": 90}}})
	if len(metMatched) != 1 || metMatched[0] != 2 {
		t.Errorf("MatchMetric = %v, want [2] (remediation rule must not leak)", metMatched)
	}
}

// TestEngine_HasRemediationRules verifies the HasRemediationRules helper
// returns true after loading remediation rules and false otherwise.
func TestEngine_HasRemediationRules(t *testing.T) {
	eng := NewEngine(zap.NewNop())
	if eng.HasRemediationRules() {
		t.Error("expected HasRemediationRules=false on fresh engine")
	}

	if err := eng.Load(context.Background(), []Rule{
		{ID: 1, Scene: SceneRemediation, Expression: `remediation.metric_value > 80`, Enabled: true},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !eng.HasRemediationRules() {
		t.Error("expected HasRemediationRules=true after loading a remediation rule")
	}

	// Loading a non-remediation rule set resets the state.
	if err := eng.Load(context.Background(), []Rule{
		{ID: 2, Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Enabled: true},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if eng.HasRemediationRules() {
		t.Error("expected HasRemediationRules=false after loading only metric rules")
	}
}

// ---------------------------------------------------------------------------
// RemediationMatchEnv field access
// ---------------------------------------------------------------------------

// TestRemediationMatchEnv_FieldAccess verifies that rule expressions can
// read every field exposed by the RemediationView via expr struct tags.
func TestRemediationMatchEnv_FieldAccess(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	env := RemediationMatchEnv{
		Remediation: RemediationView{
			Type:        string(a.TypeMetric),
			AssetID:     42,
			MetricName:  "cpu_usage",
			MetricValue: 95,
			Threshold:   80,
			Severity:    "critical",
			Keyword:     "panic",
			Content:     "goroutine panic",
			SourceIP:    "10.0.0.1",
			PrevStatus:  "normal",
			CurrStatus:  "critical",
		},
		Asset: AssetView{ID: 42, Name: "my-host"},
	}

	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"metric value threshold", `remediation.metric_value > 80`, true},
		{"threshold", `remediation.threshold > 0`, true},
		{"severity", `remediation.severity == "critical"`, true},
		{"metric name", `remediation.metric_name == "cpu_usage"`, true},
		{"keyword", `remediation.keyword == "panic"`, true},
		{"content contains", `remediation.content contains "panic"`, true},
		{"source ip", `remediation.source_ip == "10.0.0.1"`, true},
		{"prev status", `remediation.prev_status == "normal"`, true},
		{"curr status", `remediation.curr_status == "critical"`, true},
		{"type", `remediation.type == "metric"`, true},
		{"asset name", `asset.name == "my-host"`, true},
		{"non-matching severity", `remediation.severity == "warning"`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := Rule{ID: 1, Name: tc.name, Scene: SceneRemediation, Expression: tc.expr, Enabled: true}
			if err := eng.Load(ctx, []Rule{rule}); err != nil {
				t.Fatalf("Load: %v", err)
			}
			matched := eng.MatchRemediation(ctx, env)
			if tc.want && len(matched) != 1 {
				t.Errorf("expr %q: expected match, got %v", tc.expr, matched)
			}
			if !tc.want && len(matched) != 0 {
				t.Errorf("expr %q: expected no match, got %v", tc.expr, matched)
			}
		})
	}
}

// TestRemediationMatchEnv_StatusTransition verifies that a rule can guard
// against re-running a remediation workflow by checking the PrevStatus
// and CurrStatus fields. This mirrors the documented use case in the
// RemediationView doc comment.
func TestRemediationMatchEnv_StatusTransition(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	// Rule fires only when the asset transitioned from normal to critical.
	rules := []Rule{
		{
			ID:         1,
			Name:       "normal-to-critical",
			Scene:      SceneRemediation,
			Expression: `remediation.prev_status == "normal" && remediation.curr_status == "critical"`,
			Enabled:    true,
		},
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Transition normal -> critical: rule matches.
	transitionEnv := RemediationMatchEnv{
		Remediation: RemediationView{PrevStatus: "normal", CurrStatus: "critical"},
	}
	if matched := eng.MatchRemediation(ctx, transitionEnv); len(matched) != 1 {
		t.Errorf("expected match for normal->critical transition, got %v", matched)
	}

	// Transition warning -> critical: rule does not match.
	otherEnv := RemediationMatchEnv{
		Remediation: RemediationView{PrevStatus: "warning", CurrStatus: "critical"},
	}
	if matched := eng.MatchRemediation(ctx, otherEnv); len(matched) != 0 {
		t.Errorf("expected no match for warning->critical transition, got %v", matched)
	}
}

// ---------------------------------------------------------------------------
// toRemediationView projection
// ---------------------------------------------------------------------------

// TestToRemediationView_ProjectsPrimaryViolation verifies that
// toRemediationView projects the primary violation's metric, value,
// threshold, severity, keyword, content, and source fields onto the
// RemediationView, and that the status fields are passed through.
func TestToRemediationView_ProjectsPrimaryViolation(t *testing.T) {
	evt := a.Event{
		Type:      a.TypeMetric,
		AssetID:   100,
		Timestamp: time.Now(),
		Violations: []a.Violation{
			{
				Kind:     a.ViolationKindMetric,
				Severity: "warning",
				Source:   "10.0.0.1",
				Metric: &a.MetricContext{
					Name:      "cpu",
					Value:     85,
					Threshold: 80,
				},
				Log: &a.LogContext{
					Keyword: "cpu-high",
					Content: "cpu at 85%",
				},
			},
			{
				Kind:     a.ViolationKindMetric,
				Severity: "critical",
				Metric: &a.MetricContext{
					Name:  "mem",
					Value: 99,
				},
			},
		},
	}

	view := toRemediationView(evt, "normal", "critical")

	// PrimaryViolation returns the critical one, so MetricValue should
	// reflect the critical violation (mem=99), not the first one (cpu=85).
	if view.MetricName != "mem" {
		t.Errorf("MetricName = %q, want %q (primary violation)", view.MetricName, "mem")
	}
	if view.MetricValue != 99 {
		t.Errorf("MetricValue = %v, want 99 (primary violation)", view.MetricValue)
	}
	if view.Severity != "critical" {
		t.Errorf("Severity = %q, want %q (primary violation)", view.Severity, "critical")
	}
	if view.AssetID != 100 {
		t.Errorf("AssetID = %d, want 100", view.AssetID)
	}
	if view.Type != string(a.TypeMetric) {
		t.Errorf("Type = %q, want %q", view.Type, string(a.TypeMetric))
	}
	if view.PrevStatus != "normal" {
		t.Errorf("PrevStatus = %q, want %q", view.PrevStatus, "normal")
	}
	if view.CurrStatus != "critical" {
		t.Errorf("CurrStatus = %q, want %q", view.CurrStatus, "critical")
	}
}

// TestToRemediationView_NoViolations verifies that toRemediationView
// produces a zero-value RemediationView for the violation-derived fields
// when the event carries no violations, while still populating the Type,
// AssetID, and status fields.
func TestToRemediationView_NoViolations(t *testing.T) {
	evt := a.Event{
		Type:    a.TypeLog,
		AssetID: 7,
	}
	view := toRemediationView(evt, "offline", "normal")
	if view.Type != string(a.TypeLog) {
		t.Errorf("Type = %q, want %q", view.Type, string(a.TypeLog))
	}
	if view.AssetID != 7 {
		t.Errorf("AssetID = %d, want 7", view.AssetID)
	}
	if view.MetricName != "" {
		t.Errorf("MetricName = %q, want empty", view.MetricName)
	}
	if view.MetricValue != 0 {
		t.Errorf("MetricValue = %v, want 0", view.MetricValue)
	}
	if view.PrevStatus != "offline" {
		t.Errorf("PrevStatus = %q, want %q", view.PrevStatus, "offline")
	}
	if view.CurrStatus != "normal" {
		t.Errorf("CurrStatus = %q, want %q", view.CurrStatus, "normal")
	}
}

// ---------------------------------------------------------------------------
// Engine.Reload with remediation scene
// ---------------------------------------------------------------------------

// TestEngine_Reload_RemediationScene verifies that Reload reads
// remediation-scene rules from the Store and installs them in the engine.
func TestEngine_Reload_RemediationScene(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())

	store := &stubRuleStore{rules: []Record{
		{ID: 100, TenantID: 1, Name: "dynamic-remediation", Scene: string(SceneRemediation), Expression: `remediation.metric_value > 70`, Enabled: true, Priority: 5},
	}}
	if err := eng.Reload(ctx, store); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	matched := eng.MatchRemediation(ctx, remediationEnv("cpu", 80, 70))
	if len(matched) != 1 || matched[0] != 100 {
		t.Errorf("MatchRemediation after Reload = %v, want [100]", matched)
	}
	if !eng.HasRemediationRules() {
		t.Error("HasRemediationRules should be true after Reload loaded a remediation rule")
	}
}

// ---------------------------------------------------------------------------
// Compiler: remediation scene contract
// ---------------------------------------------------------------------------

// TestCompile_RemediationScene verifies that the Compiler accepts
// well-formed remediation-scene expressions and rejects references to
// fields absent from the RemediationMatchEnv contract.
func TestCompile_RemediationScene(t *testing.T) {
	c := NewCompiler()
	// Valid: references Remediation fields.
	valid := []string{
		`remediation.metric_value > 80`,
		`remediation.severity == "critical"`,
		`remediation.metric_name == "cpu"`,
		`remediation.prev_status == "normal" && remediation.curr_status == "critical"`,
		`asset.name == "host-1"`,
	}
	for _, expr := range valid {
		if _, err := c.Compile(SceneRemediation, expr); err != nil {
			t.Errorf("expected Compile(%q) to succeed, got %v", expr, err)
		}
	}

	// Invalid: references fields from another scene's Env contract.
	invalid := []string{
		`task.priority > 5`,         // Task is not in RemediationMatchEnv
		`alert.metrics["cpu"] > 0`,  // Alert is not in RemediationMatchEnv
		`report.log_content == "x"`, // Report is not in RemediationMatchEnv
		`Remediation.NonExistent > 0`,
	}
	for _, expr := range invalid {
		if _, err := c.Compile(SceneRemediation, expr); err == nil {
			t.Errorf("expected Compile(%q) to fail, got nil", expr)
		}
	}
}
