// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package executor

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/retry"
	"github.com/tickraft/tickraft/pkg/types"
	"go.uber.org/zap"
)

// defaultExecutionTimeout is used when the request does not specify a timeout.
const defaultExecutionTimeout = 30 * time.Second

// doExecute performs the actual task execution: look up executor, run with
// timeout and retry, infer status, publish completion event, and record
// the result.
//
// release is called exactly once when the task — including any async retries
// — has fully completed. It decrements the runner's WaitGroup so that Stop
// can wait for in-flight tasks. release is guarded by sync.Once in dispatch,
// so it is safe to call from both onComplete and the panic recovery handler.
//
// A defer-recover guards the entire execution: if an executor panics despite
// its own recovery logic (or lacks one), the panic is caught here, logged at
// Error level with a stack trace, and the task is finished with an abnormal
// status. This prevents a single misbehaving executor from crashing the
// runner process.
func (r *runner) doExecute(ctx context.Context, req ExecutionRequest, release func()) {
	start := time.Now()

	// Panic isolation: catch any panic that escapes the executor or retry
	// machinery, log it, finish the task as abnormal, and release the
	// WaitGroup slot. release is sync.Once-guarded so calling it here is
	// safe even if onComplete has already run (though that would be a bug
	// in the retry library, not in this code).
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("panic recovered in task execution",
				zap.Int64("task_id", req.ID),
				zap.String("executor_name", req.ExecutorName),
				zap.Any("panic", rec),
				zap.Stack("stack"),
			)
			r.finish(ctx, req, nil, fmt.Errorf("executor panic: %v", rec), 0, start)
			release()
		}
	}()

	executor, err := r.registry.LookupWithOp(req.ExecutorName, req.Operation)
	if err != nil {
		r.logger.Error("executor lookup failed",
			zap.Int64("task_id", req.ID),
			zap.String("executor_name", req.ExecutorName),
			zap.String("operation", req.Operation.String()),
			zap.Error(err),
		)
		r.finish(ctx, req, nil, err, 0, start)
		release()
		return
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultExecutionTimeout
	}

	retryCfg, retryErr := r.buildRetry(req)
	if retryErr != nil {
		r.logger.Error("failed to build retry config, executing without retry",
			zap.Int64("task_id", req.ID),
			zap.Error(retryErr),
		)
		retryCfg = nil
	}

	// attempts tracks how many times execFn was invoked across the
	// retry loop. RetryCount is attempts-1 (the first call is the
	// initial attempt, not a retry). These variables are written by
	// execFn and read by onComplete. They are safe from data races
	// because execFn and onComplete are never invoked concurrently:
	// in the async path the wheel schedules attempts sequentially, and
	// in the sync path they run in the same goroutine.
	var (
		lastResult *Result
		execErr    error
		attempts   int
	)

	execFn := func() error {
		attempts++
		execCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		result, e := executor.Execute(execCtx, req)
		// Release the previous attempt's result before overwriting
		// lastResult. This reuses pooled Result objects across retry
		// attempts, keeping memory pressure low under heavy load.
		if lastResult != nil {
			ReleaseResult(lastResult)
		}
		lastResult = result
		execErr = e
		if e != nil {
			return e
		}
		if result != nil && result.Status != types.AssetStatusNormal {
			return fmt.Errorf("execution status abnormal: %s", result.Status)
		}
		return nil
	}

	onComplete := func(_ error) {
		defer release()
		defer func() {
			// Return the final result to the pool after finish has
			// consumed it. finish copies all needed fields into the
			// event payload and execution record, so the Result is
			// safe to recycle here.
			if lastResult != nil {
				ReleaseResult(lastResult)
			}
		}()
		retryCount := 0
		if attempts > 1 {
			retryCount = attempts - 1
		}
		r.finish(ctx, req, lastResult, execErr, retryCount, start)
	}

	switch {
	case retryCfg != nil && r.wheel != nil:
		// Async retry via time wheel: retry delays are scheduled as
		// one-shot wheel callbacks, freeing the worker goroutine
		// during waits.
		if e := retryCfg.DoAsync(ctx, execFn, r.wheel, onComplete); e != nil {
			r.logger.Debug("async retry setup failed, falling back to sync",
				zap.Int64("task_id", req.ID),
				zap.Error(e),
			)
			if err := retryCfg.Do(ctx, execFn); err != nil {
				r.logger.Debug("execution completed with retry error",
					zap.Int64("task_id", req.ID),
					zap.Error(err),
				)
			}
			onComplete(nil)
		}
	case retryCfg != nil:
		// Synchronous retry (no time wheel injected).
		if e := retryCfg.Do(ctx, execFn); e != nil {
			r.logger.Debug("execution completed with retry error",
				zap.Int64("task_id", req.ID),
				zap.Error(e),
			)
		}
		onComplete(nil)
	default:
		// No retry configured.
		if e := execFn(); e != nil {
			r.logger.Debug("execution failed",
				zap.Int64("task_id", req.ID),
				zap.Error(e),
			)
		}
		onComplete(nil)
	}
}

// finish saves the execution record and publishes the completion event.
// retryCount is the number of retries attempted (0 when the task succeeded
// on the first attempt or when no retry config was applied).
func (r *runner) finish(_ context.Context, req ExecutionRequest, result *Result, execErr error, retryCount int, start time.Time) {
	duration := time.Since(start)
	finishedAt := time.Now()

	status, errorMsg := inferStatus(result, execErr)

	// Save execution record before publishing the completion event so that
	// by the time any subscriber observes the completion, the record is
	// already durable. The event bus dispatches asynchronously, so
	// publishing first could let a consumer (or a test waiting on the
	// event) read the store before the record exists.
	record := ExecutionRecord{
		TaskID:       req.ID,
		TenantID:     req.TenantID,
		AssetID:      req.AssetID,
		ExecutorName: req.ExecutorName,
		Operation:    req.Operation,
		Status:       status,
		Duration:     duration,
		RetryCount:   retryCount,
		StartedAt:    start,
		FinishedAt:   finishedAt,
		ErrorMsg:     errorMsg,
		RunID:        req.RunID,
		TriggerType:  req.TriggerType,
	}
	if result != nil {
		record.StatusCode = result.StatusCode
		record.Output = result.Body
	}
	if saveErr := r.records.Save(record); saveErr != nil {
		r.logger.Warn("failed to save execution record",
			zap.Int64("task_id", req.ID),
			zap.Error(saveErr),
		)
	}

	// Publish completion event.
	if r.bus != nil {
		payload := event.ExecutionPayload{
			ExecutionID: strconv.FormatInt(req.ID, 10),
			TenantID:    strconv.FormatInt(req.TenantID, 10),
			AssetID:     strconv.FormatInt(req.AssetID, 10),
			Status:      string(status),
			Error:       errorMsg,
		}
		if result != nil {
			payload.StatusCode = result.StatusCode
			payload.Output = result.Body
			payload.Duration = int64(result.Duration)
		}
		if err := event.Publish(context.Background(), r.bus, event.TypeExecutionCompleted, payload, event.WithMetadata(req.Metadata)); err != nil {
			r.logger.Warn("failed to publish execution completed event",
				zap.Int64("task_id", req.ID),
				zap.Error(err),
			)
		}
	}

	r.logger.Info("task executed",
		zap.Int64("task_id", req.ID),
		zap.String("status", string(status)),
		zap.Duration("duration", duration),
	)
}

// buildRetry constructs a retry.Retry from the request metadata.
// It reads max_retries and retry_interval from req.Metadata.
// If max_retries is 0 or absent, a single-attempt (no-retry) config is returned.
func (r *runner) buildRetry(req ExecutionRequest) (*retry.Retry, error) {
	maxRetries := 0
	if req.Metadata != nil {
		if v, ok := req.Metadata["max_retries"]; ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				maxRetries = n
			}
		}
	}

	opts := []retry.Option{retry.WithMaxAttempts(maxRetries + 1)}

	if req.Metadata != nil {
		if v, ok := req.Metadata["retry_interval"]; ok {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				opts = append(opts, retry.WithBackoff(retry.NewFixedInterval(d)))
			}
		}
	}

	return retry.New(opts...)
}

// inferStatus determines the final asset status and error message from
// the execution result and error. This mirrors the logic in the scheduler
// engine's doExecute method:
//   - If an error occurred, status is Abnormal and the error message is the
//     error string.
//   - Otherwise, if a result is present, status and error message are taken
//     from the result.
//   - If neither error nor result is present, status defaults to Abnormal.
func inferStatus(result *Result, execErr error) (types.AssetStatus, string) {
	status := types.AssetStatusAbnormal
	errorMsg := ""
	if execErr != nil {
		errorMsg = execErr.Error()
	} else if result != nil {
		status = result.Status
		if result.ErrorMsg != "" {
			errorMsg = result.ErrorMsg
		}
	}
	return status, errorMsg
}
