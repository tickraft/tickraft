// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cron

import (
	"fmt"
	"testing"
	"time"
)

func TestDebugNext(t *testing.T) {
	expr := "0 0 12 * * *"
	sched, err := Parse(expr)
	if err != nil {
		t.Fatal(err)
	}
	s := sched.(*specSchedule)
	s.loc = time.UTC
	from, _ := time.Parse(time.RFC3339, "2026-06-18T11:59:55Z")
	current := from.In(from.Location()).Add(time.Second).Truncate(time.Second)

	for i := range 5 {
		fmt.Printf("iteration %d current=%s\n", i, current.UTC().Format(time.RFC3339))
		sec, ok := nextSetBit(s.sec, current.Second(), 59)
		fmt.Printf(" sec from=%d => %d ok=%v\n", current.Second(), sec, ok)
		if !ok {
			current = current.Add(time.Duration(60-current.Second()) * time.Second)
			current = current.Truncate(time.Minute)
			continue
		}
		if sec != current.Second() {
			current = time.Date(current.Year(), current.Month(), current.Day(), current.Hour(),
				current.Minute(), sec, 0, current.Location())
		}
		fmt.Printf(" after sec current=%s\n", current.UTC().Format(time.RFC3339))

		nextMin, ok := nextSetBit(s.min, current.Minute(), 59)
		fmt.Printf(" min from=%d => %d ok=%v\n", current.Minute(), nextMin, ok)
		if !ok {
			current = current.Add(time.Duration(60-current.Minute()) * time.Minute)
			current = current.Truncate(time.Hour)
			continue
		}
		if nextMin != current.Minute() {
			current = time.Date(current.Year(), current.Month(), current.Day(), current.Hour(), nextMin,
				current.Second(), 0, current.Location())
		}
		fmt.Printf(" after min current=%s\n", current.UTC().Format(time.RFC3339))

		hour, ok := nextSetBit(s.hour, current.Hour(), 23)
		fmt.Printf(" hour from=%d => %d ok=%v\n", current.Hour(), hour, ok)
		if !ok {
			current = time.Date(current.Year(), current.Month(), current.Day()+1, 0, 0, 0, 0, current.Location())
			continue
		}
		if hour != current.Hour() {
			current = time.Date(current.Year(), current.Month(), current.Day(), hour,
				current.Minute(), current.Second(), 0, current.Location())
		}
		fmt.Printf(" after hour current=%s\n", current.UTC().Format(time.RFC3339))

		month, ok := nextSetBit(s.month, int(current.Month()), 12)
		fmt.Printf(" month from=%d => %d ok=%v\n", current.Month(), month, ok)
		if !ok {
			current = time.Date(current.Year()+1, time.January, 1, 0, 0, 0, 0, current.Location())
			continue
		}
		if month != int(current.Month()) {
			current = time.Date(current.Year(), time.Month(month), 1, 0, 0, 0, 0, current.Location())
		}
		fmt.Printf(" after month current=%s\n", current.UTC().Format(time.RFC3339))

		fmt.Printf("matchesDay=%v secMatch=%v minMatch=%v hourMatch=%v monthMatch=%v\n",
			s.matchesDay(current), bitMatch(s.sec, current.Second()), bitMatch(s.min, current.Minute()),
			bitMatch(s.hour, current.Hour()), bitMatch(s.month, int(current.Month())))

		if bitMatch(s.sec, current.Second()) && bitMatch(s.min, current.Minute()) &&
			bitMatch(s.hour, current.Hour()) && bitMatch(s.month, int(current.Month())) {
			fmt.Printf("returning %s\n", current.UTC().Format(time.RFC3339))
			return
		}
		current = current.Add(time.Second)
	}
	t.Fatal("did not return")
}
