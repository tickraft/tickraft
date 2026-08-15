// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package asset

import (
	"context"
	"time"

	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/types"
)

// ListFilter holds optional server-side filtering criteria for listing
// assets. A zero-value filter matches all assets.
type ListFilter struct {
	// Keyword matches assets whose name or asset_key contains the
	// (case-insensitive) substring. Empty matches all.
	Keyword string
	// AssetType filters by an exact asset type match. Empty matches all.
	AssetType string
	// Status filters by an exact status match. Empty matches all.
	Status string
}

// Store persists and queries Asset entities.
//
// This is the persistence port for the asset domain. The GORM-backed
// implementation lives in store.go (NewStore); the no-op default lives
// in noop.go (NoopStore). The callers provides its own
// implementation with multi-tenant filtering backed by the "biz_asset"
// table for the user-facing asset inventory.
//
// The interface includes the full method set (CRUD + management and quota
// helpers) so the asset management API handler can consume it directly
// without a separate wider interface. Implementations must be safe for
// concurrent use.
type Store interface {
	// Migrate creates or updates the assets table schema.
	Migrate(ctx context.Context) error
	// Create inserts a new asset.
	Create(ctx context.Context, a *Asset) error
	// Update updates an existing asset.
	Update(ctx context.Context, a *Asset) error
	// GetByID retrieves an asset by its ID.
	GetByID(ctx context.Context, id int64) (*Asset, error)
	// GetByKey retrieves an asset by its unique key within a tenant.
	GetByKey(ctx context.Context, tenantID int64, key string) (*Asset, error)
	// UpdateStatus updates the asset status and last active time.
	UpdateStatus(ctx context.Context, id int64, status types.AssetStatus, activeAt time.Time) error
	// List returns a page of assets matching the filter, ordered by
	// descending ID, plus the total count. page starts at 1; size is the
	// maximum number of items. A zero-value filter returns all assets.
	List(ctx context.Context, page, size int, filter ListFilter) ([]*Asset, int64, error)
	// ListKeyset returns a page of assets using keyset (cursor-based)
	// pagination, which avoids the O(N) cost of OFFSET on deep pages.
	// The opaque next-page cursor is returned in the result; pass it as
	// req.Cursor for the subsequent page. An empty cursor means the first
	// or last page.
	ListKeyset(ctx context.Context, req pagination.PageRequest) (pagination.PageResult[*Asset], error)
	// Delete permanently removes an asset by its ID.
	Delete(ctx context.Context, id int64) error
	// CountByType returns the number of assets of the given type for a
	// specific tenant. It is used to enforce default quotas.
	CountByType(ctx context.Context, tenantID int64, assetType types.AssetType) (int64, error)
	// CountByStatus returns the number of assets grouped by status. It is
	// used by the dashboard's asset status distribution.
	CountByStatus(ctx context.Context) (map[string]int64, error)
	// ExistsByKey returns true when an asset with the given key exists in
	// any tenant. It is used by the asset-key middleware to validate the
	// X-Tickraft-Asset-Key header without requiring a tenant context.
	ExistsByKey(ctx context.Context, key string) (bool, error)
}
