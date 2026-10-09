// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/db"
)

// setupRecordStore opens an in-memory SQLite database and migrates the
// record table, returning the store and a cleanup function.
func setupRecordStore(t *testing.T) (store RecordStore, cleanup func()) {
	t.Helper()
	ctx := context.Background()
	gdb, err := db.Open(ctx, db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := Migrate(ctx, gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cleanup = func() {
		if sqlDB, e := gdb.DB(); e == nil {
			_ = sqlDB.Close()
		}
	}
	return NewRecordStore(gdb), cleanup
}

// TestRecordAlertStampsEventID pins the EventID correlation contract:
// every record persisted from one dispatch carries the event's EventID so
// inbound card callbacks can locate them.
func TestRecordAlertStampsEventID(t *testing.T) {
	store, cleanup := setupRecordStore(t)
	defer cleanup()
	ctx := context.Background()

	evt := Event{
		Type:      TypeMetric,
		EventID:   "evt-abc",
		Timestamp: time.Now(),
		Violations: []Violation{
			{Kind: "metric", Severity: "critical", Metric: &MetricContext{Name: "cpu_usage", Value: 95.5}},
			{Kind: "metric", Severity: "warning", Metric: &MetricContext{Name: "mem_usage", Value: 91}},
		},
	}
	records, err := RecordAlert(ctx, store, evt)
	if err != nil {
		t.Fatalf("record alert: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records: got %d, want 2", len(records))
	}
	for _, r := range records {
		if r.EventID != "evt-abc" {
			t.Errorf("record %d EventID: got %q, want evt-abc", r.ID, r.EventID)
		}
	}
}

// TestFindByEventID pins the lookup side of the correlation contract:
// records sharing one event ID are returned together, and unrelated
// event IDs do not leak in.
func TestFindByEventID(t *testing.T) {
	store, cleanup := setupRecordStore(t)
	defer cleanup()
	ctx := context.Background()

	evt := Event{
		Type:      TypeMetric,
		EventID:   "evt-1",
		Timestamp: time.Now(),
		Violations: []Violation{
			{Kind: "metric", Metric: &MetricContext{Name: "cpu_usage", Value: 95.5}},
			{Kind: "metric", Metric: &MetricContext{Name: "mem_usage", Value: 91}},
		},
	}
	if _, err := RecordAlert(ctx, store, evt); err != nil {
		t.Fatalf("record alert: %v", err)
	}
	other := evt
	other.EventID = "evt-2"
	other.Violations = evt.Violations[:1]
	if _, err := RecordAlert(ctx, store, other); err != nil {
		t.Fatalf("record other alert: %v", err)
	}

	found, err := store.FindByEventID(ctx, "evt-1")
	if err != nil {
		t.Fatalf("find by event: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("records for evt-1: got %d, want 2", len(found))
	}
	for _, r := range found {
		if r.EventID != "evt-1" {
			t.Errorf("leaked record from %q", r.EventID)
		}
	}

	// Unknown and empty event IDs return no rows without error.
	if got, err := store.FindByEventID(ctx, "evt-missing"); err != nil || len(got) != 0 {
		t.Errorf("missing event: got (%d, %v), want (0, nil)", len(got), err)
	}
	if got, err := store.FindByEventID(ctx, ""); err != nil || len(got) != 0 {
		t.Errorf("empty event: got (%d, %v), want (0, nil)", len(got), err)
	}
}
