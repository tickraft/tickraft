// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

package executor

import (
	"strings"

	"go.uber.org/zap"

	"github.com/bytedance/sonic"

	"github.com/tickraft/tickraft/pkg/expr"
	"github.com/tickraft/tickraft/pkg/types"
)

// judgmentCacheCapacity bounds the LRU cache of compiled judgment programs.
const judgmentCacheCapacity = 512

// judgmentCache caches compiled execution-judgment programs (LRU, keyed by
// env type + expression) and is shared by every judgment call site.
var judgmentCache = expr.NewProgramCache(judgmentCacheCapacity)

// ConfigExpression extracts the optional "expression" key from an
// executor config JSON blob. It is the cross-executor convention carrying
// the execution judgment from configuration stores (monitor points,
// remediation rules) into the Metadata channel consumed by ApplyJudgment.
// Assembly layers call this when building an ExecutionRequest; the runner
// itself only reads Metadata (the same split as max_retries).
func ConfigExpression(config string) string {
	if config == "" {
		return ""
	}
	var raw struct {
		Expression string `json:"expression"`
	}
	if err := sonic.Unmarshal([]byte(config), &raw); err != nil {
		return ""
	}
	return raw.Expression
}

// ApplyJudgment evaluates a user-defined execution judgment expression
// against the ExecutionEnv projection of result and overrides the result's
// protocol-default status. It is the single judgment implementation,
// invoked at every point an executor result is consumed before a
// success/failure decision: the runner's retry decision (doExecute) and
// the remediation operators' circuit-breaker outcome.
//
// Semantics:
//   - An empty expression leaves the result untouched (protocol default).
//   - true judges success: Status is forced to normal.
//   - false judges failure: Status is forced to abnormal and a note is
//     appended to ErrorMsg.
//   - A runtime evaluation failure warns and falls back to the protocol
//     default; the result keeps its executor-assigned status.
//
// A nil result is a no-op.
func ApplyJudgment(expression string, result *Result, logger *zap.Logger) {
	if expression == "" || result == nil {
		return
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	ok, err := judgmentCache.Eval(expression, buildExecutionEnv(result))
	if err != nil {
		logger.Warn("executor: judgment expression eval failed, falling back to protocol default",
			zap.String("expr", expression),
			zap.Error(err),
		)
		return
	}
	if ok {
		result.Status = types.AssetStatusNormal
		return
	}
	result.Status = types.AssetStatusAbnormal
	result.ErrorMsg = appendJudgmentNote(result.ErrorMsg)
}

// judgmentFailureNote is appended to ErrorMsg when the user expression
// judges an execution failed.
const judgmentFailureNote = "expression judged failure"

// appendJudgmentNote appends the judgment-failure note to an error
// message, tolerating an empty message and avoiding duplicates when the
// executor already recorded the same note on an earlier attempt.
func appendJudgmentNote(errorMsg string) string {
	if errorMsg == "" {
		return judgmentFailureNote
	}
	if strings.Contains(errorMsg, judgmentFailureNote) {
		return errorMsg
	}
	return errorMsg + "; " + judgmentFailureNote
}
