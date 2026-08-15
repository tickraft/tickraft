// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package asset

import (
	"context"
	"time"

	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/types"
)

// NoopStore is a no-op Store implementation. All read operations return
// errdefs.ErrNotFound; all write operations are discarded. It is useful as a
// default when no persistence is configured, for testing, or as a sentinel
// value when no real store is available.
type NoopStore struct{}

// Compile-time assertion that NoopStore satisfies Store.
var _ Store = NoopStore{}

// Migrate is a no-op and always returns nil.
func (NoopStore) Migrate(_ context.Context) error { return nil }

// Create is a no-op and always returns nil.
func (NoopStore) Create(_ context.Context, _ *Asset) error { return nil }

// Update is a no-op and always returns nil.
func (NoopStore) Update(_ context.Context, _ *Asset) error { return nil }

// GetByID always returns nil, errdefs.ErrNotFound because no assets are persisted.
func (NoopStore) GetByID(_ context.Context, _ int64) (*Asset, error) {
	return nil, errdefs.ErrNotFound
}

// GetByKey always returns nil, errdefs.ErrNotFound because no assets are persisted.
func (NoopStore) GetByKey(_ context.Context, _ int64, _ string) (*Asset, error) {
	return nil, errdefs.ErrNotFound
}

// UpdateStatus is a no-op and always returns nil.
func (NoopStore) UpdateStatus(_ context.Context, _ int64, _ types.AssetStatus, _ time.Time) error {
	return nil
}

// List always returns an empty page because no assets are persisted.
func (NoopStore) List(_ context.Context, _, _ int, _ ListFilter) ([]*Asset, int64, error) {
	return nil, 0, nil
}

// ListKeyset always returns an empty page because no assets are persisted.
func (NoopStore) ListKeyset(_ context.Context, _ pagination.PageRequest) (pagination.PageResult[*Asset], error) {
	return pagination.PageResult[*Asset]{}, nil
}

// Delete always returns errdefs.ErrNotFound because no assets are persisted.
func (NoopStore) Delete(_ context.Context, _ int64) error {
	return errdefs.ErrNotFound
}

// CountByType always returns 0 because no assets are persisted.
func (NoopStore) CountByStatus(_ context.Context) (map[string]int64, error) {
	return map[string]int64{}, nil
}

func (NoopStore) CountByType(_ context.Context, _ int64, _ types.AssetType) (int64, error) {
	return 0, nil
}

// ExistsByKey always returns false because no assets are persisted.
func (NoopStore) ExistsByKey(_ context.Context, _ string) (bool, error) {
	return false, nil
}
