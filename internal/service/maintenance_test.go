// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/task"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/types"
)

// mockBlacklistStore records CleanExpired invocations so tests can assert that
// the blacklist cleanup still runs alongside the execution log retention
// cleanup.
type mockBlacklistStore struct {
	cleanCalled bool
	cleanErr    error
}

func (m *mockBlacklistStore) Add(context.Context, string, time.Time) error { return nil }
func (m *mockBlacklistStore) Exists(context.Context, string) (bool, error) { return false, nil }
func (m *mockBlacklistStore) CleanExpired(context.Context) error {
	m.cleanCalled = true
	return m.cleanErr
}

// mockExecutionStore records DeleteExecutionsOlderThan invocations so tests
// can assert the cutoff passed by runMaintenanceSweep.
type mockExecutionStore struct {
	deleteCalled bool
	deleteBefore time.Time
	deleteErr    error
}

func (m *mockExecutionStore) Save(context.Context, *task.Execution) error {
	return nil
}
func (m *mockExecutionStore) List(context.Context, int64, int) ([]*task.Execution, error) {
	return nil, nil
}
func (m *mockExecutionStore) Query(_ context.Context, _ task.ExecutionQuery,
	_, _ int) ([]*task.Execution, int64, error) {
	return nil, 0, nil
}
func (m *mockExecutionStore) Get(_ context.Context, _ int64) (*task.Execution, error) {
	return nil, task.ErrExecutionNotFound
}
func (m *mockExecutionStore) MarkTimeout(
	_ context.Context, _ int64, _ time.Time, _ int64) (bool, error) {
	return false, nil
}
func (m *mockExecutionStore) DeleteExecutionsOlderThan(_ context.Context, before time.Time) error {
	m.deleteCalled = true
	m.deleteBefore = before
	return m.deleteErr
}
func (m *mockExecutionStore) Stats(context.Context, time.Time, time.Time, int64) (task.ExecutionStatsResult, error) {
	return task.ExecutionStatsResult{}, nil
}
func (m *mockExecutionStore) StatsByDay(context.Context, time.Time, time.Time, int64) ([]task.DailyStat, error) {
	return nil, nil
}
func (m *mockExecutionStore) Migrate(context.Context) error {
	return nil
}

// Compile-time assertions that the mocks satisfy the interfaces used by
// runMaintenanceSweep.
var (
	_ auth.BlacklistStore = (*mockBlacklistStore)(nil)
	_ task.ExecutionStore = (*mockExecutionStore)(nil)
)

// newTestLogger returns a no-op zap logger suitable for tests that only
// assert side effects on the mocks.
func newTestLogger() *zap.Logger {
	return zap.NewNop()
}

// sweepCfg builds the maintenanceConfig for the given stores and retention
// window, the way runMaintenance assembles it in production.
func sweepCfg(bl auth.BlacklistStore, exec task.ExecutionStore, retentionDays int) maintenanceConfig {
	return maintenanceConfig{
		blacklistStore: bl,
		executionStore: exec,
		retentionDays:  retentionDays,
	}
}

// TestRunMaintenanceSweep_DeletesOldExecutions verifies that when an execution
// store and a positive retention window are provided, the sweep invokes
// DeleteExecutionsOlderThan with a cutoff approximately retentionDays in the
// past, and still cleans expired blacklist tokens.
func TestRunMaintenanceSweep_DeletesOldExecutions(t *testing.T) {
	bl := &mockBlacklistStore{}
	exec := &mockExecutionStore{}
	retentionDays := 7

	start := time.Now()
	runMaintenanceSweep(context.Background(), newTestLogger(), sweepCfg(bl, exec, retentionDays))

	if !bl.cleanCalled {
		t.Error("expected blacklist CleanExpired to be called")
	}
	if !exec.deleteCalled {
		t.Fatal("expected DeleteExecutionsOlderThan to be called")
	}

	// The cutoff should be roughly retentionDays ago. Allow a 5-second
	// tolerance for test scheduling latency.
	wantBefore := start.AddDate(0, 0, -retentionDays)
	maxDelta := 5 * time.Second
	delta := exec.deleteBefore.Sub(wantBefore)
	if delta < -maxDelta || delta > maxDelta {
		t.Errorf("DeleteExecutionsOlderThan before = %v, want ~%v (delta %v)",
			exec.deleteBefore, wantBefore, delta)
	}
}

// TestRunMaintenanceSweep_NilExecutionStore verifies that a nil execution
// store skips the retention cleanup without panicking, while blacklist
// cleanup still runs.
func TestRunMaintenanceSweep_NilExecutionStore(t *testing.T) {
	bl := &mockBlacklistStore{}

	runMaintenanceSweep(context.Background(), newTestLogger(), sweepCfg(bl, nil, 7))

	if !bl.cleanCalled {
		t.Error("expected blacklist CleanExpired to be called")
	}
}

// TestRunMaintenanceSweep_ZeroRetentionDays verifies that a non-positive
// retention window skips the retention cleanup, even when an execution store
// is present.
func TestRunMaintenanceSweep_ZeroRetentionDays(t *testing.T) {
	bl := &mockBlacklistStore{}
	exec := &mockExecutionStore{}

	runMaintenanceSweep(context.Background(), newTestLogger(), sweepCfg(bl, exec, 0))

	if !bl.cleanCalled {
		t.Error("expected blacklist CleanExpired to be called")
	}
	if exec.deleteCalled {
		t.Error("did not expect DeleteExecutionsOlderThan to be called for zero retention")
	}
}

// TestRunMaintenanceSweep_NegativeRetentionDays verifies that a negative
// retention window is treated as "disabled" and skips the retention cleanup.
func TestRunMaintenanceSweep_NegativeRetentionDays(t *testing.T) {
	bl := &mockBlacklistStore{}
	exec := &mockExecutionStore{}

	runMaintenanceSweep(context.Background(), newTestLogger(), sweepCfg(bl, exec, -1))

	if !bl.cleanCalled {
		t.Error("expected blacklist CleanExpired to be called")
	}
	if exec.deleteCalled {
		t.Error("did not expect DeleteExecutionsOlderThan to be called for negative retention")
	}
}

// TestRunMaintenanceSweep_DeleteErrorDoesNotAbort verifies that an error from
// DeleteExecutionsOlderThan does not panic and does not prevent the blacklist
// cleanup; the error is only logged. The blacklist cleanup runs first and is
// independent of the retention cleanup outcome.
func TestRunMaintenanceSweep_DeleteErrorDoesNotAbort(t *testing.T) {
	bl := &mockBlacklistStore{}
	exec := &mockExecutionStore{deleteErr: errors.New("db unavailable")}

	runMaintenanceSweep(context.Background(), newTestLogger(), sweepCfg(bl, exec, 30))

	if !bl.cleanCalled {
		t.Error("expected blacklist CleanExpired to be called even when delete errors")
	}
	if !exec.deleteCalled {
		t.Error("expected DeleteExecutionsOlderThan to be attempted")
	}
}

// TestRunMaintenanceSweep_BlacklistErrorDoesNotAbortRetention verifies that a
// blacklist cleanup error does not prevent the retention cleanup from running;
// the two cleanups are independent.
func TestRunMaintenanceSweep_BlacklistErrorDoesNotAbortRetention(t *testing.T) {
	bl := &mockBlacklistStore{cleanErr: errors.New("blacklist unavailable")}
	exec := &mockExecutionStore{}

	runMaintenanceSweep(context.Background(), newTestLogger(), sweepCfg(bl, exec, 30))

	if !bl.cleanCalled {
		t.Error("expected blacklist CleanExpired to be attempted")
	}
	if !exec.deleteCalled {
		t.Error("expected DeleteExecutionsOlderThan to run even when blacklist cleanup errors")
	}
}

// TestRunMaintenanceSweep_ProbeRetention verifies that a configured probe
// record store is swept in the same pass as the execution logs, under the
// same retention window: records older than the cutoff disappear, newer
// ones survive, and a nil store skips the sweep without panicking.
func TestRunMaintenanceSweep_ProbeRetention(t *testing.T) {
	dbc, err := db.Open(context.Background(), db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := dbc.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	// Save updates the monitor point row too, so both telemetry tables
	// must exist even though the sweep only asserts probe rows.
	if err := telemetry.Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("migrate monitor points: %v", err)
	}
	probeStore := telemetry.NewProbeRecordStore(dbc)
	if err := probeStore.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now()
	old := executor.ExecutionRecord{
		TaskID:       -(telemetry.ProbeTaskIDOffset + 1),
		Operation:    executor.OpProbe,
		ExecutorName: "icmp",
		Status:       types.AssetStatusNormal,
		StartedAt:    now.AddDate(0, 0, -40),
	}
	fresh := old
	fresh.StartedAt = now.AddDate(0, 0, -1)
	for _, rec := range []executor.ExecutionRecord{old, fresh} {
		if err := probeStore.Save(context.Background(), rec); err != nil {
			t.Fatalf("save seed record: %v", err)
		}
	}

	cfg := sweepCfg(&mockBlacklistStore{}, nil, 30)
	cfg.probeStore = probeStore
	runMaintenanceSweep(context.Background(), newTestLogger(), cfg)

	var remaining []telemetry.ProbeRecord
	if err := dbc.Find(&remaining).Error; err != nil {
		t.Fatalf("load remaining: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("remaining probe records = %d, want 1 (only the fresh row)", len(remaining))
	}
	if !remaining[0].StartedAt.Equal(fresh.StartedAt) {
		t.Errorf("surviving record started at %v, want the fresh row %v",
			remaining[0].StartedAt, fresh.StartedAt)
	}

	// A nil probe store skips the sweep without panicking.
	cfg.probeStore = nil
	runMaintenanceSweep(context.Background(), newTestLogger(), cfg)
}
