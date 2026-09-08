// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package status

import (
	"context"
	"fmt"
	"time"

	"github.com/bytedance/sonic"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tickraft/tickraft/pkg/db"
)

// statusConfig is the GORM row model for the sys_status_config table. It
// stores a single row (id=1) holding the status page configuration. The
// component map is persisted as a JSON column; the typed Config shape is
// produced by the store's (de)serialization helpers.
type statusConfig struct {
	ID int64 `gorm:"primaryKey;autoIncrement:false"`
	// Title is the page title.
	Title string `gorm:"column:title;size:255;not null;default:'Service Status'"`
	// Description is the optional page subtitle.
	Description string `gorm:"column:description;size:1024;not null;default:''"`
	// Enabled controls whether the public page is served.
	Enabled bool `gorm:"column:enabled;not null;default:false"`
	// ComponentsJSON is the serialized component map ([]Component).
	ComponentsJSON string    `gorm:"column:components;type:text;not null;default:''"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName overrides the default GORM table name.
func (statusConfig) TableName() string { return "sys_status_config" }

// configRowID is the fixed primary key of the singleton sys_status_config
// row.
const configRowID = int64(1)

// Store persists the status page configuration in sys_status_config.
type Store struct {
	dbc *gorm.DB
}

// NewStore creates a status configuration store backed by the given GORM
// instance.
func NewStore(dbc *gorm.DB) *Store {
	return &Store{dbc: dbc}
}

// Migrate creates the sys_status_config table if it does not exist and
// seeds the singleton row with default values (page disabled, no
// component map).
func (s *Store) Migrate(ctx context.Context) error {
	if err := s.dbc.WithContext(ctx).AutoMigrate(&statusConfig{}); err != nil {
		return fmt.Errorf("migrate sys_status_config: %w", err)
	}
	var count int64
	if err := s.dbc.WithContext(ctx).Model(&statusConfig{}).
		Where("id = ?", configRowID).Count(&count).Error; err != nil {
		return fmt.Errorf("seed sys_status_config: count: %w", err)
	}
	if count == 0 {
		seed := statusConfig{ID: configRowID}
		if err := s.dbc.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
			return fmt.Errorf("seed sys_status_config: insert: %w", err)
		}
	}
	return nil
}

// Get returns the current configuration.
func (s *Store) Get(ctx context.Context) (*Config, error) {
	var row statusConfig
	if err := s.dbc.WithContext(ctx).First(&row, configRowID).Error; err != nil {
		return nil, fmt.Errorf("get status config: %w", db.MapError(err))
	}
	return row.config()
}

// Update persists the configuration and returns the stored result.
func (s *Store) Update(ctx context.Context, cfg *Config) (*Config, error) {
	comps, err := sonic.Marshal(cfg.Components)
	if err != nil {
		return nil, fmt.Errorf("encode status components: %w", err)
	}
	// Select pins the write to the config columns (zero values included,
	// so Enabled=false persists); updated_at is set by autoUpdateTime.
	if err := s.dbc.WithContext(ctx).Model(&statusConfig{}).
		Where("id = ?", configRowID).
		Select("title", "description", "enabled", "components").
		Updates(statusConfig{
			Title:          cfg.Title,
			Description:    cfg.Description,
			Enabled:        cfg.Enabled,
			ComponentsJSON: string(comps),
		}).Error; err != nil {
		return nil, fmt.Errorf("update status config: %w", db.MapError(err))
	}
	return s.Get(ctx)
}

// config decodes the row into the typed configuration shape. An empty or
// unparsable component map degrades to no components; the row is written
// only by Update, which serializes with the same encoder.
func (r statusConfig) config() (*Config, error) {
	cfg := &Config{
		Title:       r.Title,
		Description: r.Description,
		Enabled:     r.Enabled,
	}
	if r.ComponentsJSON != "" {
		if err := sonic.Unmarshal([]byte(r.ComponentsJSON), &cfg.Components); err != nil {
			return nil, fmt.Errorf("decode status components: %w", err)
		}
	}
	return cfg, nil
}
