// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"time"

	"gorm.io/gorm"
)

// Record is the GORM persistence model for the sys_prism_channel
// table. It stores notification channel definitions managed through the
// CRUD API at /api/v1/prism/channels.
//
// The Config field holds a JSON-encoded channel.Config payload so the
// BuildFromRecord function can construct a runtime alert.Channel from a
// database row. The open-source edition supports the "webhook" and "email"
// types; additional types are injected via the extension SPI
// (channel.Register).
type Record struct {
	// ID is the auto-incremented primary key.
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// Name is the human-readable channel name.
	Name string `gorm:"column:name;type:varchar(255);not null" json:"name"`
	// Type selects the channel implementation. Matched
	// case-insensitively against registered factories. The open-source
	// edition supports "webhook".
	Type string `gorm:"column:type;type:varchar(32);not null" json:"type"`
	// Config is the JSON-encoded channel.Config payload interpreted by
	// the factory selected by Type.
	Config string `gorm:"column:config;type:text;not null" json:"config"`
	// Enabled indicates whether the channel is active and eligible to
	// receive alert notifications.
	Enabled bool `gorm:"column:enabled;not null;default:true" json:"enabled"`
	// LastUsedAt records the last time the channel successfully
	// delivered a notification. It is updated by the prism engine
	// after a successful Send. A nil value means the channel has never
	// been used.
	LastUsedAt *time.Time `gorm:"column:last_used_at" json:"last_used_at,omitempty"`
	// CreatedAt is the channel creation timestamp.
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	// UpdatedAt is the channel last-update timestamp.
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	// DeletedAt records the soft-delete timestamp.
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

// TableName returns the database table name for Record.
func (Record) TableName() string { return "sys_prism_channel" }
