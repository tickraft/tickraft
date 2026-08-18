// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/types"
)

// MarkOffline marks the asset as offline in the store and publishes a
// status-change event on the bus. It is the shared implementation for all
// processors whose timeout semantics are identical: transition to
// types.AssetStatusOffline and notify subscribers.
//
// Parameters:
//   - ctx controls cancellation of the store lookup and update.
//   - store persists the status transition.
//   - bus publishes the StatusChange event. May be nil to skip publishing.
//   - logger records the transition at warn level.
//   - assetID is the asset to mark offline.
//   - assetType is the asset type hint; the actual stored type is preferred
//     when the asset exists in the store.
//   - reason is a human-readable description included in the event payload.
//
// Returns an error if the store update fails.
func MarkOffline(
	ctx context.Context,
	store asset.Store,
	bus event.Bus,
	logger *zap.Logger,
	assetID int64,
	assetType types.AssetType,
	reason string,
) error {
	if store == nil {
		return fmt.Errorf("telemetry: asset store is not configured")
	}

	prevStatus := types.AssetStatusUnknown
	payload := event.StatusChangePayload{
		AssetID:   strconv.FormatInt(assetID, 10),
		AssetType: string(assetType),
		Reason:    reason,
	}

	if a, err := store.GetByID(ctx, assetID); err == nil && a != nil {
		prevStatus = a.Status
		payload.TenantID = strconv.FormatInt(a.TenantID, 10)
		payload.AssetKey = a.AssetKey
		payload.AssetType = string(a.AssetType)
	}

	if err := store.UpdateStatus(ctx, assetID, types.AssetStatusOffline, time.Now()); err != nil {
		return fmt.Errorf("telemetry: update status on timeout: %w", err)
	}

	payload.PrevStatus = string(prevStatus)
	payload.CurrStatus = string(types.AssetStatusOffline)
	payload.DetectedAt = time.Now().UnixNano()

	if bus != nil {
		if pubErr := event.Publish(ctx, bus, event.TypeAssetStatusChanged, payload); pubErr != nil {
			if logger != nil {
				logger.Warn("failed to publish status change event on offline",
					zap.Int64("asset_id", assetID),
					zap.Error(pubErr),
				)
			}
		}
	}

	if logger != nil {
		logger.Warn("asset timeout, marked offline",
			zap.Int64("asset_id", assetID),
			zap.String("asset_type", string(assetType)),
			zap.String("prev_status", string(prevStatus)),
		)
	}

	return nil
}
