// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/pagination"
)

// ErrChannelNotFound is returned when a channel cannot be located by its ID.
var ErrChannelNotFound = errors.New("channel: not found")

// Store is the GORM-backed channel persistence layer. It provides CRUD
// operations for notification channel definitions stored in the
// sys_prism_channel table.
type Store struct {
	dbc *gorm.DB
}

// NewStore creates a Store backed by the given *gorm.DB.
func NewStore(dbc *gorm.DB) *Store {
	return &Store{dbc: dbc}
}

// Migrate runs AutoMigrate for the Channel table. It is intended to be
// invoked from the application's migration phase at startup.
func (s *Store) Migrate(ctx context.Context) error {
	if err := s.dbc.WithContext(ctx).AutoMigrate(&Channel{}); err != nil {
		return fmt.Errorf("migrate channel table: %w", err)
	}
	return nil
}

// Create inserts a new channel. The ID, CreatedAt, and UpdatedAt
// are populated by the database on success.
func (s *Store) Create(ctx context.Context, m *Channel) error {
	if m == nil {
		return fmt.Errorf("channel: create channel: nil model")
	}
	if err := s.dbc.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("channel: create channel: %w", db.MapError(err))
	}
	return nil
}

// recordUpdateColumns lists the user-editable columns touched by Update.
// Engine-owned state (last_used_at) and lifecycle fields (tenant_id,
// created_at, deleted_at) are deliberately absent so a request-bound model
// can never clear them.
var recordUpdateColumns = []string{
	"name", "type", "config", "enabled", "updated_at",
}

// Update applies a column-level update limited to the user-editable
// columns. The ID field identifies the row to update; engine-owned state
// (last_used_at) is never touched, so a PUT cannot reset the channel's
// usage bookkeeping. A RowsAffected count of zero is reported as
// ErrChannelNotFound.
func (s *Store) Update(ctx context.Context, ch *Channel) error {
	if ch == nil {
		return fmt.Errorf("channel: update channel: nil model")
	}
	result := s.dbc.WithContext(ctx).
		Model(&Channel{}).
		Where("id = ?", ch.ID).
		Select(recordUpdateColumns).
		Updates(ch)
	if result.Error != nil {
		return fmt.Errorf("channel: update channel: %w", db.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrChannelNotFound
	}
	return nil
}

// GetByID retrieves a channel by its ID. Returns
// ErrChannelNotFound when no channel with the given ID exists.
func (s *Store) GetByID(ctx context.Context, id int64) (*Channel, error) {
	var ch Channel
	if err := s.dbc.WithContext(ctx).First(&ch, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrChannelNotFound
		}
		return nil, fmt.Errorf("channel: get channel: %w", db.MapError(err))
	}
	return &ch, nil
}

// List returns a page of channels ordered by descending ID, plus
// the total count. page starts at 1; size is the maximum number of items
// returned. Soft-deleted rows are excluded.
func (s *Store) List(ctx context.Context, page, size int) ([]*Channel, int64, error) {
	page, size = pagination.Clamp(page, size)

	var total int64
	if err := s.dbc.WithContext(ctx).Model(&Channel{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("channel: list channels: %w", db.MapError(err))
	}

	var channels []*Channel
	offset := (page - 1) * size
	if err := s.dbc.WithContext(ctx).
		Order("id DESC").
		Offset(offset).
		Limit(size).
		Find(&channels).Error; err != nil {
		return nil, 0, fmt.Errorf("channel: list channels: %w", db.MapError(err))
	}
	return channels, total, nil
}

// DeleteByID soft-deletes the channel identified by id. A
// RowsAffected count of zero is reported as ErrChannelNotFound so
// callers can detect the missing-channel case without inspecting the
// underlying error type.
func (s *Store) DeleteByID(ctx context.Context, id int64) error {
	result := s.dbc.WithContext(ctx).
		Where("id = ?", id).
		Delete(&Channel{})
	if result.Error != nil {
		return fmt.Errorf("channel: delete channel: %w", db.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrChannelNotFound
	}
	return nil
}

// TouchLastUsedAt sets last_used_at to the given time for the channel
// identified by id. It is intended to be called by the prism engine
// after a successful notification delivery. A RowsAffected count of
// zero is reported as errdefs.ErrNotFound so callers can detect a
// missing channel without a separate GetByID round trip.
func (s *Store) TouchLastUsedAt(ctx context.Context, id int64, at time.Time) error {
	result := s.dbc.WithContext(ctx).
		Model(&Channel{}).
		Where("id = ?", id).
		Update("last_used_at", at)
	if result.Error != nil {
		return fmt.Errorf("channel: touch last_used_at: %w", db.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("channel: touch last_used_at: %w", errdefs.ErrNotFound)
	}
	return nil
}

// ListEnabled returns all enabled channels ordered by ID ascending.
// It is used by the prism engine to load active channels into memory at
// startup and during hot-reload.
func (s *Store) ListEnabled(ctx context.Context) ([]*Channel, error) {
	var models []*Channel
	if err := s.dbc.WithContext(ctx).
		Where("enabled = ?", true).
		Order("id ASC").
		Find(&models).Error; err != nil {
		return nil, fmt.Errorf("channel: list enabled: %w", db.MapError(err))
	}
	return models, nil
}
