// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/event"
)

// triggerCollector records every ExecutionTriggered payload published on
// the engine's bus. Bus delivery is asynchronous (per-type consumer
// goroutine), so assertions wait on a channel instead of counting.
type triggerCollector struct {
	events chan event.ExecutionPayload
}

func newTriggerCollector(m *Engine) *triggerCollector {
	c := &triggerCollector{events: make(chan event.ExecutionPayload, 64)}
	_, _ = event.Subscribe[event.ExecutionPayload](m.bus, event.TypeExecutionTriggered,
		func(_ context.Context, ev event.Event[event.ExecutionPayload]) error {
			c.events <- ev.Payload
			return nil
		})
	return c
}

// await returns the next n trigger payloads, failing the test on timeout.
func (c *triggerCollector) await(t *testing.T, n int) []event.ExecutionPayload {
	t.Helper()
	got := make([]event.ExecutionPayload, 0, n)
	for len(got) < n {
		select {
		case ev := <-c.events:
			got = append(got, ev)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for trigger events: got %d of %d", len(got), n)
		}
	}
	return got
}

// assertEmpty fails if any further event arrives within the window.
func (c *triggerCollector) assertEmpty(t *testing.T, wait time.Duration) {
	t.Helper()
	select {
	case ev := <-c.events:
		t.Fatalf("unexpected trigger event %+v", ev)
	case <-time.After(wait):
	}
}

// assertAllCatchup fails unless every collected payload carries the
// catchup trigger annotation.
func assertAllCatchup(t *testing.T, events []event.ExecutionPayload) {
	t.Helper()
	for i := range events {
		if events[i].TriggerType != string(TriggerTypeCatchup) {
			t.Fatalf("event %d trigger type = %q, want catchup", i, events[i].TriggerType)
		}
	}
}

// TestMissedSlotsIntervalDivision verifies the arithmetic slot enumeration
// for interval schedules: slots at watermark+k*interval, the replay cap
// keeping the latest slots, and the dropped count reporting the overflow.
func TestMissedSlotsIntervalDivision(t *testing.T) {
	watermark := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	now := watermark.Add(90 * time.Minute)

	slots, dropped := missedSlots("30m", ScheduleTypeInterval, 30*time.Minute, watermark, now)
	if len(slots) != 3 || dropped != 0 {
		t.Fatalf("slots = %v (dropped %d), want 3 slots, 0 dropped", slots, dropped)
	}
	for i, want := range []time.Duration{30 * time.Minute, 60 * time.Minute, 90 * time.Minute} {
		if !slots[i].Equal(watermark.Add(want)) {
			t.Fatalf("slot %d = %v, want %v", i, slots[i], watermark.Add(want))
		}
	}

	// 25 missed hourly slots: cap keeps the latest 10, drops 15.
	now = watermark.Add(25 * time.Hour)
	slots, dropped = missedSlots("1h", ScheduleTypeInterval, time.Hour, watermark, now)
	if len(slots) != 10 || dropped != 15 {
		t.Fatalf("cap: slots = %d (dropped %d), want 10 slots, 15 dropped", len(slots), dropped)
	}
	if !slots[0].Equal(watermark.Add(16 * time.Hour)) {
		t.Fatalf("first capped slot = %v, want watermark+16h", slots[0])
	}

	// No elapsed full interval: nothing missed.
	slots, dropped = missedSlots("1h", ScheduleTypeInterval, time.Hour, watermark, watermark.Add(30*time.Minute))
	if slots != nil || dropped != 0 {
		t.Fatalf("partial interval: slots = %v (dropped %d), want none", slots, dropped)
	}

	// Watermark already current or in the future: nothing missed.
	slots, dropped = missedSlots("1h", ScheduleTypeInterval, time.Hour, now, watermark)
	if slots != nil || dropped != 0 {
		t.Fatalf("future watermark: slots = %v (dropped %d), want none", slots, dropped)
	}
}

// TestMissedSlotsCronEnumeration verifies cron slot enumeration through
// the cron package's Next() semantics, and that once/event schedules have
// no replayable slots.
func TestMissedSlotsCronEnumeration(t *testing.T) {
	// A cron firing at :00 and :30 of every hour.
	watermark := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	now := time.Date(2026, 9, 1, 2, 0, 0, 0, time.Local)

	slots, dropped := missedSlots("0,30 * * * *", ScheduleTypeCron, 0, watermark, now)
	if len(slots) != 4 || dropped != 0 {
		t.Fatalf("slots = %v (dropped %d), want 4 slots [00:30 01:00 01:30 02:00]", slots, dropped)
	}

	if slots, dropped = missedSlots("", ScheduleTypeEvent, 0, watermark, now); slots != nil || dropped != 0 {
		t.Fatalf("event schedule: slots = %v (dropped %d), want none", slots, dropped)
	}
}

// TestInSleepWindow covers window matching: same-day, cross-midnight,
// full-day, weekday masks, and misses.
func TestInSleepWindow(t *testing.T) {
	nightly := SleepWindow{Start: "00:00", End: "06:00", Days: []int{0, 1, 2, 3, 4, 5, 6}}
	late := SleepWindow{Start: "22:00", End: "06:00", Days: []int{5}} // Friday 22:00 → Saturday 06:00
	fullday := SleepWindow{Start: "00:00", End: "24:00", Days: []int{0}}
	weekday := SleepWindow{Start: "09:00", End: "17:00", Days: []int{1, 2, 3, 4, 5}}
	malformed := SleepWindow{Start: "bad", End: "06:00", Days: []int{1}} // parse failure → window skipped
	nodays := SleepWindow{Start: "00:00", End: "06:00"}                  // no weekday mask → never matches

	cases := []struct {
		name    string
		windows []SleepWindow
		at      time.Time
		want    bool
	}{
		{"nightly 03:00", []SleepWindow{nightly}, time.Date(2026, 9, 9, 3, 0, 0, 0, time.Local), true},
		{"nightly 06:00 exclusive end", []SleepWindow{nightly}, time.Date(2026, 9, 9, 6, 0, 0, 0, time.Local), false},
		{"nightly 23:59", []SleepWindow{nightly}, time.Date(2026, 9, 9, 23, 59, 0, 0, time.Local), false},
		{"cross-midnight friday 23:00", []SleepWindow{late}, time.Date(2026, 9, 11, 23, 0, 0, 0, time.Local), true},
		{"cross-midnight saturday 05:00", []SleepWindow{late}, time.Date(2026, 9, 12, 5, 0, 0, 0, time.Local), true},
		{"cross-midnight sat 06:00 end", []SleepWindow{late}, time.Date(2026, 9, 12, 6, 0, 0, 0, time.Local), false},
		{"cross-midnight wednesday 23:00", []SleepWindow{late}, time.Date(2026, 9, 9, 23, 0, 0, 0, time.Local), false},
		{"full day sunday", []SleepWindow{fullday}, time.Date(2026, 9, 13, 12, 0, 0, 0, time.Local), true},
		{"full day monday", []SleepWindow{fullday}, time.Date(2026, 9, 14, 12, 0, 0, 0, time.Local), false},
		{"weekday 09:00 start", []SleepWindow{weekday}, time.Date(2026, 9, 9, 9, 0, 0, 0, time.Local), true},
		{"weekday 17:00 end", []SleepWindow{weekday}, time.Date(2026, 9, 9, 17, 0, 0, 0, time.Local), false},
		{"weekday saturday", []SleepWindow{weekday}, time.Date(2026, 9, 12, 12, 0, 0, 0, time.Local), false},
		{"malformed window skipped", []SleepWindow{malformed}, time.Date(2026, 9, 9, 3, 0, 0, 0, time.Local), false},
		{"no days skipped", []SleepWindow{nodays}, time.Date(2026, 9, 9, 3, 0, 0, 0, time.Local), false},
	}
	for _, tc := range cases {
		if got := InSleepWindow(tc.windows, tc.at); got != tc.want {
			t.Fatalf("%s: InSleepWindow = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestValidateCatchupConfig covers the request validation domain.
func TestValidateCatchupConfig(t *testing.T) {
	ok := []struct {
		name    string
		policy  string
		windows []SleepWindow
	}{
		{"absent policy", "", nil},
		{"skip", CatchupPolicySkip, nil},
		{"once", CatchupPolicyOnce, nil},
		{"all", CatchupPolicyAll, nil},
		{"valid window", CatchupPolicyAll, []SleepWindow{{Start: "00:00", End: "06:00", Days: []int{0, 6}}}},
		{"cross midnight", "all", []SleepWindow{{Start: "22:00", End: "06:00", Days: []int{5}}}},
		{"full day", "all", []SleepWindow{{Start: "00:00", End: "24:00", Days: []int{0}}}},
	}
	for _, tc := range ok {
		if err := ValidateCatchupConfig(tc.policy, tc.windows); err != nil {
			t.Fatalf("%s: unexpected error %v", tc.name, err)
		}
	}
	bad := []struct {
		name    string
		policy  string
		windows []SleepWindow
	}{
		{"bad policy", "sometimes", nil},
		{"bad start", "all", []SleepWindow{{Start: "0:00", End: "06:00", Days: []int{1}}}},
		{"bad end", "all", []SleepWindow{{Start: "00:00", End: "24:01", Days: []int{1}}}},
		{"start equals end", "all", []SleepWindow{{Start: "06:00", End: "06:00", Days: []int{1}}}},
		{"no days", "all", []SleepWindow{{Start: "00:00", End: "06:00"}}},
		{"day out of range", "all", []SleepWindow{{Start: "00:00", End: "06:00", Days: []int{7}}}},
		{"negative day", "all", []SleepWindow{{Start: "00:00", End: "06:00", Days: []int{-1}}}},
	}
	for _, tc := range bad {
		if err := ValidateCatchupConfig(tc.policy, tc.windows); err == nil {
			t.Fatalf("%s: expected error, got nil", tc.name)
		}
	}
	windows := make([]SleepWindow, 0, sleepWindowMaxCount+1)
	for i := range sleepWindowMaxCount + 1 {
		windows = append(windows, SleepWindow{Start: "01:00", End: "02:00", Days: []int{i % 7}})
	}
	if err := ValidateCatchupConfig("all", windows); err == nil {
		t.Fatal("window count over cap: expected error")
	}
}

// TestRunCatchupPolicies drives the recovery pipeline for the three
// policies against a stale watermark and asserts dispatch counts, the
// catchup annotation, and that a second recovery over the advanced
// watermark replays nothing.
func TestRunCatchupPolicies(t *testing.T) {
	for _, tc := range []struct {
		policy string
		wantN  int
	}{
		{CatchupPolicySkip, 0},
		{CatchupPolicyOnce, 1},
		{CatchupPolicyAll, 5},
	} {
		t.Run(tc.policy, func(t *testing.T) {
			m := newTestManager(t, nil)
			defer func() { _ = m.Stop(context.Background()) }()
			store := newMockStore()
			m.store = store

			watermark := time.Now().Add(-5 * time.Hour)
			task := Task{
				ID: 1, ExecutorType: "http", Schedule: "1h", Enabled: true,
				CatchupPolicy: tc.policy, LastScheduledAt: &watermark,
			}
			if err := store.Save(context.Background(), &task); err != nil {
				t.Fatalf("seed: %v", err)
			}
			m.setTask(task)

			c := newTriggerCollector(m)
			m.runCatchup(context.Background(), task, time.Now())

			if tc.wantN == 0 {
				c.assertEmpty(t, 300*time.Millisecond)
			} else {
				assertAllCatchup(t, c.await(t, tc.wantN))
				c.assertEmpty(t, 300*time.Millisecond)
			}

			// Watermark is now current: a second engine restoring over
			// the same store replays nothing.
			m2, err := NewEngine(WithLogger(zap.NewNop()), WithStore(store))
			if err != nil {
				t.Fatalf("second engine: %v", err)
			}
			defer func() { _ = m2.Stop(context.Background()) }()
			c2 := newTriggerCollector(m2)
			if err := m2.Restore(context.Background()); err != nil {
				t.Fatalf("restore: %v", err)
			}
			c2.assertEmpty(t, 300*time.Millisecond)

			got, err := store.Get(context.Background(), 1)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.LastScheduledAt == nil || got.LastScheduledAt.Before(watermark) {
				t.Fatalf("watermark = %v, want advanced past %v", got.LastScheduledAt, watermark)
			}
		})
	}
}

// TestRunCatchupFirstStartNoReplay pins the NULL-watermark semantics: a
// task that has never dispatched must not replay history on first start.
func TestRunCatchupFirstStartNoReplay(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	task := Task{ID: 1, ExecutorType: "http", Schedule: "1h", Enabled: true, CatchupPolicy: CatchupPolicyAll}
	m.setTask(task)
	c := newTriggerCollector(m)
	m.runCatchup(context.Background(), task, time.Now())
	c.assertEmpty(t, 300*time.Millisecond)
}

// TestRunCatchupCapDroppedSlots asserts the replay cap with a watermark 25
// hours behind on an hourly task under policy all.
func TestRunCatchupCapDroppedSlots(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	watermark := time.Now().Add(-25 * time.Hour)
	task := Task{
		ID: 1, ExecutorType: "http", Schedule: "1h", Enabled: true,
		CatchupPolicy: CatchupPolicyAll, LastScheduledAt: &watermark,
	}
	m.setTask(task)
	c := newTriggerCollector(m)
	m.runCatchup(context.Background(), task, time.Now())
	assertAllCatchup(t, c.await(t, catchupMaxSlots))
	c.assertEmpty(t, 300*time.Millisecond)
}

// TestSleepWindowSuppressionAdvancesWatermark pins the invariant that a
// suppressed slot counts as consumed: dispatch is skipped silently and the
// watermark still moves forward.
func TestSleepWindowSuppressionAdvancesWatermark(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()
	store := newMockStore()
	m.store = store

	watermark := time.Now().Add(-3 * time.Hour)
	// A window covering all of today suppresses every missed slot.
	task := Task{
		ID: 1, ExecutorType: "http", Schedule: "1h", Enabled: true,
		CatchupPolicy:   CatchupPolicyAll,
		LastScheduledAt: &watermark,
		SleepWindows:    []SleepWindow{{Start: "00:00", End: "24:00", Days: []int{0, 1, 2, 3, 4, 5, 6}}},
	}
	if err := store.Save(context.Background(), &task); err != nil {
		t.Fatalf("seed: %v", err)
	}
	m.setTask(task)

	c := newTriggerCollector(m)
	m.runCatchup(context.Background(), task, time.Now())
	c.assertEmpty(t, 300*time.Millisecond)

	got, err := store.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastScheduledAt == nil || !got.LastScheduledAt.After(watermark) {
		t.Fatalf("watermark = %v, want advanced past %v (window suppression must consume slots)",
			got.LastScheduledAt, watermark)
	}
}

// TestDispatchGatedWindowSuppressesRegularFire drives the shared dispatch
// gate directly: a wheel fire landing inside a window neither dispatches
// nor leaves the watermark stale.
func TestDispatchGatedWindowSuppressesRegularFire(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	now := time.Now()
	window := SleepWindow{Start: "00:00", End: "24:00", Days: []int{int(now.Weekday())}}
	task := Task{ID: 1, ExecutorType: "http", Enabled: true, SleepWindows: []SleepWindow{window}}
	m.setTask(task)

	c := newTriggerCollector(m)
	m.dispatchGated(context.Background(), task, now, TriggerTypeSchedule)
	c.assertEmpty(t, 300*time.Millisecond)

	got, err := m.getTask(1)
	if err != nil {
		t.Fatalf("getTask: %v", err)
	}
	if got.LastScheduledAt == nil {
		t.Fatal("suppressed fire did not advance the watermark")
	}
}

// TestAdvanceWatermarkMonotonic pins the no-regression contract.
func TestAdvanceWatermarkMonotonic(t *testing.T) {
	m := mustNewManager()
	early := time.Now().Add(-2 * time.Hour)
	late := time.Now().Add(-1 * time.Hour)
	m.setTask(Task{ID: 1, ExecutorType: "http", LastScheduledAt: &late})

	m.advanceWatermark(1, early)
	got, err := m.getTask(1)
	if err != nil {
		t.Fatalf("getTask: %v", err)
	}
	if !got.LastScheduledAt.Equal(late) {
		t.Fatalf("watermark regressed to %v, want %v", got.LastScheduledAt, late)
	}
	m.advanceWatermark(1, late.Add(time.Minute))
	got, _ = m.getTask(1)
	if got.LastScheduledAt.Before(late) {
		t.Fatalf("watermark did not advance: %v", got.LastScheduledAt)
	}
}

// TestManualTriggerDoesNotAdvanceWatermark pins that slot-unbound triggers
// leave the watermark untouched.
func TestManualTriggerDoesNotAdvanceWatermark(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	watermark := time.Now().Add(-3 * time.Hour)
	task := Task{ID: 1, ExecutorType: "http", Schedule: "1h", Enabled: true, LastScheduledAt: &watermark}
	if err := m.Register(context.Background(), task); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := m.Schedule(context.Background(), 1); err != nil {
		t.Fatalf("manual schedule: %v", err)
	}
	got, err := m.getTask(1)
	if err != nil {
		t.Fatalf("getTask: %v", err)
	}
	if !got.LastScheduledAt.Equal(watermark) {
		t.Fatalf("manual trigger moved the watermark: %v, want %v", got.LastScheduledAt, watermark)
	}
}

// TestResumeReplaysPerCatchupPolicy covers the pause/resume interplay: the
// watermark does not advance while paused, and Resume replays per policy.
func TestResumeReplaysPerCatchupPolicy(t *testing.T) {
	m := newTestManager(t, nil)
	defer func() { _ = m.Stop(context.Background()) }()

	watermark := time.Now().Add(-2 * time.Hour)
	task := Task{
		ID: 1, ExecutorType: "http", Schedule: "1h", Enabled: true,
		CatchupPolicy: CatchupPolicyAll, LastScheduledAt: &watermark,
	}
	if err := m.Register(context.Background(), task); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := m.Pause(1); err != nil {
		t.Fatalf("pause: %v", err)
	}

	c := newTriggerCollector(m)
	if err := m.Resume(1); err != nil {
		t.Fatalf("resume: %v", err)
	}
	assertAllCatchup(t, c.await(t, 2))
	c.assertEmpty(t, 300*time.Millisecond)
}

// TestRestoreCatchupEndToEnd is the spec's sqlite e2e: seed a persisted
// task with a stale watermark, Restore a fresh engine over the same real
// database, and assert the replay count, the catchup annotation, the
// persisted columns round-trip, and that a second Restore replays nothing.
// Recovery semantics must run against a real store — a mock-only Store
// cannot observe the persisted watermark path.
func TestRestoreCatchupEndToEnd(t *testing.T) {
	dbc := openTaskStoreDB(t)
	store := NewStore(dbc)

	watermark := time.Now().Add(-4 * time.Hour)
	seed := Task{
		ID: 100, TenantID: 1, Name: "catchup-e2e", ExecutorType: "http",
		Schedule: "1h", Enabled: true, CatchupPolicy: CatchupPolicyAll,
		LastScheduledAt: &watermark,
		// An empty-days window never matches: deterministic replay count
		// regardless of the wall-clock minute the test runs at.
		SleepWindows: []SleepWindow{{Start: "01:00", End: "02:00"}},
	}
	if err := store.Save(context.Background(), &seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Save never writes last_scheduled_at (AdvanceScheduleWatermark is its
	// only writer); seed the stale watermark exactly as a real engine
	// would have left it.
	if err := store.AdvanceScheduleWatermark(context.Background(), seed.ID, watermark); err != nil {
		t.Fatalf("seed watermark: %v", err)
	}

	m, err := NewEngine(WithLogger(zap.NewNop()), WithStore(store))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	defer func() { _ = m.Stop(context.Background()) }()

	c := newTriggerCollector(m)
	if err := m.Restore(context.Background()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	assertAllCatchup(t, c.await(t, 4))
	c.assertEmpty(t, 300*time.Millisecond)

	got, err := store.Get(context.Background(), 100)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastScheduledAt == nil || got.LastScheduledAt.Before(watermark) {
		t.Fatalf("persisted watermark = %v, want advanced past %v", got.LastScheduledAt, watermark)
	}
	if got.CatchupPolicy != CatchupPolicyAll {
		t.Fatalf("persisted catchup_policy = %q", got.CatchupPolicy)
	}
	if len(got.SleepWindows) != 1 || got.SleepWindows[0].Start != "01:00" {
		t.Fatalf("persisted sleep_windows = %+v", got.SleepWindows)
	}

	// A second Restore over the now-current watermark replays nothing.
	c2 := newTriggerCollector(m)
	if err := m.Restore(context.Background()); err != nil {
		t.Fatalf("second restore: %v", err)
	}
	c2.assertEmpty(t, 300*time.Millisecond)
}
