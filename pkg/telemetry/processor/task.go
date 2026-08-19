// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package processor implements the built-in telemetry processors for the
// tickraft distribution. Each processor owns the state-machine
// logic for a single asset type: it derives the target status from an
// incoming telemetry, persists it through asset.Store, and emits status-change
// events when a timeout occurs.
package processor

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/types"
)

// Task handles heartbeat telemetry for task assets.
// A task reporting in is treated as alive (normal); an explicit abnormal
// status in the telemetry is respected as-is.
//
// It implements the telemetry.Processor interface.
type Task struct {
	store  asset.Store
	bus    event.Bus
	logger *zap.Logger
}

// Compile-time assertion that Task implements telemetry.Processor.
var _ telemetry.Processor = (*Task)(nil)

// NewTask creates a new Task processor.
//
// The store persists status transitions, and the bus publishes status-change
// events when a task times out.
func NewTask(store asset.Store, bus event.Bus, logger *zap.Logger) *Task {
	return &Task{
		store:  store,
		bus:    bus,
		logger: logger,
	}
}

// Type returns the asset type this processor handles.
func (p *Task) Type() types.AssetType {
	return types.AssetTypeTask
}

// Process handles a task heartbeat telemetry.
//
// The status is derived from the telemetry: if t.Status is set it is used
// directly, otherwise a heartbeat is interpreted as StatusNormal. The derived
// status is persisted through the store and compared against the previous
// status to determine whether a transition occurred.
func (p *Task) Process(ctx context.Context, t *telemetry.Telemetry) (*telemetry.ProcessResult, error) {
	if t == nil {
		return nil, fmt.Errorf("telemetry is nil")
	}

	currStatus := inferTaskStatus(t)
	prevStatus := p.lookupStatus(ctx, t.AssetID)

	// Note: status persistence is handled by the stateManager via the manager
	// pipeline (m.state.UpdateStatus). The Processor only computes the new
	// status and returns it; writing to the store here would cause a double
	// write because the manager also writes after Process returns.

	reason := fmt.Sprintf("task heartbeat: status=%s", currStatus)

	var alerts []telemetry.AlertContext
	if currStatus == types.AssetStatusAbnormal {
		alerts = append(alerts, telemetry.AlertContext{
			Level:   string(types.SeverityWarning),
			Title:   "Task Abnormal",
			Message: fmt.Sprintf("Task %d reported abnormal status", t.AssetID),
		})
	}

	return &telemetry.ProcessResult{
		PrevStatus: prevStatus,
		CurrStatus: currStatus,
		Reason:     reason,
		Alerts:     alerts,
	}, nil
}

// OnTimeout handles the task timeout scenario by marking the asset offline
// and publishing a status-change event.
func (p *Task) OnTimeout(ctx context.Context, assetID int64) error {
	return telemetry.MarkOffline(ctx, p.store, p.bus, p.logger, telemetry.OfflineParams{
		AssetID:   assetID,
		AssetType: types.AssetTypeTask,
		Reason:    "task timeout",
	})
}

// inferTaskStatus derives the task status from the telemetry. A heartbeat without
// an explicit status is treated as normal; an explicitly set status is
// respected.
func inferTaskStatus(t *telemetry.Telemetry) types.AssetStatus {
	switch t.Status {
	case types.AssetStatusNormal, types.AssetStatusAbnormal, types.AssetStatusOffline:
		return t.Status
	default:
		// Unknown still counts as alive: a task reporting in is up.
		return types.AssetStatusNormal
	}
}

// lookupStatus returns the currently persisted status for the given asset,
// or StatusUnknown when the asset cannot be loaded.
func (p *Task) lookupStatus(ctx context.Context, assetID int64) types.AssetStatus {
	a, err := p.store.GetByID(ctx, assetID)
	if err != nil || a == nil {
		p.logger.Debug("could not load previous task status",
			zap.Int64("asset_id", assetID),
			zap.Error(err),
		)
		return types.AssetStatusUnknown
	}
	return a.Status
}
