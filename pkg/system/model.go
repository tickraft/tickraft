// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package system provides the system configuration, runtime info, and
// global stats contract (Service) plus its database-backed implementation. It persists system configuration in a
// dedicated DB table and derives runtime info and global statistics
// from build-time variables and the runtime's task / asset stores.
package system

import "time"

// Config represents the system configuration. It is the single model for
// the sys_config singleton row: the gorm tags are consumed when the struct
// is embedded into the storage row model below, so the wire shape and the
// column shape are defined in one place.
type Config struct {
	LogLevel      string `json:"log_level" gorm:"column:log_level;size:20;not null;default:'info'"`
	DefaultLang   string `json:"default_lang" gorm:"column:default_lang;size:20;not null;default:'zh-Hans'"`
	RetentionDays int    `json:"retention_days" gorm:"column:retention_days;type:integer;not null;default:30"`
	// NetworkEnvironment selects the deployment environment for
	// notification rendering: internet (default, full interactive
	// rendering) or isolated (plain-notification degrade). Values are
	// constrained to the two enum words by UpdateConfig.
	NetworkEnvironment string `json:"network_environment" gorm:"column:network_environment;not null;default:'internet'"`
}

// systemConfig is the GORM row model for the sys_config table. It stores
// a single row (id=1) holding the global system configuration. The table
// is created by Migrate and seeded with default values on first access.
// The configuration fields themselves are defined once by the embedded
// Config, whose gorm tags supply the column names and types.
type systemConfig struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	Config    `gorm:"embedded"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName overrides the default GORM table name.
func (systemConfig) TableName() string { return "sys_config" }

// configRowID is the fixed primary key of the singleton sys_config row.
const configRowID = int64(1)
