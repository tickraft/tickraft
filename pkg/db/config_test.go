// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package db

import (
	"strings"
	"testing"
)

func TestConfig_DSN_EmptyConfig(t *testing.T) {
	cfg := Config{}
	if got := cfg.DSN(); got != "" {
		t.Errorf("DSN() = %q, want %q", got, "")
	}
}

func TestConfig_DSN_NoParams(t *testing.T) {
	cfg := Config{Driver: "sqlite3", Addr: ":memory:"}
	want := "sqlite3://:memory:"
	if got := cfg.DSN(); got != want {
		t.Errorf("DSN() = %q, want %q", got, want)
	}
}

func TestConfig_DSN_WithParams(t *testing.T) {
	cfg := Config{
		Driver: "sqlite3",
		Addr:   "/var/lib/tickraft.db",
		Params: map[string]string{"busy_timeout": "10000"},
	}
	want := "sqlite3:///var/lib/tickraft.db?busy_timeout=10000"
	if got := cfg.DSN(); got != want {
		t.Errorf("DSN() = %q, want %q", got, want)
	}
}

func TestConfig_DSN_ParamsSorted(t *testing.T) {
	cfg := Config{
		Driver: "sqlite3",
		Addr:   "path",
		Params: map[string]string{"zebra": "z", "alpha": "a"},
	}
	want := "sqlite3://path?alpha=a&zebra=z"
	if got := cfg.DSN(); got != want {
		t.Errorf("DSN() = %q, want %q", got, want)
	}
}

func TestConfig_DSN_RoundTrip(t *testing.T) {
	cfg, err := Parse("sqlite:///var/lib/tickraft.db?busy_timeout=10000")
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	want := "sqlite3:///var/lib/tickraft.db?busy_timeout=10000"
	if got := cfg.DSN(); got != want {
		t.Errorf("DSN() round-trip = %q, want %q", got, want)
	}
}

func TestConfig_DSN_WithCredentialAndDatabase(t *testing.T) {
	cfg := Config{
		Driver:     "mysql",
		Addr:       "127.0.0.1:3306",
		Credential: Credential{Username: "u", Password: "p"},
		Params:     map[string]string{"database": "appdb", "charset": "utf8mb4"},
	}
	want := "mysql://u:p@127.0.0.1:3306/appdb?charset=utf8mb4"
	if got := cfg.DSN(); got != want {
		t.Errorf("DSN() = %q, want %q", got, want)
	}
}

func TestConfig_DSN_DatabaseExcludedFromQuery(t *testing.T) {
	cfg := Config{
		Driver: "postgresql",
		Addr:   "localhost:5432",
		Params: map[string]string{"database": "appdb", "sslmode": "disable"},
	}
	got := cfg.DSN()
	if strings.Contains(got, "database=appdb") {
		t.Errorf("DSN() = %q, database must not appear in query", got)
	}
	if !strings.Contains(got, "/appdb") {
		t.Errorf("DSN() = %q, want /appdb path segment", got)
	}
}
