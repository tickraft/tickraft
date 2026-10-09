// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/cron"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// Catch-up policy values for Task.CatchupPolicy.
const (
	// CatchupPolicySkip discards missed slots on recovery (the historical
	// behavior and the column default).
	CatchupPolicySkip = "skip"
	// CatchupPolicyOnce replays only the most recent missed slot.
	CatchupPolicyOnce = "once"
	// CatchupPolicyAll replays every missed slot up to catchupMaxSlots.
	CatchupPolicyAll = "all"
)

const (
	// catchupMaxSlots bounds how many missed slots a single recovery may
	// dispatch per task. Slots beyond the cap are dropped with a
	// structured log; interval tasks merge naturally (elapsed / interval)
	// so the cap only bites after very long downtime.
	catchupMaxSlots = 10
	// catchupMaxIterations bounds the cron Next() enumeration while
	// computing missed slots, so a pathological expression cannot spin
	// the recovery loop. One-minute crons reach a million iterations only
	// after roughly two years of downtime.
	catchupMaxIterations = 1_000_000
	// sleepWindowMaxCount caps the windows a task may declare. Operators
	// need one or two; the cap keeps request bodies and per-fire checks
	// trivially bounded.
	sleepWindowMaxCount = 8
	// minutesPerDay is the exclusive upper bound of a time-of-day in
	// minutes; "24:00" is accepted as an end value meaning end of day.
	minutesPerDay = 24 * 60
)

// SleepWindow is one recurring dispatch-suppression window on a task's
// schedule. Start and End are wall-clock times of day ("HH:MM"); End also
// accepts "24:00" for a full-day window. Days lists the weekdays the
// window is active on, numbered like time.Weekday (0=Sunday..6=Saturday).
// A window with Start > End crosses midnight and belongs to the weekday it
// starts on: Days=[5] Start=22:00 End=06:00 suppresses Friday 22:00
// through Saturday 06:00. Start == End is rejected (a zero-length window
// is meaningless and a 24-hour window is expressed as 00:00–24:00).
type SleepWindow struct {
	// Start is the window's opening time of day ("HH:MM").
	Start string `json:"start"`
	// End is the window's closing time of day ("HH:MM", or "24:00").
	End string `json:"end"`
	// Days lists the active weekdays (0=Sunday..6=Saturday).
	Days []int `json:"days"`
}

// ValidateCatchupConfig validates the catch-up policy value and the sleep
// window list of an incoming task request. An empty policy means "absent"
// (the caller normalizes it to skip) and passes; windows must be
// well-formed whenever present.
func ValidateCatchupConfig(policy string, windows []SleepWindow) error {
	switch policy {
	case "", CatchupPolicySkip, CatchupPolicyOnce, CatchupPolicyAll:
	default:
		return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
			fmt.Sprintf("invalid catchup_policy %q: must be one of skip, once, all", policy))
	}
	if len(windows) > sleepWindowMaxCount {
		return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
			fmt.Sprintf("too many sleep windows: maximum %d", sleepWindowMaxCount))
	}
	for i, w := range windows {
		start, ok := parseTimeOfDay(w.Start)
		if !ok {
			return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
				fmt.Sprintf("sleep window %d: invalid start %q: must be HH:MM", i, w.Start))
		}
		end, ok := parseTimeOfDay(w.End)
		if !ok {
			return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
				fmt.Sprintf("sleep window %d: invalid end %q: must be HH:MM or 24:00", i, w.End))
		}
		if start == end {
			return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
				fmt.Sprintf("sleep window %d: start and end must differ (use 00:00–24:00 for a full day)", i))
		}
		if len(w.Days) == 0 {
			return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
				fmt.Sprintf("sleep window %d: days is required", i))
		}
		for _, d := range w.Days {
			if d < 0 || d > 6 {
				return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
					fmt.Sprintf("sleep window %d: day %d out of range 0-6", i, d))
			}
		}
	}
	return nil
}

// parseTimeOfDay parses a strict "HH:MM" string (zero-padded, 24-hour)
// into minutes of day. The end value "24:00" (minutesPerDay) is accepted
// so a full-day window can be expressed; no other value may reach 24:00.
func parseTimeOfDay(s string) (int, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, false
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, false
	}
	total := h*60 + m
	if h < 0 || h > 24 || m < 0 || m >= 60 || total > minutesPerDay {
		return 0, false
	}
	return total, true
}

// InSleepWindow reports whether at falls inside any of the task's sleep
// windows. Evaluation uses the process's local timezone: windows express
// an operator's wall-clock night. Malformed windows (failed validation at
// write time, or a hand-edited row) are skipped rather than suppressing
// dispatch — suppression is the dangerous direction to get wrong.
func InSleepWindow(windows []SleepWindow, at time.Time) bool {
	tod := at.Hour()*60 + at.Minute()
	wd := int(at.Weekday())
	for _, w := range windows {
		start, okS := parseTimeOfDay(w.Start)
		end, okE := parseTimeOfDay(w.End)
		if !okS || !okE || start == end || len(w.Days) == 0 {
			continue
		}
		if start < end {
			if containsDay(w.Days, wd) && tod >= start && tod < end {
				return true
			}
			continue
		}
		// Cross-midnight window: [start, 24:00) on an active day plus
		// [00:00, end) on the following day.
		if (containsDay(w.Days, wd) && tod >= start) ||
			(containsDay(w.Days, (wd+6)%7) && tod < end) {
			return true
		}
	}
	return false
}

// containsDay reports whether days contains the given weekday.
func containsDay(days []int, day int) bool {
	for _, d := range days {
		if d == day {
			return true
		}
	}
	return false
}

// missedSlots enumerates the scheduled slots in the half-open interval
// (watermark, now] and returns the latest at most catchupMaxSlots of them
// plus the count of older slots dropped by that cap. Interval schedules
// compute the count arithmetically; cron schedules iterate Next(). Only
// cron and interval schedules have slots — the caller filters once/event.
func missedSlots(schedule string, scheduleType ScheduleType, interval time.Duration,
	watermark, now time.Time,
) (slots []time.Time, dropped int64) {
	if !watermark.Before(now) {
		return nil, 0
	}
	switch scheduleType {
	case ScheduleTypeInterval:
		if interval <= 0 {
			return nil, 0
		}
		total := int64(now.Sub(watermark) / interval)
		if total <= 0 {
			return nil, 0
		}
		first := total - catchupMaxSlots + 1
		first = max(first, 1)
		slots = make([]time.Time, 0, total-first+1)
		for k := first; k <= total; k++ {
			slots = append(slots, watermark.Add(time.Duration(k)*interval))
		}
		return slots, total - int64(len(slots))
	case ScheduleTypeCron:
		sched, err := cron.Parse(schedule)
		if err != nil {
			return nil, 0
		}
		var enumerated int64
		next := sched.Next(watermark)
		for !next.After(now) && enumerated < catchupMaxIterations {
			enumerated++
			if len(slots) == catchupMaxSlots {
				// Keep a rolling window of the latest slots; older ones
				// are beyond the replay cap.
				copy(slots, slots[1:])
				slots = slots[:len(slots)-1]
				dropped++
			}
			slots = append(slots, next)
			next = sched.Next(next)
		}
		if enumerated == catchupMaxIterations {
			// Enumeration hit the safety valve; the true missed count is
			// at least what was enumerated. Report it as dropped so the
			// log line stays honest without a second enumeration pass.
			dropped = enumerated
			slots = nil
		}
		return slots, dropped
	default:
		return nil, 0
	}
}

// runCatchup replays the slots a task missed since its watermark, per the
// task's catch-up policy, and then moves the watermark to now. It runs on
// Restore (process restart) and Resume (task re-enabled after pause); on
// both paths the watermark did not advance while scheduling was down.
//
// Each replayed slot passes through the same gates as a regular fire —
// shard ownership, sleep window (evaluated at the slot's own time), the
// dependency check, and the Concurrency == 1 running claim — so recovery
// never bypasses dispatch semantics. Dispatch consistency is
// at-least-once: the watermark moves to now after the loop, and a crash
// mid-replay re-dispatches at most the slots of one window.
func (e *Engine) runCatchup(ctx context.Context, task Task, now time.Time) {
	if task.CatchupPolicy != CatchupPolicyOnce && task.CatchupPolicy != CatchupPolicyAll {
		return
	}
	if task.LastScheduledAt == nil {
		// Never dispatched: the first start must not replay history.
		return
	}
	scheduleType, interval, err := ClassifySchedule(task.Schedule)
	if err != nil {
		return
	}
	if scheduleType != ScheduleTypeCron && scheduleType != ScheduleTypeInterval {
		return
	}
	// Only the shard that owns the task may replay it; every worker runs
	// Restore, and without this guard each would dispatch its own copy.
	if !e.shardManager.Owns(task.ID) {
		return
	}

	slots, dropped := missedSlots(task.Schedule, scheduleType, interval, *task.LastScheduledAt, now)
	if dropped > 0 {
		e.logger.Warn("catchup slots dropped over replay cap",
			zap.Int64("task_id", task.ID),
			zap.Int64("dropped", dropped),
			zap.Int("replayed", len(slots)),
		)
	}
	if task.CatchupPolicy == CatchupPolicyOnce && len(slots) > 0 {
		slots = slots[len(slots)-1:]
	}
	for _, slot := range slots {
		e.dispatchGated(ctx, task, slot, TriggerTypeCatchup)
	}
	// Advance to now regardless of what was replayed: dropped and
	// dependency-skipped slots count as consumed too, so the next
	// recovery does not see them again. Monotonic, so this never races a
	// concurrent advance from one of the replays above.
	e.advanceWatermark(task.ID, now)
}

// advanceWatermark moves the task's schedule watermark (last_scheduled_at)
// forward to at, in memory and in the store. Regressions are ignored: the
// watermark is the "most recent consumed slot" marker and a late reader's
// stale copy must not move it backwards. Store failures are logged, not
// propagated — a lost watermark advance costs at most one extra replay on
// the next recovery, which the at-least-once contract already allows.
func (e *Engine) advanceWatermark(taskID int64, at time.Time) {
	e.taskMu.Lock()
	t, ok := e.tasks[taskID]
	if !ok {
		e.taskMu.Unlock()
		return
	}
	if t.LastScheduledAt != nil && !t.LastScheduledAt.Before(at) {
		e.taskMu.Unlock()
		return
	}
	t.LastScheduledAt = &at
	e.tasks[taskID] = t
	e.taskMu.Unlock()

	if e.store != nil {
		if err := e.store.AdvanceScheduleWatermark(context.Background(), taskID, at); err != nil {
			e.logger.Warn("failed to advance schedule watermark",
				zap.Int64("task_id", taskID),
				zap.Error(err),
			)
		}
	}
}
