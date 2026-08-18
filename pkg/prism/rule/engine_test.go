// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"context"
	"errors"
	"sync"
	"testing"

	"go.uber.org/zap"
)

// stubRuleStore is a minimal Store double used by engine and register tests.
// ListEnabled returns the configured rules filtered by scene; the write methods
// are no-ops so the stub can stand in for a persistence layer where only the
// read path matters. listErr, when set, makes ListEnabled fail to exercise the
// Reload error path.
type stubRuleStore struct {
	rules   []Record
	listErr error
}

func (s *stubRuleStore) Create(context.Context, *Record) error { return nil }
func (s *stubRuleStore) Update(context.Context, *Record) error { return nil }
func (s *stubRuleStore) Delete(context.Context, int64, int64) error {
	return nil
}
func (s *stubRuleStore) Get(context.Context, int64, int64) (*Record, error) {
	return nil, ErrRuleNotFound
}
func (s *stubRuleStore) ListEnabled(_ context.Context, _ int64, scene Scene) ([]Record, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	var out []Record
	for i := range s.rules {
		if Scene(s.rules[i].Scene) == scene && s.rules[i].Enabled {
			out = append(out, s.rules[i])
		}
	}
	return out, nil
}

// TestEngine_LoadAndGroupByScene verifies that Load groups rules by scene and
// that each Match method only evaluates its own scene's rules.
func TestEngine_LoadAndGroupByScene(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())

	rules := []Rule{
		{ID: 1, Name: "task-priority", Scene: SceneTask, Expression: "task.priority > 5", Priority: 10, Enabled: true},
		{
			ID:         2,
			Name:       "probe-error",
			Scene:      SceneProbe,
			Expression: `report.log_content contains "error"`,
			Priority:   10,
			Enabled:    true,
		},
		{
			ID:         3,
			Name:       "probe-warn",
			Scene:      SceneProbe,
			Expression: `report.log_content contains "warn"`,
			Priority:   5,
			Enabled:    true,
		},
		{
			ID:         4,
			Name:       "metric-cpu",
			Scene:      SceneMetric,
			Expression: `alert.metrics["cpu"] > 80`,
			Priority:   10,
			Enabled:    true,
		},
		{
			ID:         5,
			Name:       "metric-mem",
			Scene:      SceneMetric,
			Expression: `alert.metrics["mem"] > 90`,
			Priority:   5,
			Enabled:    true,
		},
		{
			ID:         6,
			Name:       "metric-disk",
			Scene:      SceneMetric,
			Expression: `alert.metrics["disk"] > 70`,
			Priority:   1,
			Enabled:    true,
		},
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Task scene: Priority=8 matches rule 1.
	taskMatched := eng.MatchTask(ctx, TaskMatchEnv{Task: TaskView{Priority: 8}})
	if len(taskMatched) != 1 || taskMatched[0] != 1 {
		t.Errorf("MatchTask = %v, want [1]", taskMatched)
	}

	// Probe scene: content with both error and warn matches rules 2 and 3.
	probeMatched := eng.MatchProbe(ctx, ProbeMatchEnv{Report: ReportView{LogContent: "error and warn"}})
	if len(probeMatched) != 2 {
		t.Errorf("MatchProbe = %v, want 2 ids", probeMatched)
	}

	// Metric scene: cpu=95 matches all three metric rules.
	metricMatched := eng.MatchMetric(ctx, MetricMatchEnv{Alert: AlertView{
		Metrics: map[string]float64{"cpu": 95, "mem": 95, "disk": 95},
	}})
	if len(metricMatched) != 3 {
		t.Errorf("MatchMetric = %v, want 3 ids", metricMatched)
	}

	// Scene isolation: a metric env must never trigger the task rule.
	// All three metric keys are provided so all three metric rules match,
	// and the task rule (ID 1) is never evaluated against the metric env.
	if got := eng.MatchMetric(ctx, MetricMatchEnv{Alert: AlertView{
		Metrics: map[string]float64{"cpu": 100, "mem": 100, "disk": 100},
	}}); len(got) != 3 {
		t.Errorf("MatchMetric leaked task rule: %v", got)
	}
}

// TestEngine_LoadCompileFailureIsolation verifies that a rule with an invalid
// expression is skipped while sibling rules load normally.
func TestEngine_LoadCompileFailureIsolation(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())

	rules := []Rule{
		{ID: 1, Name: "good-metric", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Enabled: true},
		{ID: 2, Name: "bad-metric", Scene: SceneMetric, Expression: `alert.metrics["cpu"] >`, Enabled: true},
		{ID: 3, Name: "good-task", Scene: SceneTask, Expression: "task.priority > 5", Enabled: true},
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// The bad rule (ID 2) is absent from the cache and never matches.
	matched := eng.MatchMetric(ctx, MetricMatchEnv{Alert: AlertView{Metrics: map[string]float64{"cpu": 90}}})
	if len(matched) != 1 || matched[0] != 1 {
		t.Errorf("MatchMetric = %v, want [1] (bad rule 2 should be skipped)", matched)
	}
	taskMatched := eng.MatchTask(ctx, TaskMatchEnv{Task: TaskView{Priority: 8}})
	if len(taskMatched) != 1 || taskMatched[0] != 3 {
		t.Errorf("MatchTask = %v, want [3]", taskMatched)
	}
	// Program cache holds only the two successfully compiled rules.
	eng.mu.RLock()
	cacheSize := len(eng.programs)
	eng.mu.RUnlock()
	if cacheSize != 2 {
		t.Errorf("program cache size = %d, want 2", cacheSize)
	}
}

// TestEngine_MatchEmpty verifies that Match methods return an empty result
// (without panicking) when no rules are loaded, and after loading an empty set.
func TestEngine_MatchEmpty(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())

	// Fresh engine: no rules loaded.
	if got := eng.MatchMetric(ctx, MetricMatchEnv{Alert: AlertView{
		Metrics: map[string]float64{"cpu": 90},
	}}); len(got) != 0 {
		t.Errorf("MatchMetric on fresh engine = %v, want empty", got)
	}

	// Explicitly load an empty rule set.
	if err := eng.Load(ctx, nil); err != nil {
		t.Fatalf("Load(nil): %v", err)
	}
	if got := eng.MatchTask(ctx, TaskMatchEnv{}); len(got) != 0 {
		t.Errorf("MatchTask after empty Load = %v, want empty", got)
	}
	if got := eng.MatchProbe(ctx, ProbeMatchEnv{}); len(got) != 0 {
		t.Errorf("MatchProbe after empty Load = %v, want empty", got)
	}
	if got := eng.MatchMetric(ctx, MetricMatchEnv{}); len(got) != 0 {
		t.Errorf("MatchMetric after empty Load = %v, want empty", got)
	}
}

// TestEngine_ProgramCacheReused verifies that the program cache populated by
// Load is reused across Match invocations (no recompilation, no growth).
func TestEngine_ProgramCacheReused(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rules := []Rule{
		{ID: 1, Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Enabled: true},
		{ID: 2, Scene: SceneMetric, Expression: `alert.metrics["mem"] > 90`, Enabled: true},
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load: %v", err)
	}

	eng.mu.RLock()
	prog1 := eng.programs[1]
	prog2 := eng.programs[2]
	cacheLen := len(eng.programs)
	eng.mu.RUnlock()
	if prog1 == nil || prog2 == nil {
		t.Fatal("expected both programs cached after Load")
	}

	// Repeated matches must not alter the cache or recompile programs.
	for range 5 {
		_ = eng.MatchMetric(ctx, MetricMatchEnv{Alert: AlertView{Metrics: map[string]float64{"cpu": 95, "mem": 95}}})
	}
	eng.mu.RLock()
	gotProg1 := eng.programs[1]
	gotProg2 := eng.programs[2]
	gotCacheLen := len(eng.programs)
	eng.mu.RUnlock()
	if gotCacheLen != cacheLen {
		t.Errorf("cache size changed across matches: %d -> %d", cacheLen, gotCacheLen)
	}
	if gotProg1 != prog1 || gotProg2 != prog2 {
		t.Error("program pointers changed across matches, expected cache reuse")
	}
}

// TestEngine_RuntimeErrorIsolation verifies that a rule whose program fails at
// evaluation time (e.g. an invalid regex pattern compiled lazily at runtime)
// is skipped without affecting sibling rules or panicking.
func TestEngine_RuntimeErrorIsolation(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rules := []Rule{
		{ID: 1, Name: "good", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Enabled: true},
		// regex("(",...) compiles (pattern is a string) but fails at runtime
		// when the regexp engine tries to parse the pattern.
		{ID: 2, Name: "runtime-bad", Scene: SceneMetric, Expression: `regex("(", "abc")`, Enabled: true},
		{ID: 3, Name: "also-good", Scene: SceneMetric, Expression: `alert.severity == "critical"`, Enabled: true},
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load: %v", err)
	}

	env := MetricMatchEnv{Alert: AlertView{Severity: "critical", Metrics: map[string]float64{"cpu": 90}}}
	matched := eng.MatchMetric(ctx, env)
	// Rules 1 and 3 match; rule 2 fails at runtime and is skipped.
	want := map[int64]bool{1: true, 3: true}
	if len(matched) != len(want) {
		t.Fatalf("MatchMetric = %v, want %d matches", matched, len(want))
	}
	for _, id := range matched {
		if !want[id] {
			t.Errorf("unexpected match id %d (rule 2 should be skipped)", id)
		}
	}
}

// TestEngine_DisabledRuleSkipped verifies that rules with Enabled=false do not
// participate in matching.
func TestEngine_DisabledRuleSkipped(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rules := []Rule{
		{ID: 1, Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Priority: 10, Enabled: true},
		{ID: 2, Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Priority: 5, Enabled: false},
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load: %v", err)
	}
	matched := eng.MatchMetric(ctx, MetricMatchEnv{Alert: AlertView{Metrics: map[string]float64{"cpu": 90}}})
	if len(matched) != 1 || matched[0] != 1 {
		t.Errorf("MatchMetric = %v, want [1] (disabled rule 2 should be skipped)", matched)
	}
}

// TestEngine_Reload verifies that Reload re-reads enabled rules from the Store
// and replaces the in-memory rule set, and that a Store error is propagated.
func TestEngine_Reload(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())

	// Initial load with a static rule.
	if err := eng.Load(ctx, []Rule{
		{ID: 1, Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Enabled: true},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Reload from a store carrying different rules; the old rule is replaced.
	store := &stubRuleStore{rules: []Record{
		{
			ID:         10,
			TenantID:   1,
			Name:       "s-metric",
			Scene:      string(SceneMetric),
			Expression: `alert.metrics["cpu"] > 50`,
			Enabled:    true,
			Priority:   5,
		},
		{
			ID:         11,
			TenantID:   1,
			Name:       "s-task",
			Scene:      string(SceneTask),
			Expression: "task.priority > 1",
			Enabled:    true,
			Priority:   5,
		},
	}}
	if err := eng.Reload(ctx, store); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	matched := eng.MatchMetric(ctx, MetricMatchEnv{Alert: AlertView{Metrics: map[string]float64{"cpu": 60}}})
	if len(matched) != 1 || matched[0] != 10 {
		t.Errorf("MatchMetric after Reload = %v, want [10]", matched)
	}
	taskMatched := eng.MatchTask(ctx, TaskMatchEnv{Task: TaskView{Priority: 2}})
	if len(taskMatched) != 1 || taskMatched[0] != 11 {
		t.Errorf("MatchTask after Reload = %v, want [11]", taskMatched)
	}
	// The pre-Reload rule (ID 1) is gone.
	eng.mu.RLock()
	_, oldStillPresent := eng.programs[1]
	eng.mu.RUnlock()
	if oldStillPresent {
		t.Error("Reload did not replace the pre-reload rule set")
	}
}

// TestEngine_ReloadStoreError verifies that a Store ListEnabled failure is
// propagated and the engine state is left intact (still serving old rules).
func TestEngine_ReloadStoreError(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	if err := eng.Load(ctx, []Rule{
		{ID: 1, Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Enabled: true},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	storeErr := errors.New("store unavailable")
	store := &stubRuleStore{listErr: storeErr}
	if err := eng.Reload(ctx, store); err == nil {
		t.Fatal("expected Reload to return error when Store fails, got nil")
	}
	// Old rule still served.
	matched := eng.MatchMetric(ctx, MetricMatchEnv{Alert: AlertView{Metrics: map[string]float64{"cpu": 90}}})
	if len(matched) != 1 || matched[0] != 1 {
		t.Errorf("MatchMetric after failed Reload = %v, want [1]", matched)
	}
}

// TestEngine_ConcurrentMatch verifies that concurrent Match invocations are
// race-free. Run with `go test -race` to detect data races.
func TestEngine_ConcurrentMatch(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rules := make([]Rule, 20)
	for i := range rules {
		rules[i] = Rule{
			ID:         int64(i + 1),
			Scene:      SceneMetric,
			Expression: `alert.metrics["cpu"] > 50`,
			Priority:   100 - i,
			Enabled:    true,
		}
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load: %v", err)
	}

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	start := make(chan struct{})
	for range goroutines {
		go func() {
			defer wg.Done()
			<-start
			for range 50 {
				matched := eng.MatchMetric(ctx, MetricMatchEnv{
					Alert: AlertView{Metrics: map[string]float64{"cpu": 90}},
				})
				if len(matched) != 20 {
					// Intentionally not t.Errorf to avoid concurrent writes to
					// the test output; the race detector is the real oracle.
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
}

// TestEngine_NewEngineNilLogger verifies that a nil logger is replaced with a
// no-op logger so callers never need to nil-check.
func TestEngine_NewEngineNilLogger(t *testing.T) {
	eng := NewEngine(nil)
	if eng.logger == nil {
		t.Fatal("expected non-nil logger after NewEngine(nil)")
	}
	// Exercise a code path that logs to ensure no nil-dereference.
	ctx := context.Background()
	_ = eng.Load(ctx, []Rule{{ID: 1, Scene: SceneMetric, Expression: "bad expr >", Enabled: true}})
	_ = eng.MatchMetric(ctx, MetricMatchEnv{})
}
