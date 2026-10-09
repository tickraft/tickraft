// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/timewheel"
	"github.com/tickraft/tickraft/pkg/types"
)

// timeoutEntry tracks the time wheel entry and timeout duration for an asset.
type timeoutEntry struct {
	entryID timewheel.EntryID
	timeout time.Duration
}

// stateManager manages asset state persistence, timeout detection, and status caching.
type stateManager struct {
	mu      sync.RWMutex
	store   asset.Store
	dbc     *gorm.DB
	wheel   timewheel.Wheel
	cache   map[int64]types.AssetStatus
	entries map[int64]timeoutEntry
	// lastActive records when the asset last renewed its timeout entry
	// (heartbeat report or bootstrap registration). Timeout callbacks
	// compare it against the entry timeout to drop stale fires.
	lastActive map[int64]time.Time
	logger     *zap.Logger
	// onTimeout is called when an asset times out.
	onTimeout func(ctx context.Context, assetID int64)
}

// newStateManager creates a new stateManager.
func newStateManager(
	store asset.Store,
	dbc *gorm.DB,
	wheel timewheel.Wheel,
	logger *zap.Logger,
	onTimeout func(ctx context.Context, assetID int64),
) *stateManager {
	return &stateManager{
		store:      store,
		dbc:        dbc,
		wheel:      wheel,
		cache:      make(map[int64]types.AssetStatus),
		entries:    make(map[int64]timeoutEntry),
		lastActive: make(map[int64]time.Time),
		logger:     logger,
		onTimeout:  onTimeout,
	}
}

// armEntry adds a fresh timeout entry to the wheel for the asset. The wheel
// hands each callback its own entry ID, so fireTimeout can recognize
// superseded entries. The callback must use that parameter rather than the
// Add return value: the return value is assigned only after Add returns,
// which has no happens-before edge to the pool-worker callback — and an
// already-expired duration dispatches the callback before Add returns at
// all. The caller must hold sm.mu.
func (sm *stateManager) armEntry(assetID int64, timeout time.Duration) timewheel.EntryID {
	return sm.wheel.Add(timeout, func(firedID timewheel.EntryID) {
		sm.fireTimeout(assetID, firedID)
	})
}

// RegisterAsset adds a timeout entry to the time wheel for the given asset.
// It initializes the cached status to StatusUnknown when the asset is seen
// for the first time. A re-registration replaces the previous wheel entry:
// the old entry is removed first so it cannot fire after being superseded.
func (sm *stateManager) RegisterAsset(assetID int64, timeout time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if prev, exists := sm.entries[assetID]; exists {
		sm.wheel.Remove(prev.entryID)
	}

	entryID := sm.armEntry(assetID, timeout)
	sm.entries[assetID] = timeoutEntry{
		entryID: entryID,
		timeout: timeout,
	}
	// Stamp the arm time so a bootstrap-registered asset that never reports
	// is declared silent after one full timeout window.
	sm.lastActive[assetID] = time.Now()

	// Initialize cache with unknown status if not already present.
	if _, exists := sm.cache[assetID]; !exists {
		sm.cache[assetID] = types.AssetStatusUnknown
	}
}

// UnregisterAsset removes an asset from the time wheel and cache.
func (sm *stateManager) UnregisterAsset(assetID int64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if entry, exists := sm.entries[assetID]; exists {
		sm.wheel.Remove(entry.entryID)
		delete(sm.entries, assetID)
	}
	delete(sm.cache, assetID)
	delete(sm.lastActive, assetID)
}

// UpdateActive renews the timeout entry for an asset (heartbeat). An asset
// whose entry is missing — it has no passive point registered, e.g. it was
// created while the engine was down or reports without any passive point —
// is auto-registered with the default threshold so heartbeat-loss detection
// still covers it.
func (sm *stateManager) UpdateActive(assetID int64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	entry, exists := sm.entries[assetID]
	if !exists {
		entry.timeout = DefaultHeartbeatTimeout
		sm.logger.Info("asset auto-registered for observation on first report",
			zap.Int64("asset_id", assetID),
			zap.Duration("timeout", entry.timeout),
		)
	}

	// Re-arm by adding a fresh entry and removing the old one instead of
	// using wheel.Renew: an entry already dequeued by the wheel cannot be
	// renewed and would come back with an empty callback, silently
	// disabling timeout detection for the asset.
	newEntryID := sm.armEntry(assetID, entry.timeout)
	if exists {
		sm.wheel.Remove(entry.entryID)
	}
	sm.entries[assetID] = timeoutEntry{
		entryID: newEntryID,
		timeout: entry.timeout,
	}
	sm.lastActive[assetID] = time.Now()

	if _, cached := sm.cache[assetID]; !cached {
		sm.cache[assetID] = types.AssetStatusUnknown
	}
}

// fireTimeout guards a timeout callback against races with concurrent
// reports. A report that arrived after this entry was armed has already
// re-armed a fresh entry and stamped lastActive, so a superseded or
// freshly-renewed callback is dropped instead of marking a live asset
// offline.
func (sm *stateManager) fireTimeout(assetID int64, firedID timewheel.EntryID) {
	sm.mu.RLock()
	entry, exists := sm.entries[assetID]
	last := sm.lastActive[assetID]
	sm.mu.RUnlock()

	// The wheel has one-second resolution: the delay is truncated to whole
	// slots and the arm position loses its sub-second phase, so a
	// legitimate fire can land up to ~2s before the nominal deadline.
	// Tolerate that slack so genuine timeouts are never dropped; the
	// entry-identity check above remains the primary race guard.
	threshold := max(entry.timeout-2*time.Second, 0)

	if !exists || entry.entryID != firedID || time.Since(last) < threshold {
		sm.logger.Debug("stale timeout callback skipped",
			zap.Int64("asset_id", assetID),
			zap.Int64("fired_entry", int64(firedID)),
		)
		return
	}

	sm.logger.Info("asset timeout detected",
		zap.Int64("asset_id", assetID),
		zap.Duration("timeout", entry.timeout),
	)
	if sm.onTimeout != nil {
		sm.onTimeout(context.Background(), assetID)
	}
}

// GetStatus returns the cached status for an asset.
func (sm *stateManager) GetStatus(assetID int64) types.AssetStatus {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if status, exists := sm.cache[assetID]; exists {
		return status
	}
	return types.AssetStatusUnknown
}

// UpdateStatus persists a status change, records history, and only then
// commits the cached value, so a failed write leaves the previous status in
// place and the next report retries the transition. It returns true if the
// status actually changed.
//
// The lock is held across the store write: status transitions are rare, and
// serializing them keeps the cache commit order identical to the
// persistence order.
func (sm *stateManager) UpdateStatus(
	ctx context.Context,
	assetID int64,
	newStatus types.AssetStatus,
	reason string,
) (bool, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	prevStatus, exists := sm.cache[assetID]
	if !exists {
		prevStatus = types.AssetStatusUnknown
	} else if prevStatus == newStatus {
		return false, nil
	}

	if err := sm.persistStatus(ctx, assetID, prevStatus, newStatus, reason); err != nil {
		return false, err
	}
	sm.cache[assetID] = newStatus

	if exists {
		sm.logger.Info("asset status changed",
			zap.Int64("asset_id", assetID),
			zap.String("prev_status", string(prevStatus)),
			zap.String("curr_status", string(newStatus)),
			zap.String("reason", reason),
		)
	}
	return true, nil
}

// SyncStatus refreshes the cached status of an asset from the store. It is
// used after out-of-band status writes — timeout transitions performed by
// processors — so the cache does not diverge from the persisted state and
// suppress a later legitimate transition.
func (sm *stateManager) SyncStatus(ctx context.Context, assetID int64) {
	a, err := sm.store.GetByID(ctx, assetID)
	if err != nil || a == nil {
		sm.logger.Warn("timeout cache sync: fetch asset failed",
			zap.Int64("asset_id", assetID),
			zap.Error(err),
		)
		return
	}

	sm.mu.Lock()
	sm.cache[assetID] = a.Status
	sm.mu.Unlock()
}

// persistStatus writes the new status to the store and then records the
// history row. The store update is the authoritative write; a history
// failure surfaces as an error so the caller leaves the cache uncommitted
// and the next report retries both writes (re-writing the same status is
// harmless). The caller must hold sm.mu.
func (sm *stateManager) persistStatus(
	ctx context.Context,
	assetID int64,
	prevStatus, newStatus types.AssetStatus,
	reason string,
) error {
	if err := sm.store.UpdateStatus(ctx, assetID, newStatus, time.Now()); err != nil {
		return fmt.Errorf("update status in store: %w", err)
	}

	if sm.dbc == nil {
		return nil
	}

	history := &StatusHistory{
		AssetID:    assetID,
		PrevStatus: prevStatus,
		CurrStatus: newStatus,
		Reason:     reason,
	}
	// Populate TenantID and AssetType (both not-null columns) from the
	// asset store when available.
	if a, err := sm.store.GetByID(ctx, assetID); err == nil && a != nil {
		history.TenantID = a.TenantID
		history.AssetType = string(a.AssetType)
	}
	if err := sm.dbc.WithContext(ctx).Create(history).Error; err != nil {
		return fmt.Errorf("create status history: %w", err)
	}
	return nil
}
