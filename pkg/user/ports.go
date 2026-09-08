// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package user

import (
	"context"
	"time"
)

// Store provides user data access operations.
//
// This is the persistence port for the user domain. The GORM-backed
// implementation lives in store.go (NewStore); the callers
// defines its own implementation in internal/store with multi-tenant
// filtering and additional methods such as UpdateLastLoginAt.
//
// Implementations must be safe for concurrent use. The runtime is
// single-tenant: there is no tenant-scoped access path.
type Store interface {
	// GetByUsername retrieves a user by username.
	GetByUsername(ctx context.Context, username string) (*User, error)
	// GetByID retrieves a user by ID.
	GetByID(ctx context.Context, id int64) (*User, error)
	// Create creates a new user and returns the new user ID.
	Create(ctx context.Context, username, passwordHash, email string, role int64) (int64, error)
	// Update updates user fields specified in the data map.
	Update(ctx context.Context, id int64, data map[string]any) error
	// UpdatePassword updates the user's password hash.
	UpdatePassword(ctx context.Context, id int64, passwordHash string) error
	// Delete deletes a user by ID.
	Delete(ctx context.Context, id int64) error
	// List returns all users.
	List(ctx context.Context) ([]User, error)
}

// APIKeyStore provides API key data access operations.
//
// This is the persistence port for the API key domain. The GORM-backed
// implementation lives in store.go (NewAPIKeyStore); the callers
// repository defines its own implementation in internal/store with
// multi-tenant filtering.
//
// Implementations must be safe for concurrent use.
type APIKeyStore interface {
	// Create creates a new API key and returns the new key ID.
	Create(ctx context.Context, name, keyPrefix, keyHash string, expiredAt *time.Time) (int64, error)
	// List returns a page of API keys ordered by ID together with the total
	// count. page is 1-based and size is the maximum number of rows returned.
	// keyword restricts the result to keys whose name contains it; an empty
	// keyword matches all keys.
	List(ctx context.Context, page, size int, keyword string) ([]APIKey, int64, error)
	// GetByHash retrieves an API key by its hash.
	GetByHash(ctx context.Context, keyHash string) (*APIKey, error)
	// Revoke marks an API key as revoked by setting revoked_at to the
	// current time.
	Revoke(ctx context.Context, id int64) error
}
