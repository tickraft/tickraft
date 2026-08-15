// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package system provides a database-backed implementation of the
// system.Service interface. It persists system configuration in a
// dedicated DB table and derives runtime info and global statistics
// from build-time variables and the runtime's task / asset stores.
package system

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tickraft/tickraft/pkg/api/handler/system"
	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/task"
)

// systemConfig is the GORM model for the sys_config table. It stores
// a single row (id=1) holding the global system configuration. The table
// is created by Migrate and seeded with default values on first access.
type systemConfig struct {
	ID            int64     `gorm:"primaryKey;autoIncrement:false"`
	LogLevel      string    `gorm:"column:log_level;type:varchar(20);not null;default:'info'"`
	DefaultLang   string    `gorm:"column:default_lang;type:varchar(20);not null;default:'zh-Hans'"`
	RetentionDays int       `gorm:"column:retention_days;type:integer;not null;default:30"`
	UpdatedAt     time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName overrides the default GORM table name.
func (systemConfig) TableName() string { return "sys_config" }

const configRowID = int64(1)

// Compile-time interface compliance check.
var _ system.Service = (*Service)(nil)

// Service is a database-backed implementation of system.Service.
type Service struct {
	dbc        *gorm.DB
	logger     *zap.Logger
	taskStore  task.Store
	execStore  task.ExecutionStore
	assetStore asset.Store
	startAt    time.Time
}

// New creates a new database-backed system Service. The db must be a
// connected GORM instance; taskStore, execStore, and assetStore may be
// nil when the corresponding engines were not started (stats return 0
// for those data sources).
func New(dbc *gorm.DB, logger *zap.Logger, taskStore task.Store, execStore task.ExecutionStore, assetStore asset.Store) *Service {
	return &Service{
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
func (s *Service) Migrate(ctx context.Context) error {
	if err := s.dbc.WithContext(ctx).AutoMigrate(&systemConfig{}); err != nil {
		return fmt.Errorf("migrate sys_config: %w", err)
	}
	// Seed the singleton row if it does not exist.
	var count int64
	if err := s.dbc.WithContext(ctx).Model(&systemConfig{}).Where("id = ?", configRowID).Count(&count).Error; err != nil {
		return fmt.Errorf("seed sys_config: count: %w", err)
	}
	if count == 0 {
		seed := systemConfig{
			ID:            configRowID,
			LogLevel:      "info",
			DefaultLang:   "zh-Hans",
			RetentionDays: 30,
		}
		if err := s.dbc.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
			return fmt.Errorf("seed sys_config: insert: %w", err)
		}
	}
	return nil
}

// GetConfig returns the current system configuration from the database.
func (s *Service) GetConfig(ctx context.Context) (*system.Config, error) {
	var row systemConfig
	if err := s.dbc.WithContext(ctx).First(&row, configRowID).Error; err != nil {
		return nil, fmt.Errorf("get system config: %w", err)
	}
	return &system.Config{
		LogLevel:      row.LogLevel,
		DefaultLang:   row.DefaultLang,
		RetentionDays: row.RetentionDays,
	}, nil
}

// UpdateConfig updates the system configuration in the database and
// returns the persisted result.
func (s *Service) UpdateConfig(ctx context.Context, req *system.Config) (*system.Config, error) {
	if req == nil {
		return nil, fmt.Errorf("config request is nil")
	}
	updates := map[string]any{
		"log_level":      req.LogLevel,
		"default_lang":   req.DefaultLang,
		"retention_days": req.RetentionDays,
		"updated_at":     time.Now(),
	}
	if err := s.dbc.WithContext(ctx).Model(&systemConfig{}).Where("id = ?", configRowID).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("update system config: %w", err)
	}
	return s.GetConfig(ctx)
}

// GetInfo returns runtime system information derived from build metadata.
func (s *Service) GetInfo(_ context.Context) (*system.Info, error) {
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

	return &system.Info{
		Version:   version,
		BuildTags: buildTags,
		StartTime: s.startAt,
		Uptime:    time.Since(s.startAt).Round(time.Second).String(),
	}, nil
}

// GetGlobalStats returns system-wide aggregate statistics from the
// runtime's task, asset, and execution stores. Data sources that are
// not available (nil stores) contribute 0 to their respective fields.
func (s *Service) GetGlobalStats(ctx context.Context) (*system.GlobalStats, error) {
	stats := &system.GlobalStats{}

	// Total tasks from the scheduler task store.
	if s.taskStore != nil {
		tasks, err := s.taskStore.List(ctx, task.ListOptions{})
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
		execStats, err := s.execStore.Stats(ctx, startOfDay, now)
		if err != nil {
			s.logger.Warn("system stats: execution stats", zap.Error(err))
		} else {
			stats.TodayExecutions = execStats.TotalExecutions
			stats.TodaySuccessRate = execStats.SuccessRate
		}
	}

	return stats, nil
}
