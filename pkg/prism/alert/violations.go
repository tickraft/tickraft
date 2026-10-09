// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"sync"

	exprlangast "github.com/expr-lang/expr/ast"
	exprlangparser "github.com/expr-lang/expr/parser"

	"github.com/tickraft/tickraft/pkg/expr"
)

// comparisonOperators enumerates the expr-lang binary operators that
// constitute a numeric threshold comparison. Boolean (&&, ||) and
// arithmetic (+, -, ...) operators are traversed during the AST walk
// but do not, on their own, yield a metric Violation.
var comparisonOperators = map[string]struct{}{
	">":  {},
	">=": {},
	"<":  {},
	"<=": {},
	"==": {},
	"!=": {},
}

// ViolationExtractor evaluates the metric-fact comparisons of a rule
// expression against an AlertEnv and produces a Violation for every
// comparison that holds. Each produced Violation is stamped with the
// rule's ID and name so downstream records attribute to the rule.
//
// Only comparisons rooted at the metrics map are metric facts
// (metrics["cpu"] > 90, metrics.mem >= 85). Comparisons over the env
// scalars (type == "status", severity == "critical", ...) are event
// predicates: they refine whether the rule matches, not what it
// measured, and fabricating a metric violation from them would replace
// the event's real payload violation (the status or log context carried
// by the alert) with a meaningless one. Predicate-only rules therefore
// yield no violations and the payload violations survive dispatch.
//
// A compound rule such as
//
//	metrics["cpu"] > 90 && metrics["mem"] > 85
//
// yields two Violations when both conditions hold, and a single
// Violation when only one holds (for example when the conditions are
// combined with ||). Single-condition rules such as
// `metrics["cpu"] > 80` yield one Violation when they match.
//
// The extractor parses each rule expression into an AST (cached by
// source text), walks it to find BinaryNode comparisons, and lazily
// compiles+runs each comparison (and its operands) as a standalone
// sub-program via the pkg/expr kernel. Sub-programs are cached by
// source text so repeated matches against the same rule do not
// recompile.
//
// Performance optimization: when the caller reports that the full rule
// expression already matched (matched=true) and the expression is a pure
// conjunction of comparisons (only && combining comparisons, no ||), every
// comparison sub-condition is necessarily true. In that common case the
// extractor builds Violations for all comparisons without re-evaluating
// each one, eliminating the double-evaluation cost of running the full
// program and then re-running every comparison. For rules that contain ||
// the extractor falls back to per-comparison evaluation to determine which
// branches actually matched.
//
// The cache is cleared by the Engine on Load/Reload so stale parse trees
// and sub-programs from retired rules do not accumulate indefinitely.
type ViolationExtractor struct {
	compiler *Compiler

	mu          sync.Mutex
	astCache    map[string]*exprlangparser.Tree
	subPrograms map[string]*expr.Program
}

// NewViolationExtractor creates a ViolationExtractor that reuses the
// supplied Compiler's env contract for sub-program compilation. A nil
// Compiler is replaced with a fresh one so callers never need to
// nil-check.
func NewViolationExtractor(compiler *Compiler) *ViolationExtractor {
	if compiler == nil {
		compiler = NewCompiler()
	}
	return &ViolationExtractor{
		compiler:    compiler,
		astCache:    make(map[string]*exprlangparser.Tree),
		subPrograms: make(map[string]*expr.Program),
	}
}

// comparison describes a single threshold comparison extracted from a
// rule expression AST.
type comparison struct {
	source string
	left   exprlangast.Node
	right  exprlangast.Node
}

// Extract parses the rule expression, evaluates every comparison
// sub-condition against env, and returns a Violation for each condition
// that holds. Returns nil when the expression has no comparison
// sub-conditions, none of them evaluate to true, or the expression
// cannot be parsed (the Engine skips un-compilable rules at Load time,
// so a parse failure here is unexpected and is silently ignored so it
// never blocks alert dispatch).
//
// The matched parameter reports whether the full rule expression already
// evaluated to true for env. When matched is true and the expression is a
// pure conjunction of comparisons, Extract skips per-comparison
// re-evaluation (every comparison is implied true) and builds Violations
// directly, eliminating the double-evaluation cost. When matched is false
// or the expression is not a pure conjunction, each comparison is
// evaluated individually.
func (x *ViolationExtractor) Extract(_ context.Context, rule Rule, env AlertEnv, matched bool) []Violation {
	tree, ok := x.parseCached(rule.Expression)
	if !ok {
		return nil
	}
	comparisons := collectComparisons(tree.Node)
	// Keep only the metric-fact comparisons; scalar predicates do not
	// yield violations (see the type doc).
	metricFacts := make([]comparison, 0, len(comparisons))
	for _, c := range comparisons {
		if _, ok := metricsKey(c.left); ok {
			metricFacts = append(metricFacts, c)
		}
	}
	if len(metricFacts) == 0 {
		return nil
	}

	// Fast path: the full rule already matched and the expression is a
	// pure conjunction of comparisons, so every comparison sub-condition
	// is implied true. Build Violations for all of them without
	// re-evaluating each comparison.
	if matched && isPureConjunction(tree.Node) {
		violations := make([]Violation, 0, len(metricFacts))
		for _, c := range metricFacts {
			violations = append(violations, x.buildViolation(c, rule, env))
		}
		return violations
	}

	// Slow path: evaluate each comparison individually to determine which
	// sub-conditions actually hold. This is required for rules containing
	// || (a matched || does not imply both branches matched).
	violations := make([]Violation, 0, len(metricFacts))
	for _, c := range metricFacts {
		matchedSub, ok := x.evalBool(c.source, env)
		if !ok || !matchedSub {
			continue
		}
		violations = append(violations, x.buildViolation(c, rule, env))
	}
	if len(violations) == 0 {
		return nil
	}
	return violations
}

// Reset clears the AST and sub-program caches. The Engine calls Reset
// on Load so parse trees and sub-programs compiled for retired rule
// expressions are released and do not accumulate across reloads.
func (x *ViolationExtractor) Reset() {
	x.mu.Lock()
	x.astCache = make(map[string]*exprlangparser.Tree)
	x.subPrograms = make(map[string]*expr.Program)
	x.mu.Unlock()
}

// parseCached returns the cached parsed tree for expression, parsing it
// on first use. The bool result is false when parsing fails or the AST
// walk panics on an unrecognized node type; both cases are treated as
// "no violations".
func (x *ViolationExtractor) parseCached(expression string) (*exprlangparser.Tree, bool) {
	x.mu.Lock()
	if tree, ok := x.astCache[expression]; ok {
		x.mu.Unlock()
		return tree, true
	}
	x.mu.Unlock()

	tree, err := exprlangparser.Parse(expression)
	if err != nil {
		return nil, false
	}

	x.mu.Lock()
	// Another goroutine may have parsed the same expression concurrently;
	// prefer the existing entry to avoid pinning an extra tree.
	if existing, ok := x.astCache[expression]; ok {
		x.mu.Unlock()
		return existing, true
	}
	x.astCache[expression] = tree
	x.mu.Unlock()
	return tree, true
}

// collectComparisons walks the AST to collect every BinaryNode whose
// operator is a numeric comparison. It recovers from panics raised by
// ast.Walk on unrecognized node types, returning nil so a future node
// type never crashes the dispatch path.
func collectComparisons(node exprlangast.Node) []comparison {
	var out []comparison
	visitor := &comparisonVisitor{onCollect: func(c comparison) {
		out = append(out, c)
	}}
	defer func() {
		if r := recover(); r != nil {
			// ast.Walk panics on unrecognized node types. Treat as
			// "no violations" so a future node type never crashes the
			// dispatch path.
			out = nil
		}
	}()
	n := node
	exprlangast.Walk(&n, visitor)
	return out
}

// isPureConjunction reports whether node is a comparison or a
// conjunction (&&) of comparisons, with no disjunction (||) or other
// boolean structure. When true, a matched (true) expression implies
// every comparison sub-condition is also true, so the extractor can
// skip per-comparison re-evaluation.
func isPureConjunction(node exprlangast.Node) bool {
	switch n := node.(type) {
	case *exprlangast.BinaryNode:
		if _, isCmp := comparisonOperators[n.Operator]; isCmp {
			return true
		}
		if n.Operator == "&&" {
			return isPureConjunction(n.Left) && isPureConjunction(n.Right)
		}
		return false
	default:
		return false
	}
}

// comparisonVisitor implements ast.Visitor, appending every comparison
// BinaryNode it encounters to the caller-provided callback.
type comparisonVisitor struct {
	onCollect func(comparison)
}

//nolint:gocritic // implements the expr-lang ast.Visitor interface whose signature is fixed
func (v *comparisonVisitor) Visit(node *exprlangast.Node) {
	if node == nil || *node == nil {
		return
	}
	bn, ok := (*node).(*exprlangast.BinaryNode)
	if !ok {
		return
	}
	if _, isCmp := comparisonOperators[bn.Operator]; !isCmp {
		return
	}
	v.onCollect(comparison{
		source: bn.String(),
		left:   bn.Left,
		right:  bn.Right,
	})
}

// buildViolation constructs a Violation from a matched metric-fact
// comparison, resolving the observed value and threshold by evaluating
// the operands against env. The violation is stamped with the producing
// rule's id and name so persisted records attribute to the rule.
//
// The resulting Violation carries the env's Severity and Source so
// downstream ranking and rendering keep the information (the env
// projected them from the event's primary violation).
func (x *ViolationExtractor) buildViolation(c comparison, rule Rule, env AlertEnv) Violation {
	v := Violation{
		Kind:     ViolationKindMetric,
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Severity: env.Severity,
		Source:   env.Source,
		Metric: &MetricContext{
			Name: extractMetricName(c.left),
		},
	}
	if value, ok := x.evalValue(c.left, env); ok {
		v.Metric.Value = value
	}
	if threshold, ok := x.evalValue(c.right, env); ok {
		v.Metric.Threshold = threshold
	}
	return v
}

// evalBool compiles (cached) and runs a comparison sub-expression,
// returning the boolean result. The bool ok result is false when the
// sub-expression cannot be compiled or does not yield a bool.
func (x *ViolationExtractor) evalBool(source string, env AlertEnv) (value, ok bool) {
	prog, err := x.compileSub(source)
	if err != nil {
		return false, false
	}
	out, err := expr.Run(prog, env)
	if err != nil {
		return false, false
	}
	b, ok := out.(bool)
	return b, ok
}

// evalValue compiles (cached) and runs an operand sub-expression and
// coerces the result to float64. The bool ok result is false when the
// sub-expression cannot be compiled, run, or coerced to a number.
func (x *ViolationExtractor) evalValue(node exprlangast.Node, env AlertEnv) (float64, bool) {
	if node == nil {
		return 0, false
	}
	prog, err := x.compileSub(node.String())
	if err != nil {
		return 0, false
	}
	out, err := expr.Run(prog, env)
	if err != nil {
		return 0, false
	}
	return toFloat64(out)
}

// compileSub returns the cached sub-program for source, compiling it on
// first use via the Compiler's AlertEnv contract. Compilation errors
// are returned to the caller, which treats them as "skip this
// sub-condition".
func (x *ViolationExtractor) compileSub(source string) (*expr.Program, error) {
	x.mu.Lock()
	if prog, ok := x.subPrograms[source]; ok {
		x.mu.Unlock()
		return prog, nil
	}
	x.mu.Unlock()

	prog, err := x.compiler.CompileSub(source)
	if err != nil {
		return nil, err
	}

	x.mu.Lock()
	// Another goroutine may have compiled the same source concurrently;
	// prefer the existing entry to avoid pinning an extra program.
	if existing, ok := x.subPrograms[source]; ok {
		x.mu.Unlock()
		return existing, nil
	}
	x.subPrograms[source] = prog
	x.mu.Unlock()
	return prog, nil
}

// extractMetricName derives the metric name from the left operand of a
// metric-fact comparison. Extract filters comparisons to metrics-rooted
// member accesses before calling this, so the name always resolves; the
// source-text fallback is defensive for direct callers.
func extractMetricName(node exprlangast.Node) string {
	if node == nil {
		return ""
	}
	if name, ok := metricsKey(node); ok {
		return name
	}
	return node.String()
}

// metricsKey returns the metric key when node is a member access rooted
// at the top-level metrics map in either index or dot form
// (`metrics["cpu"]` or `metrics.cpu`). The bool result is false for any
// other shape; callers use it to distinguish metric facts from scalar
// event predicates.
func metricsKey(node exprlangast.Node) (string, bool) {
	member, ok := node.(*exprlangast.MemberNode)
	if !ok {
		return "", false
	}
	var key string
	switch prop := member.Property.(type) {
	case *exprlangast.StringNode:
		key = prop.Value
	case *exprlangast.IdentifierNode:
		key = prop.Value
	default:
		return "", false
	}
	if key == "" {
		return "", false
	}
	root, ok := member.Node.(*exprlangast.IdentifierNode)
	if !ok || root.Value != "metrics" {
		return "", false
	}
	return key, true
}

// toFloat64 coerces an expr-lang operand result to float64. It reports
// false for non-numeric values (strings, bools, nil, collections).
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}
