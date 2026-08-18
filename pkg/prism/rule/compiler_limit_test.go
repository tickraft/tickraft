// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"errors"
	"strings"
	"testing"
)

// TestCompile_DefaultMaxComparisons verifies that the default OSS
// MaxComparisons limit (3) accepts an expression with exactly three
// comparison sub-conditions and rejects one with four.
func TestCompile_DefaultMaxComparisons(t *testing.T) {
	c := NewCompiler()

	// Exactly 3 comparisons — the default limit — must compile.
	threeCmps := `alert.metrics["cpu"] > 80 && alert.metrics["mem"] > 90 && alert.metrics["disk"] > 70`
	if _, err := c.Compile(SceneMetric, threeCmps); err != nil {
		t.Fatalf("expected 3-comparison expression to compile under default limit, got %v", err)
	}

	// 4 comparisons exceed the default limit of 3.
	fourCmps := `alert.metrics["cpu"] > 80 && alert.metrics["mem"] > 90 && alert.metrics["disk"] > 70 ` +
		`&& alert.metrics["net"] > 50`
	_, err := c.Compile(SceneMetric, fourCmps)
	if err == nil {
		t.Fatal("expected compile error for 4 comparisons under default limit, got nil")
	}
	if !errors.Is(err, ErrRuleTooManyComparisons) {
		t.Errorf("expected ErrRuleTooManyComparisons, got %v", err)
	}
}

// TestCompile_RaisedMaxComparisons verifies that raising MaxComparisons
// via CompilerConfig allows a compound expression with more sub-conditions
// than the default 3 to compile.
func TestCompile_RaisedMaxComparisons(t *testing.T) {
	c := NewCompilerWithConfig(CompilerConfig{MaxComparisons: 6})
	// 4 comparisons exceed the default but fit under the raised limit.
	expr := `alert.metrics["cpu"] > 80 && alert.metrics["mem"] > 90 && alert.metrics["disk"] > 70 ` +
		`&& alert.metrics["net"] > 50`
	if _, err := c.Compile(SceneMetric, expr); err != nil {
		t.Fatalf("expected 4-comparison expression to compile under raised limit 6, got %v", err)
	}
}

// TestCompile_DisabledMaxComparisons verifies that a negative
// MaxComparisons disables the comparison-count check entirely so an
// expression with many sub-conditions compiles. This is the escape
// hatch used by the extended edition's compound multi-metric rules.
func TestCompile_DisabledMaxComparisons(t *testing.T) {
	c := NewCompilerWithConfig(CompilerConfig{MaxComparisons: -1})
	parts := make([]string, 10)
	for i := range parts {
		parts[i] = `alert.metrics["cpu"] > 80`
	}
	expr := strings.Join(parts, " && ")
	if _, err := c.Compile(SceneMetric, expr); err != nil {
		t.Fatalf("expected disabled-limit compiler to accept 10 comparisons, got %v", err)
	}
}

// TestCompile_ZeroMaxComparisonsFallsBackToDefault verifies that a zero
// MaxComparisons falls back to the OSS default (3) rather than disabling
// the check.
func TestCompile_ZeroMaxComparisonsFallsBackToDefault(t *testing.T) {
	c := NewCompilerWithConfig(CompilerConfig{MaxComparisons: 0})
	// 4 comparisons exceed the default of 3, so a zero-value config must
	// reject the expression.
	expr := `alert.metrics["a"] > 1 && alert.metrics["b"] > 2 && alert.metrics["c"] > 3 && alert.metrics["d"] > 4`
	_, err := c.Compile(SceneMetric, expr)
	if err == nil {
		t.Fatal("expected compile error for 4 comparisons under zero-value (default) limit, got nil")
	}
	if !errors.Is(err, ErrRuleTooManyComparisons) {
		t.Errorf("expected ErrRuleTooManyComparisons, got %v", err)
	}
}

// TestCompile_EqualityCountedAsComparison verifies that the comparison
// counter treats == and != as comparisons, not just the ordering
// operators (>, >=, <, <=).
func TestCompile_EqualityCountedAsComparison(t *testing.T) {
	c := NewCompiler()
	// Three equality checks exactly at the default limit.
	expr := `alert.severity == "critical" && alert.keyword == "oom" && alert.source == "10.0.0.1"`
	if _, err := c.Compile(SceneMetric, expr); err != nil {
		t.Fatalf("expected 3-equality expression to compile under default limit, got %v", err)
	}

	// Four equality checks exceed the limit.
	expr = `alert.severity == "critical" && alert.keyword == "oom" && alert.source == "10.0.0.1" ` +
		`&& alert.content == "x"`
	_, err := c.Compile(SceneMetric, expr)
	if err == nil {
		t.Fatal("expected compile error for 4 equality comparisons under default limit, got nil")
	}
	if !errors.Is(err, ErrRuleTooManyComparisons) {
		t.Errorf("expected ErrRuleTooManyComparisons, got %v", err)
	}
}

// TestCompile_MixedOperatorsCountedTogether verifies that ordering
// comparisons and equality comparisons are counted together against the
// MaxComparisons limit.
func TestCompile_MixedOperatorsCountedTogether(t *testing.T) {
	c := NewCompiler()
	// 2 ordering + 1 equality = 3 comparisons, at the limit.
	expr := `alert.metrics["cpu"] > 80 && alert.metrics["mem"] >= 90 && alert.severity == "critical"`
	if _, err := c.Compile(SceneMetric, expr); err != nil {
		t.Fatalf("expected 3 mixed-comparison expression to compile under default limit, got %v", err)
	}

	// 2 ordering + 2 equality = 4 comparisons, over the limit.
	expr = `alert.metrics["cpu"] > 80 && alert.metrics["mem"] >= 90 && alert.severity == "critical" ` +
		`&& alert.keyword == "oom"`
	_, err := c.Compile(SceneMetric, expr)
	if err == nil {
		t.Fatal("expected compile error for 4 mixed comparisons, got nil")
	}
	if !errors.Is(err, ErrRuleTooManyComparisons) {
		t.Errorf("expected ErrRuleTooManyComparisons, got %v", err)
	}
}

// TestCompile_BooleanOperatorsNotCounted verifies that the boolean
// operators (&&, ||) are traversed but not counted as comparisons, so a
// deeply-nested boolean expression with few comparisons still compiles.
func TestCompile_BooleanOperatorsNotCounted(t *testing.T) {
	c := NewCompiler()
	// One comparison buried in a deeply-nested boolean tree.
	expr := `((true || false) && (true || false)) && alert.metrics["cpu"] > 80`
	if _, err := c.Compile(SceneMetric, expr); err != nil {
		t.Fatalf("expected deeply-nested 1-comparison expression to compile, got %v", err)
	}
}

// TestCompile_ArithmeticOperatorsNotCounted verifies that arithmetic
// operators (+, -, *, /) are traversed but not counted as comparisons.
func TestCompile_ArithmeticOperatorsNotCounted(t *testing.T) {
	c := NewCompiler()
	// Two comparisons + arithmetic that does not count.
	expr := `alert.metrics["a"] + alert.metrics["b"] > 80 && alert.metrics["c"] > 70`
	if _, err := c.Compile(SceneMetric, expr); err != nil {
		t.Fatalf("expected expression with arithmetic + 2 comparisons to compile, got %v", err)
	}
}

// TestCompile_SubSkipsComparisonCheck verifies that CompileSub does not
// enforce the MaxComparisons limit, because a sub-expression is a
// fragment of an already-validated rule. This keeps the violation
// extractor's operand evaluation free of false rejections.
func TestCompile_SubSkipsComparisonCheck(t *testing.T) {
	c := NewCompiler()
	// A sub-expression that is itself a comparison (one comparison).
	// CompileSub must accept it even though it is not a full boolean
	// expression.
	sub := `alert.metrics["cpu"] > 80`
	if _, err := c.CompileSub(SceneMetric, sub); err != nil {
		t.Fatalf("CompileSub rejected a single comparison: %v", err)
	}

	// A sub-expression that is an operand (no comparison).
	operand := `alert.metrics["cpu"]`
	if _, err := c.CompileSub(SceneMetric, operand); err != nil {
		t.Fatalf("CompileSub rejected an operand sub-expression: %v", err)
	}
}

// TestCompile_ErrorWrapsErrRuleTooManyComparisons verifies that the error
// returned for an over-limit expression wraps the sentinel
// ErrRuleTooManyComparisons and includes the count and limit in the
// message for operator diagnostics.
func TestCompile_ErrorWrapsErrRuleTooManyComparisons(t *testing.T) {
	c := NewCompiler()
	expr := `alert.metrics["a"] > 1 && alert.metrics["b"] > 2 && alert.metrics["c"] > 3 && alert.metrics["d"] > 4`
	_, err := c.Compile(SceneMetric, expr)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRuleTooManyComparisons) {
		t.Errorf("errors.Is(ErrRuleTooManyComparisons) = false, err=%v", err)
	}
	if !strings.Contains(err.Error(), "4") {
		t.Errorf("error message should contain count 4, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "3") {
		t.Errorf("error message should contain limit 3, got %q", err.Error())
	}
}

// TestCompile_MaxComparisonsAcrossScenes verifies that the
// MaxComparisons limit is applied uniformly across all four scenes,
// not just the metric scene.
func TestCompile_MaxComparisonsAcrossScenes(t *testing.T) {
	c := NewCompiler()
	cases := []struct {
		name  string
		scene Scene
		expr  string
	}{
		{
			name:  "task",
			scene: SceneTask,
			expr:  `task.priority > 5 && task.id > 0 && task.asset_id > 0 && task.tenant_id > 0`,
		},
		{
			name:  "probe",
			scene: SceneProbe,
			expr:  `result.status_code > 200 && result.status_code < 500 && result.duration > 0 && report.asset_id > 0`,
		},
		{
			name:  "remediation",
			scene: SceneRemediation,
			expr: `remediation.metric_value > 80 && remediation.threshold > 0 && remediation.asset_id > 0 ` +
				`&& remediation.metric_value > 0`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.Compile(tc.scene, tc.expr)
			if err == nil {
				t.Fatalf("expected compile error for 4 comparisons in %s scene, got nil", tc.name)
			}
			if !errors.Is(err, ErrRuleTooManyComparisons) {
				t.Errorf("expected ErrRuleTooManyComparisons for %s scene, got %v", tc.name, err)
			}
		})
	}
}

// TestCompile_MaxComparisonsBoundary verifies the exact boundary: a rule
// with N comparisons compiles when MaxComparisons == N, and fails when
// MaxComparisons == N-1.
func TestCompile_MaxComparisonsBoundary(t *testing.T) {
	// Build a 3-comparison expression.
	expr := `alert.metrics["a"] > 1 && alert.metrics["b"] > 2 && alert.metrics["c"] > 3`

	// MaxComparisons == 3: at the boundary, must compile.
	c := NewCompilerWithConfig(CompilerConfig{MaxComparisons: 3})
	if _, err := c.Compile(SceneMetric, expr); err != nil {
		t.Errorf("expected compile success at boundary (limit=3, count=3), got %v", err)
	}

	// MaxComparisons == 2: one over the boundary, must fail.
	c = NewCompilerWithConfig(CompilerConfig{MaxComparisons: 2})
	_, err := c.Compile(SceneMetric, expr)
	if err == nil {
		t.Fatal("expected compile error one over the boundary (limit=2, count=3), got nil")
	}
	if !errors.Is(err, ErrRuleTooManyComparisons) {
		t.Errorf("expected ErrRuleTooManyComparisons, got %v", err)
	}
}

// TestCompile_ParseErrorDeferredToExprCompile verifies that when the
// comparison-count parser fails to parse the expression, the error is
// deferred to expr.Compile (which produces the canonical parse-error
// message wrapped in ErrRuleCompileFailed) rather than being silently
// swallowed or misattributed to the comparison check.
func TestCompile_ParseErrorDeferredToExprCompile(t *testing.T) {
	c := NewCompiler()
	// A syntactically invalid expression that will fail to parse during
	// the comparison count check. The error should surface as
	// ErrRuleCompileFailed, not ErrRuleTooManyComparisons.
	_, err := c.Compile(SceneMetric, `alert.metrics["cpu"] >`)
	if err == nil {
		t.Fatal("expected compile error for dangling operator, got nil")
	}
	if !errors.Is(err, ErrRuleCompileFailed) {
		t.Errorf("expected ErrRuleCompileFailed for parse error, got %v", err)
	}
	if errors.Is(err, ErrRuleTooManyComparisons) {
		t.Errorf("parse error should not be attributed to MaxComparisons check, got %v", err)
	}
}
