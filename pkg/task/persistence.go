// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/cron"
	"github.com/tickraft/tickraft/pkg/quota"
)

// Restore loads persisted tasks from the configured Store into memory
// and schedules each one in the engine. Tasks with invalid schedule
// configurations are skipped with a warning log. If no store is configured,
// Restore is a no-op.
//
// Restore is idempotent: it first tears down all previously registered
// engine entries and clears the in-memory schedule indexes, then rebuilds
// them from the store. This prevents stale schedule entries from leaking
// when Restore is called more than once (e.g. on repeated startup or
// reconfiguration).
func (e *Engine) Restore(ctx context.Context) error {
	if e.store == nil {
		return nil
	}
	tasks, err := e.store.List(ctx, ListOptions{})
	if err != nil {
		return fmt.Errorf("list tasks from store: %w", err)
	}

	// Tear down existing engine entries and clear schedule indexes so a
	// repeated Restore does not leak stale schedules for tasks that are no
	// longer in the store.
	e.mu.Lock()
	for id := range e.scheds {
		if err := e.engine.Remove(id); err != nil {
			e.logger.Warn("failed to remove task from engine during restore",
				zap.Int64("task_id", id),
				zap.Error(err),
			)
		}
	}
	e.scheds = make(map[int64]cron.Schedule)
	e.scheduleTypes = make(map[int64]ScheduleType)
	e.eventDrivenTasks = make(map[int64]struct{})
	e.mu.Unlock()

	e.taskMu.Lock()
	e.tasks = make(map[int64]Task, len(tasks))
	for _, t := range tasks {
		e.tasks[t.ID] = *t
	}
	e.taskMu.Unlock()

	e.logger.Info("restored tasks from store", zap.Int("count", len(tasks)))

	scheduled := 0
	for _, task := range tasks {
		if !task.Enabled {
			e.logger.Info("skip scheduling disabled task",
				zap.Int64("task_id", task.ID),
			)
			continue
		}
		scheduleType, interval, err := ClassifySchedule(task.Schedule)
		if err != nil {
			e.logger.Warn("skip restoring task with invalid schedule",
				zap.Int64("task_id", task.ID),
				zap.String("schedule", task.Schedule),
				zap.Error(err),
			)
			continue
		}
		if err := checkMinInterval(scheduleType, interval); err != nil {
			e.logger.Warn("skip restoring task with interval below minimum",
				zap.Int64("task_id", task.ID),
				zap.Duration("interval", interval),
				zap.Duration("min_interval", time.Duration(quota.Ceiling(quota.TypeScheduledTaskInterval))*time.Second),
				zap.Error(err),
			)
			continue
		}
		sched, err := parseSchedule(task.Schedule)
		if err != nil {
			e.logger.Warn("skip restoring task with invalid schedule",
				zap.Int64("task_id", task.ID),
				zap.String("schedule", task.Schedule),
				zap.Error(err),
			)
			continue
		}

		e.mu.Lock()
		e.scheds[task.ID] = sched
		e.scheduleTypes[task.ID] = scheduleType
		if scheduleType == ScheduleTypeEvent {
			e.eventDrivenTasks[task.ID] = struct{}{}
		} else {
			delete(e.eventDrivenTasks, task.ID)
		}
		e.mu.Unlock()

		if err := e.engine.Add(task.ID, sched, e.onFire); err != nil {
			e.logger.Warn("failed to schedule restored task",
				zap.Int64("task_id", task.ID),
				zap.Error(err),
			)
			continue
		}

		scheduled++
		e.logger.Info("restored task schedule",
			zap.Int64("task_id", task.ID),
			zap.String("schedule_type", string(scheduleType)),
		)

		// The watermark stopped advancing when the process went down;
		// replay the missed slots per the task's catch-up policy. Event
		// tasks have no slots and no-op inside runCatchup. Disabled tasks
		// are skipped above and replay when resumed instead.
		e.runCatchup(ctx, *task, time.Now())
	}

	e.logger.Info("scheduler tasks restored",
		zap.Int("loaded", len(tasks)),
		zap.Int("scheduled", scheduled),
	)
	return nil
}

// getTask retrieves a task by ID. Returns ErrTaskNotFound if not present.
func (e *Engine) getTask(id int64) (Task, error) {
	e.taskMu.RLock()
	defer e.taskMu.RUnlock()
	t, ok := e.tasks[id]
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	return t, nil
}

// setTask stores or replaces a task configuration in memory and, if a store
// is configured, persists the task to the store.
func (e *Engine) setTask(task Task) {
	e.taskMu.Lock()
	e.tasks[task.ID] = task
	e.taskMu.Unlock()

	if e.store != nil {
		if err := e.store.Save(context.Background(), &task); err != nil {
			e.logger.Error("persist task save",
				zap.Int64("task_id", task.ID),
				zap.Error(err),
			)
		}
	}
}

// deleteTask removes a task by ID from memory and, if a store is configured,
// deletes it from the store.
func (e *Engine) deleteTask(id int64) {
	e.taskMu.Lock()
	delete(e.tasks, id)
	e.taskMu.Unlock()

	if e.store != nil {
		if err := e.store.Delete(context.Background(), id); err != nil {
			e.logger.Error("persist task delete",
				zap.Int64("task_id", id),
				zap.Error(err),
			)
		}
	}
}
