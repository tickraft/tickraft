// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details

package telemetry

import (
	"context"
	"testing"

	"github.com/tickraft/tickraft/pkg/db"
)

// openSummaryDB opens an in-memory SQLite database with the monitor point
// table migrated and returns the MonitorStore under test.
func openSummaryDB(t *testing.T) *MonitorStore {
	t.Helper()
	dbc, err := db.Open(context.Background(), db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := dbc.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("migrate monitor points: %v", err)
	}
	return NewMonitorStore(dbc)
}

// seedSummaryPoint inserts a monitor point row with the given mode and
// enabled state.
func seedSummaryPoint(t *testing.T, store *MonitorStore, mode Mode, enabled bool) {
	t.Helper()
	point := &MonitorPoint{
		Name:     "point",
		Mode:     mode,
		Type:     "icmp",
		Schedule: "60s",
		Enabled:  enabled,
	}
	if err := store.Create(context.Background(), point); err != nil {
		t.Fatalf("seed monitor point: %v", err)
	}
}

// TestMonitorStoreSummary verifies the grouped mode/enabled counting over
// the full dataset, including the empty-store zero case.
func TestMonitorStoreSummary(t *testing.T) {
	store := openSummaryDB(t)
	ctx := context.Background()

	t.Run("empty store returns zeros", func(t *testing.T) {
		summary, err := store.Summary(ctx)
		if err != nil {
			t.Fatalf("Summary failed: %v", err)
		}
		if summary != (PointSummary{}) {
			t.Errorf("Summary = %+v, want zero value", summary)
		}
	})

	t.Run("counts modes and enabled states independently", func(t *testing.T) {
		seedSummaryPoint(t, store, ModeActive, true)
		seedSummaryPoint(t, store, ModeActive, false)
		seedSummaryPoint(t, store, ModeActive, true)
		seedSummaryPoint(t, store, ModePassive, true)

		summary, err := store.Summary(ctx)
		if err != nil {
			t.Fatalf("Summary failed: %v", err)
		}
		want := PointSummary{Active: 3, Passive: 1, Enabled: 3, Disabled: 1}
		if summary != want {
			t.Errorf("Summary = %+v, want %+v", summary, want)
		}
	})
}
