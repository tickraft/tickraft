// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"time"
)

// NoopStore is a no-op Store implementation. All operations succeed
// silently without persisting anything. Get returns ErrTaskNotFound, List
// returns an empty slice, and Save/Delete are no-ops. It is useful as a
// default when persistence is disabled, for testing, or as a sentinel value
// when no real store is configured.
//
// The runtime uses NoopStore as the default when no store
// is configured. callers may inject a real store via WithStore.
type NoopStore struct{}

// Compile-time assertion that NoopStore satisfies Store.
var _ Store = NoopStore{}

// Save is a no-op and always returns nil.
func (NoopStore) Save(_ context.Context, _ *Task) error { return nil }

// Get always returns nil, ErrTaskNotFound because no tasks are persisted.
func (NoopStore) Get(_ context.Context, _ int64) (*Task, error) {
	return nil, ErrTaskNotFound
}

// List always returns an empty slice and nil error.
func (NoopStore) List(_ context.Context, _ ListOptions) ([]*Task, error) {
	return nil, nil
}

// Delete is a no-op and always returns nil.
func (NoopStore) Delete(_ context.Context, _ int64) error { return nil }

// Migrate is a no-op and always returns nil.
func (NoopStore) Migrate(_ context.Context) error { return nil }

// NoopExecutionStore is a no-op ExecutionStore implementation. All operations
// succeed silently without persisting anything. Save is a no-op, List returns
// an empty slice, DeleteExecutionsOlderThan is a no-op, and Stats returns a
// zero-valued result.
type NoopExecutionStore struct{}

// Compile-time assertion that NoopExecutionStore satisfies ExecutionStore.
var _ ExecutionStore = NoopExecutionStore{}

// Save is a no-op and always returns nil.
func (NoopExecutionStore) Save(_ context.Context, _ *Execution) error { return nil }

// List always returns an empty slice and nil error.
func (NoopExecutionStore) List(_ context.Context, _ int64, _ int) ([]*Execution, error) {
	return nil, nil
}

// Query always returns an empty slice and zero total.
func (NoopExecutionStore) Query(_ context.Context, _ ExecutionQuery, _, _ int) ([]*Execution, int64, error) {
	return nil, 0, nil
}

// Get always returns nil, ErrExecutionNotFound.
func (NoopExecutionStore) Get(_ context.Context, _ int64) (*Execution, error) {
	return nil, ErrExecutionNotFound
}

// DeleteExecutionsOlderThan is a no-op and always returns nil.
func (NoopExecutionStore) DeleteExecutionsOlderThan(_ context.Context, _ time.Time) error {
	return nil
}

// Stats always returns a zero-valued ExecutionStatsResult and nil error.
func (NoopExecutionStore) Stats(_ context.Context, _, _ time.Time) (ExecutionStatsResult, error) {
	return ExecutionStatsResult{}, nil
}

// Migrate is a no-op and always returns nil.
func (NoopExecutionStore) Migrate(_ context.Context) error { return nil }
