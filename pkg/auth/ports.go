// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth

import (
	"context"
	"time"
)

// Policy defines the permission checking strategy.
// The default implementation provides an RBAC policy via [DefaultPolicy].
type Policy interface {
	// Check returns whether the user with the given role is allowed to
	// perform the specified action on the asset type.
	Check(role int, action string, assetType string) bool
}

// BlacklistStore defines the persistence operations for the JWT token
// blacklist. Implementations must be safe for concurrent use.
//
// The interface lives in the auth domain because token revocation is an
// authentication concern. The GORM-backed implementation lives in this
// package (store.go, see NewBlacklistStore).
type BlacklistStore interface {
	// Add inserts a TokenBlacklist record for the given JTI and caches the
	// entry. The cache TTL should match the token's remaining lifetime so
	// that the entry is evicted automatically once it is stale.
	Add(ctx context.Context, jti string, expiredAt time.Time) error
	// Exists reports whether the given JTI has been revoked. It should check
	// the cache first, then fall back to the database.
	Exists(ctx context.Context, jti string) (bool, error)
	// CleanExpired removes all blacklist entries whose expired_at is before
	// now. It is invoked by the periodic maintenance sweep.
	CleanExpired(ctx context.Context) error
}
