// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/types"
)

// reportKindTaskStatus is the telemetry endpoint kind for remote task
// status reports (the only task kind).
const reportKindTaskStatus = "task_status"

// Report execution status vocabulary as reported by remote reporters. It
// maps onto the storage vocabulary (completed→success).
const (
	reportCompleted = "completed"
	reportFailed    = "failed"
	reportRunning   = "running"
	reportTimeout   = "timeout"
)

// Drop reasons for a task status report. Returned by ApplyTaskReport as
// sentinel errors so callers can log the cause without string matching.
var (
	// ErrReportUnknownRef: the dispatch credential matches no execution row.
	ErrReportUnknownRef = errors.New("task: report consumer: unknown task_ref")
	// ErrReportUnknownStatus: the status is not in the report vocabulary.
	ErrReportUnknownStatus = errors.New("task: report consumer: unknown status")
	// ErrReportTerminal: the bound row is already terminal (idempotent
	// re-report or a race against the sweeper).
	ErrReportTerminal = errors.New("task: report consumer: execution already terminal")
)

// ReportResult summarizes one applied task status report.
type ReportResult struct {
	// TaskID is the task the report bound to (0 when the report dropped
	// before binding).
	TaskID int64
	// ExecID is the execution row the report touched.
	ExecID int64
	// Terminal reports whether the report closed the execution.
	Terminal bool
	// Applied reports whether a row was written (update or external
	// insert); false together with a nil error means the report was a
	// no-op drop.
	Applied bool
}

// ApplyTaskReport resolves the report's task_ref and writes the reported
// state to the execution rows, following the binding order from
// : a non-numeric ref (the dispatch credential,
// i.e. the run handle) pins the dispatch row directly; a decimal ref is a
// task number and binds the task's latest running row; when neither exists
// the report inserts a trigger_type=external row so purely externally
// driven executions still land in the history.
//
// Drops (nil error, Applied=false): an unknown dispatch credential (the
// system issued it, so a mismatch means a stale or cross-environment
// report), an off-vocabulary status, and idempotent re-reports on terminal
// rows — each signalled by its sentinel error.
//
// ApplyTaskReport is exported for the extended deployment's stream
// processor, which binds external reports with the same semantics as the
// kernel report consumer.
func ApplyTaskReport(
	ctx context.Context, dbc *gorm.DB, p event.TaskReportPayload,
) (ReportResult, error) {
	exec, taskID, err := bindTaskRef(ctx, dbc, p.TaskRef)
	if err != nil {
		return ReportResult{}, err
	}

	if exec.ID > 0 && exec.Status != StatusRunning {
		return ReportResult{TaskID: exec.TaskID, ExecID: exec.ID}, ErrReportTerminal
	}

	status, ok := reportExecStatus(p.Status)
	if !ok {
		return ReportResult{TaskID: taskID}, ErrReportUnknownStatus
	}

	terminal := status != StatusRunning
	startedAt, finishedAt := reportTimes(exec, p, terminal, time.Now())

	if exec.ID > 0 {
		updates := map[string]any{
			"status":     status,
			"output":     p.Output,
			"error_msg":  p.Error,
			"started_at": startedAt,
		}
		if terminal {
			updates["finished_at"] = finishedAt
			updates["duration"] = finishedAt.Sub(startedAt).Milliseconds()
		}
		// The status guard makes the write atomic against the sweeper and
		// competing reports: a row that already left the running state is
		// never overwritten (same pattern as MarkTimeout).
		res := dbc.WithContext(ctx).Model(&Execution{}).
			Where("id = ? AND status = ?", exec.ID, StatusRunning).
			Updates(updates)
		if res.Error != nil {
			return ReportResult{TaskID: taskID, ExecID: exec.ID},
				fmt.Errorf("task: report consumer: update execution: %w", db.MapError(res.Error))
		}
		if res.RowsAffected == 0 {
			// The row closed between binding and this write. Re-read to
			// distinguish the two benign causes: a row still running means
			// a racing same-status report already wrote identical values;
			// a terminal row means the sweeper or another report won the
			// race and already published completion.
			var cur Execution
			if err := dbc.WithContext(ctx).Model(&Execution{}).
				Where("id = ?", exec.ID).
				Select("status").
				Take(&cur).Error; err != nil {
				return ReportResult{TaskID: taskID, ExecID: exec.ID},
					fmt.Errorf("task: report consumer: reload execution: %w", db.MapError(err))
			}
			if cur.Status != StatusRunning {
				return ReportResult{TaskID: taskID, ExecID: exec.ID}, ErrReportTerminal
			}
		}
	} else {
		exec = &Execution{
			TaskID:      taskID,
			TenantID:    p.TenantID,
			Status:      status,
			Output:      p.Output,
			Error:       p.Error,
			StartedAt:   startedAt,
			TriggerType: string(TriggerTypeExternal),
			// The reporter's original ref, kept for traceability when the
			// row is external.
			RunID: p.TaskRef,
		}
		if terminal {
			fin := finishedAt
			exec.FinishedAt = &fin
			exec.Duration = finishedAt.Sub(startedAt).Milliseconds()
		}
		if err := dbc.WithContext(ctx).Create(exec).Error; err != nil {
			return ReportResult{TaskID: taskID},
				fmt.Errorf("task: report consumer: insert external execution: %w", db.MapError(err))
		}
	}

	return ReportResult{
		TaskID:   taskID,
		ExecID:   exec.ID,
		Terminal: terminal,
		Applied:  true,
	}, nil
}

// bindTaskRef resolves the report credential. A decimal ref is a task
// number: the task's latest running row is located, and a miss yields a
// zero-ID Execution so the report becomes a new external row. Any other
// ref is a dispatch credential (the run handle): the row is located by
// run_id regardless of status (terminal rows are found so re-reports stay
// idempotent), and an unknown credential is an error — the system issued
// it, so a mismatch means a stale or cross-environment report and must not
// silently create rows.
//
// The two ref kinds are distinguished purely by decimal parseability. This
// is safe only because run handles are constructed to never parse as a
// decimal task number (see newRunID); any change to either format must keep
// the two namespaces disjoint.
func bindTaskRef(ctx context.Context, dbc *gorm.DB, ref string) (*Execution, int64, error) {
	if taskID, err := strconv.ParseInt(ref, 10, 64); err == nil && taskID > 0 {
		var exec Execution
		err := dbc.WithContext(ctx).
			Where("task_id = ? AND status = ?", taskID, StatusRunning).
			Order("id DESC").
			First(&exec).Error
		switch {
		case err == nil:
			return &exec, exec.TaskID, nil
		case errors.Is(err, gorm.ErrRecordNotFound):
			return &Execution{}, taskID, nil
		default:
			return nil, 0, fmt.Errorf("task: report consumer: load latest running: %w", db.MapError(err))
		}
	}

	var exec Execution
	err := dbc.WithContext(ctx).
		Where("run_id = ?", ref).
		Order("id DESC").
		First(&exec).Error
	switch {
	case err == nil:
		return &exec, exec.TaskID, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, 0, ErrReportUnknownRef
	default:
		return nil, 0, fmt.Errorf("task: report consumer: load by task_ref: %w", db.MapError(err))
	}
}

// reportTimes resolves the row timestamps for a report. The report's
// timestamps are Unix nanoseconds; zero means absent. A report that omits
// them leaves the row's start anchor untouched (existing rows) or anchors
// at the server time (new external rows); the finish time defaults to now.
func reportTimes(
	exec *Execution, p event.TaskReportPayload, terminal bool, now time.Time,
) (startedAt, finishedAt time.Time) {
	startedAt = exec.StartedAt
	if exec.ID == 0 {
		startedAt = now
	}
	if p.StartedAt > 0 {
		startedAt = time.Unix(0, p.StartedAt)
	}
	finishedAt = now
	if terminal && p.FinishedAt > 0 {
		finishedAt = time.Unix(0, p.FinishedAt)
	}
	return startedAt, finishedAt
}

// reportExecStatus maps the report vocabulary onto the storage vocabulary.
func reportExecStatus(status string) (string, bool) {
	switch status {
	case reportRunning:
		return StatusRunning, true
	case reportCompleted:
		return StatusSuccess, true
	case reportFailed:
		return StatusFailed, true
	case reportTimeout:
		return StatusTimeout, true
	}
	return "", false
}

// ReportConsumer closes the Mode A loop: it consumes remote task status
// reports published as TypeTaskStatusReported events and binds them to the
// task's execution rows via ApplyTaskReport. A terminal report publishes
// TypeExecutionCompleted (ExecutionID carries the task id — the established
// payload convention) so the engine's existing subscribers release the
// Concurrency=1 slot and update dependency state.
//
// ReportConsumer is safe for concurrent use after Start.
type ReportConsumer struct {
	bus    event.Bus
	dbc    *gorm.DB
	logger *zap.Logger

	sub event.Subscription
}

// NewReportConsumer creates a report consumer. bus is the event bus the
// TypeTaskStatusReported events arrive on; dbc backs the execution row
// binding.
func NewReportConsumer(bus event.Bus, dbc *gorm.DB, logger *zap.Logger) *ReportConsumer {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ReportConsumer{
		bus:    bus,
		dbc:    dbc,
		logger: logger,
	}
}

// Start subscribes the consumer to task status report events.
func (c *ReportConsumer) Start(_ context.Context) error {
	if c.bus == nil {
		return fmt.Errorf("task: report consumer: nil event bus")
	}
	sub, err := event.Subscribe(c.bus, event.TypeTaskStatusReported,
		func(ctx context.Context, ev event.Event[event.TaskReportPayload]) error {
			c.handle(ctx, ev.Payload)
			return nil
		})
	if err != nil {
		return fmt.Errorf("task: report consumer: subscribe: %w", err)
	}
	c.sub = sub
	return nil
}

// Stop cancels the event subscription.
func (c *ReportConsumer) Stop() {
	if c.sub != nil {
		c.sub.Cancel()
	}
}

// handle applies one report; only the task_status kind is meaningful.
func (c *ReportConsumer) handle(ctx context.Context, p event.TaskReportPayload) {
	if p.Kind != reportKindTaskStatus {
		c.logger.Warn("task report: unknown kind, ignoring",
			zap.String("kind", p.Kind),
			zap.String("task_ref", p.TaskRef),
		)
		return
	}

	res, err := ApplyTaskReport(ctx, c.dbc, p)
	if err != nil {
		c.logger.Warn("task report: apply failed",
			zap.String("task_ref", p.TaskRef),
			zap.String("status", p.Status),
			zap.Error(err),
		)
		return
	}
	if res.Applied {
		c.logger.Info("task execution report applied",
			zap.Int64("task_id", res.TaskID),
			zap.Int64("execution_id", res.ExecID),
			zap.String("status", p.Status),
		)
	}
	if res.Terminal {
		c.publishCompleted(ctx, res.TaskID, p)
	}
}

// publishCompleted announces a terminal execution so the engine's existing
// subscribers release the concurrency slot and update dependency state.
// ExecutionID carries the task id, the payload convention across the task
// domain; Status the asset vocabulary the dependency checker understands.
// The publish is detached from the consumer ctx so a shutdown cannot lose
// the terminal announcement.
func (c *ReportConsumer) publishCompleted(ctx context.Context, taskID int64, p event.TaskReportPayload) {
	if c.bus == nil {
		return
	}
	status, ok := reportExecStatus(p.Status)
	if !ok {
		return
	}
	assetStatus := types.AssetStatusAbnormal
	if status == StatusSuccess {
		assetStatus = types.AssetStatusNormal
	}
	payload := event.ExecutionPayload{
		ExecutionID: strconv.FormatInt(taskID, 10),
		TenantID:    strconv.FormatInt(p.TenantID, 10),
		Action:      "completed",
		Status:      string(assetStatus),
		Error:       p.Error,
		Output:      p.Output,
	}
	if err := event.Publish(context.WithoutCancel(ctx), c.bus, event.TypeExecutionCompleted, payload); err != nil {
		c.logger.Warn("task report: publish execution completed failed",
			zap.Int64("task_id", taskID),
			zap.Error(err),
		)
	}
}
