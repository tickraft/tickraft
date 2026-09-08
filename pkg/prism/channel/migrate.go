// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// Migrate runs GORM AutoMigrate for the channel domain tables
// (sys_prism_channel and sys_prism_delivery). It is invoked by the prism
// engine's migration phase at startup. It is safe to call repeatedly;
// existing tables and data are preserved.
func Migrate(ctx context.Context, dbc *gorm.DB) error {
	if err := dbc.WithContext(ctx).AutoMigrate(&Channel{}, &DeliveryRecord{}); err != nil {
		return fmt.Errorf("migrate channel tables: %w", err)
	}
	return nil
}
