// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package expr

import "errors"

var (
	// ErrCompileFailed wraps every compile-time failure: syntax errors,
	// unknown top-level variables, type mismatches, non-boolean results,
	// and MaxNodes overflow. The wrapped expr-lang error carries the
	// position information and should be surfaced to the user verbatim.
	ErrCompileFailed = errors.New("expression compile failed")

	// ErrEvalFailed wraps a runtime evaluation failure. Runtime failures
	// are rare because types are checked at compile time; the remaining
	// cause is a typo inside a map-backed domain object (for example
	// asset.naem), which entry validation catches via sample evaluation.
	ErrEvalFailed = errors.New("expression eval failed")

	// ErrNotBool reports that RunBool was called on a program whose
	// result is not a boolean. This can only happen for programs produced
	// by CompileSub (fragments without the AsBool constraint); programs
	// from Compile always yield a boolean.
	ErrNotBool = errors.New("expression result is not boolean")

	// ErrNilEnv reports that a nil env was supplied where an env type
	// contract is required. Compilation and cache keying both derive the
	// contract from the env value's type.
	ErrNilEnv = errors.New("expression env is nil")
)
