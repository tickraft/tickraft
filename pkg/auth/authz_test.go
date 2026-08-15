// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/auth/jwt"
	"github.com/tickraft/tickraft/pkg/auth/password"
	"github.com/tickraft/tickraft/pkg/user"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const testJWTSecret = "test-secret-that-is-at-least-32-bytes-long!"

// newTestAuthDB opens an in-memory SQLite database migrated with the auth models.
func newTestAuthDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbc, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := dbc.AutoMigrate(
		&user.User{},
		&user.APIKey{},
		&auth.TokenBlacklist{},
	); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return dbc
}

// newTestService creates a Service for testing using real store instances.
func newTestService(t *testing.T) (*auth.Service, user.Store, user.APIKeyStore, auth.BlacklistStore) {
	t.Helper()
	dbc := newTestAuthDB(t)
	users := user.NewStore(dbc, nil)
	apiKeys := user.NewAPIKeyStore(dbc, nil)
	blacklist := auth.NewBlacklistStore(dbc, nil)

	blacklistChecker := func(jti string) (bool, error) {
		return blacklist.Exists(context.Background(), jti)
	}

	jwtMgr, err := jwt.New(jwt.Config{
		Secret:        testJWTSecret,
		AccessExpire:  2 * time.Hour,
		RefreshExpire: 7 * 24 * time.Hour,
		Issuer:        "tickraft-test",
	}, blacklistChecker)
	if err != nil {
		t.Fatalf("jwt.New() error = %v", err)
	}

	svc := auth.NewService(jwtMgr, users, apiKeys, blacklist)
	return svc, users, apiKeys, blacklist
}

// seedUser creates a user directly in the database for testing.
func seedUser(t *testing.T, users user.Store, username, pwd string) *user.User {
	t.Helper()
	hash, err := password.Hash(pwd)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	id, err := users.Create(context.Background(), username, hash, "", int64(auth.RoleAdmin))
	if err != nil {
		t.Fatalf("create seed user: %v", err)
	}
	return &user.User{
		ID:           id,
		Username:     username,
		PasswordHash: hash,
		Role:         auth.RoleAdmin,
		Status:       1,
		CreatedAt:    time.Now(),
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestLogin_Success(t *testing.T) {
	svc, users, _, _ := newTestService(t)
	seedUser(t, users, "admin", "Password1")

	tp, err := svc.Login(context.Background(), "admin", "Password1")
	if err != nil {
		t.Fatalf("Login() error = %v, want nil", err)
	}
	if tp.AccessToken == "" {
		t.Error("AccessToken is empty")
	}
	if tp.RefreshToken == "" {
		t.Error("RefreshToken is empty")
	}
}

func TestLogin_InvalidUsername(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	_, err := svc.Login(context.Background(), "ab", "Password1")
	if !errors.Is(err, auth.ErrInvalidUsername) {
		t.Fatalf("Login() error = %v, want ErrInvalidUsername", err)
	}

	_, err = svc.Login(context.Background(), "user@name", "Password1")
	if !errors.Is(err, auth.ErrInvalidUsername) {
		t.Fatalf("Login() error = %v, want ErrInvalidUsername", err)
	}
}

func TestLogin_UserNotFound(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	_, err := svc.Login(context.Background(), "nonexistent", "Password1")
	if !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("Login() error = %v, want ErrUnauthorized", err)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	svc, users, _, _ := newTestService(t)
	seedUser(t, users, "admin", "Password1")

	_, err := svc.Login(context.Background(), "admin", "WrongPass1")
	if !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("Login() error = %v, want ErrUnauthorized", err)
	}
}

func TestLogin_RateLimiting(t *testing.T) {
	svc, users, _, _ := newTestService(t)
	seedUser(t, users, "admin", "Password1")

	// Fail 5 times to trigger lockout.
	for i := 0; i < auth.MaxLoginFails; i++ {
		_, _ = svc.Login(context.Background(), "admin", "WrongPass1")
	}

	// The 6th attempt should be rate-limited.
	_, err := svc.Login(context.Background(), "admin", "Password1")
	if !errors.Is(err, auth.ErrTooManyRequests) {
		t.Fatalf("Login() after lockout error = %v, want ErrTooManyRequests", err)
	}
}

func TestLogout_Success(t *testing.T) {
	svc, users, _, blacklist := newTestService(t)
	seedUser(t, users, "admin", "Password1")

	tp, err := svc.Login(context.Background(), "admin", "Password1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	// Parse the access token to get JTI and expiry.
	claims, err := jwt.Parse(tp.AccessToken, testJWTSecret)
	if err != nil {
		t.Fatalf("Parse access token: %v", err)
	}
	accessJTI := claims.JTI()
	accessExpire := claims.ExpiresAt.Time

	// Parse refresh token to verify blacklisting later.
	refreshClaims, err := jwt.Parse(tp.RefreshToken, testJWTSecret)
	if err != nil {
		t.Fatalf("Parse refresh token: %v", err)
	}
	refreshJTI := refreshClaims.JTI()

	err = svc.Logout(context.Background(), accessJTI, accessExpire, tp.RefreshToken)
	if err != nil {
		t.Fatalf("Logout() error = %v, want nil", err)
	}

	exists, err := blacklist.Exists(context.Background(), accessJTI)
	if err != nil {
		t.Fatalf("blacklist.Exists() error = %v", err)
	}
	if !exists {
		t.Error("access JTI should be in blacklist")
	}

	exists, err = blacklist.Exists(context.Background(), refreshJTI)
	if err != nil {
		t.Fatalf("blacklist.Exists() error = %v", err)
	}
	if !exists {
		t.Error("refresh JTI should be in blacklist")
	}
}

func TestRefreshToken_Success(t *testing.T) {
	svc, users, _, _ := newTestService(t)
	seedUser(t, users, "admin", "Password1")

	tp, err := svc.Login(context.Background(), "admin", "Password1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	newTP, err := svc.RefreshToken(context.Background(), tp.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshToken() error = %v", err)
	}
	if newTP.AccessToken == "" {
		t.Error("new AccessToken is empty")
	}
	if newTP.RefreshToken == "" {
		t.Error("new RefreshToken is empty")
	}
	if newTP.AccessToken == tp.AccessToken {
		t.Error("new AccessToken should differ from old")
	}
}

func TestRefreshToken_InvalidToken(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	_, err := svc.RefreshToken(context.Background(), "invalid-token")
	if err == nil {
		t.Fatal("RefreshToken() with invalid token should return error")
	}
}

func TestChangePassword_Success(t *testing.T) {
	svc, users, _, _ := newTestService(t)
	u := seedUser(t, users, "admin", "OldPass1")

	err := svc.ChangePassword(context.Background(), u.ID, "OldPass1", "NewPass1", "")
	if err != nil {
		t.Fatalf("ChangePassword() error = %v, want nil", err)
	}

	// Verify the new password works for login.
	_, err = svc.Login(context.Background(), "admin", "NewPass1")
	if err != nil {
		t.Fatalf("Login() with new password error = %v", err)
	}
}

func TestChangePassword_WrongOldPassword(t *testing.T) {
	svc, users, _, _ := newTestService(t)
	u := seedUser(t, users, "admin", "OldPass1")

	err := svc.ChangePassword(context.Background(), u.ID, "WrongOld1", "NewPass1", "")
	if !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("ChangePassword() error = %v, want ErrUnauthorized", err)
	}
}

func TestChangePassword_WeakNewPassword(t *testing.T) {
	svc, users, _, _ := newTestService(t)
	u := seedUser(t, users, "admin", "OldPass1")

	tests := []struct {
		name string
		pwd  string
	}{
		{"too short", "Ab1"},
		{"no digit", "abcdefgh"},
		{"no letter", "12345678"},
		{"too long", string(make([]byte, 129))}, // 129 chars
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Fill the too-long password with valid chars.
			if tt.name == "too long" {
				for i := range tt.pwd {
					tt.pwd = tt.pwd[:i] + "A" + tt.pwd[i+1:]
				}
				tt.pwd = "A1" + tt.pwd[2:]
			}
			err := svc.ChangePassword(context.Background(), u.ID, "OldPass1", tt.pwd, "")
			if !errors.Is(err, auth.ErrWeakPassword) {
				t.Fatalf("ChangePassword() with %q error = %v, want ErrWeakPassword", tt.name, err)
			}
		})
	}
}

func TestCreateAPIKey_Success(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	raw, info, err := svc.CreateAPIKey(context.Background(), "test-key", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}
	if raw == "" {
		t.Error("raw key is empty")
	}
	if info == nil {
		t.Fatal("info is nil")
	}
	if info.ID <= 0 {
		t.Error("info.ID should be positive")
	}
	if info.Name != "test-key" {
		t.Errorf("info.Name = %q, want %q", info.Name, "test-key")
	}
	if info.KeyPrefix == "" {
		t.Error("info.KeyPrefix is empty")
	}
}

func TestListAPIKeys_Success(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	_, _, err := svc.CreateAPIKey(context.Background(), "key1", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}
	_, _, err = svc.CreateAPIKey(context.Background(), "key2", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}

	keys, total, err := svc.ListAPIKeys(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("ListAPIKeys() error = %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("ListAPIKeys() returned %d keys, want 2", len(keys))
	}
	if total != 2 {
		t.Fatalf("ListAPIKeys() total = %d, want 2", total)
	}
}

func TestRevokeAPIKey_Success(t *testing.T) {
	svc, _, apiKeys, _ := newTestService(t)

	_, info, err := svc.CreateAPIKey(context.Background(), "test-key", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}

	err = svc.RevokeAPIKey(context.Background(), info.ID)
	if err != nil {
		t.Fatalf("RevokeAPIKey() error = %v", err)
	}

	// Verify the key is revoked.
	key, err := apiKeys.GetByHash(context.Background(), info.KeyHash)
	if err != nil {
		t.Fatalf("GetByHash() error = %v", err)
	}
	if key.RevokedAt.IsZero() {
		t.Error("key.RevokedAt should be set after revocation")
	}
}

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid", "admin", false},
		{"valid with underscore", "admin_user", false},
		{"valid with digits", "user123", false},
		{"too short", "ab", true},
		{"too long", string(make([]byte, 65)), true},
		{"special chars", "user@name", true},
		{"hyphen", "user-name", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := auth.ValidateUsername(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateUsername(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid", "Password1", false},
		{"too short", "Ab1", true},
		{"no digit", "abcdefgh", true},
		{"no letter", "12345678", true},
		{"boundary 8 chars", "Passwor1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := auth.ValidatePassword(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePassword(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestLogin_RateLimitResetOnSuccess(t *testing.T) {
	svc, users, _, _ := newTestService(t)
	seedUser(t, users, "admin", "Password1")

	// Fail 4 times (one below threshold).
	for i := 0; i < auth.MaxLoginFails-1; i++ {
		_, _ = svc.Login(context.Background(), "admin", "WrongPass1")
	}

	// Succeed — this should reset the counter.
	_, err := svc.Login(context.Background(), "admin", "Password1")
	if err != nil {
		t.Fatalf("Login() after 4 fails should succeed, error = %v", err)
	}

	// Now fail 4 more times — should NOT be locked (counter was reset).
	for i := 0; i < auth.MaxLoginFails-1; i++ {
		_, _ = svc.Login(context.Background(), "admin", "WrongPass1")
	}

	// Next attempt should still work (not locked).
	_, err = svc.Login(context.Background(), "admin", "Password1")
	if err != nil {
		t.Fatalf("Login() should succeed after counter reset, error = %v", err)
	}
}

// TestCleanupExpiredFails verifies that cleanupExpiredFails removes stale
// entries while preserving fresh and still-locked entries.
func TestCleanupExpiredFails(t *testing.T) {
	svc := auth.NewServiceForCleanupTest()

	// Insert a stale entry (last failure well beyond TTL).
	svc.Mu().Lock()
	svc.SetLoginFail("stale_user", auth.LoginFailsEntry{
		Count:        2,
		LastFailedAt: time.Now().Add(-auth.FailEntryTTLConst - time.Minute),
	})
	// Insert a fresh entry (last failure just now).
	svc.SetLoginFail("fresh_user", auth.LoginFailsEntry{
		Count:        1,
		LastFailedAt: time.Now(),
	})
	// Insert a locked entry: lastFailedAt is old, but lockedUntil is
	// still in the future — must NOT be removed.
	svc.SetLoginFail("locked_user", auth.LoginFailsEntry{
		Count:        auth.MaxLoginFails,
		LastFailedAt: time.Now().Add(-auth.FailEntryTTLConst - time.Minute),
		LockedUntil:  time.Now().Add(5 * time.Minute),
	})
	svc.Mu().Unlock()

	svc.CleanupExpiredFails()

	if svc.HasLoginFail("stale_user") {
		t.Error("stale entry should have been cleaned up")
	}
	if !svc.HasLoginFail("fresh_user") {
		t.Error("fresh entry should not have been cleaned up")
	}
	if !svc.HasLoginFail("locked_user") {
		t.Error("locked entry should not have been cleaned up (still locked)")
	}
}

// TestCleanupGoroutine verifies that the background cleanup goroutine
// periodically removes stale entries when started via startCleanupLoop.
func TestCleanupGoroutine(t *testing.T) {
	svc := auth.NewServiceForCleanupTest()
	// Use a short interval for testing.
	svc.SetCleanupInterval(10 * time.Millisecond)
	svc.StartCleanupLoop()
	defer svc.Close()

	// Insert a stale entry.
	svc.Mu().Lock()
	svc.SetLoginFail("stale_user", auth.LoginFailsEntry{
		Count:        2,
		LastFailedAt: time.Now().Add(-auth.FailEntryTTLConst - time.Minute),
	})
	// Insert a fresh entry.
	svc.SetLoginFail("fresh_user", auth.LoginFailsEntry{
		Count:        1,
		LastFailedAt: time.Now(),
	})
	svc.Mu().Unlock()

	// Poll for the cleanup goroutine to remove the stale entry.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !svc.HasLoginFail("stale_user") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if svc.HasLoginFail("stale_user") {
		t.Error("stale entry should have been cleaned up by goroutine")
	}
	if !svc.HasLoginFail("fresh_user") {
		t.Error("fresh entry should not have been cleaned up")
	}
}

// TestCloseIsSafeOnDirectConstruction verifies that Close is a no-op
// when called on a Service constructed directly (without NewService),
// where no cleanup goroutine was started.
func TestCloseIsSafeOnDirectConstruction(t *testing.T) {
	svc := auth.NewServiceForCleanupTest()
	// Should not panic.
	svc.Close()
}
