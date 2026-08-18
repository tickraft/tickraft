// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package db

import (
	"context"
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db/errmap"
)

func TestValidatePragmaValue(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		want    string
		wantErr bool
	}{
		// busy_timeout: integer, passed through unchanged.
		{name: "busy_timeout valid", key: "busy_timeout", value: "5000", want: "5000"},
		{name: "busy_timeout zero", key: "busy_timeout", value: "0", want: "0"},
		{name: "busy_timeout invalid", key: "busy_timeout", value: "abc", wantErr: true},

		// cache_size: integer (may be negative), passed through unchanged.
		{name: "cache_size valid negative", key: "cache_size", value: "-2000", want: "-2000"},
		{name: "cache_size valid positive", key: "cache_size", value: "2000", want: "2000"},
		{name: "cache_size invalid", key: "cache_size", value: "large", wantErr: true},

		// journal_mode: enum, normalized to uppercase.
		{name: "journal_mode wal lowercase", key: "journal_mode", value: "wal", want: "WAL"},
		{name: "journal_mode delete", key: "journal_mode", value: "DELETE", want: "DELETE"},
		{name: "journal_mode off", key: "journal_mode", value: "off", want: "OFF"},
		{name: "journal_mode invalid", key: "journal_mode", value: "INVALID", wantErr: true},

		// synchronous: enum, normalized to uppercase.
		{name: "synchronous normal lowercase", key: "synchronous", value: "normal", want: "NORMAL"},
		{name: "synchronous numeric 0", key: "synchronous", value: "0", want: "0"},
		{name: "synchronous full", key: "synchronous", value: "FULL", want: "FULL"},
		{name: "synchronous invalid", key: "synchronous", value: "FAST", wantErr: true},

		// Unknown pragma key.
		{name: "unknown key", key: "evil_pragma", value: "1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validatePragmaValue(tt.key, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePragmaValue(%q, %q) error = %v, wantErr %v", tt.key, tt.value, err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("validatePragmaValue(%q, %q) = %q, want %q", tt.key, tt.value, got, tt.want)
			}
		})
	}
}

func TestClose_NilDB(t *testing.T) {
	if err := Close(nil); err != nil {
		t.Errorf("Close(nil) = %v, want nil", err)
	}
}

func TestClose_ValidDB(t *testing.T) {
	dbc, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}

	if err := Close(dbc); err != nil {
		t.Errorf("Close(valid db) = %v, want nil", err)
	}
}

func TestParse_MemoryRejected(t *testing.T) {
	// Parse rejects :memory: for production DSNs to prevent accidental data loss.
	_, err := Parse("sqlite3://:memory:")
	if !errors.Is(err, errmap.ErrMemoryNotSupported) {
		t.Errorf("Parse(:memory:) error = %v, want errors.Is errmap.ErrMemoryNotSupported", err)
	}
}

func TestOpenSQLite_MemoryAllowed(t *testing.T) {
	// Open allows :memory: when Config is constructed directly (e.g., in tests).
	// MaxOpenConns is forced to 1 so all queries share the same in-memory database.
	db, err := Open(context.Background(), Config{
		Driver: "sqlite3",
		Addr:   ":memory:",
	})
	if err != nil {
		t.Fatalf("Open(:memory:) error = %v, want nil", err)
	}
	defer func() { _ = Close(db) }()

	if err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	if err := db.Exec("INSERT INTO t (id) VALUES (1)").Error; err != nil {
		t.Fatalf("insert: %v", err)
	}
	var id int
	if err := db.Raw("SELECT id FROM t").Scan(&id).Error; err != nil {
		t.Fatalf("select: %v", err)
	}
	if id != 1 {
		t.Errorf("got id = %d, want 1", id)
	}
}
