// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package system

import (
	"context"
	"time"
)

// Config represents the system configuration.
type Config struct {
	LogLevel      string `json:"log_level"`
	DefaultLang   string `json:"default_lang"`
	RetentionDays int    `json:"retention_days"`
}

// Info represents runtime system information.
type Info struct {
	Version   string    `json:"version"`
	BuildTags string    `json:"build_tags"`
	StartTime time.Time `json:"start_time"`
	Uptime    string    `json:"uptime"`
}

// GlobalStats holds system-wide aggregate statistics for the dashboard
// overview. TotalDevices reflects the number of registered resources;
// TodayExecutions and TodaySuccessRate are scoped to the current UTC day.
// AssetStatusCounts breaks the asset inventory down by status
// (normal/abnormal/offline/unknown). Implementations that lack access to a
// given data source return 0 for the corresponding field.
type GlobalStats struct {
	TotalTasks         int64            `json:"total_tasks"`
	TotalDevices       int64            `json:"total_devices"`
	TodayExecutions    int64            `json:"today_executions"`
	TodaySuccessRate   float64          `json:"today_success_rate"`
	AssetStatusCounts  map[string]int64 `json:"asset_status_counts"`
}

// Service defines the operations for system configuration and info.
type Service interface {
	// GetConfig returns the current system configuration.
	GetConfig(ctx context.Context) (*Config, error)
	// UpdateConfig updates the system configuration and returns the result.
	UpdateConfig(ctx context.Context, req *Config) (*Config, error)
	// GetInfo returns the runtime system information.
	GetInfo(ctx context.Context) (*Info, error)
	// GetGlobalStats returns system-wide aggregate statistics for the
	// dashboard overview. Implementations without access to a data source
	// return 0 for the corresponding field.
	GetGlobalStats(ctx context.Context) (*GlobalStats, error)
}
