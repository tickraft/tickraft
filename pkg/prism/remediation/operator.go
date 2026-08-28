// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package remediation

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/executor/local"
	"github.com/tickraft/tickraft/pkg/types"
)

// defaultOperatorTimeout is the default execution timeout for a remediation
// operator when the request does not carry one.
const defaultOperatorTimeout = 120 * time.Second

// LocalOperator runs remediation scripts on the host machine via the
// pkg/executor/local executor. It is the only operator shipped with the
// default deployment. callers may register additional operators
// (ssh, mysql, redis, ...) against the same Operator SPI.
type LocalOperator struct {
	exec   *local.Executor
	logger *zap.Logger
}

// OperatorOption configures a LocalOperator.
type OperatorOption interface {
	apply(*LocalOperator)
}

// operatorLoggerOption sets the structured logger for a LocalOperator.
type operatorLoggerOption struct {
	logger *zap.Logger
}

func (o operatorLoggerOption) apply(op *LocalOperator) {
	if o.logger != nil {
		op.logger = o.logger
	}
}

// WithOperatorLogger sets the structured logger.
func WithOperatorLogger(logger *zap.Logger) OperatorOption {
	return operatorLoggerOption{logger: logger}
}

// NewLocalOperator creates a LocalOperator wrapping the given local executor.
// When exec is nil a default local executor is constructed.
func NewLocalOperator(exec *local.Executor, options ...OperatorOption) *LocalOperator {
	op := &LocalOperator{logger: zap.NewNop()}
	if exec == nil {
		exec = local.New(local.WithLogger(op.logger))
	}
	op.exec = exec
	for _, o := range options {
		o.apply(op)
	}
	return op
}

// Name returns the operator identifier, matching Rule.ExecutorType "local".
func (op *LocalOperator) Name() string { return localExecutorName }

// Execute runs the configured local command. A non-nil error indicates an
// infrastructure failure; a nil error with Success=false indicates the
// command ran but failed (non-zero exit or timeout). The circuit breaker
// counts the latter as a failure.
func (op *LocalOperator) Execute(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultOperatorTimeout
	}
	// Enforce the per-request timeout so a hung script cannot block the
	// remediation worker indefinitely. The local executor also applies its
	// own configured timeout; the shorter of the two wins.
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// The optional "expression" key of the rule's executor config rides
	// the metadata channel and is applied to
	// the result below so the user-defined judgment drives the circuit
	// breaker outcome.
	metadata := map[string]string{"remediation": "true"}
	if exprStr := executor.ConfigExpression(req.Config); exprStr != "" {
		metadata["expression"] = exprStr
	}

	er := executor.ExecutionRequest{
		TenantID:     req.TenantID,
		AssetID:      req.AssetID,
		ExecutorName: localExecutorName,
		Config:       req.Config,
		Operation:    executor.OpExecute,
		RunID:        req.RunID,
		TriggerType:  "remediation",
		Timeout:      timeout,
		Metadata:     metadata,
	}
	res, err := op.exec.Execute(runCtx, er)
	if err != nil {
		return nil, fmt.Errorf("remediation: local execute: %w", err)
	}
	executor.ApplyJudgment(metadata["expression"], res, op.logger)

	out := &ExecutionResult{
		Output:   res.Body,
		ErrorMsg: res.ErrorMsg,
		Duration: res.Duration,
	}
	// A timeout or non-normal status is a remediation failure (counted by
	// the circuit breaker) rather than an infrastructure error.
	out.Success = res.Status == types.AssetStatusNormal
	return out, nil
}

// localExecutorName is the executor name registered by pkg/executor/local.
const localExecutorName = "local"

// Compile-time interface assertion.
var _ Operator = (*LocalOperator)(nil)
