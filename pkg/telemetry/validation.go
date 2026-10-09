// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/cache"
)

// Validation limits for incoming telemetry.
const (
	// MaxMetricsPerReport caps the number of metric data points in a single telemetry.
	MaxMetricsPerReport = 1000
	// MaxLogsPerReport caps the number of log entries in a single telemetry.
	MaxLogsPerReport = 500
	// MaxLogBodyBytes caps the size of a single log body in bytes (64 KB).
	MaxLogBodyBytes = 64 * 1024

	// DefaultValidatorCacheTTL is the default TTL for cached asset lookups.
	DefaultValidatorCacheTTL = 5 * time.Minute
	// DefaultValidatorCacheSize is the default maximum number of cached assets.
	DefaultValidatorCacheSize = 1024
)

// Validator validates incoming telemetry before it enters the processing pipeline.
// It enforces asset existence, tenant ownership, and data size limits.
//
// Asset lookups are cached in an LRU cache with a configurable TTL to
// reduce database load on high-volume telemetry streams. Use InvalidateAsset
// to evict a cached entry when the underlying asset is updated or deleted.
type Validator struct {
	assetStore asset.Store
	cache      *cache.LRUCache
	logger     *zap.Logger
}

// NewValidator creates a new Validator backed by the given asset store.
// If store is nil, asset existence and tenant checks are skipped.
//
// The validator is configured with the default cache size and TTL. Use
// NewValidatorWithCache to customize these parameters.
func NewValidator(store asset.Store, logger *zap.Logger) *Validator {
	return NewValidatorWithCache(store, DefaultValidatorCacheSize, DefaultValidatorCacheTTL, logger)
}

// NewValidatorWithCache creates a Validator with a custom asset cache.
// maxEntries controls the LRU cache capacity; ttl controls per-entry expiration.
// Non-positive values fall back to the defaults.
func NewValidatorWithCache(store asset.Store, maxEntries int, ttl time.Duration, logger *zap.Logger) *Validator {
	if logger == nil {
		logger = zap.NewNop()
	}
	if maxEntries <= 0 {
		maxEntries = DefaultValidatorCacheSize
	}
	if ttl <= 0 {
		ttl = DefaultValidatorCacheTTL
	}
	return &Validator{
		assetStore: store,
		cache:      cache.NewLRU(maxEntries, ttl),
		logger:     logger,
	}
}

// InvalidateAsset evicts the cached entry for the given asset, if any.
// The engine should call this when an asset is updated or deleted so that
// subsequent telemetry re-fetches the latest state from the store.
func (v *Validator) InvalidateAsset(ctx context.Context, assetID int64) {
	if v.cache == nil {
		return
	}
	v.cache.Delete(ctx, assetCacheKey(assetID))
}

// assetCacheKey builds the cache key for an asset ID.
// strconv.FormatInt is used instead of fmt.Sprintf for performance.
func assetCacheKey(assetID int64) string {
	return strconv.FormatInt(assetID, 10)
}

// Validate checks a telemetry for structural and contextual validity.
// It verifies:
//   - AssetID is positive
//   - AssetType is non-empty
//   - The asset exists in the store (when a store is configured)
//   - The telemetry tenant matches the asset tenant
//   - Metrics count does not exceed MaxMetricsPerReport
//   - Log body size does not exceed MaxLogBodyBytes
func (v *Validator) Validate(ctx context.Context, t *Telemetry) error {
	if t == nil {
		return fmt.Errorf("%w: telemetry is nil", ErrValidationFailed)
	}

	// Structural checks always run.
	if t.AssetID <= 0 {
		return fmt.Errorf("%w: asset id must be positive, got %d", ErrValidationFailed, t.AssetID)
	}
	if t.AssetType == "" {
		return fmt.Errorf("%w: asset type is empty", ErrValidationFailed)
	}
	if len(t.Metrics) > MaxMetricsPerReport {
		return fmt.Errorf("%w: metrics count %d exceeds limit %d",
			ErrMetricLimitExceeded, len(t.Metrics), MaxMetricsPerReport)
	}
	if len(t.LogContent) > MaxLogBodyBytes {
		return fmt.Errorf("%w: log body size %d exceeds limit %d",
			ErrLogLimitExceeded, len(t.LogContent), MaxLogBodyBytes)
	}

	// Store-based checks require a configured asset store.
	if v.assetStore == nil {
		return nil
	}

	tenantID, err := v.loadAssetTenant(ctx, t.AssetID)
	if err != nil {
		return fmt.Errorf("%w: asset %d: %w", ErrAssetNotFound, t.AssetID, err)
	}
	if tenantID != t.TenantID {
		return fmt.Errorf("%w: telemetry tenant %d does not match asset tenant %d",
			ErrTenantMismatch, t.TenantID, tenantID)
	}

	return nil
}

// cachedAsset is the cache snapshot of an asset lookup. The validator
// caches this minimal struct instead of the full asset.Asset because the
// cache serializes entries as JSON, and asset fields guarded by json:"-"
// (tenant_id) would silently drop to zero on every cache hit.
type cachedAsset struct {
	TenantID int64 `json:"tenant_id"`
}

// loadAssetTenant fetches the owning tenant of an asset by ID, using the
// LRU cache to avoid repeated database queries for the same asset. On a
// cache miss the asset is loaded from the store and its tenant cached for
// subsequent lookups.
func (v *Validator) loadAssetTenant(ctx context.Context, assetID int64) (int64, error) {
	key := assetCacheKey(assetID)

	// Fast path: serve from cache.
	if cached, ok := cache.GetJSON[cachedAsset](ctx, v.cache, key); ok {
		return cached.TenantID, nil
	}

	// Slow path: query the store and cache the result.
	a, err := v.assetStore.GetByID(ctx, assetID)
	if err != nil {
		return 0, err
	}
	if a == nil {
		return 0, fmt.Errorf("asset store returned nil asset")
	}
	cache.SetJSON(ctx, v.cache, key, cachedAsset{TenantID: a.TenantID})
	return a.TenantID, nil
}
