// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package jwt

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret-key"

func TestSignAndParse(t *testing.T) {
	token, err := SignToken(1, 0, 2, "testuser", testSecret, time.Hour)
	if err != nil {
		t.Fatalf("SignToken() error = %v", err)
	}
	if token == "" {
		t.Fatal("SignToken() returned empty token")
	}

	claims, err := Parse(token, testSecret)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if claims.UserID != 1 {
		t.Errorf("UserID = %d, want 1", claims.UserID)
	}
	if claims.TenantID != 0 {
		t.Errorf("TenantID = %d, want 0", claims.TenantID)
	}
	if claims.Role != 2 {
		t.Errorf("Role = %d, want 2", claims.Role)
	}
}

func TestParseExpired(t *testing.T) {
	token, err := SignToken(1, 0, 2, "testuser", testSecret, -time.Hour)
	if err != nil {
		t.Fatalf("SignToken() error = %v", err)
	}

	_, err = Parse(token, testSecret)
	if err != ErrTokenExpired {
		t.Errorf("Parse() error = %v, want ErrTokenExpired", err)
	}
}

func TestParseInvalidSecret(t *testing.T) {
	token, err := SignToken(1, 0, 2, "testuser", testSecret, time.Hour)
	if err != nil {
		t.Fatalf("SignToken() error = %v", err)
	}

	_, err = Parse(token, "wrong-secret")
	if err != ErrTokenInvalid {
		t.Errorf("Parse() error = %v, want ErrTokenInvalid", err)
	}
}

func TestParseInvalidToken(t *testing.T) {
	_, err := Parse("invalid.token.string", testSecret)
	if err != ErrTokenInvalid {
		t.Errorf("Parse() error = %v, want ErrTokenInvalid", err)
	}
}

func TestSignWithCustomClaims(t *testing.T) {
	claims := Claims{
		UserID:   42,
		TenantID: 100,
		Role:     1,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := Sign(claims, testSecret)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	parsed, err := Parse(token, testSecret)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if parsed.UserID != 42 {
		t.Errorf("UserID = %d, want 42", parsed.UserID)
	}
	if parsed.TenantID != 100 {
		t.Errorf("TenantID = %d, want 100", parsed.TenantID)
	}
}

func TestSignToken(t *testing.T) {
	token, err := SignToken(1, 10, 2, "testuser", testSecret, time.Hour)
	if err != nil {
		t.Fatalf("SignToken() error = %v", err)
	}
	if token == "" {
		t.Fatal("SignToken() returned empty token")
	}

	claims, err := Parse(token, testSecret)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if claims.UserID != 1 {
		t.Errorf("UserID = %d, want 1", claims.UserID)
	}
	if claims.Username != "testuser" {
		t.Errorf("Username = %q, want %q", claims.Username, "testuser")
	}
	if claims.JTI() == "" {
		t.Error("JTI() is empty, want non-empty")
	}
	if claims.ID == "" {
		t.Error("RegisteredClaims.ID is empty, want non-empty")
	}
}

func TestNewJTI(t *testing.T) {
	jti1 := newJTI()
	jti2 := newJTI()
	if jti1 == "" {
		t.Error("newJTI() returned empty string")
	}
	if jti1 == jti2 {
		t.Error("newJTI() returned duplicate values")
	}
}

const managerSecret = "this-is-a-very-long-secret-key-32bytes"

func TestGenerateTokenPair(t *testing.T) {
	mgr, err := New(Config{Secret: managerSecret}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	pair, err := mgr.GenerateTokenPair(UserClaims{
		UID:      1,
		Username: "alice",
		Role:     2,
		TenantID: 10,
	})
	if err != nil {
		t.Fatalf("GenerateTokenPair() error = %v", err)
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

	// Validate access token.
	accessClaims, err := mgr.ValidateToken(pair.AccessToken, TokenTypeAccess)
	if err != nil {
		t.Fatalf("ValidateToken(access) error = %v", err)
	}
	if accessClaims.UID != 1 {
		t.Errorf("UID = %d, want 1", accessClaims.UID)
	}
	if accessClaims.Username != "alice" {
		t.Errorf("Username = %q, want %q", accessClaims.Username, "alice")
	}
	if accessClaims.JTI == "" {
		t.Error("JTI is empty for access token")
	}

	// Validate refresh token.
	refreshClaims, err := mgr.ValidateToken(pair.RefreshToken, TokenTypeRefresh)
	if err != nil {
		t.Fatalf("ValidateToken(refresh) error = %v", err)
	}
	if refreshClaims.UID != 1 {
		t.Errorf("UID = %d, want 1", refreshClaims.UID)
	}
	if refreshClaims.JTI == "" {
		t.Error("JTI is empty for refresh token")
	}
}

func TestValidateTokenRejectsWrongType(t *testing.T) {
	mgr, err := New(Config{Secret: managerSecret}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	pair, err := mgr.GenerateTokenPair(UserClaims{UID: 1, Username: "bob", Role: 1, TenantID: 0})
	if err != nil {
		t.Fatalf("GenerateTokenPair() error = %v", err)
	}

	// Access token should not pass as refresh token.
	_, err = mgr.ValidateToken(pair.AccessToken, TokenTypeRefresh)
	if err != ErrTokenInvalid {
		t.Errorf("ValidateToken(access as refresh) error = %v, want ErrTokenInvalid", err)
	}

	// Refresh token should not pass as access token.
	_, err = mgr.ValidateToken(pair.RefreshToken, TokenTypeAccess)
	if err != ErrTokenInvalid {
		t.Errorf("ValidateToken(refresh as access) error = %v, want ErrTokenInvalid", err)
	}
}

func TestValidateTokenRejectsBlacklisted(t *testing.T) {
	blacklist := map[string]bool{}

	mgr, err := New(Config{Secret: managerSecret}, func(jti string) (bool, error) {
		return blacklist[jti], nil
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	pair, err := mgr.GenerateTokenPair(UserClaims{UID: 1, Username: "carol", Role: 1, TenantID: 0})
	if err != nil {
		t.Fatalf("GenerateTokenPair() error = %v", err)
	}

	// Validate before blacklisting.
	claims, err := mgr.ValidateToken(pair.AccessToken, TokenTypeAccess)
	if err != nil {
		t.Fatalf("ValidateToken(before blacklist) error = %v", err)
	}

	// Add JTI to blacklist.
	blacklist[claims.JTI] = true

	// Validate after blacklisting.
	_, err = mgr.ValidateToken(pair.AccessToken, TokenTypeAccess)
	if err != ErrTokenInBlacklist {
		t.Errorf("ValidateToken(after blacklist) error = %v, want ErrTokenInBlacklist", err)
	}
}

func TestRefreshToken(t *testing.T) {
	mgr, err := New(Config{Secret: managerSecret}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	pair, err := mgr.GenerateTokenPair(UserClaims{UID: 1, Username: "dave", Role: 2, TenantID: 5})
	if err != nil {
		t.Fatalf("GenerateTokenPair() error = %v", err)
	}

	// Refresh using the refresh token.
	newPair, err := mgr.RefreshToken(pair.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshToken() error = %v", err)
	}
	if newPair.AccessToken == "" {
		t.Error("RefreshToken() returned empty AccessToken")
	}
	if newPair.RefreshToken == "" {
		t.Error("RefreshToken() returned empty RefreshToken")
	}

	// New tokens should be different from old ones.
	if newPair.AccessToken == pair.AccessToken {
		t.Error("RefreshToken() returned same AccessToken")
	}
	if newPair.RefreshToken == pair.RefreshToken {
		t.Error("RefreshToken() returned same RefreshToken")
	}

	// New access token should be valid.
	newAccessClaims, err := mgr.ValidateToken(newPair.AccessToken, TokenTypeAccess)
	if err != nil {
		t.Fatalf("ValidateToken(new access) error = %v", err)
	}
	if newAccessClaims.UID != 1 {
		t.Errorf("UID = %d, want 1", newAccessClaims.UID)
	}
	if newAccessClaims.Username != "dave" {
		t.Errorf("Username = %q, want %q", newAccessClaims.Username, "dave")
	}
	if newAccessClaims.TenantID != 5 {
		t.Errorf("TenantID = %d, want 5", newAccessClaims.TenantID)
	}
}

func TestRefreshTokenRejectsAccessToken(t *testing.T) {
	mgr, err := New(Config{Secret: managerSecret}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	pair, err := mgr.GenerateTokenPair(UserClaims{UID: 1, Username: "eve", Role: 0, TenantID: 0})
	if err != nil {
		t.Fatalf("GenerateTokenPair() error = %v", err)
	}

	// Trying to refresh with an access token should fail.
	_, err = mgr.RefreshToken(pair.AccessToken)
	if err == nil {
		t.Error("RefreshToken(access token) should return error")
	}
}

func TestNewErrorShortSecret(t *testing.T) {
	_, err := New(Config{Secret: "short"}, nil)
	if !errors.Is(err, ErrSecretTooShort) {
		t.Errorf("New(short secret) error = %v, want ErrSecretTooShort", err)
	}

	// Empty secret should also be rejected.
	_, err = New(Config{Secret: ""}, nil)
	if !errors.Is(err, ErrSecretTooShort) {
		t.Errorf("New(empty secret) error = %v, want ErrSecretTooShort", err)
	}
}

func TestDefaultExpiry(t *testing.T) {
	mgr, err := New(Config{Secret: managerSecret}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	pair, err := mgr.GenerateTokenPair(UserClaims{UID: 1, Username: "frank", Role: 1, TenantID: 0})
	if err != nil {
		t.Fatalf("GenerateTokenPair() error = %v", err)
	}

	// Access token should be valid now (default 2h).
	_, err = mgr.ValidateToken(pair.AccessToken, TokenTypeAccess)
	if err != nil {
		t.Errorf("ValidateToken(access) error = %v", err)
	}

	// Refresh token should be valid now (default 7d).
	_, err = mgr.ValidateToken(pair.RefreshToken, TokenTypeRefresh)
	if err != nil {
		t.Errorf("ValidateToken(refresh) error = %v", err)
	}
}

func TestCustomExpiry(t *testing.T) {
	mgr, err := New(Config{
		Secret:        managerSecret,
		AccessExpire:  1 * time.Minute,
		RefreshExpire: 10 * time.Minute,
		Issuer:        "tickraft-test",
	}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	pair, err := mgr.GenerateTokenPair(UserClaims{UID: 1, Username: "grace", Role: 1, TenantID: 0})
	if err != nil {
		t.Fatalf("GenerateTokenPair() error = %v", err)
	}

	// Parse the access token directly to check issuer.
	claims, err := Parse(pair.AccessToken, managerSecret)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if claims.Issuer != "tickraft-test" {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, "tickraft-test")
	}
}

func TestValidateTokenRejectsExpiredToken(t *testing.T) {
	mgr, err := New(Config{Secret: managerSecret}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Manually create an expired token using Sign directly.
	expiredClaims := Claims{
		UserID:   1,
		TenantID: 0,
		Role:     1,
		Username: "heidi",
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        newJTI(),
			Subject:   TokenTypeAccess,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	expiredToken, err := Sign(expiredClaims, managerSecret)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	_, err = mgr.ValidateToken(expiredToken, TokenTypeAccess)
	if err != ErrTokenExpired {
		t.Errorf("ValidateToken(expired) error = %v, want ErrTokenExpired", err)
	}
}

func TestNilBlacklistChecker(t *testing.T) {
	mgr, err := New(Config{Secret: managerSecret}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	pair, err := mgr.GenerateTokenPair(UserClaims{UID: 1, Username: "ivan", Role: 1, TenantID: 0})
	if err != nil {
		t.Fatalf("GenerateTokenPair() error = %v", err)
	}

	// Should work fine with nil blacklist checker.
	_, err = mgr.ValidateToken(pair.AccessToken, TokenTypeAccess)
	if err != nil {
		t.Errorf("ValidateToken() with nil blacklist error = %v", err)
	}
}

func TestRegionRoundTrip(t *testing.T) {
	mgr, err := New(Config{Secret: managerSecret}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for _, region := range []string{"cn", "global", "private", ""} {
		pair, err := mgr.GenerateTokenPair(UserClaims{
			UID:      1,
			Username: "regionuser",
			Role:     1,
			TenantID: 0,
			Region:   region,
		})
		if err != nil {
			t.Fatalf("GenerateTokenPair(region=%q) error = %v", region, err)
		}

		claims, err := mgr.ValidateToken(pair.AccessToken, TokenTypeAccess)
		if err != nil {
			t.Fatalf("ValidateToken(region=%q) error = %v", region, err)
		}
		if claims.Region != region {
			t.Errorf("Region = %q, want %q", claims.Region, region)
		}

		// Refresh tokens should also preserve the region.
		refreshClaims, err := mgr.ValidateToken(pair.RefreshToken, TokenTypeRefresh)
		if err != nil {
			t.Fatalf("ValidateToken(refresh, region=%q) error = %v", region, err)
		}
		if refreshClaims.Region != region {
			t.Errorf("Refresh Region = %q, want %q", refreshClaims.Region, region)
		}
	}
}
