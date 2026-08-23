// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"strconv"
	"time"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"
)

// DefaultHeartbeatTimeout is the offline-detection threshold applied when a
// passive point carries neither a heartbeat_timeout config key nor a usable
// Interval. Three minutes tolerates three missed 60s reports before the
// asset is declared offline.
const DefaultHeartbeatTimeout = 3 * time.Minute

// HeartbeatTimeout derives the offline-detection threshold for a passive
// monitoring point. Precedence: the point's config-JSON heartbeat_timeout
// key (seconds), then three times its Interval, then DefaultHeartbeatTimeout.
// A malformed config blob falls through to the Interval-derived value.
func HeartbeatTimeout(point MonitorPoint) time.Duration {
	if cfg := point.ConfigJSON(); cfg != "" {
		var raw struct {
			HeartbeatTimeout any `json:"heartbeat_timeout"`
		}
		if err := sonic.Unmarshal([]byte(cfg), &raw); err == nil {
			if secs, ok := toSeconds(raw.HeartbeatTimeout); ok && secs > 0 {
				return time.Duration(secs) * time.Second
			}
		}
	}
	if point.Interval > 0 {
		return 3 * time.Duration(point.Interval) * time.Second
	}
	return DefaultHeartbeatTimeout
}

// toSeconds accepts a JSON number (or numeric string) and returns it as int
// seconds. Non-numeric values are rejected.
func toSeconds(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case string:
		secs, err := strconv.Atoi(n)
		if err != nil {
			return 0, false
		}
		return secs, true
	default:
		return 0, false
	}
}

// SyncAssetObservation reconciles an asset's timeout-wheel registration
// with its enabled passive monitoring points: when the asset has at least
// one enabled passive point it is (re-)registered for observation with the
// most lenient threshold among those points; otherwise its observation is
// removed. It is the single reconciliation path shared by engine start-up
// and the point CRUD hooks, so a point create/update/delete can never leave
// a stale wheel entry behind.
func SyncAssetObservation(
	ctx context.Context,
	store *MonitorStore,
	collector Collector,
	logger *zap.Logger,
	assetID int64,
) error {
	if store == nil || collector == nil || assetID <= 0 {
		return nil
	}
	points, err := store.ListPassive(ctx)
	if err != nil {
		return err
	}
	timeout := time.Duration(0)
	observed := false
	for i := range points {
		p := &points[i]
		if !p.Enabled || p.AssetID != assetID {
			continue
		}
		observed = true
		if t := HeartbeatTimeout(*p); t > timeout {
			timeout = t
		}
	}
	if !observed {
		return collector.UnregisterAsset(ctx, assetID)
	}
	return collector.RegisterAsset(ctx, Config{
		AssetID: assetID,
		Timeout: int(timeout / time.Second),
	})
}
