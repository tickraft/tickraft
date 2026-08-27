// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/cache"
	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/user"
)

// blacklistStore is the GORM-backed implementation of BlacklistStore.
type blacklistStore struct {
	dbc   *gorm.DB
	cache *cache.LRUCache
}

// NewBlacklistStore creates a new BlacklistStore backed by the given *gorm.DB
// and optional cache. When c is nil, caching is disabled.
func NewBlacklistStore(dbc *gorm.DB, c *cache.LRUCache) BlacklistStore {
	return &blacklistStore{dbc: dbc, cache: c}
}

// Compile-time assertion that blacklistStore implements BlacklistStore.
var _ BlacklistStore = (*blacklistStore)(nil)

// Add inserts a TokenBlacklist record and caches the entry.
// The cache TTL matches the token's remaining lifetime.
func (s *blacklistStore) Add(ctx context.Context, jti string, expiredAt time.Time) error {
	if err := user.ValidateJTI(jti); err != nil {
		return err
	}
	entry := TokenBlacklist{
		TokenJTI:  jti,
		ExpiredAt: expiredAt,
	}
	if err := s.dbc.WithContext(ctx).Create(&entry).Error; err != nil {
		return db.MapError(err)
	}

	if s.cache != nil {
		ttl := time.Until(expiredAt)
		if ttl > 0 {
			cache.SetJSONWithTTL(ctx, s.cache, blacklistCacheKey(jti), true, ttl)
		}
	}

	return nil
}

// Exists checks whether a JTI exists in the blacklist.
// It checks the cache first, then falls back to the database.
func (s *blacklistStore) Exists(ctx context.Context, jti string) (bool, error) {
	if err := user.ValidateJTI(jti); err != nil {
		return false, err
	}
	if s.cache != nil {
		if found, ok := cache.GetJSON[bool](ctx, s.cache, blacklistCacheKey(jti)); ok {
			return found, nil
		}
	}

	var count int64
	err := s.dbc.WithContext(ctx).Model(&TokenBlacklist{}).
		Where("token_jti = ?", jti).
		Count(&count).Error
	if err != nil {
		return false, db.MapError(err)
	}

	if count > 0 {
		if s.cache != nil {
			cache.SetJSON(ctx, s.cache, blacklistCacheKey(jti), true)
		}
	}

	return count > 0, nil
}

// CleanExpired removes all blacklist entries whose expired_at is before now.
func (s *blacklistStore) CleanExpired(ctx context.Context) error {
	err := s.dbc.WithContext(ctx).
		Where("expired_at < ?", time.Now()).
		Delete(&TokenBlacklist{}).Error
	if err != nil {
		return db.MapError(err)
	}
	return nil
}

// blacklistCacheKey returns the cache key for a given JTI.
func blacklistCacheKey(jti string) string {
	return fmt.Sprintf("blacklist:jti:%s", jti)
}

// Migrate creates or updates the sys_token_blacklist table schema. It is
// intended to be called once during application startup by the composition
// layer and is safe to re-run: GORM AutoMigrate is idempotent (additive
// only).
func Migrate(ctx context.Context, dbc *gorm.DB) error {
	if err := dbc.WithContext(ctx).AutoMigrate(&TokenBlacklist{}); err != nil {
		return fmt.Errorf("auth: migrate tables: %w", err)
	}
	return nil
}
