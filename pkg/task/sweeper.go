// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/types"
)

const (
	// sweepInterval is how often the sweeper scans running rows.
	sweepInterval = 30 * time.Second
	// sweepMinTimeout is the floor applied to a task's configured timeout
	// when computing the reap deadline, so a zero timeout still gets a
	// meaningful window.
	sweepMinTimeout = 30 * time.Second
	// sweepGrace is the extra allowance beyond the timeout before a
	// running row is reaped, absorbing report latency and clock skew.
	sweepGrace = 300 * time.Second
	// sweepPageSize is the page size used when scanning running rows. 100
	// matches the pagination ceiling.
	sweepPageSize = 100
)

// startSweeper launches the stale-execution sweeper goroutine. It is called
// by NewEngine when an ExecutionStore is configured; stopSweeper (via Stop)
// terminates it.
func (e *Engine) startSweeper() {
	ctx, cancel := context.WithCancel(context.Background())
	e.sweepCancel = cancel
	e.sweepDone = make(chan struct{})
	go e.sweepLoop(ctx)
}

// stopSweeper cancels the sweeper and waits for the goroutine to exit.
func (e *Engine) stopSweeper(ctx context.Context) {
	if e.sweepCancel == nil {
		return
	}
	e.sweepCancel()
	select {
	case <-e.sweepDone:
	case <-ctx.Done():
		e.logger.Warn("sweep: stop timed out")
	}
}

// sweepLoop runs the periodic reap scan. One sweep runs immediately on
// startup so running rows orphaned by a restart are collected without
// waiting a full interval.
func (e *Engine) sweepLoop(ctx context.Context) {
	defer close(e.sweepDone)
	e.sweepOnce(ctx)
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.sweepOnce(ctx)
		}
	}
}

// sweepOnce scans all running execution rows and reaps those whose report
// deadline expired. Rows are collected first and reaped only after the
// scan, so the mutations cannot skip pages mid-scan; rows that went
// terminal in between are caught by MarkTimeout's status guard.
func (e *Engine) sweepOnce(ctx context.Context) {
	now := time.Now()
	for _, exec := range e.staleExecutions(ctx, now) {
		e.reap(ctx, exec, now)
	}
}

// staleExecutions returns the running rows whose reap deadline expired.
func (e *Engine) staleExecutions(ctx context.Context, now time.Time) []*Execution {
	var stale []*Execution
	page := 1
	for {
		rows, total, err := e.execStore.Query(
			ctx, ExecutionQuery{Status: StatusRunning}, page, sweepPageSize)
		if err != nil {
			e.logger.Warn("sweep: query running executions failed", zap.Error(err))
			return nil
		}
		for _, row := range rows {
			if now.After(sweepDeadline(row, e.taskTimeout(row.TaskID))) {
				stale = append(stale, row)
			}
		}
		if len(rows) < sweepPageSize || int64(page*sweepPageSize) >= total {
			return stale
		}
		page++
	}
}

// reap transitions one stale running row to timeout and announces the
// completion so occupied concurrency slots and dependencies release.
func (e *Engine) reap(ctx context.Context, exec *Execution, now time.Time) {
	durationMs := now.Sub(sweepBase(exec)).Milliseconds()
	reaped, err := e.execStore.MarkTimeout(ctx, exec.ID, now, durationMs)
	if err != nil {
		e.logger.Warn("sweep: mark execution timeout failed",
			zap.Int64("execution_id", exec.ID),
			zap.Int64("task_id", exec.TaskID),
			zap.Error(err),
		)
		return
	}
	if !reaped {
		// A terminal report landed between the scan and the reap.
		return
	}
	e.logger.Info("sweep: reaped stale running execution",
		zap.Int64("execution_id", exec.ID),
		zap.Int64("task_id", exec.TaskID),
	)
	e.publishSwept(ctx, exec)
}

// publishSwept announces the reaped execution on the bus, mirroring the
// completion event the report consumer publishes. ExecutionID carries the
// task id — the payload convention across the task domain. The publish is
// detached from the sweep ctx so a Stop racing the announcement cannot
// lose it.
func (e *Engine) publishSwept(ctx context.Context, exec *Execution) {
	if e.bus == nil {
		return
	}
	payload := event.ExecutionPayload{
		ExecutionID: strconv.FormatInt(exec.TaskID, 10),
		TenantID:    strconv.FormatInt(exec.TenantID, 10),
		Action:      "completed",
		Status:      string(types.AssetStatusAbnormal),
	}
	if err := event.Publish(context.WithoutCancel(ctx), e.bus, event.TypeExecutionCompleted, payload); err != nil {
		e.logger.Warn("sweep: publish execution completed failed",
			zap.Int64("task_id", exec.TaskID),
			zap.Error(err),
		)
	}
}

// taskTimeout resolves the configured timeout for a task. Tasks unknown to
// this engine (deleted meanwhile, or dispatched by an external driver as in
// the x standalone bridge) yield 0; the sweep floor then applies.
func (e *Engine) taskTimeout(taskID int64) time.Duration {
	task, err := e.getTask(taskID)
	if err != nil {
		return 0
	}
	return task.Timeout()
}

// sweepBase returns the time the execution started waiting: the trigger
// time when recorded, otherwise the start time.
func sweepBase(exec *Execution) time.Time {
	if exec.TriggeredAt != nil {
		return *exec.TriggeredAt
	}
	return exec.StartedAt
}

// sweepDeadline returns the reap deadline for a running execution: base
// time plus the task timeout floored at sweepMinTimeout, plus the fixed
// grace allowance.
func sweepDeadline(exec *Execution, timeout time.Duration) time.Time {
	timeout = max(timeout, sweepMinTimeout)
	return sweepBase(exec).Add(timeout + sweepGrace)
}
