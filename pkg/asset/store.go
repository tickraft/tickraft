// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package asset

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/tickraft/tickraft/pkg/db/errmap"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/types"
	"gorm.io/gorm"
)

// store implements Store backed by a GORM database connection. It persists
// Asset entities to the "assets" table using GORM's default naming
// convention.
type store struct {
	dbc *gorm.DB
}

// NewStore creates a new Store backed by the given GORM database.
func NewStore(dbc *gorm.DB) Store {
	return &store{dbc: dbc}
}

// Migrate creates or updates the assets table schema.
func (s *store) Migrate(ctx context.Context) error {
	if err := s.dbc.WithContext(ctx).AutoMigrate(&Asset{}); err != nil {
		return fmt.Errorf("asset: auto migrate: %w", err)
	}
	return nil
}

// Create inserts a new asset.
func (s *store) Create(ctx context.Context, a *Asset) error {
	if err := s.dbc.WithContext(ctx).Create(a).Error; err != nil {
		return fmt.Errorf("asset: create: %w", errmap.MapError(err))
	}
	return nil
}

// Update updates an existing asset.
func (s *store) Update(ctx context.Context, a *Asset) error {
	if err := s.dbc.WithContext(ctx).Save(a).Error; err != nil {
		return fmt.Errorf("asset: update: %w", errmap.MapError(err))
	}
	return nil
}

// GetByID retrieves an asset by its ID.
func (s *store) GetByID(ctx context.Context, id int64) (*Asset, error) {
	var a Asset
	if err := s.dbc.WithContext(ctx).First(&a, id).Error; err != nil {
		return nil, fmt.Errorf("asset: get by id: %w", errmap.MapError(err))
	}
	return &a, nil
}

// GetByKey retrieves an asset by its unique key within a tenant.
func (s *store) GetByKey(ctx context.Context, tenantID int64, key string) (*Asset, error) {
	var a Asset
	if err := s.dbc.WithContext(ctx).
		Where("tenant_id = ? AND asset_key = ?", tenantID, key).
		First(&a).Error; err != nil {
		return nil, fmt.Errorf("asset: get by key: %w", errmap.MapError(err))
	}
	return &a, nil
}

// UpdateStatus updates the asset status and last active time.
func (s *store) UpdateStatus(ctx context.Context, id int64, status types.AssetStatus, activeAt time.Time) error {
	result := s.dbc.WithContext(ctx).
		Model(&Asset{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":         string(status),
			"last_active_at": activeAt,
		})
	if result.Error != nil {
		return fmt.Errorf("asset: update status: %w", errmap.MapError(result.Error))
	}
	return nil
}

// List returns a page of assets matching the filter, ordered by descending
// ID, plus the total count. page starts at 1; size is clamped between 1 and
// 100. A zero-value filter returns all assets.
func (s *store) List(ctx context.Context, page, size int, filter ListFilter) ([]*Asset, int64, error) {
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}

	query := s.dbc.WithContext(ctx).Model(&Asset{})
	if filter.Keyword != "" {
		like := "%" + filter.Keyword + "%"
		query = query.Where("name LIKE ? OR asset_key LIKE ?", like, like)
	}
	if filter.AssetType != "" {
		query = query.Where("asset_type = ?", filter.AssetType)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("asset: list: %w", errmap.MapError(err))
	}

	var assets []*Asset
	offset := (page - 1) * size
	if err := query.
		Order("id DESC").
		Offset(offset).
		Limit(size).
		Find(&assets).Error; err != nil {
		return nil, 0, fmt.Errorf("asset: list: %w", errmap.MapError(err))
	}
	return assets, total, nil
}

// assetKeysetCursor is the keyset descriptor for asset list queries:
// ordered by descending id, which is indexed and monotonic, making it
// ideal for index range scans.
var assetKeysetCursor = pagination.Cursor{Column: "id", Direction: pagination.Desc}

// ListKeyset returns a page of assets using keyset (cursor-based)
// pagination. It replaces OFFSET with an index range scan on the id
// column, so the cost is constant regardless of how deep the cursor is.
//
// The total count is still returned for UI pagination controls; callers
// that do not need it should prefer ignoring the field rather than
// avoiding this method, since the count is a single indexed COUNT.
func (s *store) ListKeyset(ctx context.Context, req pagination.PageRequest) (pagination.PageResult[*Asset], error) {
	size := pagination.ClampSize(req.Size)

	var total int64
	if err := s.dbc.WithContext(ctx).Model(&Asset{}).Count(&total).Error; err != nil {
		return pagination.PageResult[*Asset]{}, fmt.Errorf("asset: list keyset: %w", errmap.MapError(err))
	}

	q, err := pagination.Apply(s.dbc.WithContext(ctx).Model(&Asset{}), req, assetKeysetCursor)
	if err != nil {
		return pagination.PageResult[*Asset]{}, fmt.Errorf("asset: list keyset: %w", err)
	}

	var assets []*Asset
	if err := q.Find(&assets).Error; err != nil {
		return pagination.PageResult[*Asset]{}, fmt.Errorf("asset: list keyset: %w", errmap.MapError(err))
	}

	next, err := pagination.NextCursorForSize(assetKeysetCursor, assets, size, func(a *Asset) string {
		return strconv.FormatInt(a.ID, 10)
	})
	if err != nil {
		return pagination.PageResult[*Asset]{}, fmt.Errorf("asset: list keyset: %w", err)
	}

	return pagination.PageResult[*Asset]{
		Items:      assets,
		Total:      total,
		NextCursor: next,
	}, nil
}

// Delete permanently removes an asset by its ID. Returns
// errdefs.ErrNotFound when no asset with the given ID exists.
func (s *store) Delete(ctx context.Context, id int64) error {
	result := s.dbc.WithContext(ctx).Delete(&Asset{}, id)
	if result.Error != nil {
		return fmt.Errorf("asset: delete: %w", errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("asset: delete: %w", errdefs.ErrNotFound)
	}
	return nil
}

// CountByType returns the number of assets of the given type for a
// specific tenant. It is used by the asset management API to enforce
// default quotas before creating new assets.
func (s *store) CountByType(ctx context.Context, tenantID int64, assetType types.AssetType) (int64, error) {
	var count int64
	if err := s.dbc.WithContext(ctx).
		Model(&Asset{}).
		Where("tenant_id = ? AND asset_type = ?", tenantID, string(assetType)).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("asset: count by type: %w", errmap.MapError(err))
	}
	return count, nil
}

// CountByStatus returns the number of assets grouped by status. It is
// used by the dashboard's asset status distribution.
func (s *store) CountByStatus(ctx context.Context) (map[string]int64, error) {
	var rows []struct {
		Status string
		Count  int64
	}
	if err := s.dbc.WithContext(ctx).
		Model(&Asset{}).
		Select("status, COUNT(*) AS count").
		Group("status").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("asset: count by status: %w", errmap.MapError(err))
	}
	result := make(map[string]int64, len(rows))
	for _, r := range rows {
		result[r.Status] = r.Count
	}
	return result, nil
}

// ExistsByKey returns true when an asset with the given key exists in any
// tenant. It is used by the asset-key middleware to validate the
// X-Tickraft-Asset-Key header without requiring a tenant context.
func (s *store) ExistsByKey(ctx context.Context, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	var count int64
	if err := s.dbc.WithContext(ctx).
		Model(&Asset{}).
		Where("asset_key = ?", key).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("asset: exists by key: %w", errmap.MapError(err))
	}
	return count > 0, nil
}

// Compile-time assertion that store implements Store.
var _ Store = (*store)(nil)
