// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package db

import "testing"

func TestRedactMySQLDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "empty dsn returns empty",
			dsn:  "",
			want: "",
		},
		{
			name: "password in userinfo masked",
			dsn:  "user:secret@tcp(localhost:3306)/db",
			want: "user:***@tcp(localhost:3306)/db",
		},
		{
			name: "no password in userinfo left unchanged",
			dsn:  "user@tcp(localhost:3306)/db",
			want: "user@tcp(localhost:3306)/db",
		},
		{
			name: "sensitive query param masked",
			dsn:  "user:secret@tcp(localhost:3306)/db?password=foo&charset=utf8mb4",
			want: "user:***@tcp(localhost:3306)/db?password=***&charset=utf8mb4",
		},
		{
			name: "no query string only userinfo masked",
			dsn:  "user:secret@tcp(localhost:3306)/db",
			want: "user:***@tcp(localhost:3306)/db",
		},
		{
			name: "url-style dsn with password",
			dsn:  "mysql://user:secret@localhost:3306/db",
			want: "mysql://user:***@localhost:3306/db",
		},
		{
			name: "url-style dsn with sensitive query param",
			dsn:  "mysql://user:secret@localhost:3306/db?password=foo&charset=utf8mb4",
			want: "mysql://user:***@localhost:3306/db?password=***&charset=utf8mb4",
		},
		{
			name: "non-sensitive query params left unchanged",
			dsn:  "user:secret@tcp(localhost:3306)/db?charset=utf8mb4&parseTime=true",
			want: "user:***@tcp(localhost:3306)/db?charset=utf8mb4&parseTime=true",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactMySQLDSN(tt.dsn)
			if got != tt.want {
				t.Errorf("redactMySQLDSN(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
		})
	}
}

func TestRedactPostgreSQLDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "empty dsn returns empty",
			dsn:  "",
			want: "",
		},
		{
			name: "url-style with password",
			dsn:  "postgres://user:secret@localhost:5432/db",
			want: "postgres://user:***@localhost:5432/db",
		},
		{
			name: "url-style with sensitive query param",
			dsn:  "postgres://user:secret@localhost:5432/db?password=foo&sslmode=disable",
			want: "postgres://user:***@localhost:5432/db?password=***&sslmode=disable",
		},
		{
			name: "keyword/value with password",
			dsn:  "host=localhost password=secret dbname=postgres",
			want: "host=localhost password=*** dbname=postgres",
		},
		{
			name: "keyword/value without password unchanged",
			dsn:  "host=localhost dbname=postgres",
			want: "host=localhost dbname=postgres",
		},
		{
			name: "postgresql scheme prefix handled",
			dsn:  "postgresql://user:secret@localhost:5432/db",
			want: "postgresql://user:***@localhost:5432/db",
		},
		{
			name: "url-style no password in userinfo unchanged",
			dsn:  "postgres://user@localhost:5432/db",
			want: "postgres://user@localhost:5432/db",
		},
		{
			name: "keyword/value password case insensitive",
			dsn:  "host=localhost PASSWORD=secret dbname=postgres",
			want: "host=localhost PASSWORD=*** dbname=postgres",
		},
		{
			name: "keyword/value with sensitive query-like param in url form",
			dsn:  "postgres://user:secret@localhost:5432/db?sslmode=disable&token=abc",
			want: "postgres://user:***@localhost:5432/db?sslmode=disable&token=***",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactPostgreSQLDSN(tt.dsn)
			if got != tt.want {
				t.Errorf("redactPostgreSQLDSN(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
		})
	}
}

func TestRedact(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{"empty dsn", "", ""},
		{"mysql url", "mysql://user:secret@localhost:3306/db", "mysql://user:***@localhost:3306/db"},
		{"mysql native", "user:secret@tcp(localhost:3306)/db", "user:***@tcp(localhost:3306)/db"},
		{"postgres url", "postgres://user:secret@localhost:5432/db", "postgres://user:***@localhost:5432/db"},
		{"postgresql url", "postgresql://user:secret@localhost:5432/db", "postgresql://user:***@localhost:5432/db"},
		{"sqlite passthrough", "sqlite:///var/lib/tickraft/tickraft.db", "sqlite:///var/lib/tickraft/tickraft.db"},
		{"bare path passthrough", "/var/lib/tickraft/tickraft.db", "/var/lib/tickraft/tickraft.db"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.dsn)
			if got != tt.want {
				t.Errorf("Redact(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
		})
	}
}
