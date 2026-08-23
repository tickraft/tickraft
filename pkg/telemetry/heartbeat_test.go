// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/db"
)

// fakeCollector records RegisterAsset/UnregisterAsset calls so tests can
// assert the reconciliation outcome without a live Engine.
type fakeCollector struct {
	registered   map[int64]int
	unregistered map[int64]int
	lastConfig   Config
	err          error
}

func newFakeCollector() *fakeCollector {
	return &fakeCollector{
		registered:   make(map[int64]int),
		unregistered: make(map[int64]int),
	}
}

func (f *fakeCollector) Start(context.Context) error { return nil }
func (f *fakeCollector) Stop(context.Context) error  { return nil }
func (f *fakeCollector) Submit(*Telemetry)           {}
func (f *fakeCollector) RegisterAsset(_ context.Context, c Config) error {
	f.registered[c.AssetID]++
	f.lastConfig = c
	return f.err
}

func (f *fakeCollector) UnregisterAsset(_ context.Context, assetID int64) error {
	f.unregistered[assetID]++
	return f.err
}

func TestHeartbeatTimeout(t *testing.T) {
	tests := []struct {
		name  string
		point MonitorPoint
		want  time.Duration
	}{
		{
			name:  "config number override wins",
			point: MonitorPoint{Interval: 60, Config: map[string]any{"heartbeat_timeout": float64(120)}},
			want:  120 * time.Second,
		},
		{
			name:  "config numeric string override",
			point: MonitorPoint{Interval: 60, Config: map[string]any{"heartbeat_timeout": "45"}},
			want:  45 * time.Second,
		},
		{
			name:  "non-numeric config falls through to interval",
			point: MonitorPoint{Interval: 60, Config: map[string]any{"heartbeat_timeout": "soon"}},
			want:  180 * time.Second,
		},
		{
			name:  "zero config value falls through to interval",
			point: MonitorPoint{Interval: 30, Config: map[string]any{"heartbeat_timeout": float64(0)}},
			want:  90 * time.Second,
		},
		{
			name:  "no config uses three intervals",
			point: MonitorPoint{Interval: 60},
			want:  180 * time.Second,
		},
		{
			name:  "no interval uses default",
			point: MonitorPoint{},
			want:  DefaultHeartbeatTimeout,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HeartbeatTimeout(tt.point); got != tt.want {
				t.Fatalf("HeartbeatTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

// openHeartbeatDB opens an in-memory SQLite database with the monitor
// point table migrated.
func openHeartbeatDB(t *testing.T) *MonitorStore {
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

// seedPassive inserts a passive webhook monitor point carrying the fields
// of the given point (asset binding, interval, enabled, config).
func seedPassive(t *testing.T, store *MonitorStore, point MonitorPoint) {
	t.Helper()
	point.Name = "passive"
	point.Mode = ModePassive
	point.Type = "webhook"
	if err := store.Create(context.Background(), &point); err != nil {
		t.Fatalf("seed passive point: %v", err)
	}
}

func TestSyncAssetObservationRegistersMaxTimeout(t *testing.T) {
	store := openHeartbeatDB(t)
	ctx := context.Background()
	seedPassive(t, store, MonitorPoint{AssetID: 7, Enabled: true, Interval: 30})
	seedPassive(t, store, MonitorPoint{AssetID: 7, Enabled: true, Interval: 60})

	coll := newFakeCollector()
	if err := SyncAssetObservation(ctx, store, coll, nil, 7); err != nil {
		t.Fatalf("SyncAssetObservation: %v", err)
	}
	if coll.registered[7] != 1 {
		t.Fatalf("registered[7] = %d, want 1", coll.registered[7])
	}
	if want := 180; coll.lastConfig.Timeout != want { // max(3*30s, 3*60s)
		t.Fatalf("timeout = %d, want %d", coll.lastConfig.Timeout, want)
	}
}

func TestSyncAssetObservationDisabledOnlyUnregisters(t *testing.T) {
	store := openHeartbeatDB(t)
	ctx := context.Background()
	seedPassive(t, store, MonitorPoint{AssetID: 7, Enabled: false, Interval: 60})

	coll := newFakeCollector()
	if err := SyncAssetObservation(ctx, store, coll, nil, 7); err != nil {
		t.Fatalf("SyncAssetObservation: %v", err)
	}
	if len(coll.registered) != 0 {
		t.Fatalf("unexpected registrations: %v", coll.registered)
	}
	if coll.unregistered[7] != 1 {
		t.Fatalf("unregistered[7] = %d, want 1", coll.unregistered[7])
	}
}

func TestSyncAssetObservationNoPassivePointsUnregisters(t *testing.T) {
	store := openHeartbeatDB(t)
	ctx := context.Background()
	seedPassive(t, store, MonitorPoint{AssetID: 8, Enabled: true, Interval: 60})

	coll := newFakeCollector()
	if err := SyncAssetObservation(ctx, store, coll, nil, 7); err != nil {
		t.Fatalf("SyncAssetObservation: %v", err)
	}
	if len(coll.registered) != 0 {
		t.Fatalf("unexpected registrations: %v", coll.registered)
	}
	if coll.unregistered[7] != 1 {
		t.Fatalf("unregistered[7] = %d, want 1", coll.unregistered[7])
	}
}

func TestSyncAssetObservationNilInputsNoop(t *testing.T) {
	store := openHeartbeatDB(t)
	ctx := context.Background()
	seedPassive(t, store, MonitorPoint{AssetID: 7, Enabled: true, Interval: 60})

	if err := SyncAssetObservation(ctx, nil, newFakeCollector(), nil, 7); err != nil {
		t.Fatalf("nil store: %v", err)
	}
	if err := SyncAssetObservation(ctx, store, nil, nil, 7); err != nil {
		t.Fatalf("nil collector: %v", err)
	}
	if err := SyncAssetObservation(ctx, store, newFakeCollector(), nil, 0); err != nil {
		t.Fatalf("zero asset id: %v", err)
	}
}
