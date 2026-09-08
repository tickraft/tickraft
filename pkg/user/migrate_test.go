// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package user

import (
	"context"
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newMigrateTestDB(t *testing.T) *gorm.DB {
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
	return dbc
}

func TestMigrate(t *testing.T) {
	dbc := newMigrateTestDB(t)

	if err := Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	// Verify tables exist by checking the migrator directly.
	if !dbc.Migrator().HasTable(&User{}) {
		t.Error("User table was not created")
	}
	if !dbc.Migrator().HasTable(&APIKey{}) {
		t.Error("APIKey table was not created")
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	dbc := newMigrateTestDB(t)

	// Running Migrate twice should not error.
	if err := Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("first Migrate() error = %v", err)
	}

	if err := Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
}

func TestMigrate_CanInsertData(t *testing.T) {
	dbc := newMigrateTestDB(t)

	if err := Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	// Insert a user and verify it can be queried.
	u := User{
		Username:     "testuser",
		PasswordHash: "$2a$10$hash",
		Role:         1,
	}
	if err := dbc.Create(&u).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var fetched User
	if err := dbc.Where("username = ?", "testuser").First(&fetched).Error; err != nil {
		t.Fatalf("query user: %v", err)
	}
	if fetched.Username != "testuser" {
		t.Errorf("fetched username = %q, want %q", fetched.Username, "testuser")
	}
}

func TestMigrate_Incremental(t *testing.T) {
	dbc := newMigrateTestDB(t)

	// First migration creates tables.
	if err := Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("first Migrate() error = %v", err)
	}

	// Insert data after the first migration.
	u := User{
		Username:     "persist_user",
		PasswordHash: "$2a$10$hash",
		Email:        "persist@example.com",
		Role:         1,
	}
	if err := dbc.Create(&u).Error; err != nil {
		t.Fatalf("insert user after first migration: %v", err)
	}

	// Second migration (simulating model changes / schema evolution)
	// should not break existing data.
	if err := Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}

	// Verify previously inserted data is still intact.
	var fetchedUser User
	if err := dbc.Where("username = ?", "persist_user").First(&fetchedUser).Error; err != nil {
		t.Fatalf("query user after second migration: %v", err)
	}
	if fetchedUser.Username != "persist_user" {
		t.Errorf("username after incremental migration = %q, want %q", fetchedUser.Username, "persist_user")
	}

	// Verify new data can still be inserted after the incremental migration.
	newUser := User{
		Username:     "post_migration_user",
		PasswordHash: "$2a$10$hash",
		Email:        "post@example.com",
		Role:         2,
	}
	if err := dbc.Create(&newUser).Error; err != nil {
		t.Fatalf("insert user after second migration: %v", err)
	}
}

func TestStoreCreate_EmailUniqueness(t *testing.T) {
	dbc := newMigrateTestDB(t)
	if err := Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	s := NewStore(dbc)

	// The optional email column carries no unique index: any number of
	// users may have no email at all.
	if _, err := s.Create(context.Background(), "alice", "$2a$10$hash", "", 1); err != nil {
		t.Fatalf("create user without email: %v", err)
	}
	if _, err := s.Create(context.Background(), "bob", "$2a$10$hash", "", 1); err != nil {
		t.Fatalf("create second user without email: %v", err)
	}

	// A non-empty email is enforced unique by the store layer.
	if _, err := s.Create(context.Background(), "carol", "$2a$10$hash", "dup@example.com", 1); err != nil {
		t.Fatalf("create user with email: %v", err)
	}
	_, err := s.Create(context.Background(), "dave", "$2a$10$hash", "dup@example.com", 1)
	if !errors.Is(err, ErrEmailExists) {
		t.Fatalf("duplicate email: got %v, want ErrEmailExists", err)
	}
}
