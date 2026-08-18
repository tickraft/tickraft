// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"context"
	"fmt"
	"testing"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/parser"
	"go.uber.org/zap"
)

// This file contains performance comparison benchmarks that verify the
// effectiveness of the two core optimizations in ViolationExtractor and
// Engine.MatchMetricWithViolations:
//
//  1. AST parse cache — avoids re-parsing rule expressions on every match.
//  2. Single-evaluation fast path — when a pure-conjunction rule already
//     matched, the extractor builds Violations without re-evaluating each
//     comparison sub-condition, eliminating the double-evaluation cost.
//
// Each "vs" benchmark compares the cached fast path against an uncached,
// double-evaluation baseline that re-parses on every call. The baseline
// quantifies the savings the cache and fast path provide.

// makeMetricEnv returns a MetricMatchEnv with the given metric values,
// suitable for evaluating compound metric rules.
func makeMetricEnv(metrics map[string]float64) MetricMatchEnv {
	return MetricMatchEnv{
		Alert: AlertView{
			Severity: "critical",
			Metrics:  metrics,
		},
	}
}

// ---------------------------------------------------------------------------
// Benchmark 1: AST parse cache vs re-parse on every call
// ---------------------------------------------------------------------------

// BenchmarkASTParseCache compares the cost of a cache hit (reusing a parsed
// AST) against re-parsing the expression from scratch on every call. The
// ViolationExtractor caches parsed ASTs by source text; this benchmark
// quantifies the savings.
func BenchmarkASTParseCache(b *testing.B) {
	expression := `alert.metrics["cpu"] > 90 && alert.metrics["mem"] > 85 && alert.metrics["disk"] > 80`

	// Pre-parse for the cached case.
	cachedTree, err := parser.Parse(expression)
	if err != nil {
		b.Fatalf("parse: %v", err)
	}

	b.Run("cached", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			// Simulate a cache lookup: just use the pre-parsed tree.
			_ = cachedTree
		}
	})

	b.Run("reparse", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = parser.Parse(expression)
		}
	})
}

// ---------------------------------------------------------------------------
// Benchmark 2: Single-evaluation (fast path) vs per-comparison evaluation
// ---------------------------------------------------------------------------

// BenchmarkExtractSingleEvalVsDoubleEval compares the two evaluation paths in
// ViolationExtractor.Extract:
//
//   - "single_eval" (matched=true, pure conjunction): the extractor knows
//     every comparison is true and builds Violations directly without
//     re-evaluating each comparison sub-condition.
//   - "double_eval" (matched=false): the extractor must compile and run each
//     comparison sub-condition individually to determine which ones hold.
//
// The double-eval path simulates the uncached behavior where every
// comparison was re-evaluated after the full rule already matched.
func BenchmarkExtractSingleEvalVsDoubleEval(b *testing.B) {
	ctx := context.Background()
	compiler := NewCompiler()

	cases := []struct {
		name       string
		expression string
		metrics    map[string]float64
	}{
		{
			"single_condition",
			`alert.metrics["cpu"] > 80`,
			map[string]float64{"cpu": 95},
		},
		{
			"two_conditions_conjunction",
			`alert.metrics["cpu"] > 90 && alert.metrics["mem"] > 85`,
			map[string]float64{"cpu": 95, "mem": 88},
		},
		{
			"three_conditions_conjunction",
			`alert.metrics["cpu"] > 90 && alert.metrics["mem"] > 85 && alert.metrics["disk"] > 80`,
			map[string]float64{"cpu": 95, "mem": 88, "disk": 90},
		},
		{
			"five_conditions_conjunction",
			`alert.metrics["cpu"] > 90 && alert.metrics["mem"] > 85 && alert.metrics["disk"] > 80 ` +
				`&& alert.metrics["net"] > 70 && alert.metrics["load"] > 5`,
			map[string]float64{"cpu": 95, "mem": 88, "disk": 90, "net": 75, "load": 6},
		},
	}

	for _, tc := range cases {
		rule := Rule{
			ID:         1,
			TenantID:   1,
			Name:       "bench-rule",
			Scene:      SceneMetric,
			Expression: tc.expression,
			Enabled:    true,
		}
		env := makeMetricEnv(tc.metrics)

		b.Run(tc.name+"/single_eval", func(b *testing.B) {
			ext := NewViolationExtractor(compiler)
			// Prime caches.
			_ = ext.Extract(ctx, rule, env, true)

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_ = ext.Extract(ctx, rule, env, true)
			}
		})

		b.Run(tc.name+"/double_eval", func(b *testing.B) {
			ext := NewViolationExtractor(compiler)
			// Prime caches.
			_ = ext.Extract(ctx, rule, env, false)

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_ = ext.Extract(ctx, rule, env, false)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Benchmark 3: MatchMetricWithViolations (cached, single-eval) vs uncached baseline
// ---------------------------------------------------------------------------

// BenchmarkMatchWithViolationsCachedVsUncached compares
// Engine.MatchMetricWithViolations (which uses AST cache +
// single-evaluation fast path) against an uncached, double-evaluation
// baseline that:
//
//  1. Runs the full rule program to check if it matches (same as current).
//  2. Re-parses the expression on every call (no AST cache).
//  3. Re-evaluates each comparison sub-condition individually (no fast path).
//
// This benchmark demonstrates the combined savings of both optimizations.
func BenchmarkMatchWithViolationsCachedVsUncached(b *testing.B) {
	ctx := context.Background()

	rules := []Rule{
		{
			ID:         1,
			TenantID:   1,
			Name:       "compound-cpu-mem",
			Scene:      SceneMetric,
			Expression: `alert.metrics["cpu"] > 90 && alert.metrics["mem"] > 85`,
			Priority:   10,
			Enabled:    true,
		},
	}
	env := makeMetricEnv(map[string]float64{"cpu": 95, "mem": 88})

	b.Run("cached_single_eval", func(b *testing.B) {
		eng := NewEngine(zap.NewNop())
		if err := eng.Load(ctx, rules); err != nil {
			b.Fatalf("Load: %v", err)
		}
		// Prime caches.
		_ = eng.MatchMetricWithViolations(ctx, env)

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_ = eng.MatchMetricWithViolations(ctx, env)
		}
	})

	b.Run("uncached_double_eval", func(b *testing.B) {
		eng := NewEngine(zap.NewNop())
		if err := eng.Load(ctx, rules); err != nil {
			b.Fatalf("Load: %v", err)
		}
		// Use the extractor with matched=false to force per-comparison
		// re-evaluation, simulating the double-evaluation behavior of an
		// uncached extractor. Also reset the AST cache before each
		// iteration to simulate no caching.
		ext := NewViolationExtractor(NewCompiler())

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			// Reset caches to simulate uncached, no-fast-path behavior.
			ext.Reset()
			_ = ext.Extract(ctx, rules[0], env, false)
		}
	})
}

// ---------------------------------------------------------------------------
// Benchmark 4: ViolationExtractor cache warm-up vs cold-start
// ---------------------------------------------------------------------------

// BenchmarkExtractCacheWarmup compares the first call (cold, AST and
// sub-programs not yet cached) against subsequent calls (warm, all caches
// populated). This quantifies the one-time compilation cost that the cache
// amortizes over the engine's lifetime.
func BenchmarkExtractCacheWarmup(b *testing.B) {
	ctx := context.Background()
	expression := `alert.metrics["cpu"] > 90 && alert.metrics["mem"] > 85`
	rule := Rule{
		ID:         1,
		TenantID:   1,
		Name:       "warmup-rule",
		Scene:      SceneMetric,
		Expression: expression,
		Enabled:    true,
	}
	env := makeMetricEnv(map[string]float64{"cpu": 95, "mem": 88})

	b.Run("cold_start", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			ext := NewViolationExtractor(NewCompiler())
			_ = ext.Extract(ctx, rule, env, true)
		}
	})

	b.Run("warm_cache", func(b *testing.B) {
		ext := NewViolationExtractor(NewCompiler())
		_ = ext.Extract(ctx, rule, env, true) // prime

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_ = ext.Extract(ctx, rule, env, true)
		}
	})
}

// ---------------------------------------------------------------------------
// Benchmark 5: Scale — many compound rules with single-evaluation
// ---------------------------------------------------------------------------

// BenchmarkMatchWithViolationsScaling measures how MatchMetricWithViolations
// scales as the number of compound (&&) rules grows, exercising the
// single-evaluation fast path across many rules. Each rule matches the env,
// so the extractor's fast path is taken for every rule on every iteration.
func BenchmarkMatchWithViolationsScaling(b *testing.B) {
	ctx := context.Background()

	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("%d_compound_rules", n), func(b *testing.B) {
			eng := NewEngine(zap.NewNop())
			rules := make([]Rule, n)
			for i := range n {
				rules[i] = Rule{
					ID:         int64(i + 1),
					TenantID:   1,
					Name:       fmt.Sprintf("compound-%d", i),
					Scene:      SceneMetric,
					Expression: fmt.Sprintf(`alert.metrics["cpu"] > %d && alert.metrics["mem"] > %d`, i, i),
					Priority:   n - i,
					Enabled:    true,
				}
			}
			if err := eng.Load(ctx, rules); err != nil {
				b.Fatalf("Load: %v", err)
			}
			env := makeMetricEnv(map[string]float64{"cpu": 9999, "mem": 9999})

			// Prime caches.
			_ = eng.MatchMetricWithViolations(ctx, env)

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_ = eng.MatchMetricWithViolations(ctx, env)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Benchmark 6: Direct comparison — expr.Run cost vs full extraction cost
// ---------------------------------------------------------------------------

// BenchmarkRunVsExtract isolates the cost of expr.Run (the rule matching
// step) from the cost of ViolationExtractor.Extract (the violation-building
// step), showing the relative weight of each in the total
// MatchMetricWithViolations cost.
func BenchmarkRunVsExtract(b *testing.B) {
	ctx := context.Background()
	expression := `alert.metrics["cpu"] > 90 && alert.metrics["mem"] > 85`
	rule := Rule{
		ID:         1,
		TenantID:   1,
		Name:       "isolation-rule",
		Scene:      SceneMetric,
		Expression: expression,
		Enabled:    true,
	}
	env := makeMetricEnv(map[string]float64{"cpu": 95, "mem": 88})

	compiler := NewCompiler()
	program, err := compiler.Compile(SceneMetric, expression)
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}

	b.Run("expr_run_only", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = expr.Run(program, env)
		}
	})

	b.Run("extract_only_warm", func(b *testing.B) {
		ext := NewViolationExtractor(compiler)
		_ = ext.Extract(ctx, rule, env, true) // prime

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_ = ext.Extract(ctx, rule, env, true)
		}
	})

	b.Run("run_plus_extract", func(b *testing.B) {
		ext := NewViolationExtractor(compiler)
		_ = ext.Extract(ctx, rule, env, true) // prime

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_, _ = expr.Run(program, env)
			_ = ext.Extract(ctx, rule, env, true)
		}
	})
}
