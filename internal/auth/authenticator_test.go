// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/tickraft/tickraft/internal/auth"
	authcore "github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/auth/jwt"
	"github.com/tickraft/tickraft/pkg/auth/password"
	"github.com/tickraft/tickraft/pkg/user"
)

const testSecret = "this-is-a-very-long-secret-key-32bytes"

// newTestDB opens an in-memory SQLite database migrated with the auth models.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbc, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := dbc.AutoMigrate(
		&user.User{},
		&user.APIKey{},
		&authcore.TokenBlacklist{},
	); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return dbc
}

// newTestAuthenticator creates an Authenticator for testing using real store instances.
func newTestAuthenticator(t *testing.T) (auth.Authenticator, user.Store, authcore.BlacklistStore) {
	t.Helper()
	dbc := newTestDB(t)
	users := user.NewStore(dbc)
	blacklist := authcore.NewBlacklistStore(dbc, nil)
	mgr, err := jwt.New(jwt.Config{Secret: testSecret}, nil)
	if err != nil {
		t.Fatalf("jwt.New() error = %v", err)
	}
	return auth.NewAuthenticator(users, blacklist, mgr, 4), users, blacklist
}

// addTestUser creates a user directly in the database for testing.
func addTestUser(t *testing.T, users user.Store, username, pwd string, role int) int64 {
	t.Helper()
	hashed, err := password.HashWithCost(pwd, 4)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	id, err := users.Create(context.Background(), username, hashed, "", int64(role))
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return id
}

func TestLoginSuccess(t *testing.T) {
	authn, users, _ := newTestAuthenticator(t)
	addTestUser(t, users, "alice", "secret123", authcore.RoleAdmin)

	pair, err := authn.Login(context.Background(), "alice", "secret123")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if pair.AccessToken == "" {
		t.Error("AccessToken is empty")
	}
	if pair.RefreshToken == "" {
		t.Error("RefreshToken is empty")
	}
	if pair.AccessToken == pair.RefreshToken {
		t.Error("AccessToken and RefreshToken should differ")
	}
}

func TestLoginWrongPassword(t *testing.T) {
	authn, users, _ := newTestAuthenticator(t)
	addTestUser(t, users, "alice", "secret123", authcore.RoleAdmin)

	_, err := authn.Login(context.Background(), "alice", "wrong")
	if !errors.Is(err, authcore.ErrUnauthorized) {
		t.Errorf("Login() error = %v, want ErrUnauthorized", err)
	}
}

func TestLoginUserNotFound(t *testing.T) {
	authn, _, _ := newTestAuthenticator(t)

	_, err := authn.Login(context.Background(), "nobody", "secret")
	if !errors.Is(err, authcore.ErrUnauthorized) {
		t.Errorf("Login() error = %v, want ErrUnauthorized", err)
	}
}

func TestVerifySuccess(t *testing.T) {
	authn, users, _ := newTestAuthenticator(t)
	userID := addTestUser(t, users, "bob", "pass123", authcore.RoleDeveloper)

	pair, err := authn.Login(context.Background(), "bob", "pass123")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	u, err := authn.Verify(context.Background(), pair.AccessToken)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if u.ID != userID {
		t.Errorf("User.ID = %d, want %d", u.ID, userID)
	}
	if u.Username != "bob" {
		t.Errorf("User.Username = %q, want %q", u.Username, "bob")
	}
}

func TestVerifyInvalidToken(t *testing.T) {
	authn, _, _ := newTestAuthenticator(t)

	_, err := authn.Verify(context.Background(), "invalid-token")
	if !errors.Is(err, authcore.ErrUnauthorized) {
		t.Errorf("Verify() error = %v, want ErrUnauthorized", err)
	}
}

func TestVerifyBlacklistedToken(t *testing.T) {
	dbc := newTestDB(t)
	users := user.NewStore(dbc)
	blacklist := authcore.NewBlacklistStore(dbc, nil)

	blacklistChecker := func(jti string) (bool, error) {
		return blacklist.Exists(context.Background(), jti)
	}
	mgr, err := jwt.New(jwt.Config{Secret: testSecret}, blacklistChecker)
	if err != nil {
		t.Fatalf("jwt.New() error = %v", err)
	}
	authn := auth.NewAuthenticator(users, blacklist, mgr, 4)

	addTestUser(t, users, "carol", "pass123", authcore.RoleVisitor)

	pair, err := authn.Login(context.Background(), "carol", "pass123")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	// Verify works before blacklisting.
	_, err = authn.Verify(context.Background(), pair.AccessToken)
	if err != nil {
		t.Fatalf("Verify(before blacklist) error = %v", err)
	}

	// Parse the token to get the JTI, then blacklist it.
	claims, _ := jwt.Parse(pair.AccessToken, testSecret)
	if claims != nil {
		_ = blacklist.Add(context.Background(), claims.ID, time.Now().Add(time.Hour))
	}

	_, err = authn.Verify(context.Background(), pair.AccessToken)
	if !errors.Is(err, authcore.ErrUnauthorized) {
		t.Errorf("Verify(after blacklist) error = %v, want ErrUnauthorized", err)
	}
}

func TestVerifyUserDeletedFromStore(t *testing.T) {
	authn, users, _ := newTestAuthenticator(t)
	addTestUser(t, users, "dave", "pass123", authcore.RoleAdmin)

	pair, err := authn.Login(context.Background(), "dave", "pass123")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	// Delete user from store to simulate deletion after token was issued.
	_ = users.Delete(context.Background(), 1)

	_, err = authn.Verify(context.Background(), pair.AccessToken)
	if !errors.Is(err, authcore.ErrUnauthorized) {
		t.Errorf("Verify() error = %v, want ErrUnauthorized", err)
	}
}

func TestNewAuthenticatorWithJWT(t *testing.T) {
	dbc := newTestDB(t)
	users := user.NewStore(dbc)
	blacklist := authcore.NewBlacklistStore(dbc, nil)
	mgr, err := jwt.New(jwt.Config{
		Secret:        testSecret,
		AccessExpire:  1 * time.Hour,
		RefreshExpire: 24 * time.Hour,
		Issuer:        "test-issuer",
	}, nil)
	if err != nil {
		t.Fatalf("jwt.New() error = %v", err)
	}

	authn := auth.NewAuthenticator(users, blacklist, mgr, 4)
	if authn == nil {
		t.Fatal("Authenticator is nil")
	}

	addTestUser(t, users, "frank", "pass", authcore.RoleDeveloper)
	pair, err := authn.Login(context.Background(), "frank", "pass")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if pair.AccessToken == "" {
		t.Error("AccessToken is empty")
	}

	u, err := authn.Verify(context.Background(), pair.AccessToken)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if u.Username != "frank" {
		t.Errorf("Username = %q, want %q", u.Username, "frank")
	}
}

func TestVerifyRejectsRefreshToken(t *testing.T) {
	authn, users, _ := newTestAuthenticator(t)
	addTestUser(t, users, "grace", "pass", authcore.RoleDeveloper)

	pair, err := authn.Login(context.Background(), "grace", "pass")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	// Verify should reject a refresh token (expects access token type).
	_, err = authn.Verify(context.Background(), pair.RefreshToken)
	if !errors.Is(err, authcore.ErrUnauthorized) {
		t.Errorf("Verify(refresh token) error = %v, want ErrUnauthorized", err)
	}
}

// Ensure the returned user.User matches the user.User struct.
func TestVerifyReturnsFullUser(t *testing.T) {
	authn, users, _ := newTestAuthenticator(t)
	id := addTestUser(t, users, "heidi", "pass", authcore.RoleAdmin)

	pair, err := authn.Login(context.Background(), "heidi", "pass")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	u, err := authn.Verify(context.Background(), pair.AccessToken)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	if u.ID != id {
		t.Errorf("ID = %d, want %d", u.ID, id)
	}
	// Verify it's a proper user.User with expected fields.
	_ = u.CreatedAt // should be accessible
}
