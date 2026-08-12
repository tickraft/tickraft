// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package config

import (
	"os"
	"testing"
	"time"
)

// validYAML is a minimal YAML config that passes validation. Secret fields
// use env var interpolation so tests can inject values via t.Setenv.
const validYAML = `
server:
  addr: ":6153"
  enable_cors: true
  enable_access_log: true
  max_header_bytes: 1048576
  read_timeout: "10s"
  write_timeout: "20s"
  maintenance_interval: "5m"
worker:
  concurrence: 4
  probe_timeout: "5s"
prism:
  eval_interval: "30s"
  concurrence: 8
database:
  dsn: ${TICKRAFT_TEST_DB_DSN}
auth:
  jwt_secret: ${TICKRAFT_TEST_JWT_SECRET}
  token_ttl: "24h"
logger:
  level: "info"
  mode: "debug"
`

func TestLoadFromBytes_FullConfig(t *testing.T) {
	t.Setenv("TICKRAFT_TEST_DB_DSN", "sqlite://tickraft.db")
	t.Setenv("TICKRAFT_TEST_JWT_SECRET", "super-secret-key-with-at-least-32-bytes")

	cfg, err := LoadFromBytes([]byte(validYAML))
	if err != nil {
		t.Fatalf("LoadFromBytes failed: %v", err)
	}
	if cfg.Server.Addr != ":6153" {
		t.Errorf("server.addr = %q, want %q", cfg.Server.Addr, ":6153")
	}
	if !cfg.Server.EnableCORS {
		t.Errorf("server.enable_cors = false, want true")
	}
	if cfg.Server.ReadTimeout.Duration() != 10*time.Second {
		t.Errorf("server.read_timeout = %v, want 10s", cfg.Server.ReadTimeout)
	}
	if cfg.Database.DSN != "sqlite://tickraft.db" {
		t.Errorf("database.dsn = %q, want interpolated value", cfg.Database.DSN)
	}
	if cfg.Auth.JWTSecret != "super-secret-key-with-at-least-32-bytes" {
		t.Errorf("auth.jwt_secret = %q, want interpolated value", cfg.Auth.JWTSecret)
	}
	if cfg.Auth.TokenTTL.Duration() != 24*time.Hour {
		t.Errorf("auth.token_ttl = %v, want 24h", cfg.Auth.TokenTTL)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate failed: %v", err)
	}
}

func TestLoadFromBytes_DefaultsApplied(t *testing.T) {
	// Minimal YAML: only the required fields. All other fields should get
	// their defaults from SetDefaults.
	const yaml = `
database:
  dsn: "/tmp/tickraft.db"
auth:
  jwt_secret: "secret"
`
	t.Setenv("TICKRAFT_TEST_DB_DSN", "unused")
	t.Setenv("TICKRAFT_TEST_JWT_SECRET", "unused")

	cfg, err := LoadFromBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadFromBytes failed: %v", err)
	}
	// Server defaults.
	if cfg.Server.Addr != ":6153" {
		t.Errorf("server.addr default = %q, want %q", cfg.Server.Addr, ":6153")
	}
	if !cfg.Server.EnableCORS {
		t.Errorf("server.enable_cors default = false, want true")
	}
	if !cfg.Server.EnableAccessLog {
		t.Errorf("server.enable_access_log default = false, want true")
	}
	if cfg.Server.MaxHeaderBytes != 1048576 {
		t.Errorf("server.max_header_bytes default = %d, want 1048576", cfg.Server.MaxHeaderBytes)
	}
	// Worker defaults.
	if cfg.Worker.ProbeTimeout.Duration() != 5*time.Second {
		t.Errorf("worker.probe_timeout default = %v, want 5s", cfg.Worker.ProbeTimeout)
	}
	// Prism defaults.
	if cfg.Prism.EvalInterval.Duration() != 30*time.Second {
		t.Errorf("prism.eval_interval default = %v, want 30s", cfg.Prism.EvalInterval)
	}
	if cfg.Prism.Concurrence != 8 {
		t.Errorf("prism.concurrence default = %d, want 8", cfg.Prism.Concurrence)
	}
	// Server maintenance_interval default.
	if cfg.Server.MaintenanceInterval.Duration() != 5*time.Minute {
		t.Errorf("server.maintenance_interval default = %v, want 5m", cfg.Server.MaintenanceInterval)
	}
	// Auth defaults.
	if cfg.Auth.TokenTTL.Duration() != 24*time.Hour {
		t.Errorf("auth.token_ttl default = %v, want 24h", cfg.Auth.TokenTTL)
	}
	// Logger defaults.
	if cfg.Logger.Level != "info" {
		t.Errorf("logger.level default = %q, want %q", cfg.Logger.Level, "info")
	}
	if cfg.Logger.Mode != "debug" {
		t.Errorf("logger.mode default = %q, want %q", cfg.Logger.Mode, "debug")
	}
}

func TestLoadFromBytes_YAMLOverridesDefaults(t *testing.T) {
	const yaml = `
server:
  addr: ":9090"
  enable_cors: false
database:
  dsn: "/tmp/test.db"
auth:
  jwt_secret: "secret"
  token_ttl: "1h"
logger:
  level: "debug"
`
	cfg, err := LoadFromBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadFromBytes failed: %v", err)
	}
	if cfg.Server.Addr != ":9090" {
		t.Errorf("server.addr = %q, want %q (YAML override)", cfg.Server.Addr, ":9090")
	}
	if cfg.Server.EnableCORS {
		t.Errorf("server.enable_cors = true, want false (YAML override)")
	}
	if cfg.Auth.TokenTTL.Duration() != 1*time.Hour {
		t.Errorf("auth.token_ttl = %v, want 1h (YAML override)", cfg.Auth.TokenTTL)
	}
	if cfg.Logger.Level != "debug" {
		t.Errorf("logger.level = %q, want %q (YAML override)", cfg.Logger.Level, "debug")
	}
	// Unset fields keep defaults.
	if !cfg.Server.EnableAccessLog {
		t.Errorf("server.enable_access_log = false, want true (default retained)")
	}
}

func TestInterpolateEnvVars_SimpleExpansion(t *testing.T) {
	t.Setenv("TICKRAFT_FOO", "bar-value")
	got, err := Interpolate("key: ${TICKRAFT_FOO}")
	if err != nil {
		t.Fatalf("Interpolate failed: %v", err)
	}
	if want := "key: bar-value"; got != want {
		t.Errorf("Interpolate = %q, want %q", got, want)
	}
}

func TestInterpolateEnvVars_WithDefault(t *testing.T) {
	os.Unsetenv("TICKRAFT_UNSET_VAR")
	got, err := Interpolate("key: ${TICKRAFT_UNSET_VAR:-fallback}")
	if err != nil {
		t.Fatalf("Interpolate failed: %v", err)
	}
	if want := "key: fallback"; got != want {
		t.Errorf("Interpolate = %q, want %q", got, want)
	}
}

func TestInterpolateEnvVars_DefaultUsedWhenEnvEmpty(t *testing.T) {
	t.Setenv("TICKRAFT_EMPTY_VAR", "")
	got, err := Interpolate("key: ${TICKRAFT_EMPTY_VAR:-fallback}")
	if err != nil {
		t.Fatalf("Interpolate failed: %v", err)
	}
	// os.LookupEnv returns (val="", ok=true) for an explicitly empty var, so
	// the env value (empty) is used rather than the default. This documents
	// the LookupEnv semantics.
	if want := "key: "; got != want {
		t.Errorf("Interpolate = %q, want %q", got, want)
	}
}

func TestInterpolateEnvVars_EnvSetIgnoresDefault(t *testing.T) {
	t.Setenv("TICKRAFT_SET_VAR", "actual")
	got, err := Interpolate("key: ${TICKRAFT_SET_VAR:-fallback}")
	if err != nil {
		t.Fatalf("Interpolate failed: %v", err)
	}
	if want := "key: actual"; got != want {
		t.Errorf("Interpolate = %q, want %q", got, want)
	}
}

func TestInterpolateEnvVars_EmptyDefault(t *testing.T) {
	os.Unsetenv("TICKRAFT_UNSET_VAR2")
	got, err := Interpolate("key: ${TICKRAFT_UNSET_VAR2:-}")
	if err != nil {
		t.Fatalf("Interpolate failed: %v", err)
	}
	if want := "key: "; got != want {
		t.Errorf("Interpolate = %q, want %q", got, want)
	}
}

func TestInterpolateEnvVars_MissingVarNoDefault(t *testing.T) {
	os.Unsetenv("TICKRAFT_MISSING_VAR")
	_, err := Interpolate("key: ${TICKRAFT_MISSING_VAR}")
	if err == nil {
		t.Fatalf("Interpolate: expected error for missing var, got nil")
	}
	if want := "config: environment variable TICKRAFT_MISSING_VAR is not set"; err.Error() != want {
		t.Errorf("Interpolate error = %q, want %q", err.Error(), want)
	}
}

func TestInterpolateEnvVars_NestedInYAML(t *testing.T) {
	t.Setenv("TICKRAFT_HOST", "db.internal")
	t.Setenv("TICKRAFT_PORT", "5432")
	t.Setenv("TICKRAFT_PASS", "s3cr3t")
	const input = `
database:
  dsn: "postgres://user:${TICKRAFT_PASS}@${TICKRAFT_HOST}:${TICKRAFT_PORT}/tickraft"
`
	got, err := Interpolate(input)
	if err != nil {
		t.Fatalf("Interpolate failed: %v", err)
	}
	want := `
database:
  dsn: "postgres://user:s3cr3t@db.internal:5432/tickraft"
`
	if got != want {
		t.Errorf("Interpolate = %q, want %q", got, want)
	}
}

func TestInterpolateEnvVars_NoVars(t *testing.T) {
	input := "plain string with no vars"
	got, err := Interpolate(input)
	if err != nil {
		t.Fatalf("Interpolate failed: %v", err)
	}
	if got != input {
		t.Errorf("Interpolate = %q, want %q", got, input)
	}
}

func TestInterpolateEnvVars_MultipleRefsSameLine(t *testing.T) {
	t.Setenv("TICKRAFT_A", "1")
	t.Setenv("TICKRAFT_B", "2")
	got, err := Interpolate("${TICKRAFT_A}-${TICKRAFT_B}-${TICKRAFT_A}")
	if err != nil {
		t.Fatalf("Interpolate failed: %v", err)
	}
	if want := "1-2-1"; got != want {
		t.Errorf("Interpolate = %q, want %q", got, want)
	}
}

func TestLoadFromBytes_EnvInterpolationInDSN(t *testing.T) {
	t.Setenv("TICKRAFT_DB_PASSWORD", "hunter2")
	const yaml = `
database:
  dsn: "postgres://user:${TICKRAFT_DB_PASSWORD}@localhost:5432/db"
auth:
  jwt_secret: "secret"
`
	cfg, err := LoadFromBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadFromBytes failed: %v", err)
	}
	if want := "postgres://user:hunter2@localhost:5432/db"; cfg.Database.DSN != want {
		t.Errorf("database.dsn = %q, want %q", cfg.Database.DSN, want)
	}
}

// TestLoadFromBytes_DirectFieldsPath verifies that the database can be
// configured via the direct db.Config fields (driver, addr, params) instead
// of a DSN string. When dsn is empty, the embedded fields are used directly.
func TestLoadFromBytes_DirectFieldsPath(t *testing.T) {
	const yaml = `
database:
  driver: sqlite3
  addr: /var/lib/tickraft/tickraft.db
  params:
    journal_mode: WAL
    busy_timeout: "5000"
auth:
  jwt_secret: "super-secret-key-with-at-least-32-bytes"
`
	cfg, err := LoadFromBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadFromBytes failed: %v", err)
	}
	if cfg.Database.DSN != "" {
		t.Errorf("database.dsn = %q, want empty (direct fields path)", cfg.Database.DSN)
	}
	if cfg.Database.Driver != "sqlite3" {
		t.Errorf("database.driver = %q, want %q", cfg.Database.Driver, "sqlite3")
	}
	if cfg.Database.Addr != "/var/lib/tickraft/tickraft.db" {
		t.Errorf("database.addr = %q, want %q", cfg.Database.Addr, "/var/lib/tickraft/tickraft.db")
	}
	if cfg.Database.Params["journal_mode"] != "WAL" {
		t.Errorf("database.params.journal_mode = %q, want %q", cfg.Database.Params["journal_mode"], "WAL")
	}
	if cfg.Database.Params["busy_timeout"] != "5000" {
		t.Errorf("database.params.busy_timeout = %q, want %q", cfg.Database.Params["busy_timeout"], "5000")
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate failed for direct fields path: %v", err)
	}
}

// TestLoadFromBytes_DSNPrecedenceOverDirectFields verifies that when both dsn
// and direct fields are present in the YAML, the DSN takes precedence.
func TestLoadFromBytes_DSNPrecedenceOverDirectFields(t *testing.T) {
	const yaml = `
database:
  dsn: "sqlite:///primary.db"
  driver: sqlite3
  addr: /should/be/ignored.db
auth:
  jwt_secret: "secret"
`
	cfg, err := LoadFromBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadFromBytes failed: %v", err)
	}
	if cfg.Database.DSN != "sqlite:///primary.db" {
		t.Errorf("database.dsn = %q, want %q", cfg.Database.DSN, "sqlite:///primary.db")
	}
	// Direct fields are still populated from YAML, but ResolveDBConfig should
	// ignore them when DSN is non-empty.
	if cfg.Database.Addr != "/should/be/ignored.db" {
		t.Errorf("database.addr = %q, want %q", cfg.Database.Addr, "/should/be/ignored.db")
	}
	resolved, err := cfg.Database.ResolveDBConfig()
	if err != nil {
		t.Fatalf("ResolveDBConfig failed: %v", err)
	}
	// DSN path: db.Parse produces Addr from the DSN, not from the direct field.
	if resolved.Addr != "/primary.db" {
		t.Errorf("resolved.Addr = %q, want %q (DSN takes precedence)", resolved.Addr, "/primary.db")
	}
}

func TestLoadFromBytes_MissingEnvVarReturnsError(t *testing.T) {
	os.Unsetenv("TICKRAFT_TOTALLY_MISSING")
	const yaml = `
database:
  dsn: ${TICKRAFT_TOTALLY_MISSING}
auth:
  jwt_secret: "secret"
`
	_, err := LoadFromBytes([]byte(yaml))
	if err == nil {
		t.Fatalf("LoadFromBytes: expected error for missing env var, got nil")
	}
}

func TestLoadFromBytes_InvalidYAML(t *testing.T) {
	const yaml = `
database:
  dsn: "path"
  bad_indent: "oops"
 auth:
  jwt_secret: "x"
`
	_, err := LoadFromBytes([]byte(yaml))
	if err == nil {
		t.Fatalf("LoadFromBytes: expected error for invalid YAML, got nil")
	}
}

func TestLoadFromBytes_InvalidDuration(t *testing.T) {
	const yaml = `
database:
  dsn: "path"
auth:
  jwt_secret: "secret"
  token_ttl: "not-a-duration"
`
	_, err := LoadFromBytes([]byte(yaml))
	if err == nil {
		t.Fatalf("LoadFromBytes: expected error for invalid duration, got nil")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatalf("Load: expected error for missing file, got nil")
	}
}

// TestInterpolate_Exported verifies the exported Interpolate function behaves
// correctly for simple expansion, default value substitution, and missing
// variable error reporting. Extended editions rely on this exported entry
// point to apply the same interpolation semantics to arbitrary config blobs.
func TestInterpolate_Exported(t *testing.T) {
	t.Run("simple expansion", func(t *testing.T) {
		t.Setenv("TICKRAFT_FOO", "bar")
		got, err := Interpolate("key: ${TICKRAFT_FOO}")
		if err != nil {
			t.Fatalf("Interpolate failed: %v", err)
		}
		if want := "key: bar"; got != want {
			t.Errorf("Interpolate = %q, want %q", got, want)
		}
	})

	t.Run("default value", func(t *testing.T) {
		os.Unsetenv("TICKRAFT_UNSET")
		got, err := Interpolate("key: ${TICKRAFT_UNSET:-fallback}")
		if err != nil {
			t.Fatalf("Interpolate failed: %v", err)
		}
		if want := "key: fallback"; got != want {
			t.Errorf("Interpolate = %q, want %q", got, want)
		}
	})

	t.Run("missing variable returns error", func(t *testing.T) {
		os.Unsetenv("TICKRAFT_MISSING")
		_, err := Interpolate("key: ${TICKRAFT_MISSING}")
		if err == nil {
			t.Fatalf("Interpolate: expected error for missing var, got nil")
		}
		if want := "config: environment variable TICKRAFT_MISSING is not set"; err.Error() != want {
			t.Errorf("Interpolate error = %q, want %q", err.Error(), want)
		}
	})
}
