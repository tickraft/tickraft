// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/event"
)

// newSweepEngine returns an engine whose execStore is attached directly so
// tests can drive sweepOnce synchronously; no background sweeper runs.
func newSweepEngine(t *testing.T, bus event.Bus, dbc *gorm.DB) *Engine {
	t.Helper()
	e, err := NewEngine(WithEventBus(bus), WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	e.execStore = NewExecutionStore(dbc)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.Stop(ctx)
	})
	return e
}

// insertSweepExecution seeds a running execution row whose trigger lies age
// in the past.
func insertSweepExecution(t *testing.T, dbc *gorm.DB, taskID int64, age time.Duration) *Execution {
	t.Helper()
	triggered := time.Now().Add(-age)
	exec := &Execution{
		TaskID:      taskID,
		TenantID:    1,
		Status:      StatusRunning,
		StartedAt:   triggered,
		TriggeredAt: &triggered,
		TriggerType: string(TriggerTypeSchedule),
		RunID:       "run-sweep",
	}
	if err := dbc.Create(exec).Error; err != nil {
		t.Fatalf("seed execution: %v", err)
	}
	return exec
}

func expectNoCompletion(t *testing.T, ch <-chan event.ExecutionPayload) {
	t.Helper()
	select {
	case p := <-ch:
		t.Fatalf("unexpected completion event: %+v", p)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestSweeper_ReapsStaleRunningRow(t *testing.T) {
	dbc := openTaskStoreDB(t)
	bus, completions := newReportTestBus(t)
	e := newSweepEngine(t, bus, dbc)

	// No task registered: the floor deadline (30s timeout + 300s grace)
	// applies; a 10-minute-old trigger is far past it.
	exec := insertSweepExecution(t, dbc, 7, 10*time.Minute)

	e.sweepOnce(context.Background())

	row := loadExecutionRow(t, dbc, exec.ID)
	if row.Status != StatusTimeout {
		t.Fatalf("status = %q, want %q", row.Status, StatusTimeout)
	}
	if row.FinishedAt == nil {
		t.Fatal("finished_at not stamped")
	}
	if row.Duration <= 0 {
		t.Fatalf("duration = %d, want > 0", row.Duration)
	}
	select {
	case p := <-completions:
		if p.ExecutionID != "7" {
			t.Fatalf("event execution id = %q, want 7", p.ExecutionID)
		}
		if p.Status != "abnormal" {
			t.Fatalf("event status = %q, want abnormal", p.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("completion event not published")
	}
}

func TestSweeper_KeepsFreshRunningRow(t *testing.T) {
	dbc := openTaskStoreDB(t)
	bus, completions := newReportTestBus(t)
	e := newSweepEngine(t, bus, dbc)

	exec := insertSweepExecution(t, dbc, 7, time.Minute)

	e.sweepOnce(context.Background())

	row := loadExecutionRow(t, dbc, exec.ID)
	if row.Status != StatusRunning {
		t.Fatalf("status = %q, want running", row.Status)
	}
	if row.FinishedAt != nil {
		t.Fatalf("finished_at = %v, want nil", row.FinishedAt)
	}
	expectNoCompletion(t, completions)
}

func TestSweeper_TaskTimeoutExtendsDeadline(t *testing.T) {
	dbc := openTaskStoreDB(t)
	bus, completions := newReportTestBus(t)
	e := newSweepEngine(t, bus, dbc)

	// A 1-hour timeout pushes the deadline far beyond a 10-minute-old
	// trigger, so the row must survive the sweep.
	e.setTask(Task{ID: 7, TimeoutSeconds: 3600})
	exec := insertSweepExecution(t, dbc, 7, 10*time.Minute)

	e.sweepOnce(context.Background())

	row := loadExecutionRow(t, dbc, exec.ID)
	if row.Status != StatusRunning {
		t.Fatalf("status = %q, want running", row.Status)
	}
	expectNoCompletion(t, completions)
}

func TestSweeper_ReapSkipsTerminalRow(t *testing.T) {
	dbc := openTaskStoreDB(t)
	bus, completions := newReportTestBus(t)
	e := newSweepEngine(t, bus, dbc)

	exec := insertSweepExecution(t, dbc, 7, 10*time.Minute)
	// The stale snapshot a scan would have loaded...
	snapshot := *exec
	// ...then a report lands between the scan and the reap.
	finished := time.Now()
	if err := dbc.Model(&Execution{}).Where("id = ?", exec.ID).Updates(map[string]any{
		"status":      StatusSuccess,
		"finished_at": finished,
	}).Error; err != nil {
		t.Fatalf("flip row to success: %v", err)
	}

	e.reap(context.Background(), &snapshot, time.Now())

	row := loadExecutionRow(t, dbc, exec.ID)
	if row.Status != StatusSuccess {
		t.Fatalf("status = %q, want success (report must win the race)", row.Status)
	}
	expectNoCompletion(t, completions)
}

func TestSweeper_StartupSweepAndStop(t *testing.T) {
	dbc := openTaskStoreDB(t)
	bus, completions := newReportTestBus(t)
	insertSweepExecution(t, dbc, 7, 10*time.Minute)

	// WithExecutionStore starts the background sweeper; its immediate
	// first pass must collect the restart orphan without waiting a tick.
	e, err := NewEngine(
		WithEventBus(bus),
		WithLogger(zap.NewNop()),
		WithExecutionStore(NewExecutionStore(dbc)),
	)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	select {
	case p := <-completions:
		if p.ExecutionID != "7" {
			t.Fatalf("event execution id = %q, want 7", p.ExecutionID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("startup sweep did not reap the stale row")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
}
