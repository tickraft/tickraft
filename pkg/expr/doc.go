// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package expr is the domain-agnostic expression kernel shared by every
// rule-matching surface (alert rules, remediation triggers, execution
// judgment). It owns the "how" of expressions — compiling, caching, and
// evaluating under a fixed minimal sandbox — while each consumer surface
// keeps the "when" and "what happens on a match" for itself.
//
// The sandbox applies exactly three structural constraints (see
// ):
//
//   - MaxNodes=1000 bounds the AST size of an expression;
//   - AsBool requires the expression to be a predicate (bool result);
//   - Env enables compile-time checking of top-level variables against
//     the caller's env type.
//
// There is deliberately no builtin whitelist and no comparison-count
// limit: expression authors are privileged administrators, and all
// expr-lang builtins are pure computation. Zero custom functions are
// registered; regex matching is the grammar-level `matches` operator.
//
// Env values are plain structs (typically with `expr` struct tags) whose
// domain objects (asset, metric, status, ...) are map[string]any so both
// `asset.id` and `asset["id"]` reference the same value. Map-backed
// fields cannot be checked at compile time; entry validation closes the
// gap by sample-evaluating the expression once via Validate.
//
// The expr-lang dependency is an implementation detail: this package
// returns the opaque Program type, so consumers do not import expr-lang
// (the single sanctioned exception is the AST walk in
// pkg/prism/alert/violations.go).
package expr
