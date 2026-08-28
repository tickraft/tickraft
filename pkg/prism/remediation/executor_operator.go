// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package remediation

import (
	"context"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/types"
)

// executorOperatorTimeout bounds a single executor-backed remediation
// action so a hung target cannot hold a remediation worker indefinitely.
const executorOperatorTimeout = 120 * time.Second

// executorOperator adapts an executor.Executor (local, webhook, http, ...)
// to the remediation Operator SPI. It allows remediation rules to select
// any registered executor type as their action, with the rule's
// ExecutorConfig passed through as the executor configuration JSON.
type executorOperator struct {
	name   string
	exec   executor.Executor
	logger *zap.Logger
}

// NewExecutorOperator wraps the given executor as a remediation Operator
// under the given name. The name must match Rule.ExecutorType.
func NewExecutorOperator(name string, exec executor.Executor, logger *zap.Logger) Operator {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &executorOperator{name: name, exec: exec, logger: logger}
}

// Name returns the operator identifier, matching Rule.ExecutorType.
func (o *executorOperator) Name() string { return o.name }

// Execute runs the wrapped executor with the remediation run mapped onto
// an executor.ExecutionRequest. A non-nil error indicates an
// infrastructure failure; a nil error with Success=false indicates the
// action ran but failed.
func (o *executorOperator) Execute(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error) {
	if o.exec == nil {
		return nil, errdefs.ErrInvalidArgument
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = executorOperatorTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	startedAt := time.Now()
	// The optional "expression" key of the rule's executor config rides
	// the metadata channel and is applied to
	// the result below so the user-defined judgment drives the circuit
	// breaker outcome.
	metadata := map[string]string{
		"remediation": "true",
		"rule_id":     strconv.FormatInt(req.RuleID, 10),
	}
	if exprStr := executor.ConfigExpression(req.Config); exprStr != "" {
		metadata["expression"] = exprStr
	}
	res, err := o.exec.Execute(runCtx, executor.ExecutionRequest{
		ID:           req.RuleID,
		TenantID:     req.TenantID,
		AssetID:      req.AssetID,
		ExecutorName: o.name,
		Config:       req.Config,
		Operation:    executor.OpExecute,
		Timeout:      timeout,
		RunID:        req.RunID,
		TriggerType:  "event",
		Metadata:     metadata,
	})
	duration := time.Since(startedAt)
	if err != nil {
		return &ExecutionResult{Success: false, Duration: duration, ErrorMsg: err.Error()}, nil
	}
	if res == nil {
		return &ExecutionResult{Success: false, Duration: duration, ErrorMsg: "executor returned no result"}, nil
	}
	executor.ApplyJudgment(metadata["expression"], res, o.logger)
	return &ExecutionResult{
		Success:  res.Status == types.AssetStatusNormal,
		Output:   res.Body,
		ErrorMsg: res.ErrorMsg,
		Duration: duration,
	}, nil
}
