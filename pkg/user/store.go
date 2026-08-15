// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package user

import (
	"context"
	"github.com/tickraft/tickraft/pkg/auth/apikey"
	"time"

	"github.com/tickraft/tickraft/pkg/cache"
	"github.com/tickraft/tickraft/pkg/db/errmap"
	"gorm.io/gorm"
)

// store is the GORM-backed implementation of Store.
type store struct {
	dbc   *gorm.DB
	cache *cache.LRUCache // retained for API compatibility; see NewStore note
}

// NewStore creates a new Store backed by the given *gorm.DB and an optional
// cache.
//
// The cache parameter is accepted for backward compatibility but is NOT
// used for user lookups. User objects carry a PasswordHash field tagged
// json:"-" (to prevent hash leakage in API responses), which makes
// JSON-based cache serialization lossy: a round-tripped cached entry
// would have an empty PasswordHash, causing all subsequent password
// verifications to fail with "bcrypt: hashedSecret too short". Indexed
// database lookups by username or ID are fast enough that an in-memory
// cache is not warranted.
func NewStore(dbc *gorm.DB, c *cache.LRUCache) Store {
	return &store{dbc: dbc, cache: c}
}

// Compile-time assertion that store implements Store.
var _ Store = (*store)(nil)

// GetByUsername retrieves a user by username.
func (s *store) GetByUsername(ctx context.Context, username string) (*User, error) {
	var u User
	if err := s.dbc.WithContext(ctx).Where("username = ?", username).First(&u).Error; err != nil {
		return nil, errmap.MapError(err)
	}
	return &u, nil
}

// GetByID retrieves a user by ID.
func (s *store) GetByID(ctx context.Context, id int64) (*User, error) {
	var u User
	if err := s.dbc.WithContext(ctx).Where("id = ?", id).First(&u).Error; err != nil {
		return nil, errmap.MapError(err)
	}
	return &u, nil
}

// Create creates a new user and returns the new user ID.
func (s *store) Create(ctx context.Context, username, passwordHash, email string, role int64) (int64, error) {
	if err := ValidateUsername(username); err != nil {
		return 0, err
	}
	if err := ValidatePasswordHash(passwordHash); err != nil {
		return 0, err
	}
	if err := ValidateEmail(email); err != nil {
		return 0, err
	}

	u := User{
		Username:     username,
		PasswordHash: passwordHash,
		Email:        email,
		Role:         int(role),
	}

	if err := s.dbc.WithContext(ctx).Create(&u).Error; err != nil {
		return 0, errmap.MapError(err)
	}

	return u.ID, nil
}

// Update updates user fields specified in the data map.
func (s *store) Update(ctx context.Context, id int64, data map[string]interface{}) error {
	if err := ValidateID(id); err != nil {
		return err
	}

	result := s.dbc.WithContext(ctx).Model(&User{}).Where("id = ?", id).Updates(data)
	if result.Error != nil {
		return errmap.MapError(result.Error)
	}
	return nil
}

// UpdatePassword updates the user's password hash.
func (s *store) UpdatePassword(ctx context.Context, id int64, passwordHash string) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	if err := ValidatePasswordHash(passwordHash); err != nil {
		return err
	}

	result := s.dbc.WithContext(ctx).Model(&User{}).Where("id = ?", id).Update("password_hash", passwordHash)
	if result.Error != nil {
		return errmap.MapError(result.Error)
	}
	return nil
}

// Delete deletes a user by ID.
func (s *store) Delete(ctx context.Context, id int64) error {
	if err := ValidateID(id); err != nil {
		return err
	}

	if err := s.dbc.WithContext(ctx).Delete(&User{}, id).Error; err != nil {
		return errmap.MapError(err)
	}
	return nil
}

// List returns all users.
func (s *store) List(ctx context.Context) ([]User, error) {
	var users []User
	if err := s.dbc.WithContext(ctx).Find(&users).Error; err != nil {
		return nil, errmap.MapError(err)
	}
	return users, nil
}

// apiKeyStore is the GORM-backed implementation of APIKeyStore.
type apiKeyStore struct {
	dbc   *gorm.DB
	cache *cache.LRUCache // retained for API compatibility; see NewAPIKeyStore note
}

// NewAPIKeyStore creates a new APIKeyStore backed by the given *gorm.DB
// and an optional cache.
//
// The cache parameter is accepted for backward compatibility but is NOT
// used for API key lookups, for the same reason as NewStore: APIKey.KeyHash
// carries the json:"-" tag, which makes JSON-based cache serialization
// lossy.
func NewAPIKeyStore(dbc *gorm.DB, c *cache.LRUCache) APIKeyStore {
	return &apiKeyStore{dbc: dbc, cache: c}
}

// Compile-time assertion that apiKeyStore implements APIKeyStore.
var _ APIKeyStore = (*apiKeyStore)(nil)

// Create creates a new API key and returns the new key ID.
func (s *apiKeyStore) Create(ctx context.Context, name, keyPrefix, keyHash string, expiredAt *time.Time) (int64, error) {
	if err := ValidateAPIKeyName(name); err != nil {
		return 0, err
	}
	if err := ValidateKeyPrefix(keyPrefix); err != nil {
		return 0, err
	}
	if err := ValidateKeyHash(keyHash); err != nil {
		return 0, err
	}
	ak := APIKey{
		Name:      name,
		KeyPrefix: keyPrefix,
		KeyHash:   keyHash,
		Status:    1,
		ExpiredAt: expiredAt,
	}
	if err := s.dbc.WithContext(ctx).Create(&ak).Error; err != nil {
		return 0, errmap.MapError(err)
	}
	return ak.ID, nil
}

// List returns a page of API keys ordered by ascending ID together with the
// total count of rows. page is 1-based; size is the maximum number of rows
// returned. The caller is responsible for clamping size to an upper bound.
func (s *apiKeyStore) List(ctx context.Context, page, size int) ([]APIKey, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	offset := (page - 1) * size

	var total int64
	if err := s.dbc.WithContext(ctx).Model(&APIKey{}).Count(&total).Error; err != nil {
		return nil, 0, errmap.MapError(err)
	}

	var keys []APIKey
	if err := s.dbc.WithContext(ctx).
		Order("id").
		Limit(size).
		Offset(offset).
		Find(&keys).Error; err != nil {
		return nil, 0, errmap.MapError(err)
	}
	return keys, total, nil
}

// GetByHash retrieves an API key by its hash.
func (s *apiKeyStore) GetByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	var ak APIKey
	if err := s.dbc.WithContext(ctx).Where("key_hash = ?", keyHash).First(&ak).Error; err != nil {
		return nil, errmap.MapError(err)
	}
	return &ak, nil
}

// Revoke marks an API key as revoked by setting revoked_at and flipping
// status to disabled. Both fields are written together: the auth middleware
// validates the status field, so updating only revoked_at would leave the
// key authenticating indefinitely.
func (s *apiKeyStore) Revoke(ctx context.Context, id int64) error {
	var ak APIKey
	if err := s.dbc.WithContext(ctx).First(&ak, id).Error; err != nil {
		return errmap.MapError(err)
	}

	now := time.Now()
	if err := s.dbc.WithContext(ctx).Model(&APIKey{}).Where("id = ?", id).
		Updates(map[string]any{
			"revoked_at": now,
			"status":     apikey.StatusRevoked,
		}).Error; err != nil {
		return errmap.MapError(err)
	}
	return nil
}
