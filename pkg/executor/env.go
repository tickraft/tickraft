// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package executor

import (
	"github.com/tickraft/tickraft/pkg/expr"
)

// ExecutionEnv is the evaluation environment of the execution judgment
// expression. The judgment is defined per executor config (optional
// "expression" JSON key), transmitted via ExecutionRequest.Metadata,
// and evaluated at the single choke point in the runner after the
// executor returns and before the retry decision.
//
// Variable contract (rule-engine-design §4.3):
//
//	code     int               unified result code (HTTP status, exit
//	                          code, or 0 for tcp/icmp)
//	body     string            response body / output, truncated to the
//	                          executor's cap
//	error    string            execution error message ("" on success)
//	duration float             execution duration in milliseconds
//	metrics  map[string]float  executor-produced metrics
type ExecutionEnv struct {
	// Code is the unified result code: HTTP status for http/webhook,
	// process exit code for local, 0 for tcp/icmp.
	Code int `expr:"code"`
	// Body is the response body or execution output.
	Body string `expr:"body"`
	// Error is the execution error message, empty on success.
	Error string `expr:"error"`
	// Duration is the execution duration in milliseconds.
	Duration float64 `expr:"duration"`
	// Metrics holds the executor-produced numeric metrics.
	Metrics map[string]float64 `expr:"metrics"`
}

// ExampleEnv returns a fully populated sample of ExecutionEnv. It is
// the baseline for entry validation (expr.Validate) and the
// /expr/validate endpoint: every documented field is present with a
// representative value.
func ExampleEnv() ExecutionEnv {
	return ExecutionEnv{
		Code:     200,
		Body:     `{"status":"ok"}`,
		Error:    "",
		Duration: 128.5,
		Metrics:  map[string]float64{"rtt_ms": 42.0},
	}
}

// buildExecutionEnv projects an execution Result into an ExecutionEnv
// for judgment evaluation. A nil result yields the zero env (an empty
// expression never consults it, but the judgment path must stay nil-safe).
func buildExecutionEnv(result *Result) ExecutionEnv {
	env := ExecutionEnv{Metrics: map[string]float64{}}
	if result == nil {
		return env
	}
	// The unified code: HTTP status for protocol executors (StatusCode
	// set, ExitCode always 0), process exit code for command executors
	// (StatusCode always 0), and 0 for connectivity executors.
	env.Code = result.StatusCode
	if env.Code == 0 {
		env.Code = result.ExitCode
	}
	env.Body = result.Body
	env.Error = result.ErrorMsg
	env.Duration = float64(result.Duration.Milliseconds())
	if result.Metrics != nil {
		env.Metrics = result.Metrics
	}
	return env
}

// ValidateExpression checks an execution judgment expression against
// the ExecutionEnv contract via the kernel's compile + domain-key +
// sample-evaluation pipeline. It is the single validation source shared
// by the entry points that accept an "expression" key in executor
// config JSON (monitor points, remediation rules) and the
// /expr/validate endpoint.
func ValidateExpression(expression string) error {
	return expr.Validate(ExampleEnv(), expression)
}
