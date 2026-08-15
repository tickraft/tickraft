// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/task"
	"go.uber.org/zap"
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
func (m *mockExecutionStore) Query(_ context.Context, _ task.ExecutionQuery, _, _ int) ([]*task.Execution, int64, error) {
	return nil, 0, nil
}
func (m *mockExecutionStore) Get(_ context.Context, _ int64) (*task.Execution, error) {
	return nil, task.ErrExecutionNotFound
}
func (m *mockExecutionStore) DeleteExecutionsOlderThan(_ context.Context, before time.Time) error {
	m.deleteCalled = true
	m.deleteBefore = before
	return m.deleteErr
}
func (m *mockExecutionStore) Stats(context.Context, time.Time, time.Time) (task.ExecutionStatsResult, error) {
	return task.ExecutionStatsResult{}, nil
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

// TestRunMaintenanceSweep_DeletesOldExecutions verifies that when an execution
// store and a positive retention window are provided, the sweep invokes
// DeleteExecutionsOlderThan with a cutoff approximately retentionDays in the
// past, and still cleans expired blacklist tokens.
func TestRunMaintenanceSweep_DeletesOldExecutions(t *testing.T) {
	bl := &mockBlacklistStore{}
	exec := &mockExecutionStore{}
	retentionDays := 7

	start := time.Now()
	runMaintenanceSweep(context.Background(), newTestLogger(), bl, exec, retentionDays)

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

	runMaintenanceSweep(context.Background(), newTestLogger(), bl, nil, 7)

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

	runMaintenanceSweep(context.Background(), newTestLogger(), bl, exec, 0)

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

	runMaintenanceSweep(context.Background(), newTestLogger(), bl, exec, -1)

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

	runMaintenanceSweep(context.Background(), newTestLogger(), bl, exec, 30)

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

	runMaintenanceSweep(context.Background(), newTestLogger(), bl, exec, 30)

	if !bl.cleanCalled {
		t.Error("expected blacklist CleanExpired to be attempted")
	}
	if !exec.deleteCalled {
		t.Error("expected DeleteExecutionsOlderThan to run even when blacklist cleanup errors")
	}
}
