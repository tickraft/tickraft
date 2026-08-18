// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/expr-lang/expr"
	"go.uber.org/zap"
)

// BenchmarkEngineMatch measures single-rule concurrent MatchMetric
// throughput. The engine snapshots the rule slice and program map under a
// read lock and evaluates outside the lock, so concurrent evaluators should
// scale without contention.
//
// Expression identifiers use the snake_case form matching the expr struct
// tags on AlertView (e.g. `expr:"metrics"` -> alert.metrics["cpu"]).
func BenchmarkEngineMatch(b *testing.B) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rules := []Rule{
		{
			ID:         1,
			TenantID:   1,
			Name:       "cpu-high",
			Scene:      SceneMetric,
			Expression: `alert.metrics["cpu"] > 80`,
			Priority:   10,
			Enabled:    true,
		},
	}
	if err := eng.Load(ctx, rules); err != nil {
		b.Fatalf("Load: %v", err)
	}

	env := MetricMatchEnv{
		Alert: AlertView{
			Severity: "critical",
			Metrics:  map[string]float64{"cpu": 95},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = eng.MatchMetric(ctx, env)
		}
	})
}

// BenchmarkEngineMatchManyRules measures MatchMetric latency as the metric
// rule count grows from 100 to 10000, exercising the linear evaluation loop.
func BenchmarkEngineMatchManyRules(b *testing.B) {
	ctx := context.Background()

	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("%d_rules", n), func(b *testing.B) {
			eng := NewEngine(zap.NewNop())
			rules := make([]Rule, n)
			for i := range n {
				rules[i] = Rule{
					ID:         int64(i + 1),
					TenantID:   1,
					Name:       fmt.Sprintf("rule-%d", i),
					Scene:      SceneMetric,
					Expression: fmt.Sprintf(`alert.metrics["cpu"] > %d`, i),
					Priority:   n - i,
					Enabled:    true,
				}
			}
			if err := eng.Load(ctx, rules); err != nil {
				b.Fatalf("Load: %v", err)
			}

			env := MetricMatchEnv{
				Alert: AlertView{
					Severity: "critical",
					Metrics:  map[string]float64{"cpu": 95},
				},
			}

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_ = eng.MatchMetric(ctx, env)
			}
		})
	}
}

// BenchmarkCompileCache compares the cost of evaluating a pre-compiled
// program (cached) against compiling the expression on every call
// (recompile), demonstrating the value of the engine's program cache.
func BenchmarkCompileCache(b *testing.B) {
	compiler := NewCompiler()
	expression := `alert.metrics["cpu"] > 80 && alert.severity == "critical"`
	env := MetricMatchEnv{
		Alert: AlertView{
			Severity: "critical",
			Metrics:  map[string]float64{"cpu": 95},
		},
	}

	b.Run("cached", func(b *testing.B) {
		program, err := compiler.Compile(SceneMetric, expression)
		if err != nil {
			b.Fatalf("Compile: %v", err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_, _ = expr.Run(program, env)
		}
	})

	b.Run("recompile", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			program, err := compiler.Compile(SceneMetric, expression)
			if err != nil {
				b.Fatalf("Compile: %v", err)
			}
			_, _ = expr.Run(program, env)
		}
	})
}

// BenchmarkEngineMatchMetricWithViolations measures the cost of the
// violation-extraction path, which parses each matching rule's AST and
// evaluates every comparison sub-condition as a standalone sub-program.
// The sub-program cache keeps the steady-state cost dominated by expr.Run
// rather than recompilation.
func BenchmarkEngineMatchMetricWithViolations(b *testing.B) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rules := []Rule{
		{
			ID:         1,
			TenantID:   1,
			Name:       "cpu-and-mem",
			Scene:      SceneMetric,
			Expression: `alert.metrics["cpu"] > 90 && alert.metrics["mem"] > 85`,
			Priority:   10,
			Enabled:    true,
		},
	}
	if err := eng.Load(ctx, rules); err != nil {
		b.Fatalf("Load: %v", err)
	}

	env := MetricMatchEnv{
		Alert: AlertView{
			Severity: "critical",
			Metrics:  map[string]float64{"cpu": 95, "mem": 88},
		},
	}

	// Prime the sub-program cache so steady-state measurements are not
	// dominated by first-call compilation.
	_ = eng.MatchMetricWithViolations(ctx, env)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = eng.MatchMetricWithViolations(ctx, env)
		}
	})
}

// BenchmarkTaskMatch measures the task-scene match path. Task rules
// typically reference scalar fields (Priority, ExecutorType) rather than
// map indexes, so this benchmark characterizes the cheaper evaluation
// shape for comparison against the metric path.
func BenchmarkTaskMatch(b *testing.B) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rules := make([]Rule, 100)
	for i := range 100 {
		rules[i] = Rule{
			ID:         int64(i + 1),
			TenantID:   1,
			Name:       fmt.Sprintf("task-rule-%d", i),
			Scene:      SceneTask,
			Expression: fmt.Sprintf("task.priority > %d", i),
			Priority:   100 - i,
			Enabled:    true,
		}
	}
	if err := eng.Load(ctx, rules); err != nil {
		b.Fatalf("Load: %v", err)
	}

	env := TaskMatchEnv{
		Task:  TaskView{ID: 1, Priority: 50, ExecutorType: "ssh", Timeout: 30 * time.Second},
		Asset: AssetView{ID: 1, Name: "host-1"},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = eng.MatchTask(ctx, env)
	}
}
