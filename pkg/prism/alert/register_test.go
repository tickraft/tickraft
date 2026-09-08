// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"testing"
	"time"
)

// fakeTarget collects the matchers injected by Register.
type fakeTarget struct {
	matchers []Matcher
}

func (f *fakeTarget) AddRule(m Matcher) {
	f.matchers = append(f.matchers, m)
}

// TestRegisterDisabled verifies the zero-value no-op: without static
// rules or a Store, Register returns (nil, nil) and never touches the
// target.
func TestRegisterDisabled(t *testing.T) {
	target := &fakeTarget{}
	engine, err := Register(context.Background(), target, Config{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if engine != nil {
		t.Errorf("engine = %v, want nil for zero-value Config", engine)
	}
	if len(target.matchers) != 0 {
		t.Errorf("target received %d matchers, want 0", len(target.matchers))
	}
}

// TestRegisterStaticRules verifies startup wiring: static rules are
// loaded with negative IDs (so they never collide with database
// -assigned positive IDs) and exactly one AlertMatcher is injected into
// the target.
func TestRegisterStaticRules(t *testing.T) {
	target := &fakeTarget{}
	engine, err := Register(context.Background(), target, Config{
		Rules: []Spec{
			{Name: "cpu", Expression: `metrics["cpu"] > 90`, Priority: 10, Metadata: map[string]string{"team": "ops"}},
			{Name: "mem", Expression: `metrics["mem"] > 80`},
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if engine == nil {
		t.Fatal("engine = nil, want non-nil")
	}
	if len(target.matchers) != 1 {
		t.Fatalf("target received %d matchers, want 1", len(target.matchers))
	}
	matcher, ok := target.matchers[0].(*AlertMatcher)
	if !ok {
		t.Fatalf("injected matcher type = %T, want *AlertMatcher", target.matchers[0])
	}

	// Static rules are loaded and evaluable.
	if !engine.HasRules() {
		t.Fatal("HasRules = false, want true")
	}
	matched, _ := engine.Evaluate(context.Background(), 1, testEnv(map[string]float64{"cpu": 95, "mem": 85}))
	if len(matched) != 2 {
		t.Fatalf("Evaluate matched %v, want both static rules", matched)
	}
	for _, rule := range matched {
		if rule.ID >= 0 {
			t.Errorf("static rule id = %d, want negative", rule.ID)
		}
	}

	// The injected matcher uses the same engine (filters dispatch).
	if !matcher.Match(context.Background(), metricEvent(1, 1, map[string]float64{"cpu": 95})).Forward {
		t.Error("injected matcher did not forward a matching alert")
	}

	// Stop cancels the reload-loop context; it is safe to call.
	if err := engine.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

// TestRegisterSkipsInvalidStaticRule verifies that one bad static
// expression does not block registration of the remaining rules.
func TestRegisterSkipsInvalidStaticRule(t *testing.T) {
	target := &fakeTarget{}
	engine, err := Register(context.Background(), target, Config{
		Rules: []Spec{
			{Name: "bad", Expression: `metrics["cpu"] >`},
			{Name: "good", Expression: `metrics["cpu"] > 90`},
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	matched, _ := engine.Evaluate(context.Background(), 1, testEnv(map[string]float64{"cpu": 95}))
	if len(matched) != 1 {
		t.Fatalf("Evaluate matched %v, want only the valid static rule", matched)
	}
}

// TestRegisterWithStore verifies the dynamic path: Register performs an
// initial Reload from the Store (replacing the static rule set) and
// wires the ReloadSubscriber callback for real-time refresh.
func TestRegisterWithStore(t *testing.T) {
	target := &fakeTarget{}
	lister := &fakeLister{rules: []Rule{
		{ID: 11, Name: "dynamic", Expression: `metrics["mem"] > 80`, Enabled: true},
	}}

	var reloadFn func(ctx context.Context) error
	engine, err := Register(context.Background(), target, Config{
		Store:            lister,
		EvalInterval:     time.Hour, // long enough not to fire during the test
		ReloadSubscriber: func(reload func(ctx context.Context) error) { reloadFn = reload },
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer func() { _ = engine.Stop(context.Background()) }()

	// The initial Reload replaced the (empty) static set with the
	// store's rule.
	matched, _ := engine.Evaluate(context.Background(), 1, testEnv(map[string]float64{"mem": 85}))
	if len(matched) != 1 || matched[0].ID != 11 {
		t.Fatalf("Evaluate after initial reload = %v, want [11]", matched)
	}

	// The subscriber wiring lets callers push rule changes immediately.
	lister.rules = []Rule{
		{ID: 12, Name: "replaced", Expression: `metrics["mem"] > 50`, Enabled: true},
	}
	if reloadFn == nil {
		t.Fatal("ReloadSubscriber was not invoked")
	}
	if err := reloadFn(context.Background()); err != nil {
		t.Fatalf("reloadFn: %v", err)
	}
	matched, _ = engine.Evaluate(context.Background(), 1, testEnv(map[string]float64{"mem": 60}))
	if len(matched) != 1 || matched[0].ID != 12 {
		t.Fatalf("Evaluate after subscriber reload = %v, want [12]", matched)
	}
}
