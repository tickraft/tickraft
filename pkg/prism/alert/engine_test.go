// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

// testEnv returns an AlertEnv with the given metric values, mirroring a
// typical metric alert evaluation.
func testEnv(metrics map[string]float64) AlertEnv {
	return AlertEnv{
		Type:     "metric",
		Severity: "critical",
		Source:   "10.0.0.1",
		Metrics:  metrics,
		Asset: map[string]any{
			"id":   int64(42),
			"name": "web-1",
			"type": "host",
			"tags": map[string]string{"env": "prod"},
		},
	}
}

// loadRules installs rules into a fresh engine, failing the test on the
// first compile error.
func loadRules(t *testing.T, rules ...Rule) *Engine {
	t.Helper()
	engine := NewEngine(zap.NewNop())
	if err := engine.Load(context.Background(), rules); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return engine
}

// TestEngineEvaluate covers the single-pass evaluation contract over the
// loaded rule set, including the every-variable contract through real
// rule evaluation.
func TestEngineEvaluate(t *testing.T) {
	engine := loadRules(t,
		Rule{ID: 1, TenantID: 0, Name: "cpu-high", Expression: `metrics["cpu"] > 90`, Enabled: true},
		Rule{
			ID: 2, TenantID: 0, Name: "log-fatal",
			Expression: `type == "log" && keyword contains "fatal"`, Enabled: true,
		},
	)

	matched, violations := engine.Evaluate(context.Background(), 1, testEnv(map[string]float64{"cpu": 95}))
	if len(matched) != 1 || matched[0].ID != 1 {
		t.Errorf("Evaluate(cpu=95) matched %v, want [1]", matched)
	}
	if len(violations) != 1 {
		t.Errorf("Evaluate(cpu=95) violations = %d, want 1", len(violations))
	}

	matched, violations = engine.Evaluate(context.Background(), 1, testEnv(map[string]float64{"cpu": 50}))
	if len(matched) != 0 {
		t.Errorf("Evaluate(cpu=50) matched %v, want no matches", matched)
	}
	if violations != nil {
		t.Errorf("Evaluate(cpu=50) violations = %v, want nil", violations)
	}

	logEnv := AlertEnv{Type: "log", Keyword: "fatal", Metrics: map[string]float64{}}
	matched, _ = engine.Evaluate(context.Background(), 1, logEnv)
	if len(matched) != 1 || matched[0].ID != 2 {
		t.Errorf("Evaluate(log fatal) matched %v, want [2]", matched)
	}
}

// TestEngineNonMetricEventNaturallyFailsMetricRules pins the "Metrics is
// always a non-nil empty map" contract: on a log event a metric rule
// reads missing keys as zero and evaluates to false without erroring.
func TestEngineNonMetricEventNaturallyFailsMetricRules(t *testing.T) {
	engine := loadRules(t,
		Rule{ID: 1, Expression: `metrics["cpu"] > 90`, Enabled: true},
	)
	logEnv := AlertEnv{Type: "log", Keyword: "panic", Metrics: map[string]float64{}}
	matched, _ := engine.Evaluate(context.Background(), 1, logEnv)
	if len(matched) != 0 {
		t.Errorf("metric rule matched log event: %v", matched)
	}
}

// TestEngineTenantFilter verifies the engine-internal tenant filter:
// global rules (TenantID 0) apply to every tenant, scoped rules only to
// their own tenant.
func TestEngineTenantFilter(t *testing.T) {
	engine := loadRules(t,
		Rule{ID: 1, TenantID: 0, Expression: `severity == "critical"`, Enabled: true},
		Rule{ID: 2, TenantID: 5, Expression: `severity == "critical"`, Enabled: true},
	)

	// Tenant 5 sees both the global and its own scoped rule.
	matched, _ := engine.Evaluate(context.Background(), 5, testEnv(nil))
	if len(matched) != 2 {
		t.Errorf("tenant 5: matched %v, want both rules", matched)
	}

	// Tenant 7 sees only the global rule.
	matched, _ = engine.Evaluate(context.Background(), 7, testEnv(nil))
	if len(matched) != 1 || matched[0].ID != 1 {
		t.Errorf("tenant 7: matched %v, want [1] (global only)", matched)
	}
}

// TestEngineBadRuleSkipped verifies warn-and-skip semantics: a rule with
// an invalid expression is dropped at Load time without affecting
// sibling rules.
func TestEngineBadRuleSkipped(t *testing.T) {
	engine := loadRules(t,
		Rule{ID: 1, Expression: `metrics["cpu"] >`, Enabled: true},    // syntax error
		Rule{ID: 2, Expression: `unknownvar > 1`, Enabled: true},      // unknown name
		Rule{ID: 3, Expression: `metrics["cpu"] > 90`, Enabled: true}, // valid
	)
	if !engine.HasRules() {
		t.Fatal("HasRules = false, want true (valid rule loaded)")
	}
	matched, _ := engine.Evaluate(context.Background(), 1, testEnv(map[string]float64{"cpu": 95}))
	if len(matched) != 1 || matched[0].ID != 3 {
		t.Errorf("Evaluate matched %v, want [3] (invalid rules skipped)", matched)
	}
}

// TestEngineDisabledRuleSkipped verifies that a disabled rule never
// matches even when its expression would evaluate true.
func TestEngineDisabledRuleSkipped(t *testing.T) {
	engine := loadRules(t,
		Rule{ID: 1, Expression: `metrics["cpu"] > 90`, Enabled: false},
	)
	matched, _ := engine.Evaluate(context.Background(), 1, testEnv(map[string]float64{"cpu": 95}))
	if len(matched) != 0 {
		t.Errorf("disabled rule matched: %v", matched)
	}
}

// TestEngineEmpty verifies the empty-engine contract used by
// AlertMatcher's default-allow semantics.
func TestEngineEmpty(t *testing.T) {
	engine := loadRules(t)
	if engine.HasRules() {
		t.Error("HasRules = true on empty engine, want false")
	}
	matched, violations := engine.Evaluate(context.Background(), 1, testEnv(nil))
	if matched != nil {
		t.Errorf("Evaluate on empty engine matched %v, want nil", matched)
	}
	if violations != nil {
		t.Errorf("Evaluate on empty engine violations = %v, want nil", violations)
	}
}

// TestEngineEvaluateViolations covers the structured-violation outcome:
// conjunction rules contribute one Violation per comparison, disjunction
// rules only the branches that hold, and every Violation preserves the
// env's severity and source (D-14).
func TestEngineEvaluateViolations(t *testing.T) {
	t.Run("conjunction yields one violation per comparison", func(t *testing.T) {
		engine := loadRules(t,
			Rule{ID: 1, Expression: `metrics["cpu"] > 90 && metrics["mem"] > 80`, Enabled: true},
		)
		_, violations := engine.Evaluate(context.Background(), 1,
			testEnv(map[string]float64{"cpu": 95, "mem": 85}))
		if len(violations) != 2 {
			t.Fatalf("violations = %d, want 2", len(violations))
		}
		for _, v := range violations {
			if v.Kind != "metric" {
				t.Errorf("Kind = %q, want metric", v.Kind)
			}
			if v.Severity != "critical" {
				t.Errorf("Severity = %q, want critical (env severity preserved)", v.Severity)
			}
			if v.Source != "10.0.0.1" {
				t.Errorf("Source = %q, want 10.0.0.1 (env source preserved)", v.Source)
			}
			if v.Metric == nil {
				t.Fatal("Metric context missing")
			}
		}
		// Both cpu and mem violations carry value and threshold.
		byName := map[string]float64{}
		for _, v := range violations {
			byName[v.Metric.Name] = v.Metric.Value
			if v.Metric.Threshold != 90 && v.Metric.Threshold != 80 {
				t.Errorf("metric %q threshold = %v, want 90 or 80", v.Metric.Name, v.Metric.Threshold)
			}
		}
		if byName["cpu"] != 95 || byName["mem"] != 85 {
			t.Errorf("values = %v, want cpu=95 mem=85", byName)
		}
	})

	t.Run("disjunction yields only the branch that holds", func(t *testing.T) {
		engine := loadRules(t,
			Rule{ID: 1, Expression: `metrics["cpu"] > 90 || metrics["disk"] > 95`, Enabled: true},
		)
		_, violations := engine.Evaluate(context.Background(), 1,
			testEnv(map[string]float64{"cpu": 95, "disk": 50}))
		if len(violations) != 1 {
			t.Fatalf("violations = %d, want 1 (only the cpu branch)", len(violations))
		}
		if violations[0].Metric.Name != "cpu" {
			t.Errorf("violation metric = %q, want cpu", violations[0].Metric.Name)
		}
	})

	t.Run("single comparison yields one violation", func(t *testing.T) {
		engine := loadRules(t,
			Rule{ID: 1, Expression: `metrics["cpu"] > 90`, Enabled: true},
		)
		_, violations := engine.Evaluate(context.Background(), 1,
			testEnv(map[string]float64{"cpu": 95}))
		if len(violations) != 1 {
			t.Fatalf("violations = %d, want 1", len(violations))
		}
	})

	t.Run("non-comparison rule matches without violations", func(t *testing.T) {
		engine := loadRules(t,
			Rule{ID: 1, Expression: `keyword contains "fatal"`, Enabled: true},
		)
		logEnv := AlertEnv{Type: "log", Keyword: "fatal", Metrics: map[string]float64{}}
		matched, violations := engine.Evaluate(context.Background(), 1, logEnv)
		if len(matched) != 1 {
			t.Fatalf("matched = %v, want rule matched", matched)
		}
		if violations != nil {
			t.Errorf("violations = %v, want nil (no comparisons)", violations)
		}
	})

	t.Run("no match yields nil", func(t *testing.T) {
		engine := loadRules(t,
			Rule{ID: 1, Expression: `metrics["cpu"] > 90`, Enabled: true},
		)
		matched, violations := engine.Evaluate(context.Background(), 1,
			testEnv(map[string]float64{"cpu": 50}))
		if matched != nil || violations != nil {
			t.Errorf("Evaluate = %v, %v, want nil, nil", matched, violations)
		}
	})
}

// fakeLister is an in-memory Lister used to exercise Reload.
type fakeLister struct {
	rules []Rule
}

func (f *fakeLister) ListEnabled(_ context.Context, _ int64) ([]Rule, error) {
	return f.rules, nil
}

// TestEngineReload verifies that Reload replaces the in-memory rule set
// with the store's enabled rules.
func TestEngineReload(t *testing.T) {
	engine := NewEngine(zap.NewNop())
	lister := &fakeLister{rules: []Rule{
		{ID: 11, Name: "r1", Expression: `metrics["cpu"] > 90`, Enabled: true, Priority: 10},
	}}
	if err := engine.Reload(context.Background(), lister); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	matched, _ := engine.Evaluate(context.Background(), 1, testEnv(map[string]float64{"cpu": 95}))
	if len(matched) != 1 || matched[0].ID != 11 {
		t.Fatalf("after reload: matched %v, want [11]", matched)
	}

	// A second reload with an empty store clears the rule set.
	lister.rules = nil
	if err := engine.Reload(context.Background(), lister); err != nil {
		t.Fatalf("Reload (empty): %v", err)
	}
	if engine.HasRules() {
		t.Error("HasRules after empty reload = true, want false")
	}
}

// TestEngineStopIdempotent verifies Stop is safe on an engine whose
// reload loop was never started (reloadCancel nil) and can be called
// twice.
func TestEngineStopIdempotent(t *testing.T) {
	engine := NewEngine(zap.NewNop())
	if err := engine.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := engine.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

// TestEngineEvaluateConcurrent exercises the read-lock snapshot path with
// concurrent evaluators, guarding against data races when run with
// -race.
func TestEngineEvaluateConcurrent(t *testing.T) {
	engine := loadRules(t,
		Rule{ID: 1, Expression: `metrics["cpu"] > 90`, Enabled: true},
		Rule{ID: 2, Expression: `severity == "critical"`, Enabled: true},
	)
	done := make(chan struct{})
	for range 4 {
		go func() {
			defer func() { done <- struct{}{} }()
			for range 50 {
				engine.Evaluate(context.Background(), 1, testEnv(map[string]float64{"cpu": 95}))
			}
		}()
	}
	for range 4 {
		<-done
	}
}
