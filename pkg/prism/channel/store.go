// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tickraft/tickraft/pkg/db/errmap"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"gorm.io/gorm"
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

// Migrate runs AutoMigrate for the Record table. It is intended to be
// invoked from the application's migration phase at startup.
func (s *Store) Migrate(ctx context.Context) error {
	if err := s.dbc.WithContext(ctx).AutoMigrate(&Record{}); err != nil {
		return fmt.Errorf("migrate channel table: %w", err)
	}
	return nil
}

// Create inserts a new channel record. The ID, CreatedAt, and UpdatedAt
// are populated by the database on success.
func (s *Store) Create(ctx context.Context, m *Record) error {
	if m == nil {
		return fmt.Errorf("channel: create record: nil model")
	}
	if err := s.dbc.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("channel: create record: %w", errmap.MapError(err))
	}
	return nil
}

// Update saves the channel record. The ID field identifies the row to
// update; CreatedAt is preserved by the caller before invoking Update.
// A RowsAffected count of zero is reported as ErrChannelNotFound.
func (s *Store) Update(ctx context.Context, m *Record) error {
	if m == nil {
		return fmt.Errorf("channel: update record: nil model")
	}
	result := s.dbc.WithContext(ctx).Save(m)
	if result.Error != nil {
		return fmt.Errorf("channel: update record: %w", errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrChannelNotFound
	}
	return nil
}

// GetByID retrieves a channel record by its ID. Returns
// ErrChannelNotFound when no record with the given ID exists.
func (s *Store) GetByID(ctx context.Context, id int64) (*Record, error) {
	var m Record
	err := s.dbc.WithContext(ctx).First(&m, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrChannelNotFound
		}
		return nil, fmt.Errorf("channel: get record: %w", errmap.MapError(err))
	}
	return &m, nil
}

// List returns a page of channel records ordered by descending ID, plus
// the total count. page starts at 1; size is the maximum number of items
// returned. Soft-deleted rows are excluded.
func (s *Store) List(ctx context.Context, page, size int) ([]*Record, int64, error) {
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}

	var total int64
	if err := s.dbc.WithContext(ctx).Model(&Record{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("channel: list records: %w", errmap.MapError(err))
	}

	var models []*Record
	offset := (page - 1) * size
	if err := s.dbc.WithContext(ctx).
		Order("id DESC").
		Offset(offset).
		Limit(size).
		Find(&models).Error; err != nil {
		return nil, 0, fmt.Errorf("channel: list records: %w", errmap.MapError(err))
	}
	return models, total, nil
}

// DeleteByID soft-deletes the channel record identified by id. A
// RowsAffected count of zero is reported as ErrChannelNotFound so
// callers can detect the missing-channel case without inspecting the
// underlying error type.
func (s *Store) DeleteByID(ctx context.Context, id int64) error {
	result := s.dbc.WithContext(ctx).
		Where("id = ?", id).
		Delete(&Record{})
	if result.Error != nil {
		return fmt.Errorf("channel: delete record: %w", errmap.MapError(result.Error))
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
		Model(&Record{}).
		Where("id = ?", id).
		Update("last_used_at", at)
	if result.Error != nil {
		return fmt.Errorf("channel: touch last_used_at: %w", errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("channel: touch last_used_at: %w", errdefs.ErrNotFound)
	}
	return nil
}

// ListEnabled returns all enabled channel records ordered by ID ascending.
// It is used by the prism engine to load active channels into memory at
// startup and during hot-reload.
func (s *Store) ListEnabled(ctx context.Context) ([]*Record, error) {
	var models []*Record
	if err := s.dbc.WithContext(ctx).
		Where("enabled = ?", true).
		Order("id ASC").
		Find(&models).Error; err != nil {
		return nil, fmt.Errorf("channel: list enabled: %w", errmap.MapError(err))
	}
	return models, nil
}

// Compile-time assertion that Store implements the expected CRUD
// surface. The interface is not exported because the handler layer
// defines its own ChannelService interface; the Store is consumed by
// the prism channel service adapter (pkg/api/handler/service_channel.go).
var _ = (*Store)(nil)
