// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cron

import "time"

// Schedule determines the next execution time for a cron entry.
type Schedule interface {
	Next(from time.Time) time.Time
}

// specSchedule stores precomputed bitmasks for each cron field.
// All masks are uint64 to avoid narrowing conversions at every
// construction and bit-test site; the struct is allocated once per
// entry so the extra bytes are irrelevant.
type specSchedule struct {
	sec     uint64
	min     uint64
	hour    uint64
	dom     uint64
	month   uint64
	dow     uint64
	domStar bool
	dowStar bool
	loc     *time.Location
}

// Next returns the next execution time after the given moment.
func (ss *specSchedule) Next(from time.Time) time.Time {
	loc := ss.loc
	if loc == nil {
		loc = time.Local
	}
	current := from.In(loc).Add(time.Second).Truncate(time.Second)

	iter := 0
	for maxYears := current.Year() + 5; current.Year() <= maxYears; {
		iter++
		if iter > 1000 {
			return time.Time{}
		}
		month, ok := nextSetBit(ss.month, int(current.Month()), 12)
		if !ok {
			current = time.Date(current.Year()+1, time.January, 1, 0, 0, 0, 0, loc)
			continue
		}
		if month != int(current.Month()) {
			current = time.Date(current.Year(), time.Month(month), 1, 0, 0, 0, 0, loc)
		}

		if !ss.matchesDay(current) {
			current = current.AddDate(0, 0, 1)
			current = time.Date(current.Year(), current.Month(), current.Day(), 0, 0, 0, 0, loc)
			continue
		}

		hour, ok := nextSetBit(ss.hour, current.Hour(), 23)
		if !ok {
			current = current.AddDate(0, 0, 1)
			current = time.Date(current.Year(), current.Month(), current.Day(), 0, 0, 0, 0, loc)
			continue
		}
		if hour != current.Hour() {
			current = time.Date(current.Year(), current.Month(), current.Day(), hour, 0, 0, 0, loc)
		}

		nextMin, ok := nextSetBit(ss.min, current.Minute(), 59)
		if !ok {
			current = current.Add(time.Duration(60-current.Minute()) * time.Minute)
			current = current.Truncate(time.Hour)
			continue
		}
		if nextMin != current.Minute() {
			current = time.Date(current.Year(), current.Month(), current.Day(), current.Hour(), nextMin, 0, 0, loc)
		}

		sec, ok := nextSetBit(ss.sec, current.Second(), 59)
		if !ok {
			current = current.Add(time.Duration(60-current.Second()) * time.Second)
			current = current.Truncate(time.Minute)
			continue
		}
		if sec != current.Second() {
			current = time.Date(current.Year(), current.Month(), current.Day(), current.Hour(),
				current.Minute(), sec, 0, loc)
		}

		if ss.matchesDay(current) && bitMatch(ss.month, int(current.Month())) {
			return current
		}

		current = current.Add(time.Second)
	}

	return time.Time{}
}

func (ss *specSchedule) matchesDay(t time.Time) bool {
	day := t.Day()
	weekday := int(t.Weekday())

	domMatch := bitMatch(ss.dom, day)
	dowMatch := bitMatch(ss.dow, weekday) || (weekday == 0 && bitMatch(ss.dow, 7))

	if ss.domStar && ss.dowStar {
		return true
	}
	if ss.domStar {
		return dowMatch
	}
	if ss.dowStar {
		return domMatch
	}
	return domMatch || dowMatch
}

// constantDelaySchedule is a schedule that fires at a fixed interval.
type constantDelaySchedule struct {
	Delay time.Duration
}

// Next returns the next activation time after the given time.
func (cds constantDelaySchedule) Next(t time.Time) time.Time {
	next := t.Truncate(cds.Delay).Add(cds.Delay)
	if !next.After(t) {
		next = next.Add(cds.Delay)
	}
	return next
}

// Every creates a constantDelaySchedule with the given duration.
// Durations less than one second are rounded up to one second,
// and sub-second precision is truncated.
func Every(duration time.Duration) Schedule {
	duration = max(duration, time.Second)
	duration = duration.Truncate(time.Second)
	return &constantDelaySchedule{Delay: duration}
}

// immediateSchedule fires once immediately when the entry is added and never
// again. It backs the @reboot descriptor, which runs a single time at startup.
// The zero time returned by Next is in the past, so the cron manager treats
// the entry as immediately due; after the single execution the zero return
// value causes the manager to remove the entry.
type immediateSchedule struct{}

// Next returns the zero time on every call.
func (immediateSchedule) Next(_ time.Time) time.Time {
	return time.Time{}
}

// Minutely returns a schedule that fires every minute at second 0.
// Equivalent to the cron expression "0 * * * * *".
func Minutely() Schedule {
	return &specSchedule{
		sec:     1,
		min:     allBits(0, 59),
		hour:    allBits(0, 23),
		dom:     allBits(1, 31),
		month:   allBits(1, 12),
		dow:     allBits(0, 7),
		domStar: true,
		dowStar: true,
		loc:     time.Local,
	}
}

// Hourly returns a schedule that fires every hour at minute 0, second 0.
// Equivalent to the cron expression "0 0 * * * *".
func Hourly() Schedule {
	return &specSchedule{
		sec:     1,
		min:     1,
		hour:    allBits(0, 23),
		dom:     allBits(1, 31),
		month:   allBits(1, 12),
		dow:     allBits(0, 7),
		domStar: true,
		dowStar: true,
		loc:     time.Local,
	}
}

// Daily returns a schedule that fires every day at 00:00:00.
// Equivalent to the cron expression "0 0 0 * * *".
func Daily() Schedule {
	return &specSchedule{
		sec:     1,
		min:     1,
		hour:    1,
		dom:     allBits(1, 31),
		month:   allBits(1, 12),
		dow:     allBits(0, 7),
		domStar: true,
		dowStar: true,
		loc:     time.Local,
	}
}

// Weekly returns a schedule that fires every Sunday at 00:00:00.
// Equivalent to the cron expression "0 0 0 * * 0".
func Weekly() Schedule {
	return &specSchedule{
		sec:     1,
		min:     1,
		hour:    1,
		dom:     allBits(1, 31),
		month:   allBits(1, 12),
		dow:     1,
		domStar: true,
		dowStar: false,
		loc:     time.Local,
	}
}

// Monthly returns a schedule that fires on the 1st of every month at 00:00:00.
// Equivalent to the cron expression "0 0 0 1 * *".
func Monthly() Schedule {
	return &specSchedule{
		sec:     1,
		min:     1,
		hour:    1,
		dom:     2,
		month:   allBits(1, 12),
		dow:     allBits(0, 7),
		domStar: false,
		dowStar: true,
		loc:     time.Local,
	}
}

// Yearly returns a schedule that fires on January 1st at 00:00:00.
// Equivalent to the cron expression "0 0 0 1 1 *".
func Yearly() Schedule {
	return &specSchedule{
		sec:     1,
		min:     1,
		hour:    1,
		dom:     2,
		month:   2,
		dow:     allBits(0, 7),
		domStar: false,
		dowStar: true,
		loc:     time.Local,
	}
}
