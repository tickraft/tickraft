// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/timewheel"
	"github.com/tickraft/tickraft/pkg/types"
	"go.uber.org/zap"
	"gorm.io/gorm"
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
	logger  *zap.Logger
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
		store:     store,
		dbc:       dbc,
		wheel:     wheel,
		cache:     make(map[int64]types.AssetStatus),
		entries:   make(map[int64]timeoutEntry),
		logger:    logger,
		onTimeout: onTimeout,
	}
}

// RegisterAsset adds a timeout entry to the time wheel for the given asset.
// It initializes the cached status to StatusUnknown when the asset is seen
// for the first time.
func (sm *stateManager) RegisterAsset(assetID int64, timeout time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	entryID := sm.wheel.Add(timeout, func(_ timewheel.EntryID) {
		sm.logger.Info("asset timeout detected",
			zap.Int64("asset_id", assetID),
			zap.Duration("timeout", timeout),
		)
		ctx := context.Background()
		if sm.onTimeout != nil {
			sm.onTimeout(ctx, assetID)
		}
	})

	sm.entries[assetID] = timeoutEntry{
		entryID: entryID,
		timeout: timeout,
	}

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
}

// UpdateActive renews the timeout entry for an asset (heartbeat).
func (sm *stateManager) UpdateActive(assetID int64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	entry, exists := sm.entries[assetID]
	if !exists {
		sm.logger.Warn("attempted to update active for unregistered asset",
			zap.Int64("asset_id", assetID),
		)
		return
	}

	newEntryID := sm.wheel.Renew(entry.entryID, entry.timeout)
	sm.entries[assetID] = timeoutEntry{
		entryID: newEntryID,
		timeout: entry.timeout,
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

// UpdateStatus persists a status change, updates the cache, and records history.
// It returns true if the status actually changed.
func (sm *stateManager) UpdateStatus(ctx context.Context, assetID int64, newStatus types.AssetStatus, reason string) (bool, error) {
	sm.mu.Lock()
	prevStatus, exists := sm.cache[assetID]
	if !exists {
		sm.cache[assetID] = newStatus
		sm.mu.Unlock()
		// First status assignment, persist to store.
		if err := sm.store.UpdateStatus(ctx, assetID, newStatus, time.Now()); err != nil {
			return false, fmt.Errorf("update status in store: %w", err)
		}
		return true, nil
	}

	if prevStatus == newStatus {
		sm.mu.Unlock()
		return false, nil
	}

	sm.cache[assetID] = newStatus
	sm.mu.Unlock()

	// Persist the status change and the status history atomically. When a
	// db handle is available both writes are wrapped in a single GORM
	// transaction so that a history-record failure rolls back the status
	// update. When no db handle is configured (e.g. unit tests with a mock
	// store), only the store is updated.
	if sm.dbc != nil {
		if err := sm.dbc.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := sm.store.UpdateStatus(ctx, assetID, newStatus, time.Now()); err != nil {
				return fmt.Errorf("update status in store: %w", err)
			}
			history := &StatusHistory{
				AssetID:    assetID,
				PrevStatus: prevStatus,
				CurrStatus: newStatus,
				Reason:     reason,
			}
			// Populate TenantID and AssetType (both not-null columns) from the
			// asset store when available.
			if sm.store != nil {
				if a, err := sm.store.GetByID(ctx, assetID); err == nil && a != nil {
					history.TenantID = a.TenantID
					history.AssetType = string(a.AssetType)
				}
			}
			if err := tx.Create(history).Error; err != nil {
				return fmt.Errorf("create status history: %w", err)
			}
			return nil
		}); err != nil {
			return false, err
		}
	} else {
		// No db handle available: persist via store only.
		if err := sm.store.UpdateStatus(ctx, assetID, newStatus, time.Now()); err != nil {
			return false, fmt.Errorf("update status in store: %w", err)
		}
	}

	sm.logger.Info("asset status changed",
		zap.Int64("asset_id", assetID),
		zap.String("prev_status", string(prevStatus)),
		zap.String("curr_status", string(newStatus)),
		zap.String("reason", reason),
	)

	return true, nil
}
