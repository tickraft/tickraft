// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cron

import (
	"testing"
	"time"
	"unsafe"
)

func TestSpecScheduleNextTimezone(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	sched := &specSchedule{
		sec:     1,
		min:     1,
		hour:    uint32(allBits(0, 23)),
		dom:     uint32(allBits(1, 31)),
		month:   uint16(allBits(1, 12)),
		dow:     uint8(allBits(0, 7)),
		domStar: true,
		dowStar: true,
		loc:     loc,
	}
	// 10:30 UTC = 06:30 EDT (or 05:30 EST)
	from := time.Date(2026, 6, 18, 10, 30, 0, 0, time.UTC)
	next := sched.Next(from)
	if next.Location() != loc {
		t.Fatalf("expected location %v, got %v", loc, next.Location())
	}
}

func TestSpecScheduleMemoryLayout(t *testing.T) {
	size := unsafe.Sizeof(specSchedule{})
	if size > 48 {
		t.Fatalf("SpecSchedule size %d is too large, expected < 48 bytes", size)
	}
	t.Logf("SpecSchedule size: %d bytes", size)
}

func TestConstantDelayScheduleNext(t *testing.T) {
	sched := constantDelaySchedule{Delay: 5 * time.Minute}
	from := time.Date(2026, 6, 18, 10, 32, 0, 0, time.UTC)
	next := sched.Next(from)
	expected := time.Date(2026, 6, 18, 10, 35, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("expected %s, got %s", expected, next)
	}
}

func TestSpecScheduleFiveYearLimit(t *testing.T) {
	// Feb 30 never exists — construct SpecSchedule directly to set
	// only day-30 and month-2 bits (the parser has a known issue
	// where a bare value expands to value..max).
	sched := &specSchedule{
		sec:     1,
		min:     1,
		hour:    1,
		dom:     1 << 30, // day 30 only
		month:   1 << 2,  // February only
		dow:     uint8(allBits(0, 7)),
		domStar: false,
		dowStar: true,
		loc:     time.UTC,
	}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	next := sched.Next(from)
	// The 5-year iteration cap (pkg/cron/schedule.go) must return a zero
	// time for an impossible schedule so that the scheduler can detect it
	// and skip the task. A non-zero time here would cause an unreachable
	// schedule to fire forever. Use t.Fatalf (not t.Log) so the assertion
	// actually fails when violated.
	if !next.IsZero() {
		t.Fatalf("expected zero time for impossible schedule (Feb 30), got %v", next)
	}
}

func TestSpecScheduleLeapYear(t *testing.T) {
	// Feb 29 should work in leap years — construct SpecSchedule
	// directly to set only day-29 and month-2 bits.
	sched := &specSchedule{
		sec:     1,
		min:     1,
		hour:    1,
		dom:     1 << 29, // day 29 only
		month:   1 << 2,  // February only
		dow:     uint8(allBits(0, 7)),
		domStar: false,
		dowStar: true,
		loc:     time.UTC,
	}
	from := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	next := sched.Next(from)
	expected := "2024-02-29T00:00:00Z"
	if got := next.UTC().Format(time.RFC3339); got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}

func TestMinutely(t *testing.T) {
	sched := Minutely()
	ss := sched.(*specSchedule)
	ss.loc = time.UTC
	from, _ := time.Parse(time.RFC3339, "2026-06-18T10:30:15Z")
	next := sched.Next(from)
	expected := "2026-06-18T10:31:00Z"
	if got := next.UTC().Format(time.RFC3339); got != expected {
		t.Fatalf("Minutely: expected %s, got %s", expected, got)
	}
}

func TestHourly(t *testing.T) {
	sched := Hourly()
	ss := sched.(*specSchedule)
	ss.loc = time.UTC
	from, _ := time.Parse(time.RFC3339, "2026-06-18T10:30:15Z")
	next := sched.Next(from)
	expected := "2026-06-18T11:00:00Z"
	if got := next.UTC().Format(time.RFC3339); got != expected {
		t.Fatalf("Hourly: expected %s, got %s", expected, got)
	}
}

func TestDaily(t *testing.T) {
	sched := Daily()
	ss := sched.(*specSchedule)
	ss.loc = time.UTC
	from, _ := time.Parse(time.RFC3339, "2026-06-18T10:30:15Z")
	next := sched.Next(from)
	expected := "2026-06-19T00:00:00Z"
	if got := next.UTC().Format(time.RFC3339); got != expected {
		t.Fatalf("Daily: expected %s, got %s", expected, got)
	}
}

func TestWeekly(t *testing.T) {
	sched := Weekly()
	ss := sched.(*specSchedule)
	ss.loc = time.UTC
	from, _ := time.Parse(time.RFC3339, "2026-06-18T10:30:15Z") // Thursday
	next := sched.Next(from)
	expected := "2026-06-21T00:00:00Z" // Next Sunday
	if got := next.UTC().Format(time.RFC3339); got != expected {
		t.Fatalf("Weekly: expected %s, got %s", expected, got)
	}
}

func TestMonthly(t *testing.T) {
	sched := Monthly()
	ss := sched.(*specSchedule)
	ss.loc = time.UTC
	from, _ := time.Parse(time.RFC3339, "2026-06-18T10:30:15Z")
	next := sched.Next(from)
	expected := "2026-07-01T00:00:00Z"
	if got := next.UTC().Format(time.RFC3339); got != expected {
		t.Fatalf("Monthly: expected %s, got %s", expected, got)
	}
}

func TestYearly(t *testing.T) {
	sched := Yearly()
	ss := sched.(*specSchedule)
	ss.loc = time.UTC
	from, _ := time.Parse(time.RFC3339, "2026-06-18T10:30:15Z")
	next := sched.Next(from)
	expected := "2027-01-01T00:00:00Z"
	if got := next.UTC().Format(time.RFC3339); got != expected {
		t.Fatalf("EveryYear: expected %s, got %s", expected, got)
	}
}

func TestEvery(t *testing.T) {
	// Normal duration
	sched := Every(5 * time.Minute)
	from := time.Date(2026, 6, 18, 10, 32, 0, 0, time.UTC)
	next := sched.Next(from)
	expected := time.Date(2026, 6, 18, 10, 35, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("Every(5m): expected %s, got %s", expected, next)
	}

	// Duration < 1s should round up to 1s
	shortSched := Every(500 * time.Millisecond)

	s2 := shortSched.(*constantDelaySchedule)
	if s2.Delay != time.Second {
		t.Fatalf("Every(500ms): expected delay=1s, got %s", s2.Delay)
	}
}
