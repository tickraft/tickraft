// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/types"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestMetricMatcher_WithPrismEngine verifies that MetricMatcher plugs into
// the open-source alert.Engine via the alert.Matcher interface and acts as a
// pre-filter: alerts matching a metric-scene rule are forwarded, while
// non-matching alerts are suppressed.
//
// Expression field identifiers mirror the expr struct tags on the *View types
// in env.go (e.g. AlertView.Metrics is tagged `expr:"metrics"`), so expressions
// reference snake_case names: alert.metrics["cpu"], alert.severity. The
// expr-lang field lookup is case-sensitive and keyed on the tag value.
func TestMetricMatcher_WithPrismEngine(t *testing.T) {
	ctx := context.Background()

	eng := NewEngine(zap.NewNop())
	rules := []Rule{
		{
			ID:         1,
			TenantID:   1,
			Name:       "high-cpu-critical",
			Scene:      SceneMetric,
			Expression: `alert.metrics["cpu"] > 80 && alert.severity == "critical"`,
			Priority:   10,
			Enabled:    true,
		},
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load rules: %v", err)
	}

	metricMatcher := NewMetricMatcher(eng, nil)

	target := mustNewTarget(t)
	target.AddRule(metricMatcher)

	// The prism engine should expose the matcher through Rules().
	registered := target.Rules()
	if len(registered) != 1 {
		t.Fatalf("expected 1 rule registered, got %d", len(registered))
	}
	if _, ok := registered[0].(*MetricMatcher); !ok {
		t.Fatalf("registered rule is not *MetricMatcher, got %T", registered[0])
	}

	// Case 1: cpu=90, severity="critical" -> rule matches -> forward.
	alertHit := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   100,
		TenantID:  1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:     alert.ViolationKindMetric,
			Severity: "critical",
			Metric: &alert.MetricContext{
				Name:    "cpu_usage",
				Value:   90,
				Metrics: map[string]float64{"cpu": 90},
			},
		}},
	}
	if !metricMatcher.Match(ctx, alertHit) {
		t.Error("expected Match=true for cpu=90 severity=critical, got false")
	}

	// Case 2: cpu=50, severity="critical" -> value below threshold -> suppress.
	alertLowValue := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   100,
		TenantID:  1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:     alert.ViolationKindMetric,
			Severity: "critical",
			Metric: &alert.MetricContext{
				Name:    "cpu_usage",
				Value:   50,
				Metrics: map[string]float64{"cpu": 50},
			},
		}},
	}
	if metricMatcher.Match(ctx, alertLowValue) {
		t.Error("expected Match=false for cpu=50, got true")
	}

	// Case 3: cpu=90, severity="warning" -> severity mismatch -> suppress.
	alertWrongSeverity := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   100,
		TenantID:  1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:     alert.ViolationKindMetric,
			Severity: "warning",
			Metric: &alert.MetricContext{
				Name:    "cpu_usage",
				Value:   90,
				Metrics: map[string]float64{"cpu": 90},
			},
		}},
	}
	if metricMatcher.Match(ctx, alertWrongSeverity) {
		t.Error("expected Match=false for severity=warning, got true")
	}
}

// TestProbeMatcher_AsCollectorProcessor verifies that ProbeMatcher satisfies
// the telemetry.Processor contract: it registers as a generic processor
// (Type returns "") and emits alerts when probe-scene rules match the report.
//
// "contains" is an expr-lang binary operator (not a builtin function), so the
// operator form `report.log_content contains "error"` is used; it remains
// available under expr.DisableAllBuiltins because operators are not builtins.
func TestProbeMatcher_AsCollectorProcessor(t *testing.T) {
	ctx := context.Background()

	eng := NewEngine(zap.NewNop())
	rules := []Rule{
		{
			ID:         10,
			TenantID:   1,
			Name:       "log-error-detect",
			Scene:      SceneProbe,
			Expression: `report.log_content contains "error"`,
			Priority:   5,
			Enabled:    true,
		},
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load rules: %v", err)
	}

	probeMatcher := NewProbeMatcher(eng, nil, nil, zap.NewNop())

	if got := probeMatcher.Type(); got != "" {
		t.Errorf("expected empty Type for generic processor, got %q", got)
	}

	// Case 1: LogContent contains "error" -> rule matches -> alerts emitted.
	reportHit := &telemetry.Telemetry{
		AssetID:     200,
		TenantID:    1,
		AssetType:   types.AssetTypeHost,
		SourceType:  "syslog",
		CollectedAt: time.Now(),
		LogContent:  "disk error occurred on /dev/sda1",
		LogLevel:    "ERROR",
	}
	result, err := probeMatcher.Process(ctx, reportHit)
	if err != nil {
		t.Fatalf("Process matched report: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil ProcessResult for matched report")
	}
	if len(result.Alerts) == 0 {
		t.Error("expected non-empty Alerts for matched report, got empty")
	}

	// Case 2: LogContent clean -> no rule match -> no alerts.
	reportClean := &telemetry.Telemetry{
		AssetID:     200,
		TenantID:    1,
		AssetType:   types.AssetTypeHost,
		SourceType:  "syslog",
		CollectedAt: time.Now(),
		LogContent:  "all good, system healthy",
		LogLevel:    "INFO",
	}
	resultClean, err := probeMatcher.Process(ctx, reportClean)
	if err != nil {
		t.Fatalf("Process clean report: %v", err)
	}
	if resultClean == nil {
		t.Fatal("expected non-nil ProcessResult for clean report")
	}
	if len(resultClean.Alerts) != 0 {
		t.Errorf("expected empty Alerts for clean report, got %d", len(resultClean.Alerts))
	}
}

// TestStore_CRUDFlow exercises the full Store lifecycle: create, get,
// update, reload into the engine, delete, and reload again, verifying that
// the engine's compiled rule cache stays consistent with the persisted state.
func TestStore_CRUDFlow(t *testing.T) {
	ctx := context.Background()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	compiler := NewCompiler()
	store := NewStore(db, compiler)
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Create.
	created := &Record{
		TenantID:   1,
		Name:       "cpu-high",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 80`,
		Enabled:    true,
		Priority:   10,
	}
	if err := store.Create(ctx, created); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected auto-incremented ID after Create")
	}

	// Get returns the same fields.
	got, err := store.Get(ctx, created.ID, created.TenantID)
	if err != nil {
		t.Fatalf("Get after Create: %v", err)
	}
	if got.Name != created.Name || got.Expression != `alert.metrics["cpu"] > 80` || got.Scene != string(SceneMetric) {
		t.Errorf("Get mismatch: got=%+v", got)
	}

	// Update the expression.
	got.Expression = `alert.metrics["cpu"] > 90`
	got.Priority = 20
	if err := store.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Get reflects the update.
	gotUpdated, err := store.Get(ctx, created.ID, created.TenantID)
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if gotUpdated.Expression != `alert.metrics["cpu"] > 90` {
		t.Errorf("expected updated expression, got %q", gotUpdated.Expression)
	}
	if gotUpdated.Priority != 20 {
		t.Errorf("expected updated priority 20, got %d", gotUpdated.Priority)
	}

	// Reload into the engine; the new expression (> 90) matches 95 but not 85.
	eng := NewEngine(zap.NewNop())
	if err := eng.Reload(ctx, store); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	envHit := MetricMatchEnv{
		Alert: AlertView{Metrics: map[string]float64{"cpu": 95}},
	}
	matched := eng.MatchMetric(ctx, envHit)
	if len(matched) != 1 || matched[0] != created.ID {
		t.Errorf("expected match on rule ID %d for value=95, got %v", created.ID, matched)
	}

	envMiss := MetricMatchEnv{
		Alert: AlertView{Metrics: map[string]float64{"cpu": 85}},
	}
	if matchedMiss := eng.MatchMetric(ctx, envMiss); len(matchedMiss) != 0 {
		t.Errorf("expected no match for value=85 with expr > 90, got %v", matchedMiss)
	}

	// Delete (soft-delete).
	if err := store.Delete(ctx, created.ID, created.TenantID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Get returns ErrRuleNotFound.
	if _, err := store.Get(ctx, created.ID, created.TenantID); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("expected ErrRuleNotFound after Delete, got %v", err)
	}

	// Reload again; the engine should no longer match.
	if err := eng.Reload(ctx, store); err != nil {
		t.Fatalf("Reload after Delete: %v", err)
	}
	if matchedAfterDelete := eng.MatchMetric(ctx, envHit); len(matchedAfterDelete) != 0 {
		t.Errorf("expected no match after rule deletion, got %v", matchedAfterDelete)
	}
}

// TestEngine_MultiSceneLoad verifies that the engine groups rules by scene on
// load and that each Match method only evaluates its own scene's rules.
func TestEngine_MultiSceneLoad(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())

	rules := []Rule{
		// 1 task rule.
		{ID: 1, TenantID: 1, Name: "task-priority", Scene: SceneTask, Expression: "task.priority > 5", Priority: 10, Enabled: true},
		// 2 probe rules.
		{ID: 2, TenantID: 1, Name: "probe-error", Scene: SceneProbe, Expression: `report.log_content contains "error"`, Priority: 10, Enabled: true},
		{ID: 3, TenantID: 1, Name: "probe-warn", Scene: SceneProbe, Expression: `report.log_content contains "warn"`, Priority: 5, Enabled: true},
		// 3 metric rules.
		{ID: 4, TenantID: 1, Name: "metric-cpu", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Priority: 10, Enabled: true},
		{ID: 5, TenantID: 1, Name: "metric-mem", Scene: SceneMetric, Expression: `alert.metrics["mem"] > 90`, Priority: 5, Enabled: true},
		{ID: 6, TenantID: 1, Name: "metric-disk", Scene: SceneMetric, Expression: `alert.metrics["disk"] > 70`, Priority: 1, Enabled: true},
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// MatchTask: Priority=8 > 5 -> 1 hit.
	taskEnv := TaskMatchEnv{
		Task: TaskView{ID: 100, Priority: 8, ExecutorType: "http"},
	}
	taskMatched := eng.MatchTask(ctx, taskEnv)
	if len(taskMatched) != 1 {
		t.Errorf("expected 1 task match, got %d (%v)", len(taskMatched), taskMatched)
	}

	// MatchProbe: content contains both "error" and "warn" -> 2 hits.
	probeEnv := ProbeMatchEnv{
		Report: ReportView{LogContent: "error and warn detected"},
	}
	probeMatched := eng.MatchProbe(ctx, probeEnv)
	if len(probeMatched) != 2 {
		t.Errorf("expected 2 probe matches, got %d (%v)", len(probeMatched), probeMatched)
	}

	// MatchMetric: cpu=95 matches all three (>80, >90, >70) -> 3 hits.
	metricEnv := MetricMatchEnv{
		Alert: AlertView{Metrics: map[string]float64{"cpu": 95, "mem": 95, "disk": 95}},
	}
	metricMatched := eng.MatchMetric(ctx, metricEnv)
	if len(metricMatched) != 3 {
		t.Errorf("expected 3 metric matches, got %d (%v)", len(metricMatched), metricMatched)
	}

	// Scene isolation: a low metric value matches no metric rule, and the task
	// rule is never evaluated against the metric env.
	metricEnvLow := MetricMatchEnv{
		Alert: AlertView{Metrics: map[string]float64{"cpu": 10}},
	}
	if matchedLow := eng.MatchMetric(ctx, metricEnvLow); len(matchedLow) != 0 {
		t.Errorf("expected 0 metric matches for value=10, got %v", matchedLow)
	}
}
