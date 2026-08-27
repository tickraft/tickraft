// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"errors"
	"net/http"
	"testing"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// setupEnginelessTaskService creates a TaskService with a nil engine backed by
// persistent GORM stores using an in-memory SQLite database. This mirrors the
// distributed Server role, where CRUD persists directly to the TaskStore and
// Worker nodes observe changes through their own sync path; with no engine,
// pause/resume must flip the enabled column instead of touching a wheel.
func setupEnginelessTaskService(t *testing.T) (*TaskService, Store, func()) {
	t.Helper()

	gdb, err := db.Open(ctx, db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	if err := Migrate(ctx, gdb); err != nil {
		closeUnderlyingDB(t, gdb)
		t.Fatalf("auto migrate: %v", err)
	}

	taskStore := NewStore(gdb)
	execStore := NewExecutionStore(gdb)

	svc := NewTaskService(nil, taskStore, execStore, nil, zap.NewNop())

	cleanup := func() { closeUnderlyingDB(t, gdb) }
	return svc, taskStore, cleanup
}

// TestEnginelessTaskService covers the engine-less TaskService semantics used
// by distributed Server deployments: every mutation persists through the task
// store, pause/resume flip the enabled column, and trigger degrades to an
// existence check. Assertions read rows back through the store (not the
// service) so the persisted state, not the returned copy, is what is verified.
func TestEnginelessTaskService(t *testing.T) {
	svc, taskStore, cleanup := setupEnginelessTaskService(t)
	defer cleanup()

	var taskID int64

	t.Run("Create persists row without engine", func(t *testing.T) {
		created, err := svc.CreateTask(ctx, &Task{
			Name:         "engineless-task",
			ExecutorType: "tcp",
			Schedule:     "*/30 * * * *",
			Enabled:      true,
		})
		if err != nil {
			t.Fatalf("CreateTask failed: %v", err)
		}
		taskID = created.ID
		if taskID <= 0 {
			t.Fatalf("ID = %d, want a positive assigned ID", taskID)
		}

		row, err := taskStore.Get(ctx, taskID)
		if err != nil {
			t.Fatalf("task store Get after create: %v", err)
		}
		if row.Name != "engineless-task" || !row.Enabled || row.Schedule != "*/30 * * * *" {
			t.Errorf("persisted row = %+v, want name/enabled/schedule preserved", row)
		}
		if row.TimeoutSeconds <= 0 {
			t.Errorf("TimeoutSeconds = %d, want default applied on create", row.TimeoutSeconds)
		}
	})

	t.Run("Update preserves server-managed fields", func(t *testing.T) {
		row, err := taskStore.Get(ctx, taskID)
		if err != nil {
			t.Fatalf("task store Get: %v", err)
		}
		row.TenantID = 42
		row.Priority = 7
		if err := taskStore.Save(ctx, row); err != nil {
			t.Fatalf("seed server-managed fields: %v", err)
		}
		origCreated := row.CreatedAt

		updated, err := svc.UpdateTask(ctx, taskID, &Task{
			Name:         "engineless-task-v2",
			ExecutorType: "tcp",
			Schedule:     "*/15 * * * *",
		})
		if err != nil {
			t.Fatalf("UpdateTask failed: %v", err)
		}
		if updated.Name != "engineless-task-v2" {
			t.Errorf("Name = %q, want %q", updated.Name, "engineless-task-v2")
		}

		row, err = taskStore.Get(ctx, taskID)
		if err != nil {
			t.Fatalf("task store Get after update: %v", err)
		}
		if row.TenantID != 42 || row.Priority != 7 {
			t.Errorf("server-managed fields drifted: TenantID=%d Priority=%d, want 42/7", row.TenantID, row.Priority)
		}
		if !row.CreatedAt.Equal(origCreated) {
			t.Errorf("CreatedAt changed on update: got %v, want %v", row.CreatedAt, origCreated)
		}
	})

	t.Run("Pause flips enabled off in store", func(t *testing.T) {
		if err := svc.PauseTask(ctx, taskID); err != nil {
			t.Fatalf("PauseTask failed: %v", err)
		}
		row, err := taskStore.Get(ctx, taskID)
		if err != nil {
			t.Fatalf("task store Get after pause: %v", err)
		}
		if row.Enabled {
			t.Errorf("Enabled = true after PauseTask, want false")
		}
	})

	t.Run("Resume flips enabled on in store", func(t *testing.T) {
		if err := svc.ResumeTask(ctx, taskID); err != nil {
			t.Fatalf("ResumeTask failed: %v", err)
		}
		row, err := taskStore.Get(ctx, taskID)
		if err != nil {
			t.Fatalf("task store Get after resume: %v", err)
		}
		if !row.Enabled {
			t.Errorf("Enabled = false after ResumeTask, want true")
		}
	})

	t.Run("Trigger degrades to existence check", func(t *testing.T) {
		if err := svc.TriggerTask(ctx, taskID); err != nil {
			t.Fatalf("TriggerTask on existing task: %v", err)
		}
		err := svc.TriggerTask(ctx, 99999)
		assertErrorCoder(t, err, errdefs.ErrTaskNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("Pause missing task reports not found", func(t *testing.T) {
		err := svc.PauseTask(ctx, 99999)
		assertErrorCoder(t, err, errdefs.ErrTaskNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("Delete removes row from store", func(t *testing.T) {
		if err := svc.DeleteTask(ctx, taskID); err != nil {
			t.Fatalf("DeleteTask failed: %v", err)
		}
		_, err := taskStore.Get(ctx, taskID)
		// The store-level sentinel (errdefs.ErrNotFound) is checked here
		// because this read bypasses the service's mapError translation,
		// which would otherwise map it to errdefs.ErrTaskNotFound.
		if !errors.Is(err, errdefs.ErrNotFound) {
			t.Errorf("store Get after delete: err=%v, want %v", err, errdefs.ErrNotFound)
		}
	})
}
