// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

package task

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/event"
)

// newReportTestBus creates a bus and subscribes a capture for
// TypeExecutionCompleted events, mirroring the engine's slot/dependency
// subscribers. Reports are published with the sync option so the consumer
// has fully applied by the time Publish returns; the completion event (the
// consumer publishes it async) is awaited via the returned channel.
func newReportTestBus(t *testing.T) (bus event.Bus, completions <-chan event.ExecutionPayload) {
	t.Helper()
	bus = event.NewBus()
	ch := make(chan event.ExecutionPayload, 8)
	completions = ch
	if _, err := event.Subscribe(bus, event.TypeExecutionCompleted,
		func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
			ch <- ev.Payload
			return nil
		}); err != nil {
		t.Fatalf("subscribe completions: %v", err)
	}
	return bus, completions
}

// newReportTestConsumer starts a report consumer on the bus and stops it on
// cleanup.
func newReportTestConsumer(t *testing.T, bus event.Bus, dbc *gorm.DB) *ReportConsumer {
	t.Helper()
	c := NewReportConsumer(bus, dbc, zap.NewNop())
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("start report consumer: %v", err)
	}
	t.Cleanup(c.Stop)
	return c
}

// publishReport publishes a report payload synchronously.
func publishReport(t *testing.T, bus event.Bus, p event.TaskReportPayload) {
	t.Helper()
	if err := event.Publish(context.Background(), bus, event.TypeTaskStatusReported, p,
		event.WithSync()); err != nil {
		t.Fatalf("publish report: %v", err)
	}
}

// insertRunningExecution seeds a running dispatch row for taskID carrying
// runID as its dispatch credential.
func insertRunningExecution(t *testing.T, dbc *gorm.DB, taskID int64, runID string) *Execution {
	t.Helper()
	exec := &Execution{
		TaskID:      taskID,
		TenantID:    1,
		Status:      StatusRunning,
		StartedAt:   time.Now().Add(-time.Minute),
		TriggerType: string(TriggerTypeSchedule),
		RunID:       runID,
	}
	if err := dbc.Create(exec).Error; err != nil {
		t.Fatalf("seed execution: %v", err)
	}
	return exec
}

// reportRunRef is a run-handle-shaped task_ref (the crypto/rand hex the
// engine stamps on every fire), used by the dispatch-credential tests.
const reportRunRef = "5f0e9d3c1a2b4e6f8d7c9a0b1c2d3e4f"

// TestReportConsumer_RunRefBindsRow pins the primary binding path: a report
// carrying the dispatch credential (the run handle) updates exactly the row
// with that run_id, maps completed→success, and publishes the completion
// event keyed by the task id.
func TestReportConsumer_RunRefBindsRow(t *testing.T) {
	dbc := openTaskStoreDB(t)
	bus, completions := newReportTestBus(t)
	newReportTestConsumer(t, bus, dbc)

	exec := insertRunningExecution(t, dbc, 7, reportRunRef)
	started := time.Now().Add(-time.Minute).Truncate(time.Second)
	finished := time.Now().Truncate(time.Second)

	publishReport(t, bus, event.TaskReportPayload{
		Kind:       reportKindTaskStatus,
		TaskRef:    reportRunRef,
		Status:     reportCompleted,
		StartedAt:  started.UnixNano(),
		FinishedAt: finished.UnixNano(),
		Output:     "job done",
	})

	got := loadExecutionRow(t, dbc, exec.ID)
	if got.Status != StatusSuccess {
		t.Errorf("status = %q, want %q", got.Status, StatusSuccess)
	}
	if got.Output != "job done" {
		t.Errorf("output = %q, want %q", got.Output, "job done")
	}
	if got.FinishedAt == nil {
		t.Fatalf("finished_at not set")
	}

	select {
	case p := <-completions:
		if p.ExecutionID != "7" {
			t.Errorf("completion ExecutionID = %q, want 7 (task id convention)", p.ExecutionID)
		}
		if p.Status != "normal" {
			t.Errorf("completion status = %q, want normal", p.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("completion event not published")
	}
}

// TestReportConsumer_RunRefTerminalIdempotent pins idempotency: once the
// row bound by the dispatch credential is terminal, further reports
// (re-reports, sweeper races) leave it untouched.
func TestReportConsumer_RunRefTerminalIdempotent(t *testing.T) {
	dbc := openTaskStoreDB(t)
	bus, completions := newReportTestBus(t)
	newReportTestConsumer(t, bus, dbc)

	exec := insertRunningExecution(t, dbc, 7, reportRunRef)
	exec.Status = StatusTimeout
	if err := dbc.Save(exec).Error; err != nil {
		t.Fatalf("seed terminal: %v", err)
	}

	publishReport(t, bus, event.TaskReportPayload{
		Kind:    reportKindTaskStatus,
		TaskRef: reportRunRef,
		Status:  reportCompleted,
	})

	got := loadExecutionRow(t, dbc, exec.ID)
	if got.Status != StatusTimeout {
		t.Errorf("status = %q, want %q (terminal rows are immutable)", got.Status, StatusTimeout)
	}
	select {
	case <-completions:
		t.Fatalf("completion published for an ignored report")
	default:
	}
}

// TestReportConsumer_NumericRefBindsLatestRunning pins the task-number
// fallback: a decimal task_ref binds to the task's latest running row,
// skipping older terminal rows.
func TestReportConsumer_NumericRefBindsLatestRunning(t *testing.T) {
	dbc := openTaskStoreDB(t)
	bus, _ := newReportTestBus(t)
	newReportTestConsumer(t, bus, dbc)

	older := insertRunningExecution(t, dbc, 7, "run-old")
	older.Status = StatusSuccess
	if err := dbc.Save(older).Error; err != nil {
		t.Fatalf("age execution: %v", err)
	}
	latest := insertRunningExecution(t, dbc, 7, "run-latest")

	publishReport(t, bus, event.TaskReportPayload{
		Kind:    reportKindTaskStatus,
		TaskRef: "7",
		Status:  reportFailed,
		Error:   "boom",
	})

	got := loadExecutionRow(t, dbc, latest.ID)
	if got.Status != StatusFailed {
		t.Errorf("latest status = %q, want %q", got.Status, StatusFailed)
	}
	if got.Error != "boom" {
		t.Errorf("error = %q, want boom", got.Error)
	}
	olderAfter := loadExecutionRow(t, dbc, older.ID)
	if olderAfter.Status != StatusSuccess {
		t.Errorf("terminal row retargeted: status = %q", olderAfter.Status)
	}
}

// TestReportConsumer_NumericRefInsertsExternalRow pins the last-resort path:
// a decimal task_ref with no running row inserts a trigger_type=external row
// carrying the reporter's original ref in the run_id column for traceability,
// and a later report closes it through the same binding.
func TestReportConsumer_NumericRefInsertsExternalRow(t *testing.T) {
	dbc := openTaskStoreDB(t)
	bus, completions := newReportTestBus(t)
	newReportTestConsumer(t, bus, dbc)

	publishReport(t, bus, event.TaskReportPayload{
		Kind:    reportKindTaskStatus,
		TaskRef: "9",
		Status:  reportRunning,
	})

	var rows []Execution
	if err := dbc.Where("task_id = ?", 9).Find(&rows).Error; err != nil {
		t.Fatalf("load rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].TriggerType != string(TriggerTypeExternal) {
		t.Errorf("trigger_type = %q, want external", rows[0].TriggerType)
	}
	if rows[0].Status != StatusRunning {
		t.Errorf("status = %q, want running", rows[0].Status)
	}
	if rows[0].RunID != "9" {
		t.Errorf("run_id = %q, want the reporter's original ref \"9\"", rows[0].RunID)
	}

	// The external row closes through the same task-number binding on a
	// later terminal report, and the completion event fires.
	publishReport(t, bus, event.TaskReportPayload{
		Kind:    reportKindTaskStatus,
		TaskRef: "9",
		Status:  reportCompleted,
	})
	rows[0] = loadExecutionRow(t, dbc, rows[0].ID)
	if rows[0].Status != StatusSuccess {
		t.Errorf("status after completion = %q, want success", rows[0].Status)
	}
	select {
	case <-completions:
	case <-time.After(2 * time.Second):
		t.Fatalf("completion event not published for external row")
	}
}

// TestReportConsumer_UnknownRefDropped pins the credential-mismatch rule: an
// unknown non-numeric ref (a run handle the system never issued for this
// environment) is dropped without inserting any row.
func TestReportConsumer_UnknownRefDropped(t *testing.T) {
	dbc := openTaskStoreDB(t)
	bus, completions := newReportTestBus(t)
	newReportTestConsumer(t, bus, dbc)

	publishReport(t, bus, event.TaskReportPayload{
		Kind:    reportKindTaskStatus,
		TaskRef: "c0ffee00000000000000000000000000",
		Status:  reportCompleted,
	})

	var count int64
	if err := dbc.Model(&Execution{}).Count(&count).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 0 {
		t.Errorf("rows = %d, want 0 (unknown credential must not create rows)", count)
	}
	select {
	case <-completions:
		t.Fatalf("completion published for an unknown credential")
	default:
	}
}

// TestApplyTaskReport_UnknownStatus pins the vocabulary guard: statuses
// outside running/completed/failed/timeout — notably the removed task-level
// active/paused pair — never reach the rows.
func TestApplyTaskReport_UnknownStatus(t *testing.T) {
	dbc := openTaskStoreDB(t)
	exec := insertRunningExecution(t, dbc, 7, reportRunRef)

	for _, status := range []string{"active", "paused", "succeeded", ""} {
		_, err := ApplyTaskReport(context.Background(), dbc, event.TaskReportPayload{
			Kind:    reportKindTaskStatus,
			TaskRef: reportRunRef,
			Status:  status,
		})
		if !errors.Is(err, ErrReportUnknownStatus) {
			t.Errorf("ApplyTaskReport(status=%q) error = %v, want ErrReportUnknownStatus", status, err)
		}
	}

	got := loadExecutionRow(t, dbc, exec.ID)
	if got.Status != StatusRunning {
		t.Errorf("status = %q, want running (off-vocabulary reports never touch rows)", got.Status)
	}
}

// TestApplyTaskReport_GuardRejectsSweeperRace pins the guarded-update
// semantics: when the row leaves the running state between binding and the
// write, the report is rejected with ErrReportTerminal and the winner's
// outcome stays untouched. The race is simulated with a BeforeUpdate
// callback that flips the row to timeout right before the report's guarded
// UPDATE applies, so the update matches zero rows and the re-read
// adjudication runs.
func TestApplyTaskReport_GuardRejectsSweeperRace(t *testing.T) {
	dbc := openTaskStoreDB(t)
	exec := insertRunningExecution(t, dbc, 7, reportRunRef)

	var flipped atomic.Bool
	if err := dbc.Callback().Update().Before("gorm:update").
		Register("test:sweeper-race", func(tx *gorm.DB) {
			if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "sys_schedule_log" {
				return
			}
			if !flipped.CompareAndSwap(false, true) {
				return
			}
			// The sweeper wins the race: terminalize the still-running row
			// before the report's UPDATE executes. Raw SQL bypasses the
			// update callbacks, so this does not recurse.
			if err := tx.Exec("UPDATE sys_schedule_log SET status = ?, finished_at = ? WHERE status = ?",
				StatusTimeout, time.Now(), StatusRunning).Error; err != nil {
				t.Errorf("flip row: %v", err)
			}
		}); err != nil {
		t.Fatalf("register race callback: %v", err)
	}

	_, err := ApplyTaskReport(context.Background(), dbc, event.TaskReportPayload{
		Kind:    reportKindTaskStatus,
		TaskRef: reportRunRef,
		Status:  reportCompleted,
	})
	if !errors.Is(err, ErrReportTerminal) {
		t.Errorf("ApplyTaskReport error = %v, want ErrReportTerminal", err)
	}

	got := loadExecutionRow(t, dbc, exec.ID)
	if got.Status != StatusTimeout {
		t.Errorf("status = %q, want %q (the sweeper's outcome must survive)", got.Status, StatusTimeout)
	}
	if got.FinishedAt == nil {
		t.Error("finished_at not set by the sweeper flip")
	}
}

// TestApplyTaskReport_GuardRejectsCompetingTerminal pins the same guard
// against a competing terminal report instead of the sweeper: the second
// writer is rejected and the first outcome persists.
func TestApplyTaskReport_GuardRejectsCompetingTerminal(t *testing.T) {
	dbc := openTaskStoreDB(t)
	insertRunningExecution(t, dbc, 7, reportRunRef)

	first, err := ApplyTaskReport(context.Background(), dbc, event.TaskReportPayload{
		Kind:    reportKindTaskStatus,
		TaskRef: reportRunRef,
		Status:  reportFailed,
		Error:   "boom",
	})
	if err != nil || !first.Applied {
		t.Fatalf("first terminal report: applied=%v err=%v", first.Applied, err)
	}

	second, err := ApplyTaskReport(context.Background(), dbc, event.TaskReportPayload{
		Kind:    reportKindTaskStatus,
		TaskRef: reportRunRef,
		Status:  reportCompleted,
	})
	if !errors.Is(err, ErrReportTerminal) {
		t.Errorf("second terminal report error = %v, want ErrReportTerminal", err)
	}
	if second.Applied {
		t.Error("second terminal report must not be applied")
	}

	var got Execution
	if err := dbc.First(&got, "run_id = ?", reportRunRef).Error; err != nil {
		t.Fatalf("load execution: %v", err)
	}
	if got.Status != StatusFailed || got.Error != "boom" {
		t.Errorf("row = %q/%q, want failed/boom (first terminal write wins)", got.Status, got.Error)
	}
}

// loadExecutionRow fetches one execution row by id.
func loadExecutionRow(t *testing.T, dbc *gorm.DB, id int64) Execution {
	t.Helper()
	var exec Execution
	if err := dbc.First(&exec, "id = ?", id).Error; err != nil {
		t.Fatalf("load execution %d: %v", id, err)
	}
	return exec
}
