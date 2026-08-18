// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/asset"
	a "github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/types"
)

// metricEnv builds a MetricMatchEnv carrying the supplied related-metric values
// on the Alert view. The Violation extractor resolves metric names from
// alert.metrics[<key>] map index access in the rule expression.
func metricEnv(metrics map[string]float64) MetricMatchEnv {
	return MetricMatchEnv{
		Alert: AlertView{
			Metrics: metrics,
		},
	}
}

// assertMetricViolation is a small helper that verifies the Kind field
// shared by every metric violation produced by the extractor.
func assertMetricViolation(t *testing.T, v a.Violation) {
	t.Helper()
	if v.Kind != a.ViolationKindMetric {
		t.Errorf("Kind = %q, want %q", v.Kind, a.ViolationKindMetric)
	}
	if v.Metric == nil {
		t.Fatalf("Metric context is nil for metric violation")
	}
}

// ---------------------------------------------------------------------------
// ViolationExtractor
// ---------------------------------------------------------------------------

// TestViolationExtractor_CompoundRule verifies that a compound rule
// `alert.metrics["cpu"] > 90 && alert.metrics["mem"] > 85` yields two
// Violations — one per matched condition — when both conditions hold.
func TestViolationExtractor_CompoundRule(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rule := Rule{
		ID:         1,
		Name:       "cpu-and-mem",
		Scene:      SceneMetric,
		Expression: `alert.metrics["cpu"] > 90 && alert.metrics["mem"] > 85`,
		Enabled:    true,
	}
	if err := eng.Load(ctx, []Rule{rule}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	env := metricEnv(map[string]float64{"cpu": 95, "mem": 88})
	violations := eng.MatchMetricWithViolations(ctx, env)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d: %+v", len(violations), violations)
	}

	byMetric := map[string]a.Violation{}
	for _, v := range violations {
		assertMetricViolation(t, v)
		byMetric[v.Metric.Name] = v
	}
	cpu, ok := byMetric["cpu"]
	if !ok {
		t.Fatalf("missing cpu violation, got %+v", byMetric)
	}
	if cpu.Metric.Value != 95 || cpu.Metric.Threshold != 90 {
		t.Errorf("cpu violation = {Value: %v, Threshold: %v}, want {95, 90}", cpu.Metric.Value, cpu.Metric.Threshold)
	}
	mem, ok := byMetric["mem"]
	if !ok {
		t.Fatalf("missing mem violation, got %+v", byMetric)
	}
	if mem.Metric.Value != 88 || mem.Metric.Threshold != 85 {
		t.Errorf("mem violation = {Value: %v, Threshold: %v}, want {88, 85}", mem.Metric.Value, mem.Metric.Threshold)
	}
}

// TestViolationExtractor_SingleCondition verifies that a single-condition
// rule `alert.metrics["cpu"] > 80` yields exactly one Violation when it matches.
func TestViolationExtractor_SingleCondition(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rule := Rule{
		ID:         2,
		Name:       "cpu-high",
		Scene:      SceneMetric,
		Expression: `alert.metrics["cpu"] > 80`,
		Enabled:    true,
	}
	if err := eng.Load(ctx, []Rule{rule}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	env := metricEnv(map[string]float64{"cpu": 90})
	violations := eng.MatchMetricWithViolations(ctx, env)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(violations), violations)
	}
	v := violations[0]
	assertMetricViolation(t, v)
	if v.Metric.Name != "cpu" {
		t.Errorf("Metric.Name = %q, want %q", v.Metric.Name, "cpu")
	}
	if v.Metric.Value != 90 {
		t.Errorf("Metric.Value = %v, want 90", v.Metric.Value)
	}
	if v.Metric.Threshold != 80 {
		t.Errorf("Metric.Threshold = %v, want 80", v.Metric.Threshold)
	}
}

// TestViolationExtractor_NoMatch verifies that a non-matching rule
// yields no violations.
func TestViolationExtractor_NoMatch(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rule := Rule{
		ID:         3,
		Name:       "cpu-high",
		Scene:      SceneMetric,
		Expression: `alert.metrics["cpu"] > 80`,
		Enabled:    true,
	}
	if err := eng.Load(ctx, []Rule{rule}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// cpu=50 < 80: the rule does not match, so no violations.
	env := metricEnv(map[string]float64{"cpu": 50})
	if got := eng.MatchMetricWithViolations(ctx, env); got != nil {
		t.Errorf("expected nil for non-matching rule, got %+v", got)
	}
}

// TestViolationExtractor_OrPartialMatch verifies that an OR rule yields
// one Violation for the matched branch only, not for the non-matching
// branch. This confirms each comparison is evaluated independently rather
// than assuming the whole-expression match implies all sub-conditions hold.
func TestViolationExtractor_OrPartialMatch(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rule := Rule{
		ID:         4,
		Name:       "cpu-or-mem",
		Scene:      SceneMetric,
		Expression: `alert.metrics["cpu"] > 90 || alert.metrics["mem"] > 85`,
		Enabled:    true,
	}
	if err := eng.Load(ctx, []Rule{rule}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// cpu=95 matches the first branch; mem=70 does not match the second.
	// Only the cpu violation should be returned.
	env := metricEnv(map[string]float64{"cpu": 95, "mem": 70})
	violations := eng.MatchMetricWithViolations(ctx, env)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for the matched OR branch, got %d: %+v", len(violations), violations)
	}
	v := violations[0]
	assertMetricViolation(t, v)
	if v.Metric.Name != "cpu" {
		t.Errorf("Metric.Name = %q, want %q", v.Metric.Name, "cpu")
	}
	if v.Metric.Value != 95 || v.Metric.Threshold != 90 {
		t.Errorf("violation = {Value: %v, Threshold: %v}, want {95, 90}", v.Metric.Value, v.Metric.Threshold)
	}
}

// TestViolationExtractor_MultipleRules aggregates violations across two
// matching rules so the caller receives a flat slice covering every rule.
func TestViolationExtractor_MultipleRules(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rules := []Rule{
		{ID: 10, Name: "cpu", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 90`, Enabled: true},
		{ID: 11, Name: "mem", Scene: SceneMetric, Expression: `alert.metrics["mem"] > 85`, Enabled: true},
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load: %v", err)
	}

	env := metricEnv(map[string]float64{"cpu": 95, "mem": 88})
	violations := eng.MatchMetricWithViolations(ctx, env)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations across 2 rules, got %d: %+v", len(violations), violations)
	}
}

// TestViolationExtractor_NoRules verifies that an engine with no metric
// rules returns nil rather than panicking.
func TestViolationExtractor_NoRules(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	env := metricEnv(map[string]float64{"cpu": 99})
	if got := eng.MatchMetricWithViolations(ctx, env); got != nil {
		t.Errorf("expected nil for empty rule set, got %+v", got)
	}
}

// TestViolationExtractor_DisabledRuleSkipped verifies that a disabled
// rule does not contribute violations even when its expression would
// match the env.
func TestViolationExtractor_DisabledRuleSkipped(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rules := []Rule{
		{ID: 1, Name: "enabled", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Enabled: true},
		{ID: 2, Name: "disabled", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Enabled: false},
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load: %v", err)
	}

	env := metricEnv(map[string]float64{"cpu": 90})
	violations := eng.MatchMetricWithViolations(ctx, env)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation (disabled rule skipped), got %d: %+v", len(violations), violations)
	}
}

// TestViolationExtractor_Operators verifies that every supported
// comparison operator produces a Violation whose Value and Threshold
// resolve from the left and right operands respectively.
func TestViolationExtractor_Operators(t *testing.T) {
	cases := []struct {
		name      string
		expr      string
		value     float64
		threshold float64
	}{
		{"gt", `alert.metrics["cpu"] > 80`, 90, 80},
		{"gte", `alert.metrics["cpu"] >= 80`, 80, 80},
		{"lt", `alert.metrics["cpu"] < 80`, 70, 80},
		{"lte", `alert.metrics["cpu"] <= 80`, 80, 80},
		{"eq", `alert.metrics["cpu"] == 80`, 80, 80},
		{"ne", `alert.metrics["cpu"] != 80`, 90, 80},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			eng := NewEngine(zap.NewNop())
			rule := Rule{
				ID:         1,
				Name:       tc.name,
				Scene:      SceneMetric,
				Expression: tc.expr,
				Enabled:    true,
			}
			if err := eng.Load(ctx, []Rule{rule}); err != nil {
				t.Fatalf("Load: %v", err)
			}
			env := metricEnv(map[string]float64{"cpu": tc.value})
			violations := eng.MatchMetricWithViolations(ctx, env)
			if len(violations) != 1 {
				t.Fatalf("expected 1 violation, got %d: %+v", len(violations), violations)
			}
			assertMetricViolation(t, violations[0])
			if violations[0].Metric.Threshold != tc.threshold {
				t.Errorf("Metric.Threshold = %v, want %v", violations[0].Metric.Threshold, tc.threshold)
			}
			if violations[0].Metric.Value != tc.value {
				t.Errorf("Metric.Value = %v, want %v", violations[0].Metric.Value, tc.value)
			}
		})
	}
}

// TestViolationExtractor_SubProgramCacheReused verifies that the
// extractor caches sub-programs by source text so repeated matches do not
// recompile, and that Load resets the cache.
func TestViolationExtractor_SubProgramCacheReused(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	if err := eng.Load(ctx, []Rule{
		{ID: 1, Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Enabled: true},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	env := metricEnv(map[string]float64{"cpu": 90})
	// First match populates the cache.
	if got := eng.MatchMetricWithViolations(ctx, env); len(got) != 1 {
		t.Fatalf("first match expected 1 violation, got %d", len(got))
	}
	eng.extractor.mu.Lock()
	firstCacheSize := len(eng.extractor.subPrograms)
	eng.extractor.mu.Unlock()
	if firstCacheSize == 0 {
		t.Fatal("expected sub-programs cached after first match")
	}

	// Repeated matches must not grow the cache.
	for range 5 {
		_ = eng.MatchMetricWithViolations(ctx, env)
	}
	eng.extractor.mu.Lock()
	gotCacheSize := len(eng.extractor.subPrograms)
	eng.extractor.mu.Unlock()
	if gotCacheSize != firstCacheSize {
		t.Errorf("cache size changed across matches: %d -> %d", firstCacheSize, gotCacheSize)
	}

	// Load resets the cache so stale sub-programs are released.
	if err := eng.Load(ctx, []Rule{
		{ID: 1, Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Enabled: true},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	eng.extractor.mu.Lock()
	afterReload := len(eng.extractor.subPrograms)
	eng.extractor.mu.Unlock()
	if afterReload != 0 {
		t.Errorf("expected cache cleared after Load, got %d entries", afterReload)
	}
}

// TestViolationExtractor_NonComparisonExpression verifies that a rule
// whose expression is not a numeric threshold comparison (e.g. a string
// equality) matches but yields a Violation whose numeric Value/Threshold
// stay zero because the operands are not numeric.
func TestViolationExtractor_NonComparisonExpression(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	if err := eng.Load(ctx, []Rule{
		{ID: 1, Scene: SceneMetric, Expression: `alert.severity == "critical"`, Enabled: true},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// `alert.severity == "critical"` is a comparison, but the operands are
	// strings (not numeric), so toFloat64 coercion fails and the value
	// fields stay zero.
	env := MetricMatchEnv{Alert: AlertView{Severity: "critical"}}
	violations := eng.MatchMetricWithViolations(ctx, env)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(violations), violations)
	}
	v := violations[0]
	assertMetricViolation(t, v)
	// Value/Threshold stay zero because the operands are not numeric.
	if v.Metric.Value != 0 {
		t.Errorf("Metric.Value = %v, want 0 (non-numeric operand)", v.Metric.Value)
	}
	if v.Metric.Threshold != 0 {
		t.Errorf("Metric.Threshold = %v, want 0 (non-numeric operand)", v.Metric.Threshold)
	}
}

// TestViolationExtractor_Concurrent verifies that concurrent
// MatchMetricWithViolations invocations are race-free, exercising the
// extractor's mutex-guarded sub-program cache under contention. Run
// with `go test -race` to detect data races.
func TestViolationExtractor_Concurrent(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rules := make([]Rule, 0, 10)
	for i := range 10 {
		rules = append(rules, Rule{
			ID:         int64(i + 1),
			Scene:      SceneMetric,
			Expression: `alert.metrics["cpu"] > 90 && alert.metrics["mem"] > 85`,
			Priority:   100 - i,
			Enabled:    true,
		})
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load: %v", err)
	}

	env := metricEnv(map[string]float64{"cpu": 95, "mem": 88})

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	start := make(chan struct{})
	for range goroutines {
		go func() {
			defer wg.Done()
			<-start
			for range 20 {
				violations := eng.MatchMetricWithViolations(ctx, env)
				// Each of the 10 rules matches with 2 conditions, so 20
				// violations per call. Intentionally not t.Errorf to
				// avoid concurrent writes to test output; the race
				// detector is the real oracle.
				_ = violations
			}
		}()
	}
	close(start)
	wg.Wait()
}

// ---------------------------------------------------------------------------
// MetricMatcher.MatchWithViolations
// ---------------------------------------------------------------------------

// TestMetricMatcher_MatchWithViolations_Compound verifies the
// end-to-end path: MetricMatcher projects an alert.Event into a
// MetricMatchEnv and returns one Violation per matched condition.
func TestMetricMatcher_MatchWithViolations_Compound(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	if err := eng.Load(ctx, []Rule{
		{
			ID:         1,
			Name:       "cpu-and-mem",
			Scene:      SceneMetric,
			Expression: `alert.metrics["cpu"] > 90 && alert.metrics["mem"] > 85`,
			Enabled:    true,
		},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	matcher := NewMetricMatcher(eng, nil)

	evt := a.Event{
		Type:      a.TypeMetric,
		AssetID:   100,
		TenantID:  1,
		Timestamp: time.Now(),
		Violations: []a.Violation{{
			Kind: a.ViolationKindMetric,
			Metric: &a.MetricContext{
				Name:    "system_load",
				Metrics: map[string]float64{"cpu": 95, "mem": 88},
			},
		}},
	}
	violations := matcher.MatchWithViolations(ctx, evt)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d: %+v", len(violations), violations)
	}
}

// TestMetricMatcher_MatchWithViolations_NoRules verifies the
// default-allow semantics: when no metric rules are loaded,
// MatchWithViolations returns nil so callers fall back to the
// single-value Event fields.
func TestMetricMatcher_MatchWithViolations_NoRules(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	matcher := NewMetricMatcher(eng, nil)

	evt := metricEvent("cpu", 99, "critical")
	if got := matcher.MatchWithViolations(ctx, evt); got != nil {
		t.Errorf("expected nil when no metric rules are loaded, got %+v", got)
	}
}

// TestMetricMatcher_MatchWithViolations_NoMatch verifies that a
// non-matching alert yields no violations.
func TestMetricMatcher_MatchWithViolations_NoMatch(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	if err := eng.Load(ctx, []Rule{
		{ID: 1, Name: "cpu-high", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Enabled: true},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	matcher := NewMetricMatcher(eng, nil)

	evt := metricEvent("cpu", 50, "warning")
	if got := matcher.MatchWithViolations(ctx, evt); got != nil {
		t.Errorf("expected nil for non-matching alert, got %+v", got)
	}
}

// TestMetricMatcher_MatchWithViolations_AssetEnrichment verifies that
// when an asset store is configured, the env is enriched with the asset
// so rules referencing Asset.* fields can match and produce violations.
func TestMetricMatcher_MatchWithViolations_AssetEnrichment(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	if err := eng.Load(ctx, []Rule{
		// Two comparison sub-conditions: a numeric metric threshold and a
		// string equality on asset.name. Both yield Violations; only the
		// numeric one has non-zero Value/Threshold.
		{
			ID:         1,
			Name:       "host-and-cpu",
			Scene:      SceneMetric,
			Expression: `asset.name == "my-host" && alert.metrics["cpu"] > 80`,
			Enabled:    true,
		},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	store := &stubAssetStore{asset: &asset.Asset{
		ID:        100,
		TenantID:  1,
		AssetType: types.AssetTypeHost,
		Name:      "my-host",
		Status:    types.AssetStatusNormal,
	}}
	matcher := NewMetricMatcher(eng, store)

	evt := metricEvent("cpu", 90, "critical")
	evt.AssetID = 100
	violations := matcher.MatchWithViolations(ctx, evt)
	// Two comparison sub-conditions: asset.name == "my-host" and
	// alert.metrics["cpu"] > 80. Both match, so two Violations.
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d: %+v", len(violations), violations)
	}
}

// TestMetricMatcher_ImplementsViolationMatcher verifies the
// alert.ViolationMatcher SPI is satisfied at compile time and reachable via a
// type assertion from alert.Matcher.
func TestMetricMatcher_ImplementsViolationMatcher(t *testing.T) {
	eng := NewEngine(zap.NewNop())
	matcher := NewMetricMatcher(eng, nil)

	var am a.Matcher = matcher
	vm, ok := am.(a.ViolationMatcher)
	if !ok {
		t.Fatalf("expected *MetricMatcher to satisfy alert.ViolationMatcher via alert.Matcher, got %T", am)
	}
	// Exercise the SPI once to ensure the assertion is live, not just
	// compile-time.
	if got := vm.MatchWithViolations(context.Background(), a.Event{}); got != nil {
		t.Errorf("expected nil for empty rule set via SPI, got %+v", got)
	}
}
