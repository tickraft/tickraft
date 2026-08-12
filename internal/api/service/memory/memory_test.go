// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package memory

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/api/handler"
	"github.com/tickraft/tickraft/pkg/api/handler/alert"
	"github.com/tickraft/tickraft/pkg/api/handler/system"
	"github.com/tickraft/tickraft/pkg/api/handler/task"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// ctx is a reusable background context for service-layer tests.
var ctx = context.Background()

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

// --- TaskService tests ---

// TestMemoryTaskService covers the full TaskService lifecycle in memory.
func TestMemoryTaskService(t *testing.T) {
	svc := NewTaskService()

	t.Run("Create assigns ID and timestamps", func(t *testing.T) {
		before := time.Now()
		created, err := svc.CreateTask(ctx, &task.Task{
			Name: "task-1", Executor: "http", Schedule: "*/1 * * * *", Enabled: true,
		})
		if err != nil {
			t.Fatalf("CreateTask failed: %v", err)
		}
		if created.ID != 1 {
			t.Errorf("ID = %d, want 1", created.ID)
		}
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

	t.Run("Get by ID returns matching entity", func(t *testing.T) {
		got, err := svc.GetTask(ctx, 1)
		if err != nil {
			t.Fatalf("GetTask failed: %v", err)
		}
		if got.ID != 1 || got.Name != "task-1" {
			t.Errorf("got = %+v, want id=1 name=task-1", got)
		}
	})

	t.Run("Get non-existent returns ErrTaskNotFound", func(t *testing.T) {
		_, err := svc.GetTask(ctx, 9999)
		assertErrorCoder(t, err, handler.ErrTaskNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("List with pagination", func(t *testing.T) {
		// Seed 5 more tasks (id 2..6).
		for i := 0; i < 5; i++ {
			if _, err := svc.CreateTask(ctx, &task.Task{Name: "seed"}); err != nil {
				t.Fatalf("CreateTask seed failed: %v", err)
			}
		}
		// total should be 6 now.
		items, total, err := svc.ListTasks(ctx, 1, 2, task.Filter{})
		if err != nil {
			t.Fatalf("ListTasks failed: %v", err)
		}
		if total != 6 {
			t.Errorf("total = %d, want 6", total)
		}
		if len(items) != 2 {
			t.Errorf("len(items) = %d, want 2", len(items))
		}
		if items[0].ID != 1 || items[1].ID != 2 {
			t.Errorf("items ids = [%d,%d], want [1,2]", items[0].ID, items[1].ID)
		}
		// Page beyond the last full page returns the trailing slice.
		items, _, err = svc.ListTasks(ctx, 3, 2, task.Filter{})
		if err != nil {
			t.Fatalf("ListTasks page 3 failed: %v", err)
		}
		if len(items) != 2 {
			t.Errorf("page 3 len = %d, want 2", len(items))
		}
		// Page past the end returns empty.
		items, _, err = svc.ListTasks(ctx, 10, 2, task.Filter{})
		if err != nil {
			t.Fatalf("ListTasks page 10 failed: %v", err)
		}
		if len(items) != 0 {
			t.Errorf("page 10 len = %d, want 0", len(items))
		}
	})

	t.Run("List clamps paging params", func(t *testing.T) {
		// page=0 and size=0 should not panic and should default sanely.
		items, total, err := svc.ListTasks(ctx, 0, 0, task.Filter{})
		if err != nil {
			t.Fatalf("ListTasks(0,0) failed: %v", err)
		}
		if total != 6 {
			t.Errorf("total = %d, want 6", total)
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
		updated, err := svc.UpdateTask(ctx, 1, &task.Task{
			Name: "task-1-updated", Executor: "tcp", Schedule: "*/2 * * * *", Enabled: false,
		})
		if err != nil {
			t.Fatalf("UpdateTask failed: %v", err)
		}
		if updated.ID != 1 {
			t.Errorf("ID = %d, want 1 (preserved)", updated.ID)
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
		got, _ := svc.GetTask(ctx, 1)
		if !got.UpdatedAt.After(got.CreatedAt) {
			t.Errorf("UpdatedAt = %v, want after CreatedAt = %v", got.UpdatedAt, got.CreatedAt)
		}
	})

	t.Run("Update non-existent returns ErrTaskNotFound", func(t *testing.T) {
		_, err := svc.UpdateTask(ctx, 9999, &task.Task{Name: "x"})
		assertErrorCoder(t, err, handler.ErrTaskNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("Update nil request returns ErrInvalidRequest", func(t *testing.T) {
		_, err := svc.UpdateTask(ctx, 1, nil)
		assertErrorCoder(t, err, handler.ErrInvalidRequest, http.StatusBadRequest, errdefs.CodeBadRequest)
	})

	t.Run("Delete removes entity", func(t *testing.T) {
		if err := svc.DeleteTask(ctx, 1); err != nil {
			t.Fatalf("DeleteTask failed: %v", err)
		}
		_, err := svc.GetTask(ctx, 1)
		assertErrorCoder(t, err, handler.ErrTaskNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("Delete non-existent returns ErrTaskNotFound", func(t *testing.T) {
		err := svc.DeleteTask(ctx, 9999)
		assertErrorCoder(t, err, handler.ErrTaskNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("List after delete reflects decreased total", func(t *testing.T) {
		_, total, err := svc.ListTasks(ctx, 1, 100, task.Filter{})
		if err != nil {
			t.Fatalf("ListTasks failed: %v", err)
		}
		if total != 5 {
			t.Errorf("total = %d, want 5 (after deleting 1 of 6)", total)
		}
	})

	t.Run("TriggerTask on existing returns nil", func(t *testing.T) {
		if err := svc.TriggerTask(ctx, 2); err != nil {
			t.Errorf("TriggerTask(2) = %v, want nil", err)
		}
	})

	t.Run("TriggerTask on non-existent returns ErrTaskNotFound", func(t *testing.T) {
		err := svc.TriggerTask(ctx, 9999)
		assertErrorCoder(t, err, handler.ErrTaskNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("ListExecutions returns empty with zero total", func(t *testing.T) {
		items, total, err := svc.ListExecutions(ctx, 2, 1, 20)
		if err != nil {
			t.Fatalf("ListExecutions failed: %v", err)
		}
		if total != 0 {
			t.Errorf("total = %d, want 0", total)
		}
		if len(items) != 0 {
			t.Errorf("len(items) = %d, want 0", len(items))
		}
	})

	t.Run("GetExecution returns ErrExecutionNotFound", func(t *testing.T) {
		_, err := svc.GetExecution(ctx, 1)
		assertErrorCoder(t, err, handler.ErrExecutionNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("Create nil request returns ErrInvalidRequest", func(t *testing.T) {
		_, err := svc.CreateTask(ctx, nil)
		assertErrorCoder(t, err, handler.ErrInvalidRequest, http.StatusBadRequest, errdefs.CodeBadRequest)
	})
}

// --- AlertService tests ---

// TestMemoryAlertService covers both alert rules and alert records lifecycles.
func TestMemoryAlertService(t *testing.T) {
	svc := NewAlertService()

	t.Run("Rules: Create assigns ID and timestamps", func(t *testing.T) {
		before := time.Now()
		created, err := svc.CreateRule(ctx, &alert.Rule{
			Name: "rule-1", Scene: "metric", Expression: `alert.metrics["cpu"] > 90`,
			Priority: 10, Enabled: true,
		})
		if err != nil {
			t.Fatalf("CreateRule failed: %v", err)
		}
		if created.ID != 1 {
			t.Errorf("ID = %d, want 1", created.ID)
		}
		if created.CreatedAt.Before(before) {
			t.Errorf("CreatedAt = %v, want >= %v", created.CreatedAt, before)
		}
		if !created.UpdatedAt.Equal(created.CreatedAt) {
			t.Errorf("UpdatedAt = %v, want equal to CreatedAt %v", created.UpdatedAt, created.CreatedAt)
		}
	})

	t.Run("Rules: Get by ID returns matching entity", func(t *testing.T) {
		got, err := svc.GetRule(ctx, 1)
		if err != nil {
			t.Fatalf("GetRule failed: %v", err)
		}
		if got.ID != 1 || got.Name != "rule-1" || got.Scene != "metric" {
			t.Errorf("got = %+v, want id=1 name=rule-1 scene=metric", got)
		}
	})

	t.Run("Rules: Get non-existent returns ErrRuleNotFound", func(t *testing.T) {
		_, err := svc.GetRule(ctx, 9999)
		assertErrorCoder(t, err, handler.ErrRuleNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("Rules: List with pagination", func(t *testing.T) {
		for i := 0; i < 4; i++ {
			if _, err := svc.CreateRule(ctx, &alert.Rule{Name: "seed"}); err != nil {
				t.Fatalf("CreateRule seed failed: %v", err)
			}
		}
		items, total, err := svc.ListRules(ctx, 1, 2)
		if err != nil {
			t.Fatalf("ListRules failed: %v", err)
		}
		if total != 5 {
			t.Errorf("total = %d, want 5", total)
		}
		if len(items) != 2 {
			t.Errorf("len(items) = %d, want 2", len(items))
		}
		if items[0].ID != 1 || items[1].ID != 2 {
			t.Errorf("items ids = [%d,%d], want [1,2]", items[0].ID, items[1].ID)
		}
	})

	t.Run("Rules: Update mutates fields and refreshes UpdatedAt", func(t *testing.T) {
		time.Sleep(time.Millisecond)
		updated, err := svc.UpdateRule(ctx, 1, &alert.Rule{
			Name: "rule-1-updated", Scene: "metric", Expression: `alert.metrics["mem"] < 10`,
			Priority: 5, Enabled: false,
		})
		if err != nil {
			t.Fatalf("UpdateRule failed: %v", err)
		}
		if updated.ID != 1 {
			t.Errorf("ID = %d, want 1 (preserved)", updated.ID)
		}
		if updated.Name != "rule-1-updated" {
			t.Errorf("Name = %q, want %q", updated.Name, "rule-1-updated")
		}
		if updated.Expression != `alert.metrics["mem"] < 10` {
			t.Errorf("Expression = %q, want %q", updated.Expression, `alert.metrics["mem"] < 10`)
		}
		got, _ := svc.GetRule(ctx, 1)
		if !got.UpdatedAt.After(got.CreatedAt) {
			t.Errorf("UpdatedAt = %v, want after CreatedAt = %v", got.UpdatedAt, got.CreatedAt)
		}
	})

	t.Run("Rules: Update non-existent returns ErrRuleNotFound", func(t *testing.T) {
		_, err := svc.UpdateRule(ctx, 9999, &alert.Rule{Name: "x"})
		assertErrorCoder(t, err, handler.ErrRuleNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("Rules: Delete removes entity", func(t *testing.T) {
		if err := svc.DeleteRule(ctx, 1); err != nil {
			t.Fatalf("DeleteRule failed: %v", err)
		}
		_, err := svc.GetRule(ctx, 1)
		assertErrorCoder(t, err, handler.ErrRuleNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("Rules: Delete non-existent returns ErrRuleNotFound", func(t *testing.T) {
		err := svc.DeleteRule(ctx, 9999)
		assertErrorCoder(t, err, handler.ErrRuleNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("Rules: List after delete reflects decreased total", func(t *testing.T) {
		_, total, err := svc.ListRules(ctx, 1, 100)
		if err != nil {
			t.Fatalf("ListRules failed: %v", err)
		}
		if total != 4 {
			t.Errorf("total = %d, want 4 (after deleting 1 of 5)", total)
		}
	})

	t.Run("Records: empty list and not found", func(t *testing.T) {
		items, total, err := svc.ListRecords(ctx, 1, 20)
		if err != nil {
			t.Fatalf("ListRecords failed: %v", err)
		}
		if total != 0 || len(items) != 0 {
			t.Errorf("expected empty records, got total=%d len=%d", total, len(items))
		}
		_, err = svc.GetRecord(ctx, 1)
		assertErrorCoder(t, err, handler.ErrRecordNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("Create nil rule returns ErrInvalidRequest", func(t *testing.T) {
		_, err := svc.CreateRule(ctx, nil)
		assertErrorCoder(t, err, handler.ErrInvalidRequest, http.StatusBadRequest, errdefs.CodeBadRequest)
	})
}

// --- SystemService tests ---

// TestMemorySystemService covers config defaults, persistence, and info.
func TestMemorySystemService(t *testing.T) {
	svc := NewSystemService()

	t.Run("GetConfig returns default values", func(t *testing.T) {
		cfg, err := svc.GetConfig(ctx)
		if err != nil {
			t.Fatalf("GetConfig failed: %v", err)
		}
		if cfg.LogLevel != "info" {
			t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
		}
		if cfg.DefaultLang != "zh-Hans" {
			t.Errorf("DefaultLang = %q, want %q", cfg.DefaultLang, "zh-Hans")
		}
		if cfg.RetentionDays != 30 {
			t.Errorf("RetentionDays = %d, want 30", cfg.RetentionDays)
		}
	})

	t.Run("UpdateConfig persists and returns updated config", func(t *testing.T) {
		updated, err := svc.UpdateConfig(ctx, &system.Config{
			LogLevel: "debug", DefaultLang: "en-US", RetentionDays: 7,
		})
		if err != nil {
			t.Fatalf("UpdateConfig failed: %v", err)
		}
		if updated.LogLevel != "debug" || updated.DefaultLang != "en-US" || updated.RetentionDays != 7 {
			t.Errorf("updated = %+v, want debug/en-US/7", updated)
		}
		// Verify persistence via a fresh GetConfig.
		got, _ := svc.GetConfig(ctx)
		if got.LogLevel != "debug" || got.RetentionDays != 7 {
			t.Errorf("persisted = %+v, want debug/7", got)
		}
	})

	t.Run("UpdateConfig nil returns ErrInvalidRequest", func(t *testing.T) {
		_, err := svc.UpdateConfig(ctx, nil)
		assertErrorCoder(t, err, handler.ErrInvalidRequest, http.StatusBadRequest, errdefs.CodeBadRequest)
	})

	t.Run("GetInfo returns dev build", func(t *testing.T) {
		info, err := svc.GetInfo(ctx)
		if err != nil {
			t.Fatalf("GetInfo failed: %v", err)
		}
		if info.Version != "dev" {
			t.Errorf("Version = %q, want %q", info.Version, "dev")
		}
		if info.BuildTags != "" {
		t.Errorf("BuildTags = %q, want %q", info.BuildTags, "")
	}
		if info.Uptime == "" {
			t.Error("Uptime = empty, want non-empty duration string")
		}
		if info.StartTime.IsZero() {
			t.Error("StartTime = zero, want non-zero")
		}
	})
}

// --- Constructor type checks ---

// TestMemoryServiceConstructors verifies each concrete implementation
// satisfies its service interface (guards against accidental signature drift).
// The constructors return the interface directly, so the compile-time
// assertions below are the meaningful drift guard.
func TestMemoryServiceConstructors(t *testing.T) {
	var _ task.Service = (*memoryTaskService)(nil)
	var _ alert.Service = (*memoryAlertService)(nil)
	var _ system.Service = (*memorySystemService)(nil)
}

// --- CopyTask / GetExecutionStats / GetGlobalStats tests ---

// TestMemoryTaskService_CopyTask verifies the in-memory CopyTask
// implementation: a new ID is assigned, the name defaults when empty, and the
// source is not modified.
func TestMemoryTaskService_CopyTask(t *testing.T) {
	svc := NewTaskService()

	source, err := svc.CreateTask(ctx, &task.Task{
		Name:     "original",
		Executor: "http",
		Schedule: "*/1 * * * *",
		Enabled:  true,
		Group:    "g1",
		Tags:     []string{"t1"},
		Config:   map[string]any{"key": "val"},
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	t.Run("copy with custom name", func(t *testing.T) {
		copied, err := svc.CopyTask(ctx, source.ID, "cloned")
		if err != nil {
			t.Fatalf("CopyTask failed: %v", err)
		}
		if copied.ID == source.ID {
			t.Errorf("copied ID = %d, want != source %d", copied.ID, source.ID)
		}
		if copied.Name != "cloned" {
			t.Errorf("copied Name = %q, want %q", copied.Name, "cloned")
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
	})

	t.Run("copy with empty name defaults to source name (copy)", func(t *testing.T) {
		copied, err := svc.CopyTask(ctx, source.ID, "")
		if err != nil {
			t.Fatalf("CopyTask with empty name failed: %v", err)
		}
		want := "original (copy)"
		if copied.Name != want {
			t.Errorf("copied Name = %q, want %q", copied.Name, want)
		}
	})

	t.Run("copy non-existent returns ErrTaskNotFound", func(t *testing.T) {
		_, err := svc.CopyTask(ctx, 9999, "whatever")
		assertErrorCoder(t, err, handler.ErrTaskNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("source task unchanged after copy", func(t *testing.T) {
		_, _ = svc.CopyTask(ctx, source.ID, "another")
		got, err := svc.GetTask(ctx, source.ID)
		if err != nil {
			t.Fatalf("GetTask failed: %v", err)
		}
		if got.Name != "original" {
			t.Errorf("source Name = %q, want %q", got.Name, "original")
		}
	})
}

// TestMemoryTaskService_GetExecutionStats verifies that the in-memory
// GetExecutionStats returns a zero-valued result since the memory service
// does not track executions.
func TestMemoryTaskService_GetExecutionStats(t *testing.T) {
	svc := NewTaskService()
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
}

// TestMemorySystemService_GetGlobalStats verifies that the in-memory
// GetGlobalStats returns a zero-valued result since the memory service does
// not have access to task, asset, or execution stores.
func TestMemorySystemService_GetGlobalStats(t *testing.T) {
	svc := NewSystemService()
	stats, err := svc.GetGlobalStats(ctx)
	if err != nil {
		t.Fatalf("GetGlobalStats failed: %v", err)
	}
	if stats == nil {
		t.Fatal("GetGlobalStats returned nil, want non-nil")
	}
	if stats.TotalTasks != 0 {
		t.Errorf("TotalTasks = %d, want 0", stats.TotalTasks)
	}
	if stats.TotalDevices != 0 {
		t.Errorf("TotalDevices = %d, want 0", stats.TotalDevices)
	}
	if stats.TodayExecutions != 0 {
		t.Errorf("TodayExecutions = %d, want 0", stats.TodayExecutions)
	}
	if stats.TodaySuccessRate != 0 {
		t.Errorf("TodaySuccessRate = %f, want 0", stats.TodaySuccessRate)
	}
}
