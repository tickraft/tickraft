// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package scheduler

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/api/handler"
	"github.com/tickraft/tickraft/pkg/api/handler/task"
	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/scheduler"
	schedtask "github.com/tickraft/tickraft/pkg/task"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ctx is a reusable background context for service-layer tests.
var ctx = context.Background()

// closeUnderlyingDB closes the underlying *sql.DB of a gorm.DB so that test
// fixtures release their in-memory SQLite database cleanly.
func closeUnderlyingDB(t *testing.T, dbc *gorm.DB) {
	t.Helper()
	sqlDB, err := dbc.DB()
	if err != nil {
		t.Fatalf("get underlying sql.DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql.DB: %v", err)
	}
}

// assertErrorCoder verifies that err is non-nil, matches the expected sentinel
// via errors.Is, and reports the expected HTTP status and business code
// through the errdefs.ErrorCoder interface.
func assertErrorCoder(t *testing.T, err error, sentinel error, wantStatus, wantCode int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error matching %v, got nil", sentinel)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected errors.Is(err, %v) = true, got err=%v", sentinel, err)
	}
	var ec errdefs.ErrorCoder
	if !errors.As(err, &ec) {
		t.Fatalf("expected err to implement errdefs.ErrorCoder, got %T", err)
	}
	if ec.HTTPStatus() != wantStatus {
		t.Errorf("HTTPStatus = %d, want %d", ec.HTTPStatus(), wantStatus)
	}
	if ec.Code() != wantCode {
		t.Errorf("Code = %d, want %d", ec.Code(), wantCode)
	}
}

// assertServiceErrorStatus verifies that err is non-nil, implements
// errdefs.ErrorCoder, and reports the expected HTTP status and business code.
// Unlike assertErrorCoder, it does not require a sentinel error match, which
// is needed for mapError cases that create a fresh serviceError rather than
// returning a known sentinel.
func assertServiceErrorStatus(t *testing.T, err error, wantStatus, wantCode int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var ec errdefs.ErrorCoder
	if !errors.As(err, &ec) {
		t.Fatalf("expected err to implement errdefs.ErrorCoder, got %T", err)
	}
	if ec.HTTPStatus() != wantStatus {
		t.Errorf("HTTPStatus = %d, want %d", ec.HTTPStatus(), wantStatus)
	}
	if ec.Code() != wantCode {
		t.Errorf("Code = %d, want %d", ec.Code(), wantCode)
	}
}

// setupSchedulerTaskService creates a TaskService backed by a real task
// manager and persistent GORM stores using an in-memory SQLite database. It
// returns the service, the underlying manager (for direct inspection when
// needed), and a cleanup function that stops the manager and closes the
// database.
//
// The manager is created without an explicit event bus so it owns an internal
// bus that is closed automatically on Stop. SubscribeEvents is not called
// because these tests do not exercise dependency tracking; the core
// Register/Schedule/Update/Unschedule flow does not depend on it.
func setupSchedulerTaskService(t *testing.T) (*TaskService, schedtask.Manager, func()) {
	t.Helper()

	gdb, err := db.Open(ctx, db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	if err := schedtask.Migrate(ctx, gdb); err != nil {
		closeUnderlyingDB(t, gdb)
		t.Fatalf("auto migrate: %v", err)
	}

	taskStore := schedtask.NewStore(gdb)
	execStore := schedtask.NewExecutionStore(gdb)

	eng, err := schedtask.NewService(
		schedtask.WithStore(taskStore),
		schedtask.WithLogger(zap.NewNop()),
	)
	if err != nil {
		closeUnderlyingDB(t, gdb)
		t.Fatalf("create task manager: %v", err)
	}

	svc := NewTaskService(eng, taskStore, execStore, zap.NewNop())

	cleanup := func() {
		_ = eng.Stop(ctx)
		closeUnderlyingDB(t, gdb)
	}
	return svc, eng, cleanup
}

// TestSchedulerTaskService covers the full TaskService lifecycle backed by a
// real scheduler engine and persistent SQLite stores. Subtests share a single
// service instance to verify ID monotonicity and cross-operation state
// consistency, mirroring the TestMemoryTaskService structure.
func TestSchedulerTaskService(t *testing.T) {
	svc, _, cleanup := setupSchedulerTaskService(t)
	defer cleanup()

	// taskID is reused across subtests to track the primary task under test.
	var taskID int64

	t.Run("Create assigns ID and timestamps", func(t *testing.T) {
		before := time.Now()
		created, err := svc.CreateTask(ctx, &task.Task{
			Name:     "task-1",
			Executor: "http",
			Schedule: "", // event-driven: registered but never fires on timer
			Enabled:  true,
		})
		if err != nil {
			t.Fatalf("CreateTask failed: %v", err)
		}
		if created.ID <= 0 {
			t.Fatalf("ID = %d, want > 0", created.ID)
		}
		taskID = created.ID
		if created.CreatedAt.Before(before) {
			t.Errorf("CreatedAt = %v, want >= %v", created.CreatedAt, before)
		}
		if !created.UpdatedAt.Equal(created.CreatedAt) {
			t.Errorf("UpdatedAt = %v, want equal to CreatedAt %v", created.UpdatedAt, created.CreatedAt)
		}
		if created.Name != "task-1" {
			t.Errorf("Name = %q, want %q", created.Name, "task-1")
		}
	})

	t.Run("Create with cron schedule registers in engine", func(t *testing.T) {
		created, err := svc.CreateTask(ctx, &task.Task{
			Name:     "cron-task",
			Executor: "tcp",
			Schedule: "*/30 * * * *",
			Enabled:  true,
		})
		if err != nil {
			t.Fatalf("CreateTask with cron schedule failed: %v", err)
		}
		if created.ID <= taskID {
			t.Errorf("ID = %d, want > %d (monotonic)", created.ID, taskID)
		}
		// Verify the task is retrievable from the persistent store.
		got, err := svc.GetTask(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetTask failed: %v", err)
		}
		if got.Schedule != "*/30 * * * *" {
			t.Errorf("Schedule = %q, want %q", got.Schedule, "*/30 * * * *")
		}
	})

	t.Run("Create with interval schedule registers in engine", func(t *testing.T) {
		created, err := svc.CreateTask(ctx, &task.Task{
			Name:     "interval-task",
			Executor: "http",
			Schedule: "90s",
			Enabled:  true,
		})
		if err != nil {
			t.Fatalf("CreateTask with interval schedule failed: %v", err)
		}
		got, err := svc.GetTask(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetTask failed: %v", err)
		}
		if got.Schedule != "90s" {
			t.Errorf("Schedule = %q, want %q", got.Schedule, "90s")
		}
	})

	t.Run("Create nil request returns ErrInvalidRequest", func(t *testing.T) {
		_, err := svc.CreateTask(ctx, nil)
		assertErrorCoder(t, err, handler.ErrInvalidRequest, http.StatusBadRequest, errdefs.CodeBadRequest)
	})

	t.Run("Create with empty executor returns 400", func(t *testing.T) {
		_, err := svc.CreateTask(ctx, &task.Task{Name: "no-executor", Schedule: ""})
		// The service returns a descriptive "executor is required" error
		// (a fresh serviceError, not the ErrInvalidRequest sentinel), so
		// we check status/code rather than errors.Is.
		assertServiceErrorStatus(t, err, http.StatusBadRequest, errdefs.CodeBadRequest)
	})

	t.Run("Get by ID returns matching entity", func(t *testing.T) {
		got, err := svc.GetTask(ctx, taskID)
		if err != nil {
			t.Fatalf("GetTask failed: %v", err)
		}
		if got.ID != taskID {
			t.Errorf("ID = %d, want %d", got.ID, taskID)
		}
		if got.Name != "task-1" {
			t.Errorf("Name = %q, want %q", got.Name, "task-1")
		}
		if got.Executor != "http" {
			t.Errorf("Executor = %q, want %q", got.Executor, "http")
		}
	})

	t.Run("Get non-existent returns ErrTaskNotFound", func(t *testing.T) {
		_, err := svc.GetTask(ctx, 999999)
		assertErrorCoder(t, err, handler.ErrTaskNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("List with pagination", func(t *testing.T) {
		// At this point we have 3 tasks: task-1, cron-task, interval-task.
		items, total, err := svc.ListTasks(ctx, 1, 2, task.Filter{})
		if err != nil {
			t.Fatalf("ListTasks failed: %v", err)
		}
		if total < 3 {
			t.Errorf("total = %d, want >= 3", total)
		}
		if len(items) != 2 {
			t.Fatalf("len(items) = %d, want 2", len(items))
		}
		// Verify ascending ID order.
		if items[0].ID > items[1].ID {
			t.Errorf("items not in ascending ID order: [%d, %d]", items[0].ID, items[1].ID)
		}

		// Page beyond the last full page returns the trailing slice.
		items, _, err = svc.ListTasks(ctx, 100, 2, task.Filter{})
		if err != nil {
			t.Fatalf("ListTasks page 100 failed: %v", err)
		}
		if len(items) != 0 {
			t.Errorf("page 100 len = %d, want 0", len(items))
		}
	})

	t.Run("List clamps paging params", func(t *testing.T) {
		// page=0 and size=0 should not panic and should default sanely.
		items, total, err := svc.ListTasks(ctx, 0, 0, task.Filter{})
		if err != nil {
			t.Fatalf("ListTasks(0,0) failed: %v", err)
		}
		if total < 3 {
			t.Errorf("total = %d, want >= 3", total)
		}
		if len(items) > 20 {
			t.Errorf("len(items) = %d, want <= 20 (default size)", len(items))
		}
		// size>100 capped to 100.
		items, _, err = svc.ListTasks(ctx, 1, 500, task.Filter{})
		if err != nil {
			t.Fatalf("ListTasks(1,500) failed: %v", err)
		}
		if len(items) > 100 {
			t.Errorf("len(items) = %d, want <= 100 (capped)", len(items))
		}
	})

	t.Run("Update mutates fields and refreshes UpdatedAt", func(t *testing.T) {
		time.Sleep(time.Millisecond) // ensure UpdatedAt advances past CreatedAt
		updated, err := svc.UpdateTask(ctx, taskID, &task.Task{
			Name:     "task-1-updated",
			Executor: "tcp",
			Schedule: "*/10 * * * *",
			Enabled:  false,
		})
		if err != nil {
			t.Fatalf("UpdateTask failed: %v", err)
		}
		if updated.ID != taskID {
			t.Errorf("ID = %d, want %d (preserved)", updated.ID, taskID)
		}
		if updated.Name != "task-1-updated" {
			t.Errorf("Name = %q, want %q", updated.Name, "task-1-updated")
		}
		if updated.Executor != "tcp" {
			t.Errorf("Executor = %q, want %q", updated.Executor, "tcp")
		}
		if updated.Enabled != false {
			t.Errorf("Enabled = %v, want false", updated.Enabled)
		}
		// CreatedAt preserved, UpdatedAt advanced.
		got, _ := svc.GetTask(ctx, taskID)
		if !got.UpdatedAt.After(got.CreatedAt) {
			t.Errorf("UpdatedAt = %v, want after CreatedAt = %v", got.UpdatedAt, got.CreatedAt)
		}
		if got.Name != "task-1-updated" {
			t.Errorf("persisted Name = %q, want %q", got.Name, "task-1-updated")
		}
	})

	t.Run("Update non-existent returns ErrTaskNotFound", func(t *testing.T) {
		_, err := svc.UpdateTask(ctx, 999999, &task.Task{Name: "x", Executor: "http"})
		assertErrorCoder(t, err, handler.ErrTaskNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("Update nil request returns ErrInvalidRequest", func(t *testing.T) {
		_, err := svc.UpdateTask(ctx, taskID, nil)
		assertErrorCoder(t, err, handler.ErrInvalidRequest, http.StatusBadRequest, errdefs.CodeBadRequest)
	})

	t.Run("Update with empty executor returns 400", func(t *testing.T) {
		_, err := svc.UpdateTask(ctx, taskID, &task.Task{Name: "no-executor"})
		assertServiceErrorStatus(t, err, http.StatusBadRequest, errdefs.CodeBadRequest)
	})

	t.Run("TriggerTask records execution entry", func(t *testing.T) {
		if err := svc.TriggerTask(ctx, taskID); err != nil {
			t.Fatalf("TriggerTask failed: %v", err)
		}
		// Verify an execution record was persisted.
		items, total, err := svc.ListExecutions(ctx, taskID, 1, 20, task.ExecutionFilter{})
		if err != nil {
			t.Fatalf("ListExecutions failed: %v", err)
		}
		if total < 1 {
			t.Fatalf("total = %d, want >= 1 after trigger", total)
		}
		if len(items) != 1 {
			t.Fatalf("len(items) = %d, want 1", len(items))
		}
		if items[0].TaskID != taskID {
			t.Errorf("TaskID = %d, want %d", items[0].TaskID, taskID)
		}
		if items[0].Status != "running" {
			t.Errorf("Status = %q, want %q", items[0].Status, "running")
		}
		if items[0].StartedAt.IsZero() {
			t.Error("StartedAt is zero, want non-zero")
		}
	})

	t.Run("TriggerTask non-existent returns ErrTaskNotFound", func(t *testing.T) {
		err := svc.TriggerTask(ctx, 999999)
		assertErrorCoder(t, err, handler.ErrTaskNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("ListExecutions returns empty for task without triggers", func(t *testing.T) {
		// The cron-task and interval-task have never been triggered.
		items, total, err := svc.ListExecutions(ctx, taskID+1, 1, 20, task.ExecutionFilter{})
		if err != nil {
			t.Fatalf("ListExecutions failed: %v", err)
		}
		if total != 0 || len(items) != 0 {
			t.Errorf("expected empty, got total=%d len=%d", total, len(items))
		}
	})

	t.Run("GetExecution returns ErrExecutionNotFound", func(t *testing.T) {
		_, err := svc.GetExecution(ctx, 0, 999999)
		assertErrorCoder(t, err, handler.ErrExecutionNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("Delete removes entity", func(t *testing.T) {
		if err := svc.DeleteTask(ctx, taskID); err != nil {
			t.Fatalf("DeleteTask failed: %v", err)
		}
		_, err := svc.GetTask(ctx, taskID)
		assertErrorCoder(t, err, handler.ErrTaskNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("Delete non-existent is idempotent", func(t *testing.T) {
		// The scheduler engine's Unschedule is a no-op for unknown IDs, so
		// deleting a non-existent task returns nil rather than 404. This
		// differs from the memory service but is safe and idempotent.
		if err := svc.DeleteTask(ctx, 999999); err != nil {
			t.Errorf("DeleteTask(999999) = %v, want nil (idempotent)", err)
		}
	})
}

// TestSchedulerTaskService_ScheduleToMetadata verifies the schedule-to-
// metadata helper that bridges the adapter's single-string schedule with the
// engine's schedule_type/cron_expr/interval metadata keys.
func TestSchedulerTaskService_ScheduleToMetadata(t *testing.T) {
	tests := []struct {
		name         string
		schedule     string
		wantType     string
		wantCronExpr string
		wantInterval string
	}{
		{
			name:         "empty schedule maps to event type",
			schedule:     "",
			wantType:     string(schedtask.ScheduleTypeEvent),
			wantCronExpr: "",
			wantInterval: "",
		},
		{
			name:         "duration maps to interval type",
			schedule:     "30s",
			wantType:     string(schedtask.ScheduleTypeInterval),
			wantCronExpr: "",
			wantInterval: "30s",
		},
		{
			name:         "complex duration maps to interval type",
			schedule:     "1h30m",
			wantType:     string(schedtask.ScheduleTypeInterval),
			wantCronExpr: "",
			wantInterval: "1h30m",
		},
		{
			name:         "cron expression maps to cron type",
			schedule:     "*/5 * * * *",
			wantType:     string(schedtask.ScheduleTypeCron),
			wantCronExpr: "*/5 * * * *",
			wantInterval: "",
		},
		{
			name:         "cron with descriptor maps to cron type",
			schedule:     "@daily",
			wantType:     string(schedtask.ScheduleTypeCron),
			wantCronExpr: "@daily",
			wantInterval: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := make(map[string]string)
			scheduleToMetadata(m, tt.schedule)
			if got := m["schedule_type"]; got != tt.wantType {
				t.Errorf("schedule_type = %q, want %q", got, tt.wantType)
			}
			if got := m["cron_expr"]; got != tt.wantCronExpr {
				t.Errorf("cron_expr = %q, want %q", got, tt.wantCronExpr)
			}
			if got := m["interval"]; got != tt.wantInterval {
				t.Errorf("interval = %q, want %q", got, tt.wantInterval)
			}
		})
	}

	t.Run("nil metadata is a no-op", func(t *testing.T) {
		// Should not panic.
		scheduleToMetadata(nil, "*/5 * * * *")
	})
}

// TestSchedulerTaskService_IDMonotonicity verifies that IDs assigned by
// CreateTask are monotonically increasing and that the counter is correctly
// seeded from the persistent store on first use.
func TestSchedulerTaskService_IDMonotonicity(t *testing.T) {
	svc, _, cleanup := setupSchedulerTaskService(t)
	defer cleanup()

	var prevID int64
	for i := 0; i < 5; i++ {
		created, err := svc.CreateTask(ctx, &task.Task{
			Name:     "mono-task",
			Executor: "http",
			Schedule: "",
		})
		if err != nil {
			t.Fatalf("CreateTask[%d] failed: %v", i, err)
		}
		if created.ID <= prevID {
			t.Errorf("iteration %d: ID = %d, want > %d (monotonic)", i, created.ID, prevID)
		}
		prevID = created.ID
	}
}

// TestSchedulerTaskService_IDSeededFromStore verifies that the ID counter is
// seeded from the max existing ID in the store, so newly created tasks get
// IDs that do not collide with pre-existing ones after a restart.
func TestSchedulerTaskService_IDSeededFromStore(t *testing.T) {
	gdb, err := db.Open(ctx, db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { closeUnderlyingDB(t, gdb) }()

	if err := schedtask.Migrate(ctx, gdb); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	taskStore := schedtask.NewStore(gdb)
	execStore := schedtask.NewExecutionStore(gdb)

	// First engine: create a task to seed the store with ID 1.
	eng1, err := schedtask.NewService(
		schedtask.WithStore(taskStore),
		schedtask.WithLogger(zap.NewNop()),
	)
	if err != nil {
		t.Fatalf("create engine 1: %v", err)
	}
	svc1 := NewTaskService(eng1, taskStore, execStore, zap.NewNop())
	created, err := svc1.CreateTask(ctx, &task.Task{
		Name: "seed-task", Executor: "http", Schedule: "",
	})
	if err != nil {
		t.Fatalf("CreateTask on engine 1 failed: %v", err)
	}
	if created.ID != 1 {
		t.Fatalf("seed task ID = %d, want 1", created.ID)
	}
	if err := eng1.Stop(ctx); err != nil {
		t.Fatalf("stop engine 1: %v", err)
	}

	// Second engine: the ID counter should be seeded from the store so the
	// next created task gets ID 2, not ID 1 (which would collide).
	eng2, err := schedtask.NewService(
		schedtask.WithStore(taskStore),
		schedtask.WithLogger(zap.NewNop()),
	)
	if err != nil {
		t.Fatalf("create engine 2: %v", err)
	}
	defer func() { _ = eng2.Stop(ctx) }()

	// Restore persisted tasks into the new engine before creating more.
	if err := eng2.Restore(ctx); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	svc2 := NewTaskService(eng2, taskStore, execStore, zap.NewNop())
	created2, err := svc2.CreateTask(ctx, &task.Task{
		Name: "post-restart-task", Executor: "http", Schedule: "",
	})
	if err != nil {
		t.Fatalf("CreateTask on engine 2 failed: %v", err)
	}
	if created2.ID <= created.ID {
		t.Errorf("post-restart ID = %d, want > %d (seeded from store)", created2.ID, created.ID)
	}

	// The original task should still be retrievable.
	got, err := svc2.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTask(original) failed: %v", err)
	}
	if got.Name != "seed-task" {
		t.Errorf("original task Name = %q, want %q", got.Name, "seed-task")
	}
}

// TestSchedulerTaskService_NilLogger verifies that a nil logger falls back to
// a no-op logger without panicking.
func TestSchedulerTaskService_NilLogger(t *testing.T) {
	svc, eng, cleanup := setupSchedulerTaskService(t)
	defer cleanup()

	// Replace the service with one that has a nil logger.
	nilSvc := NewTaskService(eng, svc.tasks, svc.execs, nil)

	created, err := nilSvc.CreateTask(ctx, &task.Task{
		Name: "nil-logger-task", Executor: "http", Schedule: "",
	})
	if err != nil {
		t.Fatalf("CreateTask with nil logger failed: %v", err)
	}
	if created.ID <= 0 {
		t.Errorf("ID = %d, want > 0", created.ID)
	}

	// TriggerTask logs a Warn if the execution save fails; with nil logger
	// it must not panic.
	if err := nilSvc.TriggerTask(ctx, created.ID); err != nil {
		t.Fatalf("TriggerTask with nil logger failed: %v", err)
	}
}

// TestNewSchedulerTaskService_Interface verifies the concrete type satisfies
// the task.Service interface (guards against accidental signature drift).
func TestNewSchedulerTaskService_Interface(t *testing.T) {
	var _ task.Service = (*TaskService)(nil)
	var _ task.Service = NewTaskService(nil, nil, nil, nil)
}

// TestSchedulerTaskService_ExecutionToHandler verifies the conversion from
// task.Execution to htask.Execution, particularly the FinishedAt
// zero-time to nil pointer mapping.
func TestSchedulerTaskService_ExecutionToHandler(t *testing.T) {
	t.Run("zero FinishedAt maps to nil pointer", func(t *testing.T) {
		e := &schedtask.Execution{
			ID:        1,
			TaskID:    10,
			Status:    "running",
			StartedAt: time.Now(),
		}
		h := executionToHandler(e)
		if h.FinishedAt != nil {
			t.Errorf("FinishedAt = %v, want nil for zero time", *h.FinishedAt)
		}
		if h.ID != 1 || h.TaskID != 10 || h.Status != "running" {
			t.Errorf("converted = %+v, want id=1 taskID=10 status=running", h)
		}
	})

	t.Run("non-zero FinishedAt maps to pointer", func(t *testing.T) {
		fa := time.Now().Add(time.Minute)
		e := &schedtask.Execution{
			ID:         2,
			TaskID:     20,
			Status:     "success",
			StartedAt:  time.Now(),
			FinishedAt: fa,
		}
		h := executionToHandler(e)
		if h.FinishedAt == nil {
			t.Fatal("FinishedAt = nil, want non-nil")
		}
		if !h.FinishedAt.Equal(fa) {
			t.Errorf("FinishedAt = %v, want %v", *h.FinishedAt, fa)
		}
	})

	t.Run("nil input returns zero value", func(t *testing.T) {
		h := executionToHandler(nil)
		if h.ID != 0 {
			t.Errorf("ID = %d, want 0 for nil input", h.ID)
		}
	})
}

// TestSchedulerTaskService_MapError verifies error mapping from
// scheduler/db errors to handler-level service errors.
func TestSchedulerTaskService_MapError(t *testing.T) {
	t.Run("nil error returns nil", func(t *testing.T) {
		if err := mapError(nil); err != nil {
			t.Errorf("mapError(nil) = %v, want nil", err)
		}
	})

	t.Run("errdefs.ErrNotFound maps to ErrTaskNotFound", func(t *testing.T) {
		err := mapError(errdefs.ErrNotFound)
		assertErrorCoder(t, err, handler.ErrTaskNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("task.ErrTaskNotFound maps to ErrTaskNotFound", func(t *testing.T) {
		err := mapError(schedtask.ErrTaskNotFound)
		assertErrorCoder(t, err, handler.ErrTaskNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("scheduler.ErrSchedulerStopped maps to 503", func(t *testing.T) {
		// mapError creates a fresh serviceError for the 503 case
		// (it does not return a known sentinel), so we check status/code
		// directly rather than using errors.Is.
		err := mapError(scheduler.ErrSchedulerStopped)
		assertServiceErrorStatus(t, err, http.StatusServiceUnavailable, errdefs.CodeInternal)
	})

	t.Run("generic error maps to 500", func(t *testing.T) {
		err := mapError(errors.New("some internal error"))
		assertServiceErrorStatus(t, err, http.StatusInternalServerError, errdefs.CodeInternal)
	})
}

// TestSchedulerTaskService_CopyTask verifies that CopyTask clones an existing
// task into a new task with a fresh ID, preserving all configuration except
// the ID and timestamps.
func TestSchedulerTaskService_CopyTask(t *testing.T) {
	svc, _, cleanup := setupSchedulerTaskService(t)
	defer cleanup()

	// Create a source task with non-trivial configuration.
	source, err := svc.CreateTask(ctx, &task.Task{
		Name:     "source-task",
		Executor: "http",
		Schedule: "*/5 * * * *",
		Enabled:  true,
		Group:    "backup",
		Tags:     []string{"critical", "nightly"},
		Config:   map[string]any{"url": "http://example.com"},
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	t.Run("copy with custom name", func(t *testing.T) {
		copied, err := svc.CopyTask(ctx, source.ID, "cloned-task")
		if err != nil {
			t.Fatalf("CopyTask failed: %v", err)
		}
		if copied.ID == source.ID {
			t.Errorf("copied ID = %d, want different from source %d", copied.ID, source.ID)
		}
		if copied.Name != "cloned-task" {
			t.Errorf("copied Name = %q, want %q", copied.Name, "cloned-task")
		}
		if copied.Executor != source.Executor {
			t.Errorf("copied Executor = %q, want %q", copied.Executor, source.Executor)
		}
		if copied.Schedule != source.Schedule {
			t.Errorf("copied Schedule = %q, want %q", copied.Schedule, source.Schedule)
		}
		if copied.Group != source.Group {
			t.Errorf("copied Group = %q, want %q", copied.Group, source.Group)
		}
		if len(copied.Tags) != len(source.Tags) {
			t.Errorf("copied Tags len = %d, want %d", len(copied.Tags), len(source.Tags))
		}
		if copied.CreatedAt.Equal(source.CreatedAt) {
			t.Errorf("copied CreatedAt should differ from source")
		}
	})

	t.Run("copy with empty name defaults to source name (copy)", func(t *testing.T) {
		copied, err := svc.CopyTask(ctx, source.ID, "")
		if err != nil {
			t.Fatalf("CopyTask with empty name failed: %v", err)
		}
		wantName := "source-task (copy)"
		if copied.Name != wantName {
			t.Errorf("copied Name = %q, want %q", copied.Name, wantName)
		}
	})

	t.Run("copy non-existent returns ErrTaskNotFound", func(t *testing.T) {
		_, err := svc.CopyTask(ctx, 999999, "whatever")
		assertErrorCoder(t, err, handler.ErrTaskNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("source task is not modified after copy", func(t *testing.T) {
		_, _ = svc.CopyTask(ctx, source.ID, "another-clone")
		got, err := svc.GetTask(ctx, source.ID)
		if err != nil {
			t.Fatalf("GetTask(source) after copy failed: %v", err)
		}
		if got.Name != "source-task" {
			t.Errorf("source Name = %q, want %q (source should be unchanged)", got.Name, "source-task")
		}
		if got.ID != source.ID {
			t.Errorf("source ID = %d, want %d (source should be unchanged)", got.ID, source.ID)
		}
	})
}

// TestSchedulerTaskService_GetExecutionStats verifies that GetExecutionStats
// returns correct aggregated statistics. It tests both the empty case and a
// case with mixed-status execution records saved directly via the execution
// store.
func TestSchedulerTaskService_GetExecutionStats(t *testing.T) {
	svc, _, cleanup := setupSchedulerTaskService(t)
	defer cleanup()

	t.Run("empty store returns zeros", func(t *testing.T) {
		now := time.Now()
		stats, err := svc.GetExecutionStats(ctx, now.Add(-24*time.Hour), now)
		if err != nil {
			t.Fatalf("GetExecutionStats failed: %v", err)
		}
		if stats.TotalExecutions != 0 {
			t.Errorf("TotalExecutions = %d, want 0", stats.TotalExecutions)
		}
		if stats.SuccessCount != 0 {
			t.Errorf("SuccessCount = %d, want 0", stats.SuccessCount)
		}
		if stats.FailureCount != 0 {
			t.Errorf("FailureCount = %d, want 0", stats.FailureCount)
		}
		if stats.SuccessRate != 0 {
			t.Errorf("SuccessRate = %f, want 0", stats.SuccessRate)
		}
		if stats.AverageDurationMs != 0 {
			t.Errorf("AverageDurationMs = %f, want 0", stats.AverageDurationMs)
		}
	})

	t.Run("with data returns correct aggregation", func(t *testing.T) {
		// Save execution records directly via the execution store. The
		// statuses mirror the real executor output: "normal" for success,
		// "abnormal" for failure, "triggered" for intermediate.
		records := []*schedtask.Execution{
			{TaskID: 1, Status: "normal", Duration: 1000, StartedAt: time.Now()},
			{TaskID: 1, Status: "normal", Duration: 3000, StartedAt: time.Now()},
			{TaskID: 1, Status: "abnormal", Duration: 2000, StartedAt: time.Now()},
		}
		for i, e := range records {
			if err := svc.execs.Save(ctx, e); err != nil {
				t.Fatalf("Save execution %d: %v", i, err)
			}
		}

		now := time.Now()
		stats, err := svc.GetExecutionStats(ctx, now.Add(-1*time.Hour), now.Add(1*time.Hour))
		if err != nil {
			t.Fatalf("GetExecutionStats failed: %v", err)
		}
		if stats.TotalExecutions != 3 {
			t.Errorf("TotalExecutions = %d, want 3", stats.TotalExecutions)
		}
		if stats.SuccessCount != 2 {
			t.Errorf("SuccessCount = %d, want 2", stats.SuccessCount)
		}
		if stats.FailureCount != 1 {
			t.Errorf("FailureCount = %d, want 1", stats.FailureCount)
		}
		// SuccessRate = 2/3*100 = 66.666...
		if stats.SuccessRate < 66.6 || stats.SuccessRate > 66.7 {
			t.Errorf("SuccessRate = %f, want ~66.67", stats.SuccessRate)
		}
		// AvgDuration = (1000+3000+2000)/3 = 2000
		if stats.AverageDurationMs != 2000 {
			t.Errorf("AverageDurationMs = %f, want 2000", stats.AverageDurationMs)
		}
	})
}
