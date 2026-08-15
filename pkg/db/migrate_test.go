// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/auth/password"
	"github.com/tickraft/tickraft/pkg/user"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
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

func TestAutoMigrate(t *testing.T) {
	dbc := newTestDB(t)

	err := AutoMigrate(context.Background(), dbc)
	if err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	// Verify tables exist by attempting to insert and query each model.
	if !dbc.Migrator().HasTable(&user.User{}) {
		t.Error("User table was not created")
	}
	if !dbc.Migrator().HasTable(&user.APIKey{}) {
		t.Error("APIKey table was not created")
	}
	if !dbc.Migrator().HasTable(&auth.TokenBlacklist{}) {
		t.Error("TokenBlacklist table was not created")
	}
}

func TestAutoMigrate_Idempotent(t *testing.T) {
	dbc := newTestDB(t)

	// Running AutoMigrate twice should not error.
	err := AutoMigrate(context.Background(), dbc)
	if err != nil {
		t.Fatalf("first AutoMigrate() error = %v", err)
	}

	err = AutoMigrate(context.Background(), dbc)
	if err != nil {
		t.Fatalf("second AutoMigrate() error = %v", err)
	}
}

func TestAutoMigrate_CanInsertData(t *testing.T) {
	dbc := newTestDB(t)

	err := AutoMigrate(context.Background(), dbc)
	if err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	// Insert a user and verify it can be queried.
	u := user.User{
		Username:     "testuser",
		PasswordHash: "$2a$10$hash",
		Role:         1,
	}
	if err := dbc.Create(&u).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var fetched user.User
	if err := dbc.Where("username = ?", "testuser").First(&fetched).Error; err != nil {
		t.Fatalf("query user: %v", err)
	}
	if fetched.Username != "testuser" {
		t.Errorf("fetched username = %q, want %q", fetched.Username, "testuser")
	}
}

func TestAutoMigrate_Incremental(t *testing.T) {
	dbc := newTestDB(t)

	// First migration creates tables.
	err := AutoMigrate(context.Background(), dbc)
	if err != nil {
		t.Fatalf("first AutoMigrate() error = %v", err)
	}

	// Insert data after the first migration. Email is set to a distinct
	// non-empty value because the User model enforces a unique index on
	// email; two empty strings would conflict on SQLite (empty string is a
	// value, not NULL).
	u := user.User{
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
	err = AutoMigrate(context.Background(), dbc)
	if err != nil {
		t.Fatalf("second AutoMigrate() error = %v", err)
	}

	// Verify previously inserted data is still intact.
	var fetchedUser user.User
	if err := dbc.Where("username = ?", "persist_user").First(&fetchedUser).Error; err != nil {
		t.Fatalf("query user after second migration: %v", err)
	}
	if fetchedUser.Username != "persist_user" {
		t.Errorf("username after incremental migration = %q, want %q", fetchedUser.Username, "persist_user")
	}

	// Verify new data can still be inserted after the incremental migration.
	newUser := user.User{
		Username:     "post_migration_user",
		PasswordHash: "$2a$10$hash",
		Email:        "post@example.com",
		Role:         2,
	}
	if err := dbc.Create(&newUser).Error; err != nil {
		t.Fatalf("insert user after second migration: %v", err)
	}
}

// TestEnsureAdminUser_FirstCreateRandomPassword verifies that when no password
// is supplied, EnsureAdminUser creates the admin user with role=2/status=1 and
// returns a non-empty random plaintext password that can be verified against
// the stored hash.
func TestEnsureAdminUser_FirstCreateRandomPassword(t *testing.T) {
	dbc := newTestDB(t)
	if err := AutoMigrate(context.Background(), dbc); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	generated, err := EnsureAdminUser(context.Background(), dbc, "admin", "")
	if err != nil {
		t.Fatalf("EnsureAdminUser() error = %v", err)
	}
	if generated == "" {
		t.Fatal("expected non-empty generated password, got empty string")
	}

	var u user.User
	if err := dbc.Where("username = ?", "admin").First(&u).Error; err != nil {
		t.Fatalf("query admin user: %v", err)
	}
	if u.Role != 2 {
		t.Errorf("admin role = %d, want 2", u.Role)
	}
	if u.Status != 1 {
		t.Errorf("admin status = %d, want 1", u.Status)
	}
	if u.PasswordHash == "" {
		t.Fatal("admin password hash is empty")
	}
	if err := password.Verify(u.PasswordHash, generated); err != nil {
		t.Errorf("password verify failed: %v", err)
	}
}

// TestEnsureAdminUser_FirstCreateExplicitPassword verifies that when an explicit
// password is supplied, EnsureAdminUser uses it and returns an empty string
// (so the caller does not log it again).
func TestEnsureAdminUser_FirstCreateExplicitPassword(t *testing.T) {
	dbc := newTestDB(t)
	if err := AutoMigrate(context.Background(), dbc); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	returned, err := EnsureAdminUser(context.Background(), dbc, "admin", "S3cret!pass")
	if err != nil {
		t.Fatalf("EnsureAdminUser() error = %v", err)
	}
	if returned != "" {
		t.Errorf("expected empty returned password for explicit password, got %q", returned)
	}

	var u user.User
	if err := dbc.Where("username = ?", "admin").First(&u).Error; err != nil {
		t.Fatalf("query admin user: %v", err)
	}
	if err := password.Verify(u.PasswordHash, "S3cret!pass"); err != nil {
		t.Errorf("password verify failed: %v", err)
	}
}

// TestEnsureAdminUser_RestartDoesNotOverwritePassword verifies that a second
// call to EnsureAdminUser (simulating a process restart) does not overwrite
// the existing password, regardless of whether the second call supplies a
// different password or an empty one.
func TestEnsureAdminUser_RestartDoesNotOverwritePassword(t *testing.T) {
	dbc := newTestDB(t)
	if err := AutoMigrate(context.Background(), dbc); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	if _, err := EnsureAdminUser(context.Background(), dbc, "admin", "first-password"); err != nil {
		t.Fatalf("first EnsureAdminUser() error = %v", err)
	}

	// Capture the original hash.
	var original user.User
	if err := dbc.Where("username = ?", "admin").First(&original).Error; err != nil {
		t.Fatalf("query original admin: %v", err)
	}

	// Second call with a different explicit password should not overwrite.
	if _, err := EnsureAdminUser(context.Background(), dbc, "admin", "different-password"); err != nil {
		t.Fatalf("second EnsureAdminUser() error = %v", err)
	}

	var afterSecond user.User
	if err := dbc.Where("username = ?", "admin").First(&afterSecond).Error; err != nil {
		t.Fatalf("query admin after second call: %v", err)
	}
	if afterSecond.PasswordHash != original.PasswordHash {
		t.Error("second EnsureAdminUser call overwrote the password hash")
	}
	if err := password.Verify(afterSecond.PasswordHash, "first-password"); err != nil {
		t.Errorf("original password no longer verifies after restart: %v", err)
	}

	// Third call with empty password should also not overwrite or generate.
	returned, err := EnsureAdminUser(context.Background(), dbc, "admin", "")
	if err != nil {
		t.Fatalf("third EnsureAdminUser() error = %v", err)
	}
	if returned != "" {
		t.Errorf("expected empty returned password on restart, got %q", returned)
	}

	var afterThird user.User
	if err := dbc.Where("username = ?", "admin").First(&afterThird).Error; err != nil {
		t.Fatalf("query admin after third call: %v", err)
	}
	if afterThird.PasswordHash != original.PasswordHash {
		t.Error("third EnsureAdminUser call overwrote the password hash")
	}
}

// TestEnsureAdminUser_CustomUsername verifies that EnsureAdminUser can create
// an admin with a non-default username.
func TestEnsureAdminUser_CustomUsername(t *testing.T) {
	dbc := newTestDB(t)
	if err := AutoMigrate(context.Background(), dbc); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	generated, err := EnsureAdminUser(context.Background(), dbc, "root_admin", "")
	if err != nil {
		t.Fatalf("EnsureAdminUser() error = %v", err)
	}
	if generated == "" {
		t.Fatal("expected non-empty generated password for custom username")
	}

	var u user.User
	if err := dbc.Where("username = ?", "root_admin").First(&u).Error; err != nil {
		t.Fatalf("query custom admin user: %v", err)
	}
	if u.Username != "root_admin" {
		t.Errorf("username = %q, want %q", u.Username, "root_admin")
	}
	if u.Role != 2 {
		t.Errorf("admin role = %d, want 2", u.Role)
	}

	// Verify default admin username was not created.
	var count int64
	if err := dbc.Model(&user.User{}).Where("username = ?", "admin").Count(&count).Error; err != nil {
		t.Fatalf("count default admin: %v", err)
	}
	if count != 0 {
		t.Errorf("default admin user should not exist, count = %d", count)
	}
}

// TestEnsureAdminUser_EmptyUsernameReturnsError verifies that an empty username
// is rejected.
func TestEnsureAdminUser_EmptyUsernameReturnsError(t *testing.T) {
	dbc := newTestDB(t)
	if err := AutoMigrate(context.Background(), dbc); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	returned, err := EnsureAdminUser(context.Background(), dbc, "", "whatever")
	if err == nil {
		t.Fatal("expected error for empty username, got nil")
	}
	if returned != "" {
		t.Errorf("expected empty returned password on error, got %q", returned)
	}
	if !strings.Contains(err.Error(), "admin username is required") {
		t.Errorf("error message %q does not contain expected substring", err.Error())
	}
}

// TestEnsureAdminUser_UsernameValidation verifies that EnsureAdminUser enforces
// the canonical username rule (3-64 chars, only letters/digits/underscores)
// before creating the admin user. This guards against the "initialized but
// cannot log in" bug where a custom admin_username passes EnsureAdminUser but
// is later rejected by Service.Login's validateUsername.
func TestEnsureAdminUser_UsernameValidation(t *testing.T) {
	dbc := newTestDB(t)
	if err := AutoMigrate(context.Background(), dbc); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	// Valid usernames must be created successfully.
	validNames := []string{"admin", "admin_user", "user123"}
	for _, name := range validNames {
		// Each case uses a fresh database so the unique-index does not trip
		// across iterations.
		freshDB := newTestDB(t)
		if err := AutoMigrate(context.Background(), freshDB); err != nil {
			t.Fatalf("AutoMigrate() error = %v", err)
		}
		returned, err := EnsureAdminUser(context.Background(), freshDB, name, "S3cret!pass")
		if err != nil {
			t.Errorf("EnsureAdminUser(%q) unexpected error: %v", name, err)
			continue
		}
		if returned != "" {
			t.Errorf("EnsureAdminUser(%q) expected empty returned password, got %q", name, returned)
		}
		var u user.User
		if err := freshDB.Where("username = ?", name).First(&u).Error; err != nil {
			t.Errorf("EnsureAdminUser(%q) query user: %v", name, err)
		}
	}

	// Invalid usernames must be rejected before any row is created.
	invalidNames := []string{
		"ad",                    // too short (< 3 chars)
		"admin-user",            // hyphen not allowed
		"admin.user",            // dot not allowed
		"admin user",            // space not allowed
		strings.Repeat("a", 65), // 65 chars, too long (> 64)
	}
	for _, name := range invalidNames {
		returned, err := EnsureAdminUser(context.Background(), dbc, name, "S3cret!pass")
		if err == nil {
			t.Errorf("EnsureAdminUser(%q) expected error, got nil", name)
			continue
		}
		if returned != "" {
			t.Errorf("EnsureAdminUser(%q) expected empty returned password on error, got %q", name, returned)
		}
		// The error must reference the invalid admin username and wrap the
		// underlying user.ErrInvalidUsername via %w.
		if !strings.Contains(err.Error(), "invalid admin username") {
			t.Errorf("EnsureAdminUser(%q) error = %q, want substring %q", name, err.Error(), "invalid admin username")
		}
		if !errors.Is(err, user.ErrInvalidUsername) {
			t.Errorf("EnsureAdminUser(%q) error does not wrap user.ErrInvalidUsername: %v", name, err)
		}
		// Ensure no row was created for the invalid username.
		var count int64
		if err := dbc.Model(&user.User{}).Where("username = ?", name).Count(&count).Error; err != nil {
			t.Fatalf("count users for %q: %v", name, err)
		}
		if count != 0 {
			t.Errorf("EnsureAdminUser(%q) should not have created a row, count = %d", name, count)
		}
	}

	// The default value "admin" must pass.
	defaultDB := newTestDB(t)
	if err := AutoMigrate(context.Background(), defaultDB); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	if _, err := EnsureAdminUser(context.Background(), defaultDB, "admin", ""); err != nil {
		t.Errorf("EnsureAdminUser(%q) default value unexpected error: %v", "admin", err)
	}
}
