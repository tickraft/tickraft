// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"fmt"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"
)

// builtinWhitelist is the set of expr-lang builtins permitted inside
// rule expressions. Everything else is disabled via
// expr.DisableAllBuiltins to keep the sandbox hermetic.
var builtinWhitelist = []string{
	"len", "all", "any", "one", "none", "filter", "find", "count",
	"sum", "mean", "max", "min", "median",
	"keys", "values",
	"contains", "startsWith", "endsWith",
	"now", "duration",
}

// defaultMaxNodes is the default AST node budget applied when
// CompilerConfig.MaxNodes is zero.
const defaultMaxNodes = 1000

// defaultMaxComparisons is the default comparison-count limit
// applied when CompilerConfig.MaxComparisons is zero. The limit keeps
// per-rule evaluation cost bounded so a single compound rule cannot
// dominate the engine's evaluation budget; callers can override it
// via CompilerConfig.
const defaultMaxComparisons = 3

// CompilerConfig configures a Compiler. The zero value falls back to
// defaults: MaxNodes=1000, MaxComparisons=3, and the four
// built-in custom functions (regex / containsAny / inRange / ago).
//
// Callers that need to raise the comparison limit (for example, an
// callers dispatching a compound multi-metric rule) supply
// a non-zero MaxComparisons; passing MaxComparisons < 0 is treated
// as "no limit" via the MaxComparisons > 0 check in Compile.
type CompilerConfig struct {
	// MaxNodes bounds the AST node count of a compiled expression.
	// Zero falls back to defaultMaxNodes.
	MaxNodes int
	// MaxComparisons bounds the number of comparison sub-conditions
	// (>, >=, <, <=, ==, !=) permitted inside a single expression.
	// Zero falls back to defaultMaxComparisons. A negative value
	// disables the check entirely.
	MaxComparisons int
	// CustomFunctions adds caller-supplied expr.Function options on
	// top of the four built-in custom functions (regex / containsAny
	// / inRange / ago). Nil or empty leaves the default function set.
	CustomFunctions []expr.Option
}

// Compiler compiles rule expressions into *vm.Program values. It is
// stateless: the env fields are kept as zero-value instances purely to
// serve as type contracts for expr.Compile, and the functions slice
// holds the immutable custom-function options. The zero-value Compiler
// must not be used directly; construct via NewCompiler or
// NewCompilerWithConfig.
type Compiler struct {
	taskEnv        TaskMatchEnv
	probeEnv       ProbeMatchEnv
	metricEnv      MetricMatchEnv
	remediationEnv RemediationMatchEnv
	functions      []expr.Option
	maxNodes       int
	maxComparisons int
}

// NewCompiler creates a Compiler with the four built-in custom
// functions (regex / containsAny / inRange / ago) registered and the
// default limits (MaxNodes=1000, MaxComparisons=3).
func NewCompiler() *Compiler {
	return NewCompilerWithConfig(CompilerConfig{
		MaxNodes:       defaultMaxNodes,
		MaxComparisons: defaultMaxComparisons,
	})
}

// NewCompilerWithConfig creates a Compiler using the supplied
// CompilerConfig. Zero-valued fields fall back to defaults; a
// negative MaxComparisons disables the comparison-count check.
//
// The supplied CustomFunctions are appended to the four built-in
// custom functions so callers extend (rather than replace) the
// default function set.
func NewCompilerWithConfig(cfg CompilerConfig) *Compiler {
	maxNodes := cfg.MaxNodes
	if maxNodes == 0 {
		maxNodes = defaultMaxNodes
	}
	maxComparisons := cfg.MaxComparisons
	if maxComparisons == 0 {
		maxComparisons = defaultMaxComparisons
	}

	functions := make([]expr.Option, 0, 4+len(cfg.CustomFunctions))
	functions = append(functions, regexFn, containsAnyFn, inRangeFn, agoFn)
	functions = append(functions, cfg.CustomFunctions...)

	return &Compiler{
		functions:      functions,
		maxNodes:       maxNodes,
		maxComparisons: maxComparisons,
	}
}

// Compile compiles the expression for the given scene into a reusable
// *vm.Program. The scene selects the Env type contract that drives
// compile-time field-type checking; the resulting program is safe for
// concurrent evaluation via expr.Run.
//
// Before compilation, the expression is parsed and the comparison
// sub-conditions are counted; when the count exceeds the configured
// MaxComparisons limit (and the limit is enabled), Compile returns an
// error wrapping ErrRuleTooManyComparisons so an over-compound rule
// is rejected before it reaches the program cache.
//
// Compile returns an error wrapping ErrRuleInvalidScene when the scene
// is unrecognized, or ErrRuleCompileFailed when the expression fails
// to parse, type-check, or satisfy the sandbox constraints.
func (c *Compiler) Compile(scene Scene, expression string) (*vm.Program, error) {
	if err := c.checkComparisons(expression); err != nil {
		return nil, err
	}

	env, err := c.envFor(scene)
	if err != nil {
		return nil, err
	}

	opts := make([]expr.Option, 0, len(builtinWhitelist)+len(c.functions)+5)
	opts = append(opts,
		expr.AsBool(),
		expr.MaxNodes(uint(c.maxNodes)),
		expr.DisableAllBuiltins(),
		expr.Env(env),
	)
	for _, name := range builtinWhitelist {
		opts = append(opts, expr.EnableBuiltin(name))
	}
	opts = append(opts, c.functions...)

	program, err := expr.Compile(expression, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRuleCompileFailed, err)
	}
	return program, nil
}

// CompileSub compiles a sub-expression of a rule (an operand such as
// `alert.metrics["cpu"]`, or a comparison such as
// `alert.metrics["cpu"] > 80`) into a reusable *vm.Program. It shares
// the same env contract, builtin whitelist, and custom functions as
// Compile, but omits expr.AsBool so operand sub-expressions can be
// evaluated to their native type (float64, string, ...).
//
// CompileSub intentionally skips the MaxComparisons check: the
// sub-expression is a fragment of an already-validated rule, so the
// whole-expression limit enforced by Compile suffices.
//
// The scene selects the Env type contract that drives compile-time
// field-type checking; the resulting program is safe for concurrent
// evaluation via expr.Run. CompileSub returns an error wrapping
// ErrRuleInvalidScene when the scene is unrecognized, or
// ErrRuleCompileFailed when the sub-expression fails to parse,
// type-check, or satisfy the sandbox constraints.
func (c *Compiler) CompileSub(scene Scene, source string) (*vm.Program, error) {
	env, err := c.envFor(scene)
	if err != nil {
		return nil, err
	}

	opts := make([]expr.Option, 0, len(builtinWhitelist)+len(c.functions)+4)
	opts = append(opts,
		expr.MaxNodes(uint(c.maxNodes)),
		expr.DisableAllBuiltins(),
		expr.Env(env),
	)
	for _, name := range builtinWhitelist {
		opts = append(opts, expr.EnableBuiltin(name))
	}
	opts = append(opts, c.functions...)

	program, err := expr.Compile(source, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRuleCompileFailed, err)
	}
	return program, nil
}

// envFor returns the zero-value env contract for the supplied scene.
// Returns an ErrRuleInvalidScene-wrapped error for an unrecognized
// scene.
func (c *Compiler) envFor(scene Scene) (any, error) {
	switch scene {
	case SceneTask:
		return c.taskEnv, nil
	case SceneProbe:
		return c.probeEnv, nil
	case SceneMetric:
		return c.metricEnv, nil
	case SceneRemediation:
		return c.remediationEnv, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrRuleInvalidScene, scene)
	}
}

// checkComparisons parses the expression, counts the comparison
// sub-conditions, and returns an ErrRuleTooManyComparisons-wrapped
// error when the count exceeds the configured limit. The check is
// skipped when the limit is disabled (maxComparisons < 0) or when
// parsing fails (the parse error is surfaced later by expr.Compile
// as an ErrRuleCompileFailed error, preserving the original
// diagnostics).
func (c *Compiler) checkComparisons(expression string) error {
	if c.maxComparisons <= 0 {
		return nil
	}
	tree, err := parser.Parse(expression)
	if err != nil {
		// Defer to expr.Compile for the canonical parse-error message.
		return nil
	}
	count := countComparisons(tree.Node)
	if count > c.maxComparisons {
		return fmt.Errorf("%w: %d comparisons exceed limit %d",
			ErrRuleTooManyComparisons, count, c.maxComparisons)
	}
	return nil
}

// countComparisons walks the AST rooted at node and returns the
// number of BinaryNode comparisons (>, >=, <, <=, ==, !=) it
// contains. Boolean (&&, ||) and arithmetic (+, -, ...) operators
// are traversed but do not, on their own, count as comparisons.
//
// The walk recovers from panics raised by ast.Walk on unrecognized
// node types, returning the count accumulated up to that point so a
// future node type never crashes the compile path.
func countComparisons(node ast.Node) int {
	if node == nil {
		return 0
	}
	var count int
	visitor := &countComparisonVisitor{onMatch: func() { count++ }}
	defer func() { _ = recover() }()
	n := node
	ast.Walk(&n, visitor)
	return count
}

// countComparisonVisitor implements ast.Visitor, incrementing its
// callback for every BinaryNode whose operator is a comparison.
type countComparisonVisitor struct {
	onMatch func()
}

func (v *countComparisonVisitor) Visit(node *ast.Node) {
	if node == nil || *node == nil {
		return
	}
	bn, ok := (*node).(*ast.BinaryNode)
	if !ok {
		return
	}
	if _, isCmp := comparisonOperators[bn.Operator]; isCmp {
		v.onMatch()
	}
}
