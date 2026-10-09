// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/types"
)

// flakyStore wraps mgrMockStore with an injectable UpdateStatus failure so
// tests can exercise the persist-before-cache ordering.
type flakyStore struct {
	*mgrMockStore
	failUpdate atomic.Bool
}

func (s *flakyStore) UpdateStatus(
	ctx context.Context, id int64, status types.AssetStatus, activeAt time.Time,
) error {
	if s.failUpdate.Load() {
		return errors.New("store down")
	}
	return s.mgrMockStore.UpdateStatus(ctx, id, status, activeAt)
}

// TestUpdateStatusPersistFailureKeepsCache verifies the cache is committed
// only after the store write succeeds: a failed write leaves the previous
// status in place and the next report retries the transition.
func TestUpdateStatusPersistFailureKeepsCache(t *testing.T) {
	store := &flakyStore{mgrMockStore: newMgrMockStore()}
	sm := newStateManager(store, nil, nil, zap.NewNop(), nil)
	ctx := context.Background()

	store.failUpdate.Store(true)
	if _, err := sm.UpdateStatus(ctx, 1, types.AssetStatusNormal, "first"); err == nil {
		t.Fatal("expected error on first assignment when store fails")
	}
	if got := sm.GetStatus(1); got != types.AssetStatusUnknown {
		t.Fatalf("cache after failed first assignment = %q, want unknown", got)
	}

	store.failUpdate.Store(false)
	changed, err := sm.UpdateStatus(ctx, 1, types.AssetStatusNormal, "first")
	if err != nil || !changed {
		t.Fatalf("retry first assignment: changed=%v err=%v", changed, err)
	}
	if got := sm.GetStatus(1); got != types.AssetStatusNormal {
		t.Fatalf("cache after first assignment = %q, want normal", got)
	}

	store.failUpdate.Store(true)
	if _, err := sm.UpdateStatus(ctx, 1, types.AssetStatusOffline, "timeout"); err == nil {
		t.Fatal("expected error on transition when store fails")
	}
	if got := sm.GetStatus(1); got != types.AssetStatusNormal {
		t.Fatalf("cache after failed transition = %q, want normal", got)
	}

	store.failUpdate.Store(false)
	changed, err = sm.UpdateStatus(ctx, 1, types.AssetStatusOffline, "timeout")
	if err != nil || !changed {
		t.Fatalf("retry transition: changed=%v err=%v", changed, err)
	}
	if got := sm.GetStatus(1); got != types.AssetStatusOffline {
		t.Fatalf("cache after transition = %q, want offline", got)
	}
}

// openStateDB opens an in-memory SQLite database with the status history
// table migrated.
func openStateDB(t *testing.T) *gorm.DB {
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
	if err := dbc.WithContext(context.Background()).AutoMigrate(&StatusHistory{}); err != nil {
		t.Fatalf("auto-migrate status history: %v", err)
	}
	return dbc
}

// TestUpdateStatusRecordsHistory verifies both the first assignment and
// later transitions persist a history row, with the first row carrying the
// unknown prev status.
func TestUpdateStatusRecordsHistory(t *testing.T) {
	dbc := openStateDB(t)
	store := newMgrMockStore()
	ctx := context.Background()
	if err := store.Create(ctx, &asset.Asset{
		ID: 3, AssetType: types.AssetTypeDevice, AssetKey: "dev-3",
	}); err != nil {
		t.Fatalf("seed asset: %v", err)
	}

	sm := newStateManager(store, dbc, nil, zap.NewNop(), nil)

	if changed, err := sm.UpdateStatus(ctx, 3, types.AssetStatusNormal, "first report"); err != nil || !changed {
		t.Fatalf("first assignment: changed=%v err=%v", changed, err)
	}
	if changed, err := sm.UpdateStatus(ctx, 3, types.AssetStatusOffline, "timeout"); err != nil || !changed {
		t.Fatalf("transition: changed=%v err=%v", changed, err)
	}

	var rows []StatusHistory
	if err := dbc.WithContext(ctx).Where("asset_id = ?", 3).Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("history rows = %d, want 2", len(rows))
	}
	if rows[0].PrevStatus != types.AssetStatusUnknown || rows[0].CurrStatus != types.AssetStatusNormal {
		t.Fatalf("first history row = %q->%q, want unknown->normal",
			rows[0].PrevStatus, rows[0].CurrStatus)
	}
	if rows[1].PrevStatus != types.AssetStatusNormal || rows[1].CurrStatus != types.AssetStatusOffline {
		t.Fatalf("second history row = %q->%q, want normal->offline",
			rows[1].PrevStatus, rows[1].CurrStatus)
	}
}

// TestFireTimeoutDropsStaleCallbacks verifies the timeout guard: callbacks
// for superseded entries or entries with a fresh heartbeat are dropped;
// only a current entry whose last activity is older than its timeout fires.
func TestFireTimeoutDropsStaleCallbacks(t *testing.T) {
	var fired atomic.Int32
	sm := newStateManager(newMgrMockStore(), nil, mustNewTestWheel(t, 4),
		zap.NewNop(), func(_ context.Context, _ int64) { fired.Add(1) })

	sm.RegisterAsset(1, time.Hour)
	entryID := sm.entries[1].entryID

	// Unknown asset: nothing is armed.
	sm.fireTimeout(2, entryID)
	// Superseded entry ID: the callback belongs to a removed entry.
	sm.fireTimeout(1, entryID+1000)
	// Current entry with a fresh registration timestamp: not timed out yet.
	sm.fireTimeout(1, entryID)
	if got := fired.Load(); got != 0 {
		t.Fatalf("stale callbacks fired %d times, want 0", got)
	}

	// Elapsed last activity beyond the timeout window: fires once.
	sm.mu.Lock()
	sm.lastActive[1] = time.Now().Add(-2 * time.Hour)
	sm.mu.Unlock()
	sm.fireTimeout(1, entryID)
	if got := fired.Load(); got != 1 {
		t.Fatalf("timed-out callback fired %d times, want 1", got)
	}
}

// TestUpdateActiveRearmsTimeoutEntry verifies a heartbeat delays the
// timeout: the original deadline passes without firing and the re-armed
// entry fires exactly once afterwards.
func TestUpdateActiveRearmsTimeoutEntry(t *testing.T) {
	wheel := mustNewTestWheel(t, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wheel.Start(ctx)
	defer func() { _ = wheel.Stop(context.Background()) }()

	var fired atomic.Int32
	sm := newStateManager(newMgrMockStore(), nil, wheel, zap.NewNop(),
		func(_ context.Context, _ int64) { fired.Add(1) })

	sm.RegisterAsset(1, time.Second)
	time.Sleep(400 * time.Millisecond)
	sm.UpdateActive(1)

	// The original 1s deadline has passed; the heartbeat must have
	// re-armed the entry so nothing fired yet.
	time.Sleep(400 * time.Millisecond)
	if got := fired.Load(); got != 0 {
		t.Fatalf("timeout fired before the re-armed deadline (%d calls)", got)
	}

	// The re-armed entry (deadline ~1.4s) fires once.
	time.Sleep(1200 * time.Millisecond)
	if got := fired.Load(); got != 1 {
		t.Fatalf("timeout fired %d times, want 1", got)
	}
}

// TestSyncStatusRefreshesCacheAfterOutOfBandWrite verifies the post-timeout
// cache sync: an out-of-band status write no longer suppresses the next
// legitimate transition.
func TestSyncStatusRefreshesCacheAfterOutOfBandWrite(t *testing.T) {
	store := newMgrMockStore()
	ctx := context.Background()
	if err := store.Create(ctx, &asset.Asset{
		ID: 5, AssetType: types.AssetTypeDevice, AssetKey: "dev-5",
	}); err != nil {
		t.Fatalf("seed asset: %v", err)
	}

	sm := newStateManager(store, nil, nil, zap.NewNop(), nil)
	sm.mu.Lock()
	sm.cache[5] = types.AssetStatusNormal
	sm.mu.Unlock()

	// Out-of-band timeout write, as performed by processors.
	if err := store.UpdateStatus(ctx, 5, types.AssetStatusOffline, time.Now()); err != nil {
		t.Fatalf("out-of-band update: %v", err)
	}

	sm.SyncStatus(ctx, 5)
	if got := sm.GetStatus(5); got != types.AssetStatusOffline {
		t.Fatalf("cache after sync = %q, want offline", got)
	}

	changed, err := sm.UpdateStatus(ctx, 5, types.AssetStatusNormal, "recovered")
	if err != nil || !changed {
		t.Fatalf("recovery transition: changed=%v err=%v", changed, err)
	}
}

// TestNewEngineRequiresAssetStore verifies the constructor fails fast when
// no asset store is wired instead of losing status updates at runtime.
func TestNewEngineRequiresAssetStore(t *testing.T) {
	if _, err := New(); err == nil {
		t.Fatal("expected error when no asset store is configured")
	}
}
