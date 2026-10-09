// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package scheduler

import (
	"testing"
	"time"
)

// --- Schedule constructor Tests ---

func TestConstantIntervalSchedule(t *testing.T) {
	sched := NewConstantIntervalSchedule(5 * time.Minute)
	now := time.Now()

	next := sched.Next(now)
	if !next.Equal(now.Add(5 * time.Minute)) {
		t.Errorf("next = %v, want %v", next, now.Add(5*time.Minute))
	}
}

func TestOneTimeSchedule(t *testing.T) {
	fireAt := time.Now().Add(1 * time.Hour)
	sched := NewOneTimeSchedule(fireAt)

	// Before fire time.
	next := sched.Next(time.Now())
	if !next.Equal(fireAt) {
		t.Errorf("next = %v, want %v", next, fireAt)
	}

	// After fire time.
	next = sched.Next(fireAt.Add(1 * time.Second))
	if !next.IsZero() {
		t.Errorf("next after fire should be zero, got %v", next)
	}
}

func TestImmediateSchedule(t *testing.T) {
	sched := NewImmediateSchedule()
	now := time.Now()

	// First call returns the input time.
	next := sched.Next(now)
	if !next.Equal(now) {
		t.Errorf("first next = %v, want %v", next, now)
	}

	// Subsequent calls return zero.
	next = sched.Next(now)
	if !next.IsZero() {
		t.Errorf("second next = %v, want zero", next)
	}
}

func TestNeverSchedule(t *testing.T) {
	sched := NewNeverSchedule()
	next := sched.Next(time.Now())
	if !next.IsZero() {
		t.Errorf("next = %v, want zero for neverSchedule", next)
	}
}
