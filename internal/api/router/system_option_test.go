// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package router

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/api/handler/system"
	"github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/auth/jwt"
)

// fakeSystemService is a stub system.Service used to verify that
// router.WithSystemService plumbs an injected service all the way through
// to the /api/v1/system handlers. The fake returns distinguishable values
// (Version="fake-injected", BuildTags="test") so tests can assert that the
// response originated from the fake rather than the in-memory default.
type fakeSystemService struct {
	info  *system.Info
	cfg   *system.Config
	calls int
}

func (f *fakeSystemService) GetConfig(_ context.Context) (*system.Config, error) {
	f.calls++
	return f.cfg, nil
}

func (f *fakeSystemService) UpdateConfig(_ context.Context, req *system.Config) (*system.Config, error) {
	f.calls++
	return req, nil
}

func (f *fakeSystemService) GetInfo(_ context.Context) (*system.Info, error) {
	f.calls++
	return f.info, nil
}

// GetGlobalStats returns system-wide aggregate statistics for the dashboard
// overview. The fake returns a zero-valued GlobalStats because the behavioral
// tests in this file only exercise the /api/v1/system/info endpoint.
func (f *fakeSystemService) GetGlobalStats(_ context.Context) (*system.GlobalStats, error) {
	f.calls++
	return &system.GlobalStats{}, nil
}

// Compile-time assertion that fakeSystemService satisfies the
// system.Service interface.
var _ system.Service = (*fakeSystemService)(nil)

// newFakeSystemService returns a fakeSystemService seeded with values that
// are distinguishable from the in-memory defaults (which use Version="dev"
// and BuildTags="").
func newFakeSystemService() *fakeSystemService {
	return &fakeSystemService{
		info: &system.Info{
			Version:   "fake-injected",
			BuildTags: "test",
			StartTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			Uptime:    "1s",
		},
		cfg: &system.Config{
			LogLevel:      "debug",
			DefaultLang:   "en-US",
			RetentionDays: 7,
		},
	}
}

// freeAddr returns a string "127.0.0.1:port" for a port that is currently
// free. It works by opening a listener on port 0, reading the assigned port,
// then closing the listener so the test server can bind to it. There is a
// small TOCTOU window, but in practice the port is reused immediately by
// the same process and the test server binds before any other process can
// claim it.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freeAddr: listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("freeAddr: close: %v", err)
	}
	return addr
}

// mintAccessToken generates a valid JWT access token for the given JWT
// manager. It is used to authenticate GET /api/v1/system/info requests in
// behavioral tests.
func mintAccessToken(t *testing.T, jwtMgr *jwt.JWT) string {
	t.Helper()
	pair, err := jwtMgr.GenerateTokenPair(jwt.UserClaims{
		UID:      1,
		Username: "tester",
		Role:     auth.RoleAdmin,
		TenantID: 1,
	})
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}
	return pair.AccessToken
}

// startTestServer builds a Server with the given RegisterOptions (plus all
// required standalone services), calls RegisterRoutes, and starts serving in
// a goroutine. It returns the server address and a shutdown function the
// caller must invoke via defer. The assetKeyGetter is nil so telemetry report
// endpoints fail closed.
func startTestServer(t *testing.T, opts ...RegisterOption) (addr string, shutdown func()) {
	t.Helper()
	addr = freeAddr(t)
	srv := api.NewServer(api.ServerConfig{Addr: addr})
	jwtMgr := newTestJWT(t)
	svc := auth.NewService(jwtMgr, nil, nil, nil)

	// Prepend all required services, then append caller-provided
	// overrides (e.g. WithSystemService) so the caller's option takes
	// precedence.
	allOpts := allRequiredRegisterOptions()
	allOpts = append(allOpts, opts...)
	if err := RegisterRoutes(srv, jwtMgr, svc, nil, allOpts...); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	// Poll until the server is accepting connections or fails to start.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("server exited during startup: %v", err)
			}
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}

	shutdown = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	return addr, shutdown
}

// doSystemInfoGET performs an authenticated GET /api/v1/system/info against
// the given address and returns the parsed SystemInfo payload.
func doSystemInfoGET(t *testing.T, addr, token string) system.Info {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/api/v1/system/info", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%q)", resp.StatusCode, http.StatusOK, string(body))
	}

	// Response shape: {"code":0,"message":"ok","data":{...SystemInfo...}}
	var envelope struct {
		Code    int         `json:"code"`
		Message string      `json:"message"`
		Data    system.Info `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal response: %v (body=%q)", err, string(body))
	}
	if envelope.Code != 0 {
		t.Fatalf("envelope code = %d, message = %q", envelope.Code, envelope.Message)
	}
	return envelope.Data
}

// TestWithSystemServiceReturnsRegisterOption verifies that WithSystemService
// returns a non-nil RegisterOption. This is a compile-time guard that the
// function exists and has the correct signature; it also prevents silent
// regressions if the option type is refactored.
func TestWithSystemServiceReturnsRegisterOption(t *testing.T) {
	opt := WithSystemService(newFakeSystemService())
	if opt == nil {
		t.Fatal("WithSystemService returned nil, want non-nil RegisterOption")
	}
}

// TestRegisterRoutesWithSystemServiceSucceeds verifies that passing
// WithSystemService does not break RegisterRoutes. This mirrors the existing
// TestRegisterRoutesSuccess pattern.
func TestRegisterRoutesWithSystemServiceSucceeds(t *testing.T) {
	srv := api.NewServer(api.ServerConfig{Addr: ":0"})
	jwtMgr := newTestJWT(t)
	svc := auth.NewService(jwtMgr, nil, nil, nil)

	opts := allRequiredRegisterOptions()
	opts = append(opts, WithSystemService(newFakeSystemService()))
	err := RegisterRoutes(srv, jwtMgr, svc, nil, opts...)
	if err != nil {
		t.Fatalf("RegisterRoutes with WithSystemService returned error: %v", err)
	}
}

// TestWithSystemServiceInjectsCustomService is a behavioral test that
// verifies the service passed to WithSystemService is actually used by the
// /api/v1/system/info endpoint. The fake service returns Version="fake-injected",
// which is distinct from the in-memory default of "dev", so observing it in
// the HTTP response proves the injection wired all the way through.
func TestWithSystemServiceInjectsCustomService(t *testing.T) {
	fake := newFakeSystemService()
	addr, shutdown := startTestServer(t, WithSystemService(fake))
	defer shutdown()

	token := mintAccessToken(t, newTestJWT(t))
	info := doSystemInfoGET(t, addr, token)

	if info.Version != "fake-injected" {
		t.Errorf("Version = %q, want %q (fake service not wired)", info.Version, "fake-injected")
	}
	if info.BuildTags != "test" {
		t.Errorf("BuildTags = %q, want %q", info.BuildTags, "test")
	}
	if fake.calls != 1 {
		t.Errorf("fake.GetInfo call count = %d, want 1", fake.calls)
	}
}

// TestRegisterRoutesWithInjectedSystemService verifies that the system
// service injected via allRequiredRegisterOptions is the one used by the
// /api/v1/system/info endpoint. The helper's fake service returns Version="fake-injected".
func TestRegisterRoutesWithInjectedSystemService(t *testing.T) {
	addr, shutdown := startTestServer(t)
	defer shutdown()

	token := mintAccessToken(t, newTestJWT(t))
	info := doSystemInfoGET(t, addr, token)

	if info.Version != "fake-injected" {
		t.Errorf("Version = %q, want %q", info.Version, "fake-injected")
	}
	if info.BuildTags != "test" {
		t.Errorf("BuildTags = %q, want %q", info.BuildTags, "test")
	}
}

// TestWithSystemServiceNilFailsRegistration verifies that passing a nil
// system service causes RegisterRoutes to fail in standalone mode, since
// system service is a required injection.
func TestWithSystemServiceNilFailsRegistration(t *testing.T) {
	srv := api.NewServer(api.ServerConfig{Addr: ":0"})
	jwtMgr := newTestJWT(t)
	svc := auth.NewService(jwtMgr, nil, nil, nil)

	var nilSvc system.Service = nil
	err := RegisterRoutes(srv, jwtMgr, svc, nil, WithSystemService(nilSvc))
	if err == nil {
		t.Fatal("RegisterRoutes with nil WithSystemService returned nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), "system service") {
		t.Errorf("err = %q, want to contain %q", err.Error(), "system service")
	}
}
