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

	"github.com/bytedance/sonic"
	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/types"
)

// SubscribeEvents wires the manager to the event bus.
// It subscribes to:
//   - event.TypeAssetStatusChanged: when a asset becomes abnormal, triggers
//     event-driven tasks matching the asset.
//   - event.TypeExecutionCompleted: when a task execution finishes, updates the
//     dependency checker so dependent tasks can proceed.
func (e *Engine) SubscribeEvents(_ context.Context) {
	if e.bus == nil {
		return
	}

	if _, err := event.Subscribe(e.bus, event.TypeAssetStatusChanged,
		func(_ context.Context, ev event.Event[event.StatusChangePayload]) error {
			e.handleStatusChange(ev.Payload)
			return nil
		}); err != nil {
		e.logger.Error("failed to subscribe to status change events",
			zap.Error(err),
		)
	}

	if _, err := event.Subscribe(e.bus, event.TypeExecutionCompleted,
		func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
			payload := ev.Payload
			taskID, _ := strconv.ParseInt(payload.ExecutionID, 10, 64)
			e.deps.UpdateStatus(taskID, types.AssetStatus(payload.Status))

			e.releaseRunning(taskID)

			e.logger.Debug("task completed, updated dependency status",
				zap.Int64("task_id", taskID),
				zap.String("status", payload.Status),
			)
			return nil
		}); err != nil {
		e.logger.Error("failed to subscribe to execution completed events",
			zap.Error(err),
		)
	}
}

// handleStatusChange triggers event-driven tasks matching the asset when
// it becomes abnormal.
func (e *Engine) handleStatusChange(payload event.StatusChangePayload) {
	if types.AssetStatus(payload.CurrStatus) != types.AssetStatusAbnormal {
		return
	}

	assetID, _ := strconv.ParseInt(payload.AssetID, 10, 64)
	e.logger.Info("received status change event, checking event-driven tasks",
		zap.Int64("asset_id", assetID),
		zap.String("curr_status", payload.CurrStatus),
	)

	e.mu.RLock()
	eventTaskIDs := make([]int64, 0, len(e.eventDrivenTasks))
	for taskID := range e.eventDrivenTasks {
		eventTaskIDs = append(eventTaskIDs, taskID)
	}
	e.mu.RUnlock()

	for _, taskID := range eventTaskIDs {
		task, err := e.getTask(taskID)
		if err != nil {
			continue
		}
		if !task.Enabled {
			continue
		}
		if task.AssetID != 0 && task.AssetID != assetID {
			continue
		}
		if !e.shardManager.Owns(task.ID) {
			continue
		}
		// Mirror onFire's Concurrency == 1 gate: claim the running slot
		// before triggering so a burst of status-change events cannot
		// stack overlapping runs of a no-concurrency task. trigger marks
		// the task running but does not check-and-set, so the atomic
		// claim must happen here.
		if task.Concurrency == 1 && !e.tryClaimRunning(task.ID) {
			e.logger.Warn("previous execution still running, skipping event-driven task",
				zap.Int64("task_id", task.ID),
				zap.String("skip_reason", ErrTaskRunning.Error()),
			)
			continue
		}
		e.logger.Info("triggering event-driven task",
			zap.Int64("task_id", task.ID),
			zap.Int64("asset_id", assetID),
		)
		// No upstream ctx exists on the bus-subscriber path; Background is
		// the honest base here.
		e.trigger(context.Background(), task, TriggerTypeEvent)
	}
}

// trigger publishes an ExecutionTriggered event for the given task.
// triggerType records how this run was initiated (schedule, manual or
// event) and rides the payload into the persisted execution record.
//
// trigger marks the task as running before publishing so that the
// Concurrency == 1 check in onFire can suppress overlapping fires. If the
// publish fails (or the bus is nil), the running marker is released so the
// next fire is not permanently blocked; the ExecutionCompleted subscriber is
// the normal release path for successful publishes.
func (e *Engine) trigger(ctx context.Context, task Task, triggerType TriggerType) {
	e.runningMu.Lock()
	e.running[task.ID] = struct{}{}
	e.runningMu.Unlock()

	runID := newRunID()
	if e.bus == nil {
		// No bus: nothing to do, release the running marker so the next
		// fire is not blocked.
		e.releaseRunning(task.ID)
		return
	}
	// Task-domain executions run as execute; the prober overrides the
	// operation when registering its tasks.
	operation := task.Operation
	if operation != executor.OpProbe && operation != executor.OpExecute {
		operation = executor.OpExecute
	}
	payload := event.ExecutionPayload{
		ExecutionID:          strconv.FormatInt(task.ID, 10),
		TenantID:             strconv.FormatInt(task.TenantID, 10),
		AssetID:              strconv.FormatInt(task.AssetID, 10),
		ExecutorType:         task.ExecutorType,
		Operation:            operation.String(),
		Action:               "triggered",
		TimeoutSeconds:       task.TimeoutSeconds,
		MaxRetries:           task.MaxRetries,
		RetryIntervalSeconds: task.RetryIntervalSeconds,
		RunID:                runID,
		TriggerType:          string(triggerType),
		ReportStatus:         task.ReportStatus,
	}
	if task.Config != nil {
		raw, err := sonic.Marshal(task.Config)
		if err != nil {
			e.logger.Warn("failed to serialize task config for trigger event",
				zap.Int64("task_id", task.ID),
				zap.Error(err),
			)
		} else {
			payload.Config = string(raw)
		}
	}
	var pubOpts []event.PublishOption
	if task.Metadata != nil {
		pubOpts = append(pubOpts, event.WithMetadata(task.Metadata))
	}
	// Detach from the caller's cancellation: the trigger event must reach
	// the bus even after the originating request returns, while keeping the
	// request's trace metadata.
	if err := event.Publish(context.WithoutCancel(ctx), e.bus, event.TypeExecutionTriggered, payload,
		pubOpts...); err != nil {
		e.logger.Warn("failed to publish execution triggered event",
			zap.Int64("task_id", task.ID),
			zap.Error(err),
		)
		// Publish failed: release the running marker so the next fire can
		// proceed. Without this, a Concurrency == 1 task would be
		// permanently blocked since no ExecutionCompleted event will arrive.
		e.releaseRunning(task.ID)
	} else if triggerType == TriggerTypeSchedule || triggerType == TriggerTypeCatchup {
		// A slot-bound dispatch that reached the bus is consumed as far as
		// the watermark is concerned (at-least-once: replaying after a
		// crash is allowed, skipping is not). Manual and event triggers
		// are not slot-bound and leave the watermark untouched.
		e.advanceWatermark(task.ID, time.Now())
	}
}

// newRunID generates a unique 32-char hex identifier for a task run.
//
// The format is load-bearing for report credential routing (see bindTaskRef):
// a run ID is never parseable as a decimal task number — any hex letter
// breaks base-10 parsing and an all-digit string is 32 chars long, beyond
// int64 range — and the time-based fallback carries a "run-" prefix. Any
// future ID format must preserve this invariant.
func newRunID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("run-%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
