// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package scheduler

import (
	"sync/atomic"
	"time"

	"github.com/tickraft/tickraft/pkg/cron"
)

// Schedule determines the next execution time for an entry. It is an alias
// for cron.Schedule so that the Engine API can be used without forcing
// callers to import pkg/cron directly.
type Schedule = cron.Schedule

// constantIntervalSchedule implements Schedule for fixed-interval scheduling.
type constantIntervalSchedule struct {
	interval time.Duration
}

// NewConstantIntervalSchedule creates a constantIntervalSchedule with the
// given interval. The interval must be positive.
func NewConstantIntervalSchedule(interval time.Duration) Schedule {
	return &constantIntervalSchedule{interval: interval}
}

// Next returns the next activation time after the given time.
func (s *constantIntervalSchedule) Next(from time.Time) time.Time {
	return from.Add(s.interval)
}

// oneTimeSchedule implements Schedule for a single execution at a specific
// time. The fired flag is accessed atomically because Next may be invoked
// from different goroutines (e.g. the time wheel worker and reschedule
// paths).
type oneTimeSchedule struct {
	fireAt time.Time
	fired  atomic.Bool
}

// NewOneTimeSchedule creates a oneTimeSchedule that fires once at the given
// time. After firing, subsequent calls to Next return the zero Time.
func NewOneTimeSchedule(fireAt time.Time) Schedule {
	return &oneTimeSchedule{fireAt: fireAt}
}

// Next returns the fire time if it has not yet fired, otherwise returns the
// zero Time.
func (s *oneTimeSchedule) Next(from time.Time) time.Time {
	if s.fired.Load() {
		return time.Time{}
	}
	if from.After(s.fireAt) {
		s.fired.Store(true)
		return time.Time{}
	}
	return s.fireAt
}

// immediateSchedule implements Schedule for a one-time immediate execution.
// It returns from on the first call and zero Time on subsequent calls.
// The fired flag is accessed atomically because Next may be invoked from
// different goroutines.
type immediateSchedule struct {
	fired atomic.Bool
}

// NewImmediateSchedule creates an immediateSchedule that fires once on the
// first Next() call. After firing, subsequent calls return the zero Time.
func NewImmediateSchedule() Schedule {
	return &immediateSchedule{}
}

// Next returns from on the first call, zero Time thereafter.
func (s *immediateSchedule) Next(from time.Time) time.Time {
	if s.fired.Load() {
		return time.Time{}
	}
	s.fired.Store(true)
	return from
}

// neverSchedule implements Schedule for event-driven entries. It never
// returns a fire time because event-driven entries are triggered by
// external events rather than by a time-based schedule.
type neverSchedule struct{}

// NewNeverSchedule creates a neverSchedule that never fires on a time-based
// schedule. Use this for entries that are triggered exclusively by external
// events.
func NewNeverSchedule() Schedule {
	return &neverSchedule{}
}

// Next always returns the zero Time, meaning the entry will never fire on a
// time-based schedule.
func (s *neverSchedule) Next(_ time.Time) time.Time { return time.Time{} }

// Compile-time interface assertions.
var (
	_ cron.Schedule = (*constantIntervalSchedule)(nil)
	_ cron.Schedule = (*oneTimeSchedule)(nil)
	_ cron.Schedule = (*immediateSchedule)(nil)
	_ cron.Schedule = (*neverSchedule)(nil)
)
