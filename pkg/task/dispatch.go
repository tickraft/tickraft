// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/executor"
)

// dispatchNodeHostname identifies this node in dispatch rows. Resolved once
// at startup; an unresolvable hostname leaves it empty.
var dispatchNodeHostname, _ = os.Hostname()

// DispatchStore is the task-domain adapter for the executor dispatch SPI: it
// opens the running sys_schedule_execution row when a Mode A (remote status
// reporting) task fires and closes it from the dispatch outcome. The row ID
// is the execution_id of the telemetry report contract; on a successful
// dispatch it stays running until the remote reporter (or the engine's
// sweeper) finishes it.
type DispatchStore struct {
	dbc *gorm.DB
}

// Compile-time assertion that DispatchStore satisfies the executor SPI.
var _ executor.DispatchStore = (*DispatchStore)(nil)

// NewDispatchStore creates a dispatch store backed by the given *gorm.DB.
func NewDispatchStore(dbc *gorm.DB) *DispatchStore { return &DispatchStore{dbc: dbc} }

// StartDispatch inserts the running execution row for a dispatch and returns
// its ID.
func (s *DispatchStore) StartDispatch(ctx context.Context, req executor.ExecutionRequest) (int64, error) {
	triggeredAt := req.TriggeredAt
	exec := &Execution{
		TenantID:     req.TenantID,
		TaskID:       req.ID,
		AssetID:      req.AssetID,
		ExecutorType: req.ExecutorName,
		Status:       StatusRunning,
		StartedAt:    time.Now(),
		TriggeredAt:  &triggeredAt,
		RunID:        req.RunID,
		TriggerType:  req.TriggerType,
		Node:         dispatchNodeHostname,
	}
	if err := s.dbc.WithContext(ctx).Create(exec).Error; err != nil {
		return 0, fmt.Errorf("task: start dispatch: %w", db.MapError(err))
	}
	return exec.ID, nil
}

// FinishDispatch closes a dispatch. A nil dispatchErr leaves the row running
// (awaiting the remote report) and suppresses the completion event. A
// non-nil dispatchErr writes the terminal state derived from the error
// (context.DeadlineExceeded → timeout, anything else → failed) and asks the
// caller to publish the completion event so the concurrency slot is
// released.
func (s *DispatchStore) FinishDispatch(
	ctx context.Context,
	req executor.ExecutionRequest,
	execID int64,
	dispatchErr error,
) (bool, error) {
	if dispatchErr == nil {
		return false, nil
	}
	var exec Execution
	if err := s.dbc.WithContext(ctx).First(&exec, "id = ?", execID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// The row vanished (retention cleanup, manual delete). Publish
			// so the slot is not leaked; there is nothing to update.
			return true, nil
		}
		return true, fmt.Errorf("task: finish dispatch: %w", db.MapError(err))
	}

	status := StatusFailed
	if errors.Is(dispatchErr, context.DeadlineExceeded) {
		status = StatusTimeout
	}
	now := time.Now()
	updates := map[string]any{
		"status":      status,
		"error_msg":   dispatchErr.Error(),
		"finished_at": now,
		"duration":    now.Sub(exec.StartedAt).Milliseconds(),
	}
	if err := s.dbc.WithContext(ctx).Model(&Execution{}).
		Where("id = ?", execID).
		Updates(updates).Error; err != nil {
		return true, fmt.Errorf("task: finish dispatch: %w", db.MapError(err))
	}
	return true, nil
}
