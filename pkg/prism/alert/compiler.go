// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"github.com/tickraft/tickraft/pkg/expr"
)

// alertEnv is the zero-value instance kept purely as the type contract
// handed to the kernel compiler; it is never evaluated.
var alertEnv AlertEnv

// Compiler compiles alert rule expressions against the AlertEnv
// contract by delegating to the pkg/expr kernel. It applies the fixed
// minimal sandbox (MaxNodes, AsBool, Env type checking; all expr-lang
// builtins; zero custom functions) and is safe for concurrent use.
type Compiler struct {
	kernel *expr.Compiler
}

// NewCompiler creates a Compiler with the kernel's default constraint
// set.
func NewCompiler() *Compiler {
	return &Compiler{kernel: expr.NewCompiler()}
}

// Compile compiles a predicate expression and returns the reusable
// kernel program. Errors wrap expr.ErrCompileFailed with the expr-lang
// diagnostic as the cause.
func (c *Compiler) Compile(expression string) (*expr.Program, error) {
	return c.kernel.Compile(alertEnv, expression)
}

// CompileSub compiles a fragment of an already-validated expression —
// an operand or a comparison — for ViolationExtractor's native-type
// evaluation. It omits the AsBool constraint; see expr.Compiler.
func (c *Compiler) CompileSub(source string) (*expr.Program, error) {
	return c.kernel.CompileSub(alertEnv, source)
}

// ValidateExpression checks an expression against the AlertEnv contract
// via the kernel's compile + domain-key + sample-evaluation pipeline. It
// is the package-level validation source shared by callers without a
// Compiler instance (the /expr/validate endpoint).
func ValidateExpression(expression string) error {
	return expr.Validate(ExampleEnv(), expression)
}

// Validate checks an expression against the AlertEnv contract via the
// kernel's compile + domain-key + sample-evaluation pipeline. It is the
// single validation source shared by the store write path and the
// /expr/validate endpoint.
func (c *Compiler) Validate(expression string) error {
	return ValidateExpression(expression)
}
