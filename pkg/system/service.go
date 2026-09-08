// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package system

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/task"
	"github.com/tickraft/tickraft/pkg/types"
)

// Compile-time interface compliance check.
var _ Service = (*SystemService)(nil)

// SystemService is a database-backed implementation of the Service
// contract. The name mirrors the <Domain>Service convention of the other
// domain service packages.
//
//nolint:revive // system.SystemService stutter is accepted for cross-domain naming consistency.
type SystemService struct {
	dbc        *gorm.DB
	logger     *zap.Logger
	taskStore  task.Store
	execStore  task.ExecutionStore
	assetStore asset.Store
	startAt    time.Time
}

// NewSystemService creates a new database-backed system Service. The db must be a
// connected GORM instance; taskStore, execStore, and assetStore may be
// nil when the corresponding engines were not started (stats return 0
// for those data sources).
func NewSystemService(
	dbc *gorm.DB,
	logger *zap.Logger,
	taskStore task.Store,
	execStore task.ExecutionStore,
	assetStore asset.Store,
) *SystemService {
	return &SystemService{
		dbc:        dbc,
		logger:     logger,
		taskStore:  taskStore,
		execStore:  execStore,
		assetStore: assetStore,
		startAt:    time.Now(),
	}
}

// Migrate creates the sys_config table if it does not exist and seeds
// the singleton configuration row with default values.
func (s *SystemService) Migrate(ctx context.Context) error {
	if err := s.dbc.WithContext(ctx).AutoMigrate(&systemConfig{}); err != nil {
		return fmt.Errorf("migrate sys_config: %w", err)
	}
	// Seed the singleton row if it does not exist.
	var count int64
	if err := s.dbc.WithContext(ctx).Model(&systemConfig{}).
		Where("id = ?", configRowID).Count(&count).Error; err != nil {
		return fmt.Errorf("seed sys_config: count: %w", err)
	}
	if count == 0 {
		seed := systemConfig{
			ID: configRowID,
			Config: Config{
				LogLevel:      string(types.LogLevelInfo),
				DefaultLang:   "zh-Hans",
				RetentionDays: 30,
			},
		}
		if err := s.dbc.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
			return fmt.Errorf("seed sys_config: insert: %w", err)
		}
	}
	return nil
}

// GetConfig returns the current system configuration from the database.
func (s *SystemService) GetConfig(ctx context.Context) (*Config, error) {
	var row systemConfig
	if err := s.dbc.WithContext(ctx).First(&row, configRowID).Error; err != nil {
		return nil, fmt.Errorf("get system config: %w", err)
	}
	return &row.Config, nil
}

// UpdateConfig updates the system configuration in the database and
// returns the persisted result.
func (s *SystemService) UpdateConfig(ctx context.Context, req *Config) (*Config, error) {
	if req == nil {
		return nil, fmt.Errorf("config request is nil")
	}
	// Select pins the write to the embedded config columns (zero values
	// included); updated_at is set by autoUpdateTime.
	if err := s.dbc.WithContext(ctx).Model(&systemConfig{}).
		Where("id = ?", configRowID).
		Select("log_level", "default_lang", "retention_days").
		Updates(systemConfig{Config: *req}).Error; err != nil {
		return nil, fmt.Errorf("update system config: %w", err)
	}
	return s.GetConfig(ctx)
}

// GetInfo returns runtime system information derived from build metadata.
func (s *SystemService) GetInfo(_ context.Context) (*Info, error) {
	version := "dev"
	buildTags := ""

	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range bi.Settings {
			switch setting.Key {
			case "-tags":
				buildTags = setting.Value
			case "vcs.revision":
				if setting.Value != "" {
					version = setting.Value
				}
			}
		}
		if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}
	}

	return &Info{
		Version:   version,
		BuildTags: buildTags,
		StartTime: s.startAt,
		Uptime:    time.Since(s.startAt).Round(time.Second).String(),
	}, nil
}

// GetGlobalStats returns system-wide aggregate statistics from the
// runtime's task, asset, and execution stores. Data sources that are
// not available (nil stores) contribute 0 to their respective fields.
func (s *SystemService) GetGlobalStats(ctx context.Context) (*GlobalStats, error) {
	stats := &GlobalStats{}

	// Total tasks from the scheduler task store. Synthetic rows (negative
	// IDs, other domains' recurring jobs such as probe tasks) are internal
	// and excluded so the stat reflects user-created tasks only.
	if s.taskStore != nil {
		tasks, err := s.taskStore.List(ctx, task.ListOptions{ExcludeSynthetic: true})
		if err != nil {
			s.logger.Warn("system stats: list tasks", zap.Error(err))
		} else {
			stats.TotalTasks = int64(len(tasks))
		}
	}

	// Total devices (assets) from the asset store.
	if s.assetStore != nil {
		_, total, err := s.assetStore.List(ctx, 1, 1, asset.ListFilter{})
		if err != nil {
			s.logger.Warn("system stats: list assets", zap.Error(err))
		} else {
			stats.TotalDevices = total
		}
		statusCounts, err := s.assetStore.CountByStatus(ctx)
		if err != nil {
			s.logger.Warn("system stats: count assets by status", zap.Error(err))
		} else {
			stats.AssetStatusCounts = statusCounts
		}
	}

	// Today's execution stats from the execution store.
	if s.execStore != nil {
		now := time.Now().UTC()
		startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		execStats, err := s.execStore.Stats(ctx, startOfDay, now, 0)
		if err != nil {
			s.logger.Warn("system stats: execution stats", zap.Error(err))
		} else {
			stats.TodayExecutions = execStats.TotalExecutions
			stats.TodaySuccessRate = execStats.SuccessRate
		}
	}

	return stats, nil
}
