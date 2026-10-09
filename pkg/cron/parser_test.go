// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cron

import (
	"context"
	"testing"
	"time"
)

func TestParseAndNext(t *testing.T) {
	cases := []struct {
		expr string
		from string
		next string
	}{
		{"0 0 12 * * *", "2026-06-18T11:59:55Z", "2026-06-18T12:00:00Z"},
		{"*/15 * * * * *", "2026-06-18T12:00:00Z", "2026-06-18T12:00:15Z"},
		{"0 30 9 * * MON-FRI", "2026-06-18T09:00:00Z", "2026-06-18T09:30:00Z"},
		{"0 0 0 1 JAN *", "2026-12-31T23:59:59Z", "2027-01-01T00:00:00Z"},
		{"0 0 0 * * SUN", "2026-06-13T23:59:59Z", "2026-06-14T00:00:00Z"},
	}

	for _, c := range cases {
		sched, err := Parse(c.expr)
		if err != nil {
			t.Fatalf("parse(%q) failed: %v", c.expr, err)
		}
		// Use UTC so test expectations work regardless of system timezone.
		sched.(*specSchedule).loc = time.UTC
		from, _ := time.Parse(time.RFC3339, c.from)
		next := sched.Next(from)
		if next.UTC().Format(time.RFC3339) != c.next {
			t.Fatalf("expected %s, got %s for expr %s", c.next, next.UTC().Format(time.RFC3339), c.expr)
		}
	}
}

func TestParseInvalid(t *testing.T) {
	invalid := []string{
		"",
		"* * * *",
		"61 * * * * *",
		"*/0 * * * * *",
		"abc * * * * *",
	}
	for _, expr := range invalid {
		if _, err := Parse(expr); err == nil {
			t.Fatalf("expected invalid expr %q", expr)
		}
	}
}

func TestDescriptorParsing(t *testing.T) {
	cases := []struct {
		expr string
		from string
		next string
	}{
		// @hourly: equivalent to 0 0 * * * *
		{"@hourly", "2026-06-18T10:30:00Z", "2026-06-18T11:00:00Z"},
		// @daily: equivalent to 0 0 0 * * *
		{"@daily", "2026-06-18T10:30:00Z", "2026-06-19T00:00:00Z"},
		// @midnight: same as @daily
		{"@midnight", "2026-06-18T10:30:00Z", "2026-06-19T00:00:00Z"},
		// @weekly: equivalent to 0 0 0 * * 0
		// 2026-06-18 is Thursday, next Sunday is 2026-06-21
		{"@weekly", "2026-06-18T10:30:00Z", "2026-06-21T00:00:00Z"},
		// @monthly: equivalent to 0 0 0 1 * *
		{"@monthly", "2026-06-18T10:30:00Z", "2026-07-01T00:00:00Z"},
		// @yearly: equivalent to 0 0 0 1 1 *
		{"@yearly", "2026-06-18T10:30:00Z", "2027-01-01T00:00:00Z"},
		// @annually: same as @yearly
		{"@annually", "2026-06-18T10:30:00Z", "2027-01-01T00:00:00Z"},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			sched, err := Parse(c.expr)
			if err != nil {
				t.Fatalf("Parse(%q) failed: %v", c.expr, err)
			}
			ss := sched.(*specSchedule)
			ss.loc = time.UTC
			from, _ := time.Parse(time.RFC3339, c.from)
			next := sched.Next(from)
			got := next.UTC().Format(time.RFC3339)
			if got != c.next {
				t.Fatalf("Parse(%q): expected next=%s, got=%s", c.expr, c.next, got)
			}
		})
	}
}

func TestEveryDescriptor(t *testing.T) {
	cases := []struct {
		expr string
		from string
		next string
	}{
		{"@every 1h", "2026-06-18T10:30:00Z", "2026-06-18T11:00:00Z"},
		{"@every 30m", "2026-06-18T10:30:00Z", "2026-06-18T11:00:00Z"},
		{"@every 1s", "2026-06-18T10:30:00Z", "2026-06-18T10:30:01Z"},
		{"@every 500ms", "2026-06-18T10:30:00Z", "2026-06-18T10:30:01Z"}, // rounds up to 1s
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			sched, err := Parse(c.expr)
			if err != nil {
				t.Fatalf("Parse(%q) failed: %v", c.expr, err)
			}
			from, _ := time.Parse(time.RFC3339, c.from)
			next := sched.Next(from)
			got := next.UTC().Format(time.RFC3339)
			if got != c.next {
				t.Fatalf("Parse(%q): expected next=%s, got=%s", c.expr, c.next, got)
			}
		})
	}
}

func TestTimezonePrefix(t *testing.T) {
	// Test that TZ= prefix is parsed correctly
	sched, err := Parse("TZ=UTC 0 0 9 * * MON-FRI")
	if err != nil {
		t.Fatalf("Parse with TZ= failed: %v", err)
	}
	ss := sched.(*specSchedule)
	if ss.loc == nil || ss.loc.String() != "UTC" {
		t.Fatalf("expected UTC location, got %v", ss.loc)
	}

	// Test CRON_TZ= prefix
	sched2, err := Parse("CRON_TZ=UTC 0 0 9 * * *")
	if err != nil {
		t.Fatalf("Parse with CRON_TZ= failed: %v", err)
	}
	ss2 := sched2.(*specSchedule)
	if ss2.loc == nil || ss2.loc.String() != "UTC" {
		t.Fatalf("expected UTC location, got %v", ss2.loc)
	}

	// Test invalid timezone
	_, err = Parse("TZ=Invalid/Timezone 0 0 9 * * *")
	if err == nil {
		t.Fatal("expected error for invalid timezone")
	}
}

func TestParseOption(t *testing.T) {
	// Standard 5-field parser (no seconds)
	stdParser, err := NewParser(Minute | Hour | Dom | Month | Dow)
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}
	sched, err := stdParser.Parse("30 9 * * MON-FRI")
	if err != nil {
		t.Fatalf("standard parser failed: %v", err)
	}
	ss := sched.(*specSchedule)
	ss.loc = time.UTC
	from, _ := time.Parse(time.RFC3339, "2026-06-18T09:00:00Z")
	next := sched.Next(from)
	if next.UTC().Format(time.RFC3339) != "2026-06-18T09:30:00Z" {
		t.Fatalf("expected 2026-06-18T09:30:00Z, got %s", next.UTC().Format(time.RFC3339))
	}

	// 5-field parser should reject 6-field expressions
	_, err = stdParser.Parse("0 30 9 * * MON-FRI")
	if err == nil {
		t.Fatal("expected error for 6-field expression with 5-field parser")
	}

	// Parser without Descriptor should reject @
	noDescParser, err := NewParser(SecondOptional | Minute | Hour | Dom | Month | Dow)
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}
	_, err = noDescParser.Parse("@hourly")
	if err == nil {
		t.Fatal("expected error for descriptor without Descriptor option")
	}

	// Parser with Second required needs exactly 6 fields
	secParser, err := NewParser(Second | Minute | Hour | Dom | Month | Dow)
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}
	_, err = secParser.Parse("30 9 * * MON-FRI") // 5 fields, should fail
	if err == nil {
		t.Fatal("expected error for 5-field expression with Second-required parser")
	}
	sched2, err := secParser.Parse("0 30 9 * * MON-FRI") // 6 fields, should work
	if err != nil {
		t.Fatalf("6-field parse with Second parser failed: %v", err)
	}
	ss2 := sched2.(*specSchedule)
	ss2.loc = time.UTC
	next2 := sched2.Next(from)
	if next2.UTC().Format(time.RFC3339) != "2026-06-18T09:30:00Z" {
		t.Fatalf("expected 2026-06-18T09:30:00Z, got %s", next2.UTC().Format(time.RFC3339))
	}
}

func TestQuestionMarkValidation(t *testing.T) {
	// ? in seconds field - should fail
	_, err := Parse("? * * * * *")
	if err == nil {
		t.Fatal("expected error for ? in seconds field")
	}

	// ? in minutes field - should fail
	_, err = Parse("* ? * * * *")
	if err == nil {
		t.Fatal("expected error for ? in minutes field")
	}

	// ? in hours field - should fail
	_, err = Parse("* * ? * * *")
	if err == nil {
		t.Fatal("expected error for ? in hours field")
	}

	// ? in month field - should fail
	_, err = Parse("* * * * ? *")
	if err == nil {
		t.Fatal("expected error for ? in month field")
	}

	// Both DOM and DOW are ? - should fail
	_, err = Parse("* * * ? * ?")
	if err == nil {
		t.Fatal("expected error for both DOM and DOW being ?")
	}

	// ? in DOM only - should work
	_, err = Parse("* * * ? * *")
	if err != nil {
		t.Fatalf("expected ? in DOM to work, got: %v", err)
	}

	// ? in DOW only - should work
	_, err = Parse("* * * * * ?")
	if err != nil {
		t.Fatalf("expected ? in DOW to work, got: %v", err)
	}
}

func TestUnsupportedSyntax(t *testing.T) {
	unsupported := []string{
		"0 0 0 L * *",
		"0 0 0 1W * *",
		"0 0 0 * * 1#2",
		"0 0 0 l * *", // lowercase L
	}
	for _, expr := range unsupported {
		_, err := Parse(expr)
		if err == nil {
			t.Fatalf("expected error for unsupported syntax in %q", expr)
		}
	}
}

func TestInvalidDescriptors(t *testing.T) {
	cases := []string{
		"@every",     // missing duration
		"@every abc", // invalid duration
		"@invalid",   // unrecognized descriptor
	}
	for _, expr := range cases {
		_, err := Parse(expr)
		if err == nil {
			t.Fatalf("expected error for descriptor %q", expr)
		}
	}
}

func TestRebootDescriptor(t *testing.T) {
	// @reboot fires once immediately at startup and never again.
	// It is case-insensitive.
	cases := []string{
		"@reboot",
		"@REBOOT",
		"@Reboot",
		"@ReBoOt",
	}
	for _, expr := range cases {
		t.Run(expr, func(t *testing.T) {
			sched, err := Parse(expr)
			if err != nil {
				t.Fatalf("Parse(%q) failed: %v", expr, err)
			}
			now := time.Now()
			next := sched.Next(now)
			// immediateSchedule returns the zero time, which the cron
			// manager treats as "immediately due" and removes the entry
			// after a single execution.
			if !next.IsZero() {
				t.Fatalf("Parse(%q): expected zero time, got %v", expr, next)
			}
		})
	}
}

func TestRebootFiresOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	crontab := New(WithContext(ctx))
	defer crontab.Stop(ctx)

	called := make(chan struct{}, 4)
	sched, err := Parse("@reboot")
	if err != nil {
		t.Fatal(err)
	}
	if err := crontab.Add(1, sched, Lambda(func(_ context.Context) {
		select {
		case called <- struct{}{}:
		default:
		}
	})); err != nil {
		t.Fatal(err)
	}

	// Should fire exactly once.
	select {
	case <-called:
	case <-time.After(3 * time.Second):
		t.Fatal("@reboot job did not run")
	}

	// Should NOT fire again.
	select {
	case <-called:
		t.Fatal("@reboot job fired more than once")
	case <-time.After(time.Second):
		// Good: it did not fire again.
	}
}

func TestSingleValueField(t *testing.T) {
	// "0 5 * * * *" means second=0, minute=5 only, every hour
	sched, err := Parse("0 5 * * * *")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	ss := sched.(*specSchedule)
	ss.loc = time.UTC

	// From 10:04:59, next should be 10:05:00
	from1, _ := time.Parse(time.RFC3339, "2026-06-18T10:04:59Z")
	next1 := sched.Next(from1)
	if got := next1.UTC().Format(time.RFC3339); got != "2026-06-18T10:05:00Z" {
		t.Fatalf("from 10:04:59: expected 2026-06-18T10:05:00Z, got %s", got)
	}

	// From 10:05:01, next should be 11:05:00 (not 10:06:00 or any other minute)
	from2, _ := time.Parse(time.RFC3339, "2026-06-18T10:05:01Z")
	next2 := sched.Next(from2)
	if got := next2.UTC().Format(time.RFC3339); got != "2026-06-18T11:05:00Z" {
		t.Fatalf("from 10:05:01: expected 2026-06-18T11:05:00Z, got %s", got)
	}

	// Verify step syntax still works: "0 5/10 * * * *" = minutes 5,15,25,35,45,55
	sched2, err := Parse("0 5/10 * * * *")
	if err != nil {
		t.Fatalf("Parse 5/10 failed: %v", err)
	}
	ss2 := sched2.(*specSchedule)
	ss2.loc = time.UTC

	from3, _ := time.Parse(time.RFC3339, "2026-06-18T10:04:59Z")
	next3 := sched2.Next(from3)
	if got := next3.UTC().Format(time.RFC3339); got != "2026-06-18T10:05:00Z" {
		t.Fatalf("5/10 from 10:04:59: expected 10:05:00Z, got %s", got)
	}

	from4, _ := time.Parse(time.RFC3339, "2026-06-18T10:05:01Z")
	next4 := sched2.Next(from4)
	if got := next4.UTC().Format(time.RFC3339); got != "2026-06-18T10:15:00Z" {
		t.Fatalf("5/10 from 10:05:01: expected 10:15:00Z, got %s", got)
	}
}

func TestManagerAddRemove(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	crontab := New(WithContext(ctx))

	called := make(chan struct{}, 1)
	sched, err := Parse("0/2 * * * * *")
	if err != nil {
		t.Fatal(err)
	}
	_ = crontab.Add(1, sched, Lambda(func(_ context.Context) {
		select {
		case called <- struct{}{}:
		default:
		}
	}))

	select {
	case <-called:
	case <-time.After(3 * time.Second):
		t.Fatal("job did not run")
	}

	crontab.Remove(1)
}
