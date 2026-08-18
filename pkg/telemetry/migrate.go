// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Migrate creates or updates the monitor_points table schema and optionally
// ports legacy CollectionConfig records into the unified table. It is
// intended to be called once during application startup, after
// migrateCollectorTables has created the CollectionConfig table. It is safe
// to call repeatedly: GORM AutoMigrate is idempotent (additive only), and the
// data migration is guarded by an emptiness check so it runs at most once.
//
// The open-source edition does not ship separate prober or listener tables;
// the former CollectionConfig table (sys_collect_config) is the closest
// predecessor. When monitor_points is empty and sys_collect_config contains
// rows, each row is ported as an active monitoring point (Mode=ModeActive)
// so existing observation configurations are preserved across the upgrade.
func Migrate(ctx context.Context, dbc *gorm.DB, logger *zap.Logger) error {
	if logger == nil {
		logger = zap.NewNop()
	}

	// Create or update the monitor_points table schema.
	if err := dbc.WithContext(ctx).AutoMigrate(&MonitorPoint{}); err != nil {
		return fmt.Errorf("telemetry: auto-migrate monitor_points table: %w", err)
	}

	// Data migration: port legacy CollectionConfig rows into monitor_points.
	// This block is idempotent — it only runs when monitor_points is empty.
	if err := migrateLegacyCollectConfigs(ctx, dbc, logger); err != nil {
		return fmt.Errorf("telemetry: migrate legacy collect configs: %w", err)
	}

	return nil
}

// migrateLegacyCollectConfigs ports rows from sys_collect_config into
// monitor_points when the target table is empty. Each CollectionConfig row
// becomes a MonitorPoint with Mode=ModeActive, Type derived from
// CollectType (defaulting to "icmp" when empty), and Config copied from
// CollectConfig. The migration is skipped when monitor_points already
// contains rows, making it safe to run on every startup.
func migrateLegacyCollectConfigs(ctx context.Context, dbc *gorm.DB, logger *zap.Logger) error {
	// Skip when monitor_points already has data.
	var count int64
	if err := dbc.WithContext(ctx).Model(&MonitorPoint{}).Count(&count).Error; err != nil {
		return fmt.Errorf("count monitor_points: %w", err)
	}
	if count > 0 {
		return nil
	}

	// Check whether the legacy table exists. On a fresh install it may not
	// have been created yet (migrateCollectorTables creates it, but the
	// caller may invoke Migrate before that). A missing table is not an
	// error — there is simply nothing to port.
	var legacyCount int64
	legacyTable := (CollectionConfig{}).TableName()
	if !dbc.WithContext(ctx).Migrator().HasTable(legacyTable) {
		return nil
	}
	if err := dbc.WithContext(ctx).Model(&CollectionConfig{}).Count(&legacyCount).Error; err != nil {
		return fmt.Errorf("count legacy %q: %w", legacyTable, err)
	}
	if legacyCount == 0 {
		return nil
	}

	// Load and port each legacy row.
	var configs []CollectionConfig
	if err := dbc.WithContext(ctx).Find(&configs).Error; err != nil {
		return fmt.Errorf("load legacy %q: %w", legacyTable, err)
	}

	points := make([]MonitorPoint, 0, len(configs))
	for i := range configs {
		c := configs[i]
		proberType := c.CollectType
		if proberType == "" {
			proberType = "icmp"
		}
		points = append(points, MonitorPoint{
			TenantID: c.TenantID,
			Name:     fmt.Sprintf("asset-%d", c.AssetID),
			Mode:     ModeActive,
			Type:     proberType,
			Status:   MonitorStatusInactive,
			Interval: c.ProbeInterval,
			Timeout:  c.Timeout,
			Enabled:  c.Enable,
			Config:   c.CollectConfig,
		})
	}

	if err := dbc.WithContext(ctx).Create(&points).Error; err != nil {
		return fmt.Errorf("port legacy configs to monitor_points: %w", err)
	}

	logger.Info("migrated legacy collect configs to monitor_points",
		zap.Int64("legacy_count", legacyCount),
		zap.Int("ported_count", len(points)),
	)
	return nil
}
