// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package config

import "testing"

// TestServerConfigDefaultAddr verifies that ServerConfig.SetDefaults
// populates Addr with the canonical default listen address ":6153".
// The default port is the foundation of the single-port deployment model;
// drifting from this default would silently break standalone deployments
// that rely on the built-in default.
func TestServerConfigDefaultAddr(t *testing.T) {
	var c ServerConfig
	c.SetDefaults()

	if c.Addr != ":6153" {
		t.Errorf("ServerConfig.Addr default = %q, want %q", c.Addr, ":6153")
	}
}

// TestAuthConfigDefaultAdminUsername verifies that AuthConfig.SetDefaults
// populates AdminUsername with the canonical default "admin".
func TestAuthConfigDefaultAdminUsername(t *testing.T) {
	var c AuthConfig
	c.SetDefaults()

	if c.AdminUsername != "admin" {
		t.Errorf("AuthConfig.AdminUsername default = %q, want %q", c.AdminUsername, "admin")
	}
}

// TestAuthConfigDefaultAdminPasswordEmpty verifies that AuthConfig.SetDefaults
// leaves AdminPassword empty so a random password is generated at startup.
func TestAuthConfigDefaultAdminPasswordEmpty(t *testing.T) {
	var c AuthConfig
	c.SetDefaults()

	if c.AdminPassword != "" {
		t.Errorf("AuthConfig.AdminPassword default = %q, want empty", c.AdminPassword)
	}
}

// TestLoggerConfigDefaultRetentionDays verifies that LoggerConfig.SetDefaults
// populates RetentionDays with the canonical default of 30 days.
func TestLoggerConfigDefaultRetentionDays(t *testing.T) {
	var c LoggerConfig
	c.SetDefaults()

	if c.RetentionDays != 30 {
		t.Errorf("LoggerConfig.RetentionDays default = %d, want 30", c.RetentionDays)
	}
}

// TestI18nConfigDefaults verifies that I18nConfig.SetDefaults populates
// DefaultLocale with "zh-Hans" and SupportedLocales with the builtin set
// ["zh-Hans", "en-US"]. The defaults must match pkg/i18n.DefaultLocale and
// pkg/i18n.BuiltinLocales so the language switcher only offers locales
// backed by shipped asset bundles.
func TestI18nConfigDefaults(t *testing.T) {
	var c I18nConfig
	c.SetDefaults()

	if c.DefaultLocale != "zh-Hans" {
		t.Errorf("I18nConfig.DefaultLocale default = %q, want %q", c.DefaultLocale, "zh-Hans")
	}
	want := []string{"zh-Hans", "en-US"}
	if len(c.SupportedLocales) != len(want) {
		t.Fatalf("I18nConfig.SupportedLocales default = %v, want %v", c.SupportedLocales, want)
	}
	for i, v := range want {
		if c.SupportedLocales[i] != v {
			t.Errorf("I18nConfig.SupportedLocales[%d] = %q, want %q", i, c.SupportedLocales[i], v)
		}
	}
}
