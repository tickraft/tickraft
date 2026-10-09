// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/cache"
)

// newBlacklistTestStore builds a cached blacklist store over an in-memory
// SQLite database.
func newBlacklistTestStore(t *testing.T) BlacklistStore {
	t.Helper()
	dbc, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := dbc.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	if err := Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return NewBlacklistStore(dbc, cache.NewLRU(128, time.Minute))
}

func TestBlacklistStore_AddAndExists(t *testing.T) {
	s := newBlacklistTestStore(t)
	ctx := context.Background()

	if exists, err := s.Exists(ctx, "jti-a"); err != nil || exists {
		t.Fatalf("Exists(unblacklisted) = (%v, %v), want (false, nil)", exists, err)
	}
	if err := s.Add(ctx, "jti-a", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if exists, err := s.Exists(ctx, "jti-a"); err != nil || !exists {
		t.Fatalf("Exists(blacklisted) = (%v, %v), want (true, nil)", exists, err)
	}
}

func TestBlacklistStore_AddOverwritesNegativeCache(t *testing.T) {
	// A miss is cached as a negative verdict; a subsequent Add through the
	// same store must overwrite it so revocation takes effect immediately.
	s := newBlacklistTestStore(t)
	ctx := context.Background()

	if exists, err := s.Exists(ctx, "jti-b"); err != nil || exists {
		t.Fatalf("Exists(before Add) = (%v, %v), want (false, nil)", exists, err)
	}
	if err := s.Add(ctx, "jti-b", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if exists, err := s.Exists(ctx, "jti-b"); err != nil || !exists {
		t.Fatalf("Exists(after Add) = (%v, %v), want (true, nil)", exists, err)
	}
}

func TestBlacklistStore_CleanExpired(t *testing.T) {
	s := newBlacklistTestStore(t)
	ctx := context.Background()

	if err := s.Add(ctx, "jti-stale", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := s.CleanExpired(ctx); err != nil {
		t.Fatalf("CleanExpired() error = %v", err)
	}
	// The expired row is gone and no cached verdict resurrects it.
	if exists, err := s.Exists(ctx, "jti-stale"); err != nil || exists {
		t.Fatalf("Exists(after CleanExpired) = (%v, %v), want (false, nil)", exists, err)
	}
}
