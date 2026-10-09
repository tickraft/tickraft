// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package system

import (
	"context"

	"gorm.io/gorm"
)

// Network environment values for Config.NetworkEnvironment. The knob is
// the L0 degrade switch: isolated deployments render plain-text
// notifications (no interactive cards, no external asset references) so
// alert delivery keeps working on intranet-only networks. It is a plain
// capability knob without any licensing notion.
const (
	// NetworkEnvironmentInternet keeps the full interactive rendering.
	NetworkEnvironmentInternet = "internet"
	// NetworkEnvironmentIsolated downgrades every channel to the plain
	// notification form.
	NetworkEnvironmentIsolated = "isolated"
)

// PlainNotificationOnly reports whether channels must render the plain
// degraded form: true only when the deployment is explicitly isolated.
// An empty value reads as internet, preserving the pre-knob rendering.
func (c Config) PlainNotificationOnly() bool {
	return c.NetworkEnvironment == NetworkEnvironmentIsolated
}

// ReadNetworkEnvironment loads network_environment from the sys_config
// singleton, best-effort: a missing table, row, or column (the system
// domain has not migrated yet — prism starts before the API server)
// yields "" which Config.PlainNotificationOnly reads as internet.
func ReadNetworkEnvironment(ctx context.Context, dbc *gorm.DB) string {
	var env string
	if err := dbc.WithContext(ctx).
		Table((systemConfig{}).TableName()).
		Select("network_environment").
		Where("id = ?", configRowID).
		Row().Scan(&env); err != nil {
		return ""
	}
	return env
}
