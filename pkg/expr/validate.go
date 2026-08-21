// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package expr

import (
	"fmt"
	"sort"
	"strings"

	exprlangast "github.com/expr-lang/expr/ast"
	exprlangparser "github.com/expr-lang/expr/parser"
)

// validateCompiler is the shared compiler used by Validate. Validation
// runs on API write paths and the /expr/validate endpoint, neither of
// which is hot enough to warrant caching.
var validateCompiler = NewCompiler()

// Validate checks that expression is a well-formed predicate for the
// env contract and returns nil when it is. The sample must be a fully
// populated example env (each env definition exports an ExampleEnv for
// this purpose).
//
// Three passes catch complementary error classes:
//
//   - compilation rejects syntax errors, unknown top-level variables,
//     type mismatches, non-boolean results, and MaxNodes overflow;
//   - the DomainMap key check rejects typos in closed domain objects
//     (asset.naem), which compile silently and evaluate to nil because
//     the field type is any;
//   - the sample run rejects residual runtime errors, such as ordered
//     comparisons between mismatched map-field types.
//
// Validate is side-effect free and does not persist anything. Errors
// wrap ErrCompileFailed or ErrEvalFailed with a diagnostic suitable for
// direct display to the user.
func Validate(sample any, expression string) error {
	program, err := validateCompiler.Compile(sample, expression)
	if err != nil {
		return err
	}
	if err := checkDomainKeys(sample, expression); err != nil {
		return err
	}
	_, err = Run(program, sample)
	return err
}

// checkDomainKeys parses expression and verifies that every constant
// member access whose base is a static env path (an identifier chain
// like asset or asset.tags) lands on an existing key when the base
// resolves to a DomainMap. Accesses on open maps and computed bases
// are skipped.
func checkDomainKeys(sample any, expression string) error {
	tree, err := exprlangparser.Parse(expression)
	if err != nil {
		// Already surfaced by Compile; parse here only to obtain the AST.
		return nil
	}
	visitor := &domainKeyVisitor{sample: sample}
	node := tree.Node
	defer func() {
		// ast.Walk panics on unrecognized node types; the sample run in
		// Validate still covers such expressions, so treat a panicking
		// walk as "nothing found" rather than failing validation.
		_ = recover()
	}()
	exprlangast.Walk(&node, visitor)
	return visitor.err
}

// domainKeyVisitor implements ast.Visitor, checking one MemberNode per
// Visit call.
type domainKeyVisitor struct {
	sample any
	err    error
}

//nolint:gocritic // implements the expr-lang ast.Visitor interface whose signature is fixed
func (v *domainKeyVisitor) Visit(node *exprlangast.Node) {
	if node == nil || *node == nil || v.err != nil {
		return
	}
	member, ok := (*node).(*exprlangast.MemberNode)
	if !ok {
		return
	}
	prop, ok := member.Property.(*exprlangast.StringNode)
	if !ok || prop.Value == "" {
		return
	}
	if !isStaticPath(member.Node) {
		return
	}
	v.check(member.Node.String(), prop.Value)
}

// check evaluates base against the sample and, when it resolves to a
// DomainMap, requires key to be present.
func (v *domainKeyVisitor) check(base, key string) {
	program, err := validateCompiler.CompileSub(v.sample, base)
	if err != nil {
		return
	}
	value, err := Run(program, v.sample)
	if err != nil {
		return
	}
	domain, ok := value.(DomainMap)
	if !ok {
		return
	}
	if _, present := domain[key]; present {
		return
	}
	v.err = fmt.Errorf("%w: unknown field %s.%s (available: %s)",
		ErrCompileFailed, base, key, sortedKeys(domain))
}

// isStaticPath reports whether node is an identifier or a chain of
// constant member accesses rooted at an identifier — the shape whose
// value is fully determined by the env (asset, asset.tags, ...).
func isStaticPath(node exprlangast.Node) bool {
	switch n := node.(type) {
	case *exprlangast.IdentifierNode:
		return true
	case *exprlangast.MemberNode:
		if _, ok := n.Property.(*exprlangast.StringNode); !ok {
			return false
		}
		return isStaticPath(n.Node)
	default:
		return false
	}
}

// sortedKeys lists a DomainMap's keys in sorted order for error
// messages.
func sortedKeys(m DomainMap) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
