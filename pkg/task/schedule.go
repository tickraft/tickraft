// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"fmt"
	"time"

	"github.com/tickraft/tickraft/pkg/cron"
	"github.com/tickraft/tickraft/pkg/scheduler"
)

// ClassifySchedule derives the schedule type from a task's schedule string:
// "" is event-driven, a valid Go duration string ("30s", "5m", "1h30m") is
// a fixed interval, and anything else is a cron expression. Cron
// expressions and interval values are validated; the returned error is nil
// exactly when parseSchedule would succeed.
func ClassifySchedule(schedule string) (ScheduleType, time.Duration, error) {
	switch {
	case schedule == "":
		return ScheduleTypeEvent, 0, nil
	case isIntervalSchedule(schedule):
		interval, err := time.ParseDuration(schedule)
		if err != nil {
			return ScheduleTypeCron, 0, fmt.Errorf("task: parse interval %q: %w", schedule, err)
		}
		if interval <= 0 {
			return ScheduleTypeInterval, interval, fmt.Errorf("task: interval must be positive, got %s", interval)
		}
		return ScheduleTypeInterval, interval, nil
	default:
		if _, err := cron.Parse(schedule); err != nil {
			return ScheduleTypeCron, 0, fmt.Errorf("%w: %w", scheduler.ErrInvalidCronExpr, err)
		}
		return ScheduleTypeCron, 0, nil
	}
}

// isIntervalSchedule reports whether the schedule string is a valid Go
// duration (e.g. "30s", "5m", "1h30m").
func isIntervalSchedule(schedule string) bool {
	_, err := time.ParseDuration(schedule)
	return err == nil
}

// parseSchedule converts a task's schedule string to a scheduler.Schedule:
// "" yields a never schedule (event-driven tasks are triggered externally),
// a duration string yields a constant interval, anything else is parsed as
// a cron expression.
func parseSchedule(schedule string) (scheduler.Schedule, error) {
	switch {
	case schedule == "":
		return scheduler.NewNeverSchedule(), nil
	case isIntervalSchedule(schedule):
		interval, err := time.ParseDuration(schedule)
		if err != nil {
			return nil, fmt.Errorf("task: parse interval %q: %w", schedule, err)
		}
		if interval <= 0 {
			return nil, fmt.Errorf("task: interval must be positive, got %s", interval)
		}
		return scheduler.NewConstantIntervalSchedule(interval), nil
	default:
		sched, err := cron.Parse(schedule)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", scheduler.ErrInvalidCronExpr, err)
		}
		return sched, nil
	}
}
