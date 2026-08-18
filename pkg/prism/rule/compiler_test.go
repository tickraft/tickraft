// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"errors"
	"strings"
	"testing"

	"github.com/expr-lang/expr"
)

// TestCompile_Success verifies that the Compiler accepts well-formed
// expressions for each of the four scenes and returns a reusable program.
func TestCompile_Success(t *testing.T) {
	c := NewCompiler()
	cases := []struct {
		name       string
		scene      Scene
		expression string
		env        any
		want       any
	}{
		{
			name:       "task priority threshold",
			scene:      SceneTask,
			expression: "task.priority > 5",
			env:        TaskMatchEnv{Task: TaskView{Priority: 8}},
			want:       true,
		},
		{
			name:       "probe status code",
			scene:      SceneProbe,
			expression: "result.status_code >= 300",
			env:        ProbeMatchEnv{Result: ResultView{StatusCode: 404}},
			want:       true,
		},
		{
			name:       "metric value threshold",
			scene:      SceneMetric,
			expression: `alert.metrics["cpu"] > 80`,
			env:        MetricMatchEnv{Alert: AlertView{Metrics: map[string]float64{"cpu": 90}}},
			want:       true,
		},
		{
			name:       "remediation metric value threshold",
			scene:      SceneRemediation,
			expression: "remediation.metric_value > 80",
			env:        RemediationMatchEnv{Remediation: RemediationView{MetricValue: 90}},
			want:       true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := c.Compile(tc.scene, tc.expression)
			if err != nil {
				t.Fatalf("Compile(%q) returned unexpected error: %v", tc.expression, err)
			}
			if prog == nil {
				t.Fatal("Compile returned nil program without error")
			}
			out, err := expr.Run(prog, tc.env)
			if err != nil {
				t.Fatalf("expr.Run returned unexpected error: %v", err)
			}
			if out != tc.want {
				t.Errorf("expr.Run output = %v, want %v", out, tc.want)
			}
		})
	}
}

// TestCompile_SyntaxError verifies that malformed expressions fail to compile
// and the error wraps ErrRuleCompileFailed.
func TestCompile_SyntaxError(t *testing.T) {
	c := NewCompiler()
	_, err := c.Compile(SceneTask, "task.priority >")
	if err == nil {
		t.Fatal("expected compile error for dangling operator, got nil")
	}
	if !errors.Is(err, ErrRuleCompileFailed) {
		t.Errorf("expected ErrRuleCompileFailed, got %v", err)
	}
}

// TestCompile_TypeError verifies that type-mismatched comparisons fail at
// compile time and wrap ErrRuleCompileFailed.
func TestCompile_TypeError(t *testing.T) {
	c := NewCompiler()
	_, err := c.Compile(SceneTask, `task.priority == "high"`)
	if err == nil {
		t.Fatal("expected compile error for int==string comparison, got nil")
	}
	if !errors.Is(err, ErrRuleCompileFailed) {
		t.Errorf("expected ErrRuleCompileFailed, got %v", err)
	}
}

// TestCompile_NonBoolRejectedByAsBool verifies that an expression returning a
// non-boolean value is rejected at compile time because the Compiler pins the
// result type with expr.AsBool().
func TestCompile_NonBoolRejectedByAsBool(t *testing.T) {
	c := NewCompiler()
	_, err := c.Compile(SceneTask, "task.priority")
	if err == nil {
		t.Fatal("expected compile error for non-bool expression, got nil")
	}
	if !errors.Is(err, ErrRuleCompileFailed) {
		t.Errorf("expected ErrRuleCompileFailed, got %v", err)
	}
}

// TestCompile_UnknownField verifies that referencing a field absent from the
// scene's Env contract fails compilation.
func TestCompile_UnknownField(t *testing.T) {
	c := NewCompiler()
	_, err := c.Compile(SceneTask, "task.non_existent > 0")
	if err == nil {
		t.Fatal("expected compile error for unknown field, got nil")
	}
	if !errors.Is(err, ErrRuleCompileFailed) {
		t.Errorf("expected ErrRuleCompileFailed, got %v", err)
	}
}

// TestCompile_InvalidScene verifies that an unrecognized scene is rejected with
// ErrRuleInvalidScene.
func TestCompile_InvalidScene(t *testing.T) {
	c := NewCompiler()
	_, err := c.Compile(Scene("invalid"), "true")
	if err == nil {
		t.Fatal("expected error for invalid scene, got nil")
	}
	if !errors.Is(err, ErrRuleInvalidScene) {
		t.Errorf("expected ErrRuleInvalidScene, got %v", err)
	}
}

// TestCompile_MaxNodesExceeded verifies that an expression exceeding the
// MaxNodes(1000) budget fails compilation. The comparison-count check is
// disabled (MaxComparisons=-1) so the MaxNodes budget is the only guard
// exercised here.
func TestCompile_MaxNodesExceeded(t *testing.T) {
	c := NewCompilerWithConfig(CompilerConfig{MaxComparisons: -1})
	// Build `alert.metrics["cpu"] > 0 and ...` with enough conditions to
	// exceed the 1000-node budget by a wide margin.
	const conditions = 600
	parts := make([]string, conditions)
	for i := range parts {
		parts[i] = `alert.metrics["cpu"] > 0`
	}
	expression := strings.Join(parts, " and ")

	_, err := c.Compile(SceneMetric, expression)
	if err == nil {
		t.Fatal("expected compile error for oversized expression, got nil")
	}
	if !errors.Is(err, ErrRuleCompileFailed) {
		t.Errorf("expected ErrRuleCompileFailed, got %v", err)
	}
}

// TestCompile_SandboxDisablesBuiltin verifies that builtins outside the
// whitelist are unavailable: expr.DisableAllBuiltins() disables every builtin
// and only the whitelisted names are re-enabled, so a disabled builtin such as
// flatten or sort is reported as an unknown name.
func TestCompile_SandboxDisablesBuiltin(t *testing.T) {
	c := NewCompiler()
	disabled := []string{
		"flatten([[1, 2], [3]])",
		"sort([3, 1, 2])",
	}
	for _, expression := range disabled {
		_, err := c.Compile(SceneMetric, expression)
		if err == nil {
			t.Errorf("expected compile error for disabled builtin %q, got nil", expression)
			continue
		}
		if !errors.Is(err, ErrRuleCompileFailed) {
			t.Errorf("expected ErrRuleCompileFailed for %q, got %v", expression, err)
		}
	}
}

// TestCompile_WhitelistedBuiltinAvailable verifies that a representative
// whitelisted builtin (len, keys) remains available in the sandbox.
//
// Note: `contains`, `startsWith`, and `endsWith` are listed in the compiler's
// whitelist but are exposed by expr-lang as INFIX OPERATORS (`x contains y`),
// not as function-call syntax (`contains(x, y)`). The lexer tokenizes them as
// Operator tokens, so the function-call form is a syntax error. This test
// therefore exercises `len` and `keys`, which are genuine functions.
func TestCompile_WhitelistedBuiltinAvailable(t *testing.T) {
	c := NewCompiler()
	cases := []struct {
		scene      Scene
		expression string
		env        any
	}{
		{SceneTask, "len(task.executor_type) > 0", TaskMatchEnv{Task: TaskView{ExecutorType: "ssh"}}},
		{
			SceneMetric,
			"len(alert.metrics) > 0",
			MetricMatchEnv{Alert: AlertView{Metrics: map[string]float64{"cpu": 90}}},
		},
		{
			SceneMetric,
			"len(keys(alert.metrics)) > 0",
			MetricMatchEnv{Alert: AlertView{Metrics: map[string]float64{"cpu": 90}}},
		},
	}
	for _, tc := range cases {
		prog, err := c.Compile(tc.scene, tc.expression)
		if err != nil {
			t.Fatalf("Compile(%q) returned unexpected error: %v", tc.expression, err)
		}
		if _, err := expr.Run(prog, tc.env); err != nil {
			t.Fatalf("expr.Run(%q) returned unexpected error: %v", tc.expression, err)
		}
	}
}
