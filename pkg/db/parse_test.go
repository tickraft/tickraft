// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package db

import (
	"errors"
	"testing"
)

func TestParse_BarePath(t *testing.T) {
	cfg, err := Parse("tickraft.db")
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if cfg.Driver != "sqlite3" {
		t.Errorf("Driver = %q, want %q", cfg.Driver, "sqlite3")
	}
	if cfg.Addr != "tickraft.db" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, "tickraft.db")
	}
	if cfg.Params != nil {
		t.Errorf("Params = %v, want nil", cfg.Params)
	}
}

func TestParse_MemoryPath(t *testing.T) {
	_, err := Parse(":memory:")
	if err == nil {
		t.Fatal("Parse(\":memory:\") returned nil error, want ErrMemoryNotSupported")
	}
	if !errors.Is(err, ErrMemoryNotSupported) {
		t.Errorf("Parse(\":memory:\") error = %v, want errors.Is ErrMemoryNotSupported", err)
	}
}

func TestParse_EmptyString(t *testing.T) {
	_, err := Parse("")
	if err == nil {
		t.Fatal("Parse(\"\") returned nil error, want ErrDSNRequired")
	}
	if !errors.Is(err, ErrDSNRequired) {
		t.Errorf("Parse(\"\") error = %v, want errors.Is ErrDSNRequired", err)
	}
}

func TestParse_SQLiteScheme(t *testing.T) {
	cfg, err := Parse("sqlite:///var/lib/tickraft.db")
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if cfg.Driver != "sqlite3" {
		t.Errorf("Driver = %q, want %q", cfg.Driver, "sqlite3")
	}
	if cfg.Addr != "/var/lib/tickraft.db" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, "/var/lib/tickraft.db")
	}
	if cfg.Params != nil {
		t.Errorf("Params = %v, want nil", cfg.Params)
	}
}

func TestParse_SQLite3Scheme(t *testing.T) {
	cfg, err := Parse("sqlite3:///var/lib/tickraft.db")
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if cfg.Driver != "sqlite3" {
		t.Errorf("Driver = %q, want %q", cfg.Driver, "sqlite3")
	}
	if cfg.Addr != "/var/lib/tickraft.db" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, "/var/lib/tickraft.db")
	}
	if cfg.Params != nil {
		t.Errorf("Params = %v, want nil", cfg.Params)
	}
}

func TestParse_WithQueryParams(t *testing.T) {
	cfg, err := Parse("sqlite:///var/lib/tickraft.db?busy_timeout=10000&journal_mode=WAL")
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if cfg.Driver != "sqlite3" {
		t.Errorf("Driver = %q, want %q", cfg.Driver, "sqlite3")
	}
	if cfg.Addr != "/var/lib/tickraft.db" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, "/var/lib/tickraft.db")
	}
	if cfg.Params == nil {
		t.Fatal("Params is nil, want populated map")
	}
	if got := cfg.Params["busy_timeout"]; got != "10000" {
		t.Errorf("Params[busy_timeout] = %q, want %q", got, "10000")
	}
	if got := cfg.Params["journal_mode"]; got != "WAL" {
		t.Errorf("Params[journal_mode] = %q, want %q", got, "WAL")
	}
	if len(cfg.Params) != 2 {
		t.Errorf("Params has %d entries, want 2", len(cfg.Params))
	}
}

func TestParse_UnsupportedScheme(t *testing.T) {
	_, err := Parse("mysql://user:pass@host:port/db")
	if err == nil {
		t.Fatal("Parse with unsupported scheme returned nil error, want ErrUnsupportedDriver")
	}
	if !errors.Is(err, ErrUnsupportedDriver) {
		t.Errorf("Parse unsupported scheme error = %v, want errors.Is ErrUnsupportedDriver", err)
	}
}

func TestParse_SQLiteSchemeEmptyURL(t *testing.T) {
	_, err := Parse("sqlite://")
	if err == nil {
		t.Fatal("Parse(\"sqlite://\") returned nil error, want ErrDSNRequired")
	}
	if !errors.Is(err, ErrDSNRequired) {
		t.Errorf("Parse(\"sqlite://\") error = %v, want errors.Is ErrDSNRequired", err)
	}
}

func TestSplitURLQuery_NoQuery(t *testing.T) {
	urlPart, params := SplitURLQuery("/var/lib/tickraft.db")
	if urlPart != "/var/lib/tickraft.db" {
		t.Errorf("urlPart = %q, want %q", urlPart, "/var/lib/tickraft.db")
	}
	if params != nil {
		t.Errorf("params = %v, want nil", params)
	}
}

func TestSplitURLQuery_WithParams(t *testing.T) {
	urlPart, params := SplitURLQuery("user:pass@host:port/db?charset=utf8mb4&parseTime=True")
	wantURL := "user:pass@host:port/db"
	if urlPart != wantURL {
		t.Errorf("urlPart = %q, want %q", urlPart, wantURL)
	}
	if params == nil {
		t.Fatal("params is nil, want populated map")
	}
	if got := params["charset"]; got != "utf8mb4" {
		t.Errorf("params[charset] = %q, want %q", got, "utf8mb4")
	}
	if got := params["parseTime"]; got != "True" {
		t.Errorf("params[parseTime] = %q, want %q", got, "True")
	}
	if len(params) != 2 {
		t.Errorf("params has %d entries, want 2", len(params))
	}
}

func TestSplitURLQuery_EmptyString(t *testing.T) {
	urlPart, params := SplitURLQuery("")
	if urlPart != "" {
		t.Errorf("urlPart = %q, want %q", urlPart, "")
	}
	if params != nil {
		t.Errorf("params = %v, want nil", params)
	}
}

func TestSplitURLQuery_EmptyQuery(t *testing.T) {
	urlPart, params := SplitURLQuery("path?")
	if urlPart != "path" {
		t.Errorf("urlPart = %q, want %q", urlPart, "path")
	}
	if params != nil {
		t.Errorf("params = %v, want nil", params)
	}
}
