// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package user

import "time"

// User represents a system user.
//
// The runtime defines only the fields required for single-process
// authentication and administration. Extended fields (TenantID, MFASecret,
// MFAEnabled, LastLoginAt, etc.) are added by the extended model package
// via struct embedding, which keeps this definition focused on
// single-tenant authentication.
//
// GORM tags are retained so that the GORM-backed implementation in
// pkg/store can migrate and query the sys_user table without duplicating
// the schema definition.
type User struct {
	ID                 int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Username           string    `gorm:"uniqueIndex;size:64;not null" json:"username"`
	PasswordHash       string    `gorm:"size:255;not null" json:"-"`
	Nickname           string    `gorm:"size:64" json:"nickname,omitempty"`
	Role               int       `gorm:"not null;default:0" json:"role"` // 0=viewer 1=developer 2=admin
	Email              string    `gorm:"size:128" json:"email,omitempty"`
	Status             int       `gorm:"not null;default:1" json:"status"` // 0=disabled 1=active
	Language           string    `gorm:"size:16;not null;default:'zh-Hans'" json:"language"`
	AlertFormatStyle   string    `gorm:"size:32;not null;default:'detailed'" json:"alert_format_style"`
	MustChangePassword bool      `gorm:"column:must_change_password;not null;default:false" json:"must_change_password"`
	CreatedAt          time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName returns the database table name for User.
func (User) TableName() string { return "sys_user" }

// APIKey represents an API key for programmatic access.
type APIKey struct {
	ID              int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name            string     `gorm:"size:128;not null" json:"name"`
	KeyPrefix       string     `gorm:"column:key_prefix;size:16;not null" json:"key_prefix"`
	KeyHash         string     `gorm:"column:key_hash;size:128;uniqueIndex;not null" json:"-"`
	Status          int        `gorm:"not null;default:1" json:"status"` // 0=disabled 1=active
	IPWhitelist     string     `gorm:"column:ip_whitelist;size:512" json:"ip_whitelist,omitempty"`
	PermissionLevel string     `gorm:"column:permission_level;size:32" json:"permission_level,omitempty"`
	CreatedAt       time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	ExpiredAt       *time.Time `gorm:"column:expired_at" json:"expired_at,omitempty"`
	RevokedAt       *time.Time `gorm:"column:revoked_at" json:"revoked_at,omitempty"`
}

// TableName returns the database table name for APIKey.
func (APIKey) TableName() string { return "sys_api_key" }
