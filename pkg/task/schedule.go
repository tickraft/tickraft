// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"fmt"
	"time"

	"github.com/tickraft/tickraft/pkg/cron"
	"github.com/tickraft/tickraft/pkg/scheduler"
)

// Metadata keys used to persist schedule configuration on tasks.
const (
	metaKeyScheduleType = "schedule_type"
	metaKeyCronExpr     = "cron_expr"
)

// extractScheduleConfig reads the schedule type, cron expression, and
// interval from a task's Metadata map. Defaults to ScheduleTypeCron with
// empty values when the corresponding keys are absent.
func extractScheduleConfig(task Task) (ScheduleType, string, time.Duration) {
	scheduleType := ScheduleTypeCron
	cronExpr := ""
	interval := time.Duration(0)

	if task.Metadata != nil {
		if v, ok := task.Metadata[metaKeyScheduleType]; ok {
			scheduleType = ScheduleType(v)
		}
		if v, ok := task.Metadata[metaKeyCronExpr]; ok {
			cronExpr = v
		}
		if v, ok := task.Metadata[string(ScheduleTypeInterval)]; ok {
			if d, err := time.ParseDuration(v); err == nil {
				interval = d
			}
		}
	}
	return scheduleType, cronExpr, interval
}

// parseSchedule converts schedule configuration to a scheduler.Schedule.
// It supports cron expressions, fixed intervals, one-time, and event-driven
// schedules.
func parseSchedule(scheduleType ScheduleType, cronExpr string, interval time.Duration) (scheduler.Schedule, error) {
	switch scheduleType {
	case ScheduleTypeCron:
		if cronExpr == "" {
			return nil, fmt.Errorf("%w: cron expression is empty", scheduler.ErrInvalidCronExpr)
		}
		sched, err := cron.Parse(cronExpr)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", scheduler.ErrInvalidCronExpr, err)
		}
		return sched, nil

	case ScheduleTypeInterval:
		if interval <= 0 {
			return nil, fmt.Errorf("task: interval must be positive, got %v", interval)
		}
		return scheduler.NewConstantIntervalSchedule(interval), nil

	case ScheduleTypeOnce:
		if interval <= 0 {
			return scheduler.NewImmediateSchedule(), nil
		}
		return scheduler.NewOneTimeSchedule(time.Now().Add(interval)), nil

	case ScheduleTypeEvent:
		return scheduler.NewNeverSchedule(), nil

	default:
		return nil, fmt.Errorf("task: unsupported schedule type %q", scheduleType)
	}
}
