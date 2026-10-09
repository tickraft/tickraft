// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrate(t *testing.T) {
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

	if !dbc.Migrator().HasTable(&TokenBlacklist{}) {
		t.Error("TokenBlacklist table was not created")
	}

	// Running Migrate twice should not error.
	if err := Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}

	// Insert a blacklist entry and verify it can be queried.
	entry := TokenBlacklist{
		TokenJTI:  "test-jti",
		ExpiredAt: time.Now().Add(time.Minute),
	}
	if err := dbc.Create(&entry).Error; err != nil {
		t.Fatalf("insert blacklist entry: %v", err)
	}

	var count int64
	if err := dbc.Model(&TokenBlacklist{}).Where("token_jti = ?", "test-jti").Count(&count).Error; err != nil {
		t.Fatalf("query blacklist entry: %v", err)
	}
	if count != 1 {
		t.Errorf("blacklist entry count = %d, want 1", count)
	}
}
