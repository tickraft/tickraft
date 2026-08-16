// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/auth/jwt"
)

// TestAuthLogin verifies login success and failure paths.
func TestAuthLogin(t *testing.T) {
	hs := newHarness(t)

	status, env := hs.do("POST", "/api/v1/auth/login",
		map[string]string{"username": adminUsername, "password": "wrong-password"}, "")
	if status != http.StatusUnauthorized || env.Code == 0 {
		t.Fatalf("bad credentials: expected 401 with non-zero code, got %d code=%d", status, env.Code)
	}

	// Success returns both tokens.
	status, env = hs.do("POST", "/api/v1/auth/login",
		map[string]string{"username": adminUsername, "password": adminPassword}, "")
	var pair struct {
		AccessToken        string `json:"access_token"`
		RefreshToken       string `json:"refresh_token"`
		MustChangePassword bool   `json:"must_change_password"`
	}
	hs.mustOK(status, env, "login", &pair)
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatalf("login: missing tokens in response")
	}
}

// TestAuthTokenExpired asserts that an expired access token returns the
// dedicated 40101 code so the frontend can trigger its refresh flow.
func TestAuthTokenExpired(t *testing.T) {
	hs := newHarness(t)

	shortLived, err := jwt.New(jwt.Config{
		Secret:       testJWTSecret,
		AccessExpire: 1 * time.Second,
		Issuer:       "tickraft",
	}, nil)
	if err != nil {
		t.Fatalf("create short-lived jwt: %v", err)
	}
	pair, err := shortLived.GenerateTokenPair(jwt.UserClaims{
		UID: 1, Username: adminUsername, Role: 2,
	})
	if err != nil {
		t.Fatalf("generate short-lived token: %v", err)
	}
	token := pair.AccessToken
	time.Sleep(1100 * time.Millisecond)

	status, env := hs.do("GET", "/api/v1/system/info", nil, token)
	if status != http.StatusUnauthorized {
		t.Fatalf("expired token: expected HTTP 401, got %d", status)
	}
	if env.Code != 40101 {
		t.Fatalf("expired token: expected envelope code 40101, got %d (%s)", env.Code, env.Message)
	}
}

// TestAuthRefreshRotation verifies refresh rotation and that a redeemed
// refresh token cannot be exchanged twice.
func TestAuthRefreshRotation(t *testing.T) {
	hs := newHarness(t)

	login := func() (access, refresh string) {
		status, env := hs.do("POST", "/api/v1/auth/login",
			map[string]string{"username": adminUsername, "password": adminPassword}, "")
		var pair struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}
		hs.mustOK(status, env, "login", &pair)
		return pair.AccessToken, pair.RefreshToken
	}

	_, refresh := login()

	rotate := func(rt string) (int, *response, string, string) {
		status, env := hs.do("POST", "/api/v1/auth/refresh",
			map[string]string{"refresh_token": rt}, "")
		var pair struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}
		if status == http.StatusOK && env.Code == 0 {
			if err := json.Unmarshal(env.Data, &pair); err != nil {
				t.Fatalf("refresh: decode pair: %v", err)
			}
		}
		return status, env, pair.AccessToken, pair.RefreshToken
	}

	// First rotation succeeds and returns a new pair.
	status, env, access2, refresh2 := rotate(refresh)
	hs.mustOK(status, env, "refresh", nil)
	if access2 == "" || refresh2 == "" || refresh2 == refresh {
		t.Fatalf("refresh: expected rotated token pair")
	}

	// The new access token is accepted.
	status, env = hs.do("GET", "/api/v1/system/info", nil, access2)
	if status != http.StatusOK {
		t.Fatalf("rotated access token rejected: HTTP %d code=%d", status, env.Code)
	}

	// Redeeming the old refresh token again must fail.
	status, env, _, _ = rotate(refresh)
	if status == http.StatusOK && env.Code == 0 {
		t.Fatalf("reused refresh token: expected rejection, got success")
	}
}

// TestAuthLogoutBlacklist verifies that logout revokes the access token.
func TestAuthLogoutBlacklist(t *testing.T) {
	hs := newHarness(t)

	token := hs.login(adminUsername, adminPassword)

	status, env := hs.do("POST", "/api/v1/auth/logout",
		map[string]string{"refresh_token": ""}, token)
	if status != http.StatusOK {
		t.Fatalf("logout: expected HTTP 200, got %d code=%d", status, env.Code)
	}

	status, _ = hs.do("GET", "/api/v1/system/info", nil, token)
	if status != http.StatusUnauthorized {
		t.Fatalf("token after logout: expected HTTP 401, got %d", status)
	}
}

// TestChangePassword verifies the password change flow end to end and
// restores the original password afterwards (shared admin account).
func TestChangePassword(t *testing.T) {
	hs := newHarness(t)

	token := hs.login(adminUsername, adminPassword)
	newPwd := "Rotated-Password-456"

	status, _ := hs.do("PUT", "/api/v1/auth/password", map[string]string{
		"old_password": "wrong-old-password",
		"new_password": newPwd,
	}, token)
	if status == http.StatusBadRequest {
		t.Fatalf("change password with wrong old password: expected non-400 rejection, got %d", status)
	}
	if status == http.StatusOK {
		t.Fatalf("change password with wrong old password: expected failure, got 200")
	}

	status, env := hs.do("PUT", "/api/v1/auth/password", map[string]string{
		"old_password": adminPassword,
		"new_password": newPwd,
	}, token)
	if status != http.StatusOK {
		t.Fatalf("change password: expected HTTP 200, got %d code=%d (%s)",
			status, env.Code, env.Message)
	}

	// Old password no longer authenticates; the new one does.
	status, _ = hs.do("POST", "/api/v1/auth/login",
		map[string]string{"username": adminUsername, "password": adminPassword}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("login with old password after change: expected 401, got %d", status)
	}
	hs.login(adminUsername, newPwd)

	// Restore for the other tests sharing the admin account.
	token = hs.login(adminUsername, newPwd)
	status, env = hs.do("PUT", "/api/v1/auth/password", map[string]string{
		"old_password": newPwd,
		"new_password": adminPassword,
	}, token)
	if status != http.StatusOK {
		t.Fatalf("restore password: expected HTTP 200, got %d code=%d", status, env.Code)
	}
}

// TestAPIKeyLifecycle covers create → list → authenticated call → revoke.
func TestAPIKeyLifecycle(t *testing.T) {
	hs := newHarness(t)

	token := hs.login(adminUsername, adminPassword)

	status, env := hs.do("POST", "/api/v1/auth/apikeys",
		map[string]any{"name": "httpapi-test-key"}, token)
	var created struct {
		ID     int64  `json:"id"`
		RawKey string `json:"raw_key"`
	}
	hs.mustOK(status, env, "create apikey", &created)
	if created.RawKey == "" {
		t.Fatalf("create apikey: raw key not returned")
	}

	// The raw key authenticates API calls.
	reqStatus, reqEnv := hs.doAPIKey("GET", "/api/v1/system/info", created.RawKey)
	if reqStatus != http.StatusOK {
		t.Fatalf("apikey auth call: expected HTTP 200, got %d code=%d", reqStatus, reqEnv.Code)
	}

	// List shows the key (PageData envelope).
	pd := hs.listPage(token, "/api/v1/auth/apikeys?page=1&page_size=20")
	if pd.Total < 1 {
		t.Fatalf("list apikeys: expected at least 1 item, got total=%d", pd.Total)
	}

	// Revoke, then the same key must be rejected.
	status, env = hs.do("DELETE",
		"/api/v1/auth/apikeys/"+jsonInt64(created.ID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("revoke apikey: expected HTTP 200, got %d code=%d", status, env.Code)
	}
	// The router caches API key lookups for 30s, so revocation takes
	// effect within that TTL window (documented production behavior).
	deadline := time.Now().Add(35 * time.Second)
	reqStatus = 0
	for time.Now().Before(deadline) {
		reqStatus, _ = hs.doAPIKey("GET", "/api/v1/system/info", created.RawKey)
		// Revoked keys are rejected with 403 (api key revoked) once the
		// router's 30s key cache expires; 401 would also be acceptable.
		if reqStatus == http.StatusUnauthorized || reqStatus == http.StatusForbidden {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if reqStatus != http.StatusUnauthorized && reqStatus != http.StatusForbidden {
		t.Fatalf("revoked apikey: expected 401/403 within 35s, got %d", reqStatus)
	}
}

func jsonInt64(v int64) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(raw)
}
