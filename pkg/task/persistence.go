// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"fmt"
	"time"

	"github.com/tickraft/tickraft/pkg/cron"
	"github.com/tickraft/tickraft/pkg/quota"
	"go.uber.org/zap"
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
func (m *Service) Restore(ctx context.Context) error {
	if m.store == nil {
		return nil
	}
	tasks, err := m.store.List(ctx, ListOptions{})
	if err != nil {
		return fmt.Errorf("list tasks from store: %w", err)
	}

	// Tear down existing engine entries and clear schedule indexes so a
	// repeated Restore does not leak stale schedules for tasks that are no
	// longer in the store.
	m.mu.Lock()
	for id := range m.scheds {
		if err := m.engine.Remove(id); err != nil {
			m.logger.Warn("failed to remove task from engine during restore",
				zap.Int64("task_id", id),
				zap.Error(err),
			)
		}
	}
	m.scheds = make(map[int64]cron.Schedule)
	m.scheduleTypes = make(map[int64]ScheduleType)
	m.eventDrivenTasks = make(map[int64]struct{})
	m.mu.Unlock()

	m.taskMu.Lock()
	m.tasks = make(map[int64]Task, len(tasks))
	for _, t := range tasks {
		m.tasks[t.ID] = *t
	}
	m.taskMu.Unlock()

	m.logger.Info("restored tasks from store", zap.Int("count", len(tasks)))

	scheduled := 0
	for _, task := range tasks {
		scheduleType, cronExpr, interval := extractScheduleConfig(*task)
		if err := checkMinInterval(scheduleType, interval); err != nil {
			m.logger.Warn("skip restoring task with interval below minimum",
				zap.Int64("task_id", task.ID),
				zap.Duration("interval", interval),
				zap.Duration("min_interval", time.Duration(quota.Ceiling(quota.TypeScheduledTaskInterval))*time.Second),
				zap.Error(err),
			)
			continue
		}
		sched, err := parseSchedule(scheduleType, cronExpr, interval)
		if err != nil {
			m.logger.Warn("skip restoring task with invalid schedule",
				zap.Int64("task_id", task.ID),
				zap.String("schedule_type", string(scheduleType)),
				zap.Error(err),
			)
			continue
		}

		m.mu.Lock()
		m.scheds[task.ID] = sched
		m.scheduleTypes[task.ID] = scheduleType
		if scheduleType == ScheduleTypeEvent {
			m.eventDrivenTasks[task.ID] = struct{}{}
		} else {
			delete(m.eventDrivenTasks, task.ID)
		}
		m.mu.Unlock()

		if err := m.engine.Add(task.ID, sched, m.onFire); err != nil {
			m.logger.Warn("failed to schedule restored task",
				zap.Int64("task_id", task.ID),
				zap.Error(err),
			)
			continue
		}

		scheduled++
		m.logger.Info("restored task schedule",
			zap.Int64("task_id", task.ID),
			zap.String("schedule_type", string(scheduleType)),
		)
	}

	m.logger.Info("scheduler tasks restored",
		zap.Int("loaded", len(tasks)),
		zap.Int("scheduled", scheduled),
	)
	return nil
}

// getTask retrieves a task by ID. Returns ErrTaskNotFound if not present.
func (m *Service) getTask(id int64) (Task, error) {
	m.taskMu.RLock()
	defer m.taskMu.RUnlock()
	t, ok := m.tasks[id]
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	return t, nil
}

// setTask stores or replaces a task configuration in memory and, if a store
// is configured, persists the task to the store.
func (m *Service) setTask(task Task) {
	m.taskMu.Lock()
	m.tasks[task.ID] = task
	m.taskMu.Unlock()

	if m.store != nil {
		if err := m.store.Save(context.Background(), &task); err != nil {
			m.logger.Error("persist task save",
				zap.Int64("task_id", task.ID),
				zap.Error(err),
			)
		}
	}
}

// deleteTask removes a task by ID from memory and, if a store is configured,
// deletes it from the store.
func (m *Service) deleteTask(id int64) {
	m.taskMu.Lock()
	delete(m.tasks, id)
	m.taskMu.Unlock()

	if m.store != nil {
		if err := m.store.Delete(context.Background(), id); err != nil {
			m.logger.Error("persist task delete",
				zap.Int64("task_id", id),
				zap.Error(err),
			)
		}
	}
}

// listTasks returns all stored tasks.
func (m *Service) listTasks() []Task {
	m.taskMu.RLock()
	defer m.taskMu.RUnlock()
	result := make([]Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		result = append(result, t)
	}
	return result
}
