// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package expr

import (
	"fmt"
	"reflect"

	exprlang "github.com/expr-lang/expr"
	exprlangvm "github.com/expr-lang/expr/vm"
)

// isNilEnv reports whether env is untyped nil or a typed nil pointer —
// either would panic inside expr-lang's env analysis, so both are
// rejected up front with ErrNilEnv.
func isNilEnv(env any) bool {
	if env == nil {
		return true
	}
	switch t := reflect.TypeOf(env); t.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return reflect.ValueOf(env).IsNil()
	default:
		return false
	}
}

// defaultMaxNodes is the AST node budget of every compiled expression.
// It bounds compile and eval cost of oversized (for example
// accidentally pasted) expressions.
const defaultMaxNodes = 1000

// Program is an opaque handle to a compiled expression. It is immutable
// and safe for concurrent evaluation via Run.
type Program struct {
	inner *exprlangvm.Program
}

// Compiler compiles expressions under the fixed minimal sandbox:
// MaxNodes, AsBool, and Env type checking. It holds no mutable state
// and is safe for concurrent use.
type Compiler struct {
	maxNodes int
}

// NewCompiler creates a Compiler with the default constraint set
// (MaxNodes=1000, AsBool, Env type checking; all expr-lang builtins
// enabled; no custom functions).
func NewCompiler() *Compiler {
	return &Compiler{maxNodes: defaultMaxNodes}
}

// Compile compiles a predicate expression against the env type
// contract and returns the reusable Program. The env value may be an
// instance or a type of the env struct; its shape drives compile-time
// checking of top-level variable names and types.
//
// Compile enforces that the expression yields a boolean (AsBool) and
// fits the node budget. It returns an error wrapping ErrCompileFailed
// (with the expr-lang diagnostic, including position, as the cause) or
// ErrNilEnv when env is nil.
func (c *Compiler) Compile(env any, expression string) (*Program, error) {
	if isNilEnv(env) {
		return nil, ErrNilEnv
	}
	program, err := exprlang.Compile(expression,
		exprlang.AsBool(),
		exprlang.MaxNodes(uint(c.maxNodes)),
		exprlang.Env(env),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCompileFailed, err)
	}
	return &Program{inner: program}, nil
}

// CompileSub compiles a fragment of an already-validated expression —
// an operand (such as asset.id) or a comparison (such as
// metrics["cpu"] > 90) — so it can be evaluated to its native type.
// It shares the env contract and node budget with Compile but omits
// AsBool, because fragments are not predicates.
func (c *Compiler) CompileSub(env any, source string) (*Program, error) {
	if isNilEnv(env) {
		return nil, ErrNilEnv
	}
	program, err := exprlang.Compile(source,
		exprlang.MaxNodes(uint(c.maxNodes)),
		exprlang.Env(env),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCompileFailed, err)
	}
	return &Program{inner: program}, nil
}

// Run evaluates the program against env and returns the raw result.
// It returns an error wrapping ErrEvalFailed on a runtime failure.
func Run(program *Program, env any) (any, error) {
	out, err := exprlang.Run(program.inner, env)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEvalFailed, err)
	}
	return out, nil
}

// RunBool evaluates the program against env and asserts a boolean
// result. Programs from Compile always satisfy the assertion; programs
// from CompileSub yield ErrNotBool when their result is not a boolean.
func RunBool(program *Program, env any) (bool, error) {
	out, err := Run(program, env)
	if err != nil {
		return false, err
	}
	b, ok := out.(bool)
	if !ok {
		return false, fmt.Errorf("%w: got %T", ErrNotBool, out)
	}
	return b, nil
}
