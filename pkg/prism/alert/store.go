// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/tickraft/tickraft/pkg/db/errmap"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"gorm.io/gorm"
)

// recordStore is the GORM-backed implementation of RecordStore,
// accessing the database through a *gorm.DB.
type recordStore struct {
	dbc *gorm.DB
}

// NewRecordStore creates a new RecordStore backed by the given
// *gorm.DB.
func NewRecordStore(dbc *gorm.DB) *recordStore {
	return &recordStore{dbc: dbc}
}

// Create inserts a new alert record. The ID and CreatedAt are populated by
// the database on success.
func (s *recordStore) Create(ctx context.Context, m *Record) error {
	if m == nil {
		return fmt.Errorf("alert: create record: nil model")
	}
	if err := s.dbc.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("alert: create record: %w", errmap.MapError(err))
	}
	return nil
}

// CreateBatch inserts multiple alert records in a single DB round-trip,
// reducing per-violation INSERT overhead for events carrying multiple
// violations. Empty slices are a no-op.
func (s *recordStore) CreateBatch(ctx context.Context, models []*Record) error {
	if len(models) == 0 {
		return nil
	}
	if err := s.dbc.WithContext(ctx).CreateInBatches(models, 100).Error; err != nil {
		return fmt.Errorf("alert: create records batch: %w", errmap.MapError(err))
	}
	return nil
}

// GetByID retrieves an alert record by its ID. Returns errdefs.ErrNotFound
// when no record with the given ID exists.
func (s *recordStore) GetByID(ctx context.Context, id int64) (*Record, error) {
	var m Record
	if err := s.dbc.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, fmt.Errorf("alert: get record: %w", errmap.MapError(err))
	}
	return &m, nil
}

// List returns a page of alert records matching the filter, ordered by
// descending ID, plus the total count. page starts at 1; size is the maximum
// number of items. A zero-value filter returns all records.
func (s *recordStore) List(ctx context.Context, page, size int, filter RecordFilter) ([]*Record, int64, error) {
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}

	query := s.dbc.WithContext(ctx).Model(&Record{})
	if filter.Severity != "" {
		query = query.Where("severity = ?", filter.Severity)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if !filter.From.IsZero() {
		query = query.Where("triggered_at >= ?", filter.From)
	}
	if !filter.To.IsZero() {
		query = query.Where("triggered_at <= ?", filter.To)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("alert: list records: %w", errmap.MapError(err))
	}

	var models []*Record
	offset := (page - 1) * size
	if err := query.
		Order("id DESC").
		Offset(offset).
		Limit(size).
		Find(&models).Error; err != nil {
		return nil, 0, fmt.Errorf("alert: list records: %w", errmap.MapError(err))
	}
	return models, total, nil
}

// Acknowledge transitions the alert record identified by id to the
// "acknowledged" status and sets acknowledged_at to the current time. It
// returns the updated record. Returns errdefs.ErrNotFound when no record
// with the given ID exists.
func (s *recordStore) Acknowledge(ctx context.Context, id int64) (*Record, error) {
	now := time.Now()
	result := s.dbc.WithContext(ctx).
		Model(&Record{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":          "acknowledged",
			"acknowledged_at": now,
		})
	if result.Error != nil {
		return nil, fmt.Errorf("alert: acknowledge record: %w", errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("alert: acknowledge record: %w", errdefs.ErrNotFound)
	}

	var m Record
	if err := s.dbc.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, fmt.Errorf("alert: acknowledge record: %w", errmap.MapError(err))
	}
	return &m, nil
}

// Resolve transitions the alert record identified by id to the "resolved"
// status and sets resolved_at to the current time. It returns the updated
// record. Returns errdefs.ErrNotFound when no record with the given ID
// exists.
func (s *recordStore) Resolve(ctx context.Context, id int64) (*Record, error) {
	now := time.Now()
	result := s.dbc.WithContext(ctx).
		Model(&Record{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":      "resolved",
			"resolved_at": now,
		})
	if result.Error != nil {
		return nil, fmt.Errorf("alert: resolve record: %w", errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("alert: resolve record: %w", errdefs.ErrNotFound)
	}

	var m Record
	if err := s.dbc.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, fmt.Errorf("alert: resolve record: %w", errmap.MapError(err))
	}
	return &m, nil
}

// Compile-time assertion that recordStore implements RecordStore.
var _ RecordStore = (*recordStore)(nil)

// Migrate creates or updates the sys_prism_record table schema. It is
// intended to be called once during application startup. The sys_prism_rule
// table is migrated by the rule package's Store.Migrate.
func Migrate(ctx context.Context, dbc *gorm.DB) error {
	if err := dbc.WithContext(ctx).AutoMigrate(&Record{}); err != nil {
		return fmt.Errorf("alert: migrate tables: %w", err)
	}
	return nil
}
