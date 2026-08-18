// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/types"
)

// SubscribeEvents wires the manager to the event bus.
// It subscribes to:
//   - event.TypeAssetStatusChanged: when a asset becomes abnormal, triggers
//     event-driven tasks matching the asset.
//   - event.TypeExecutionCompleted: when a task execution finishes, updates the
//     dependency checker so dependent tasks can proceed.
func (m *Service) SubscribeEvents(_ context.Context) {
	if m.bus == nil {
		return
	}

	if _, err := event.Subscribe(m.bus, event.TypeAssetStatusChanged,
		func(_ context.Context, ev event.Event[event.StatusChangePayload]) error {
			m.handleStatusChange(ev.Payload)
			return nil
		}); err != nil {
		m.logger.Error("failed to subscribe to status change events",
			zap.Error(err),
		)
	}

	if _, err := event.Subscribe(m.bus, event.TypeExecutionCompleted,
		func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
			payload := ev.Payload
			taskID, _ := strconv.ParseInt(payload.ExecutionID, 10, 64)
			m.deps.UpdateStatus(taskID, types.AssetStatus(payload.Status))

			m.releaseRunning(taskID)

			m.logger.Debug("task completed, updated dependency status",
				zap.Int64("task_id", taskID),
				zap.String("status", payload.Status),
			)
			return nil
		}); err != nil {
		m.logger.Error("failed to subscribe to execution completed events",
			zap.Error(err),
		)
	}
}

// handleStatusChange triggers event-driven tasks matching the asset when
// it becomes abnormal.
func (m *Service) handleStatusChange(payload event.StatusChangePayload) {
	if types.AssetStatus(payload.CurrStatus) != types.AssetStatusAbnormal {
		return
	}

	assetID, _ := strconv.ParseInt(payload.AssetID, 10, 64)
	m.logger.Info("received status change event, checking event-driven tasks",
		zap.Int64("asset_id", assetID),
		zap.String("curr_status", payload.CurrStatus),
	)

	m.mu.RLock()
	eventTaskIDs := make([]int64, 0, len(m.eventDrivenTasks))
	for taskID := range m.eventDrivenTasks {
		eventTaskIDs = append(eventTaskIDs, taskID)
	}
	m.mu.RUnlock()

	for _, taskID := range eventTaskIDs {
		task, err := m.getTask(taskID)
		if err != nil {
			continue
		}
		if task.AssetID != 0 && task.AssetID != assetID {
			continue
		}
		if !m.shardManager.Owns(task.ID) {
			continue
		}
		m.logger.Info("triggering event-driven task",
			zap.Int64("task_id", task.ID),
			zap.Int64("asset_id", assetID),
		)
		m.trigger(task)
	}
}

// trigger publishes an ExecutionTriggered event for the given task.
//
// trigger marks the task as running before publishing so that the
// Concurrency == 1 check in onFire can suppress overlapping fires. If the
// publish fails (or the bus is nil), the running marker is released so the
// next fire is not permanently blocked; the ExecutionCompleted subscriber is
// the normal release path for successful publishes.
func (m *Service) trigger(task Task) {
	m.runningMu.Lock()
	m.running[task.ID] = struct{}{}
	m.runningMu.Unlock()

	runID := newRunID()
	if m.bus == nil {
		// No bus: nothing to do, release the running marker so the next
		// fire is not blocked.
		m.releaseRunning(task.ID)
		return
	}
	payload := event.ExecutionPayload{
		ExecutionID:  strconv.FormatInt(task.ID, 10),
		TenantID:     strconv.FormatInt(task.TenantID, 10),
		AssetID:      strconv.FormatInt(task.AssetID, 10),
		ExecutorType: task.ExecutorName,
		Config:       task.Config,
		Action:       task.Operation.String(),
		Timeout:      int64(task.Timeout),
		RunID:        runID,
		TriggerType:  string(TriggerTypeSchedule),
	}
	var pubOpts []event.PublishOption
	if task.Metadata != nil {
		pubOpts = append(pubOpts, event.WithMetadata(task.Metadata))
	}
	if err := event.Publish(context.Background(), m.bus, event.TypeExecutionTriggered, payload,
		pubOpts...); err != nil {
		m.logger.Warn("failed to publish execution triggered event",
			zap.Int64("task_id", task.ID),
			zap.Error(err),
		)
		// Publish failed: release the running marker so the next fire can
		// proceed. Without this, a Concurrency == 1 task would be
		// permanently blocked since no ExecutionCompleted event will arrive.
		m.releaseRunning(task.ID)
	}
}

// newRunID generates a unique 32-char hex identifier for a task run.
func newRunID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("run-%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
