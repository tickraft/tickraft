// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package config

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/user"
)

// newValidConfig returns a Config that passes Validate. Tests mutate specific
// fields to trigger validation failures.
func newValidConfig() *Config {
	c := &Config{}
	c.SetDefaults()
	c.Database.DSN = "sqlite://tickraft.db"
	c.Auth.JWTSecret = "test-jwt-secret-with-at-least-32-bytes-length"
	return c
}

func TestValidate_ValidConfig(t *testing.T) {
	c := newValidConfig()
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate failed for valid config: %v", err)
	}
}

func TestValidate_MissingDatabaseDSN(t *testing.T) {
	c := newValidConfig()
	c.Database.DSN = ""
	c.Database.Driver = ""
	c.Database.Addr = ""
	err := c.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for missing database config, got nil")
	}
	if !strings.Contains(err.Error(), "database.dsn or database.driver is required") {
		t.Errorf("Validate error = %q, want substring %q", err.Error(), "database.dsn or database.driver is required")
	}
}

// TestValidate_DirectFieldsValid verifies that a config with an empty DSN but
// valid direct db.Config fields (driver + address) passes validation. This
// covers the structured-fields configuration path.
func TestValidate_DirectFieldsValid(t *testing.T) {
	c := newValidConfig()
	c.Database.DSN = ""
	c.Database.Driver = "sqlite3"
	c.Database.Addr = "/var/lib/tickraft/tickraft.db"
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate failed for direct fields: %v", err)
	}
}

// TestValidate_DirectFieldsMissingDriver verifies that an empty DSN with a
// missing driver fails validation with a descriptive error.
func TestValidate_DirectFieldsMissingDriver(t *testing.T) {
	c := newValidConfig()
	c.Database.DSN = ""
	c.Database.Driver = ""
	c.Database.Addr = "/var/lib/tickraft/tickraft.db"
	err := c.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for missing driver, got nil")
	}
	if !strings.Contains(err.Error(), "database.driver is required") {
		t.Errorf("Validate error = %q, want substring %q", err.Error(), "database.driver is required")
	}
}

// TestValidate_DirectFieldsMissingAddress verifies that an empty DSN with a
// driver but missing address fails validation.
func TestValidate_DirectFieldsMissingAddress(t *testing.T) {
	c := newValidConfig()
	c.Database.DSN = ""
	c.Database.Driver = "sqlite3"
	c.Database.Addr = ""
	err := c.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for missing address, got nil")
	}
	if !strings.Contains(err.Error(), "database.addr is required") {
		t.Errorf("Validate error = %q, want substring %q", err.Error(), "database.addr is required")
	}
}

// TestValidate_DirectFieldsMemoryDatabase verifies that the :memory: address
// is rejected even in the direct-fields path.
func TestValidate_DirectFieldsMemoryDatabase(t *testing.T) {
	c := newValidConfig()
	c.Database.DSN = ""
	c.Database.Driver = "sqlite3"
	c.Database.Addr = ":memory:"
	err := c.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for memory database, got nil")
	}
	if !strings.Contains(err.Error(), "memory databases are not supported") {
		t.Errorf("Validate error = %q, want substring %q", err.Error(), "memory databases are not supported")
	}
}

// TestValidate_DSNPrecedenceOverDirectFields verifies that when both DSN and
// direct fields are set, the DSN path takes precedence and validation passes
// without checking the direct fields.
func TestValidate_DSNPrecedenceOverDirectFields(t *testing.T) {
	c := newValidConfig()
	c.Database.DSN = "sqlite://tickraft.db"
	c.Database.Driver = "" // direct fields intentionally left empty
	c.Database.Addr = ""
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate failed when DSN takes precedence: %v", err)
	}
}

// TestResolveDBConfig_DSNPath verifies that ResolveDBConfig parses the DSN
// via db.Parse when DSN is non-empty, returning a populated db.Config.
func TestResolveDBConfig_DSNPath(t *testing.T) {
	c := &DatabaseConfig{DSN: "sqlite:///var/lib/tickraft/tickraft.db"}
	resolved, err := c.ResolveDBConfig()
	if err != nil {
		t.Fatalf("ResolveDBConfig failed: %v", err)
	}
	if resolved.Driver != "sqlite3" {
		t.Errorf("resolved.Driver = %q, want %q", resolved.Driver, "sqlite3")
	}
	if resolved.Addr != "/var/lib/tickraft/tickraft.db" {
		t.Errorf("resolved.Addr = %q, want %q", resolved.Addr, "/var/lib/tickraft/tickraft.db")
	}
}

// TestResolveDBConfig_DirectFieldsPath verifies that ResolveDBConfig returns
// the embedded db.Config fields as-is when DSN is empty.
func TestResolveDBConfig_DirectFieldsPath(t *testing.T) {
	c := &DatabaseConfig{}
	c.DSN = ""
	c.Driver = "sqlite3"
	c.Addr = "/var/lib/tickraft/tickraft.db"
	c.Params = map[string]string{"journal_mode": "WAL"}
	resolved, err := c.ResolveDBConfig()
	if err != nil {
		t.Fatalf("ResolveDBConfig failed: %v", err)
	}
	if resolved.Driver != "sqlite3" {
		t.Errorf("resolved.Driver = %q, want %q", resolved.Driver, "sqlite3")
	}
	if resolved.Addr != "/var/lib/tickraft/tickraft.db" {
		t.Errorf("resolved.Addr = %q, want %q", resolved.Addr, "/var/lib/tickraft/tickraft.db")
	}
	if resolved.Params["journal_mode"] != "WAL" {
		t.Errorf("resolved.Params[journal_mode] = %q, want %q", resolved.Params["journal_mode"], "WAL")
	}
}

// TestResolveDBConfig_DSNTakesPrecedence verifies that when both DSN and
// direct fields are set, ResolveDBConfig uses the DSN path and ignores the
// direct fields.
func TestResolveDBConfig_DSNTakesPrecedence(t *testing.T) {
	c := &DatabaseConfig{}
	c.DSN = "sqlite:///from-dsn.db"
	c.Driver = "sqlite3"
	c.Addr = "/from-direct-fields.db"
	resolved, err := c.ResolveDBConfig()
	if err != nil {
		t.Fatalf("ResolveDBConfig failed: %v", err)
	}
	if resolved.Addr != "/from-dsn.db" {
		t.Errorf("resolved.Addr = %q, want %q (DSN takes precedence)", resolved.Addr, "/from-dsn.db")
	}
}

// TestResolveDBConfig_InvalidDSN verifies that ResolveDBConfig propagates
// parse errors from db.Parse when the DSN is invalid.
func TestResolveDBConfig_InvalidDSN(t *testing.T) {
	c := &DatabaseConfig{DSN: "mysql://user:pass@host:3306/db"}
	_, err := c.ResolveDBConfig()
	if err == nil {
		t.Fatalf("ResolveDBConfig: expected error for unsupported driver, got nil")
	}
}

func TestValidate_MissingJWTSecret(t *testing.T) {
	c := newValidConfig()
	c.Auth.JWTSecret = ""
	err := c.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for missing jwt_secret, got nil")
	}
	if !strings.Contains(err.Error(), "auth.jwt_secret is required") {
		t.Errorf("Validate error = %q, want substring %q", err.Error(), "auth.jwt_secret is required")
	}
}

func TestValidate_ShortJWTSecret(t *testing.T) {
	c := newValidConfig()
	c.Auth.JWTSecret = "secret"
	err := c.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for short jwt_secret, got nil")
	}
	if !strings.Contains(err.Error(), "auth.jwt_secret must be at least 32 bytes") {
		t.Errorf("Validate error = %q, want substring %q", err.Error(), "auth.jwt_secret must be at least 32 bytes")
	}
}

func TestValidate_InvalidLogLevel(t *testing.T) {
	c := newValidConfig()
	c.Logger.Level = "trace"
	err := c.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for invalid log level, got nil")
	}
	want := `config: invalid logger.level "trace", must be one of: debug, info, warn, error`
	if err.Error() != want {
		t.Errorf("Validate error = %q, want %q", err.Error(), want)
	}
}

func TestValidate_InvalidLogMode(t *testing.T) {
	c := newValidConfig()
	c.Logger.Mode = "production"
	err := c.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for invalid log mode, got nil")
	}
	want := `config: invalid logger.mode "production", must be one of: debug, release`
	if err.Error() != want {
		t.Errorf("Validate error = %q, want %q", err.Error(), want)
	}
}

func TestValidate_InvalidServerAddr(t *testing.T) {
	cases := []struct {
		name string
		addr string
	}{
		{"empty", ""},
		{"no_port", "localhost"},
		{"port_out_of_range_high", ":70000"},
		{"port_zero", ":0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newValidConfig()
			c.Server.Addr = tc.addr
			err := c.Validate()
			if err == nil {
				t.Fatalf("Validate: expected error for server.addr %q, got nil", tc.addr)
			}
			if !strings.Contains(err.Error(), "server.addr") {
				t.Errorf("Validate error = %q, want substring %q", err.Error(), "server.addr")
			}
		})
	}
}

func TestValidate_ValidServerAddrs(t *testing.T) {
	cases := []string{":8080", "0.0.0.0:8080", "127.0.0.1:9090", "localhost:8080"}
	for _, addr := range cases {
		c := newValidConfig()
		c.Server.Addr = addr
		if err := c.Validate(); err != nil {
			t.Errorf("Validate failed for addr %q: %v", addr, err)
		}
	}
}

func TestValidate_NegativePoolSizes(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
		field  string
	}{
		{"negative_worker_concurrence", func(c *Config) { c.Worker.Concurrence = -1 }, "worker.concurrence"},
		{"negative_prism_concurrence", func(c *Config) { c.Prism.Concurrence = -1 }, "prism.concurrence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newValidConfig()
			tc.mutate(c)
			err := c.Validate()
			if err == nil {
				t.Fatalf("Validate: expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("Validate error = %q, want substring %q", err.Error(), tc.field)
			}
		})
	}
}

func TestValidate_ZeroPoolSizesAllowed(t *testing.T) {
	c := newValidConfig()
	c.Worker.Concurrence = 0 // auto
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate failed for zero pool sizes: %v", err)
	}
}

func TestValidate_ZeroTokenTTL(t *testing.T) {
	c := newValidConfig()
	c.Auth.TokenTTL = Duration(0)
	err := c.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for zero token_ttl, got nil")
	}
	if !strings.Contains(err.Error(), "auth.token_ttl") {
		t.Errorf("Validate error = %q, want substring %q", err.Error(), "auth.token_ttl")
	}
}

func TestValidate_ZeroEvalInterval(t *testing.T) {
	c := newValidConfig()
	c.Prism.EvalInterval = Duration(0)
	err := c.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for zero eval_interval, got nil")
	}
	if !strings.Contains(err.Error(), "prism.eval_interval") {
		t.Errorf("Validate error = %q, want substring %q", err.Error(), "prism.eval_interval")
	}
}

func TestDuration_UnmarshalText(t *testing.T) {
	cases := []struct {
		input string
		want  time.Duration
	}{
		{"5s", 5 * time.Second},
		{"1m30s", 90 * time.Second},
		{"24h", 24 * time.Hour},
		{"500ms", 500 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			var d Duration
			if err := d.UnmarshalText([]byte(tc.input)); err != nil {
				t.Fatalf("UnmarshalText failed: %v", err)
			}
			if d.Duration() != tc.want {
				t.Errorf("Duration = %v, want %v", d.Duration(), tc.want)
			}
		})
	}
}

func TestDuration_UnmarshalText_Empty(t *testing.T) {
	var d Duration
	if err := d.UnmarshalText([]byte("")); err != nil {
		t.Fatalf("UnmarshalText failed for empty input: %v", err)
	}
	if !d.IsZero() {
		t.Errorf("Duration = %v, want zero", d)
	}
}

func TestDuration_UnmarshalText_Invalid(t *testing.T) {
	var d Duration
	err := d.UnmarshalText([]byte("not-a-duration"))
	if err == nil {
		t.Fatalf("UnmarshalText: expected error for invalid duration, got nil")
	}
}

func TestDuration_MarshalText(t *testing.T) {
	d := Duration(5 * time.Second)
	got, err := d.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText failed: %v", err)
	}
	if string(got) != "5s" {
		t.Errorf("MarshalText = %q, want %q", string(got), "5s")
	}
}

// TestValidate_RetentionDaysDefaultFallback verifies that a non-positive
// logger.retention_days is normalized to the default of 30 without failing
// validation, applying the default value.
func TestValidate_RetentionDaysDefaultFallback(t *testing.T) {
	cases := []struct {
		name string
		got  int
	}{
		{"zero", 0},
		{"negative", -5},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c := newValidConfig()
			c.Logger.RetentionDays = tc.got
			if err := c.Validate(); err != nil {
				t.Fatalf("Validate failed for %s retention_days: %v", tc.name, err)
			}
			if c.Logger.RetentionDays != 30 {
				t.Errorf("RetentionDays = %d after fallback, want 30", c.Logger.RetentionDays)
			}
		})
	}
}

// TestValidate_RetentionDaysRespected verifies that a valid positive
// logger.retention_days is preserved unchanged through validation.
func TestValidate_RetentionDaysRespected(t *testing.T) {
	c := newValidConfig()
	c.Logger.RetentionDays = 90
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if c.Logger.RetentionDays != 90 {
		t.Errorf("RetentionDays = %d, want 90", c.Logger.RetentionDays)
	}
}

// TestValidate_I18nDefaultLocaleMissing verifies that an empty
// i18n.default_locale fails validation. The default locale is the fallback
// terminator for the Registry; an empty value would break the fallback chain.
func TestValidate_I18nDefaultLocaleMissing(t *testing.T) {
	c := newValidConfig()
	c.I18n.DefaultLocale = ""
	err := c.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for empty default_locale, got nil")
	}
	if !strings.Contains(err.Error(), "i18n.default_locale is required") {
		t.Errorf("Validate error = %q, want substring %q", err.Error(), "i18n.default_locale is required")
	}
}

// TestValidate_I18nDefaultLocaleInvalid verifies that a syntactically invalid
// i18n.default_locale fails validation. Full BCP 47 validation is deferred to
// pkg/i18n.Parse, but obviously malformed values are rejected early.
func TestValidate_I18nDefaultLocaleInvalid(t *testing.T) {
	c := newValidConfig()
	c.I18n.DefaultLocale = "zh CN!"
	c.I18n.SupportedLocales = []string{"zh CN!"}
	err := c.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for invalid default_locale, got nil")
	}
	if !strings.Contains(err.Error(), "invalid i18n.default_locale") {
		t.Errorf("Validate error = %q, want substring %q", err.Error(), "invalid i18n.default_locale")
	}
}

// TestValidate_I18nDefaultLocaleNotInSupported verifies that the default
// locale must be reachable from supported_locales so the frontend language
// switcher always offers the fallback locale. Language-only matches are
// accepted (e.g. "zh" covers default "zh-Hans").
func TestValidate_I18nDefaultLocaleNotInSupported(t *testing.T) {
	c := newValidConfig()
	c.I18n.DefaultLocale = "zh-Hans"
	c.I18n.SupportedLocales = []string{"en-US", "ja"}
	err := c.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for default_locale not in supported_locales, got nil")
	}
	if !strings.Contains(err.Error(), "must be listed in i18n.supported_locales") {
		t.Errorf("Validate error = %q, want substring %q", err.Error(), "must be listed in i18n.supported_locales")
	}
}

// TestValidate_I18nLanguageOnlyMatchAccepted verifies that the default locale
// is accepted when supported_locales contains a language-only match (e.g.
// default "zh-Hans" is covered by entry "zh"), mirroring the Registry fallback
// chain semantics.
func TestValidate_I18nLanguageOnlyMatchAccepted(t *testing.T) {
	c := newValidConfig()
	c.I18n.DefaultLocale = "zh-Hans"
	c.I18n.SupportedLocales = []string{"zh", "en-US"}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate failed for language-only match: %v", err)
	}
}

// TestValidate_I18nEmptySupportedLocales verifies that an empty
// supported_locales list passes validation. SetDefaults populates the builtin
// set, but a YAML file that explicitly sets an empty list is treated as
// "use builtin" to avoid surprising operators.
func TestValidate_I18nEmptySupportedLocales(t *testing.T) {
	c := newValidConfig()
	c.I18n.SupportedLocales = []string{}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate failed for empty supported_locales: %v", err)
	}
}

// TestValidate_I18nExtendedLocales verifies that an extended-style locale
// list passes validation. This documents that operators can extend
// supported_locales when extended locale packs are registered.
func TestValidate_I18nExtendedLocales(t *testing.T) {
	c := newValidConfig()
	c.I18n.DefaultLocale = "zh-Hans"
	c.I18n.SupportedLocales = []string{"zh-Hans", "zh-Hant", "en-US", "en-GB", "ja"}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate failed for extended locales: %v", err)
	}
}

// TestValidate_AdminUsernameDefault verifies that the default admin username
// "admin" passes validation. SetDefaults populates this value, so a config
// that does not override auth.admin_username must validate cleanly.
func TestValidate_AdminUsernameDefault(t *testing.T) {
	c := newValidConfig()
	// newValidConfig already calls SetDefaults, so AdminUsername == "admin".
	if c.Auth.AdminUsername != "admin" {
		t.Fatalf("expected default admin username %q, got %q", "admin", c.Auth.AdminUsername)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate failed for default admin username: %v", err)
	}
}

// TestValidate_AdminUsernameValid verifies that valid custom admin usernames
// pass validation. These are the same names that pkg/user.ValidateUsername
// and pkg/db.EnsureAdminUser accept, ensuring config load, DB init, and login
// all agree on the canonical rule.
func TestValidate_AdminUsernameValid(t *testing.T) {
	validNames := []string{"admin", "admin_user", "user123", "root_admin", strings.Repeat("a", 64)}
	for _, name := range validNames {
		c := newValidConfig()
		c.Auth.AdminUsername = name
		if err := c.Validate(); err != nil {
			t.Errorf("Validate failed for valid admin_username %q: %v", name, err)
		}
	}
}

// TestValidate_AdminUsernameInvalid verifies that illegal admin usernames
// (hyphens, dots, spaces, too short, too long, empty) are rejected at config
// load time with a descriptive error that wraps user.ErrInvalidUsername.
// This is the fail-fast guard against the "initialized but cannot log in"
// bug where EnsureAdminUser succeeds but Service.Login rejects the same name.
func TestValidate_AdminUsernameInvalid(t *testing.T) {
	cases := []string{
		"ad",                    // too short
		"admin-user",            // hyphen
		"admin.user",            // dot
		"admin user",            // space
		"",                      // empty
		strings.Repeat("a", 65), // too long
	}
	for _, name := range cases {
		name := name
		t.Run(name, func(t *testing.T) {
			c := newValidConfig()
			c.Auth.AdminUsername = name
			err := c.Validate()
			if err == nil {
				t.Fatalf("Validate: expected error for admin_username %q, got nil", name)
			}
			if !strings.Contains(err.Error(), "auth.admin_username") {
				t.Errorf("Validate error = %q, want substring %q", err.Error(), "auth.admin_username")
			}
			if !errors.Is(err, user.ErrInvalidUsername) {
				t.Errorf("Validate error for %q does not wrap user.ErrInvalidUsername: %v", name, err)
			}
		})
	}
}
