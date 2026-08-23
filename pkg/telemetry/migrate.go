// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// Migrate creates or updates the monitor_points table schema. It is
// intended to be called once during application startup and is safe to
// call repeatedly: GORM AutoMigrate is idempotent (additive only).
func Migrate(ctx context.Context, dbc *gorm.DB) error {
	if err := dbc.WithContext(ctx).AutoMigrate(&MonitorPoint{}); err != nil {
		return fmt.Errorf("telemetry: auto-migrate monitor_points table: %w", err)
	}
	return nil
}
