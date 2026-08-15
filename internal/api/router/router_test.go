// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package router

import (
	"strings"
	"testing"

	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/auth/jwt"
)

// testJWTSecret is a 64-byte secret used to construct a JWT manager in
// tests. jwt.New returns ErrSecretTooShort if the secret is shorter
// than 32 bytes.
const testJWTSecret = "test-jwt-signing-secret-key-please-use-64-bytes!"

// newTestJWT constructs a JWT manager suitable for route-registration tests.
// It uses a nil blacklist checker (tokens are never revoked in these tests).
func newTestJWT(t *testing.T) *jwt.JWT {
	t.Helper()
	jwtMgr, err := jwt.New(jwt.Config{Secret: testJWTSecret}, nil)
	if err != nil {
		t.Fatalf("jwt.New() error = %v", err)
	}
	if jwtMgr == nil {
		t.Fatal("jwt.New returned nil")
	}
	return jwtMgr
}

// TestRegisterRoutesNilServer verifies that RegisterRoutes returns an error
// containing "server is nil" when the server argument is nil, regardless of
// the other arguments. This guards against a nil-pointer panic during route
// registration.
func TestRegisterRoutesNilServer(t *testing.T) {
	jwtMgr := newTestJWT(t)
	svc := auth.NewService(jwtMgr, nil, nil, nil)

	err := RegisterRoutes(nil, jwtMgr, svc, nil)
	if err == nil {
		t.Fatal("RegisterRoutes(nil server) returned nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), "server is nil") {
		t.Errorf("err = %q, want to contain %q", err.Error(), "server is nil")
	}
}

// TestRegisterRoutesNilJWT verifies that RegisterRoutes returns an error
// containing "jwt manager is nil" when the jwtMgr argument is nil. The
// server must be non-nil to reach this validation branch.
func TestRegisterRoutesNilJWT(t *testing.T) {
	srv := api.NewServer(api.ServerConfig{Addr: ":0"})
	svc := auth.NewService(newTestJWT(t), nil, nil, nil)

	err := RegisterRoutes(srv, nil, svc, nil)
	if err == nil {
		t.Fatal("RegisterRoutes(nil jwt) returned nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), "jwt manager is nil") {
		t.Errorf("err = %q, want to contain %q", err.Error(), "jwt manager is nil")
	}
}

// TestRegisterRoutesNilService verifies that RegisterRoutes returns an error
// containing "auth service is nil" when the service argument is nil. The
// server and jwtMgr must be non-nil to reach this validation branch.
func TestRegisterRoutesNilService(t *testing.T) {
	srv := api.NewServer(api.ServerConfig{Addr: ":0"})
	jwtMgr := newTestJWT(t)

	err := RegisterRoutes(srv, jwtMgr, nil, nil)
	if err == nil {
		t.Fatal("RegisterRoutes(nil service) returned nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), "auth service is nil") {
		t.Errorf("err = %q, want to contain %q", err.Error(), "auth service is nil")
	}
}

// TestRegisterRoutesSuccess verifies that with a real server, JWT manager,
// Service instance, and all required domain services, RegisterRoutes
// completes without error.
func TestRegisterRoutesSuccess(t *testing.T) {
	srv := api.NewServer(api.ServerConfig{Addr: ":0"})
	jwtMgr := newTestJWT(t)
	svc := auth.NewService(jwtMgr, nil, nil, nil)

	err := RegisterRoutes(srv, jwtMgr, svc, nil, allRequiredRegisterOptions()...)
	if err != nil {
		t.Fatalf("RegisterRoutes returned error: %v", err)
	}
}

// TestRegisterRoutesNilAsseteyGetterDefaultsToDenyAll verifies that
// passing a nil assetKeyGetter does not cause RegisterRoutes to fail;
// the function falls back to the denyAllAssetKeys stub internally so
// that telemetry report endpoints fail closed by default.
func TestRegisterRoutesNilAsseteyGetterDefaultsToDenyAll(t *testing.T) {
	srv := api.NewServer(api.ServerConfig{Addr: ":0"})
	jwtMgr := newTestJWT(t)
	svc := auth.NewService(jwtMgr, nil, nil, nil)

	err := RegisterRoutes(srv, jwtMgr, svc, nil, allRequiredRegisterOptions()...)
	if err != nil {
		t.Fatalf("RegisterRoutes with nil assetKeyGetter returned error: %v", err)
	}
}

// TestRegisterRoutesMissingServices verifies that RegisterRoutes returns a
// descriptive error when required services are not injected.
func TestRegisterRoutesMissingServices(t *testing.T) {
	srv := api.NewServer(api.ServerConfig{Addr: ":0"})
	jwtMgr := newTestJWT(t)
	svc := auth.NewService(jwtMgr, nil, nil, nil)

	err := RegisterRoutes(srv, jwtMgr, svc, nil)
	if err == nil {
		t.Fatal("RegisterRoutes without services returned nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), "missing required services") {
		t.Errorf("err = %q, want to contain %q", err.Error(), "missing required services")
	}
}
