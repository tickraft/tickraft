// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cron

import (
	"testing"
	"time"
)

func BenchmarkSpecScheduleNext(b *testing.B) {
	sched, err := Parse("0 30 9 * * MON-FRI")
	if err != nil {
		b.Fatal(err)
	}
	ss := sched.(*specSchedule)
	ss.loc = time.UTC
	from := time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC)

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		ss.Next(from)
	}
}

func BenchmarkSpecScheduleNextComplex(b *testing.B) {
	sched, err := Parse("*/5 */15 9-17 1,15 * MON-FRI")
	if err != nil {
		b.Fatal(err)
	}
	ss := sched.(*specSchedule)
	ss.loc = time.UTC
	from := time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC)

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		ss.Next(from)
	}
}

func BenchmarkConstantDelayScheduleNext(b *testing.B) {
	sched := constantDelaySchedule{Delay: 5 * time.Minute}
	from := time.Date(2026, 6, 18, 10, 32, 0, 0, time.UTC)

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		sched.Next(from)
	}
}

func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		_, err := Parse("0 30 9 * * MON-FRI")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseWithDescriptor(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		_, err := Parse("@hourly")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseWithTimezone(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		_, err := Parse("TZ=UTC 0 30 9 * * MON-FRI")
		if err != nil {
			b.Fatal(err)
		}
	}
}
