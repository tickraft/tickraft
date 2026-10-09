// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Recognized SQLite PRAGMA keys, kept as constants so the settings list and
// the validation switch share the same literals.
const (
	pragmaBusyTimeout = "busy_timeout"
	pragmaCacheSize   = "cache_size"
	pragmaJournalMode = "journal_mode"
	pragmaSynchronous = "synchronous"
)

// openSQLite creates a GORM database instance for SQLite3 with optimized
// PRAGMA settings and a tuned connection pool.
func openSQLite(ctx context.Context, cfg Config) (*gorm.DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("db: open sqlite3 cancelled: %w", err)
	}
	if cfg.Addr == "" {
		return nil, ErrDSNRequired
	}

	// In-memory databases (":memory:") are rejected by Parse for production
	// DSNs. Tests that construct Config directly may still use ":memory:" for
	// isolated, fast test execution.
	if cfg.Addr != memoryAddr {
		if _, err := os.Stat(filepath.Dir(cfg.Addr)); err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("db: check sqlite3 directory: %w", err)
			}

			if err = os.MkdirAll(filepath.Dir(cfg.Addr), 0o750); err != nil {
				return nil, fmt.Errorf("db: create sqlite3 directory: %w", err)
			}
		}
	}

	gormCfg := &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
	}

	dbc, err := gorm.Open(sqlite.Open(cfg.Addr), gormCfg)
	if err != nil {
		return nil, fmt.Errorf("db: open sqlite3: %w", err)
	}

	if err = applySQLitePragmas(dbc, cfg.Params); err != nil {
		closeOnErr(dbc)
		return nil, err
	}

	sqlDB, err := dbc.DB()
	if err != nil {
		closeOnErr(dbc)
		return nil, fmt.Errorf("db: get underlying sql.DB: %w", err)
	}

	// With WAL journal mode (the default set in applySQLitePragmas), SQLite
	// allows concurrent readers alongside a single writer. A MaxOpenConns of
	// 1 serializes all access and hurts read throughput; default to 4 to
	// permit read concurrency while keeping write contention low.
	//
	// In-memory databases (":memory:") are an exception: each connection
	// gets its own private database, so MaxOpenConns must be 1 to ensure
	// all queries share the same in-memory state.
	maxOpen, maxIdle, connMaxLifetime := sqlitePoolSettings(cfg)
	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(maxIdle)
	sqlDB.SetConnMaxLifetime(connMaxLifetime)

	zap.L().Info("sqlite3 database opened",
		zap.String("address", cfg.Addr),
		zap.Int("max_open_conns", maxOpen),
		zap.Int("max_idle_conns", maxIdle),
		zap.Duration("conn_max_lifetime", connMaxLifetime),
	)
	return dbc, nil
}

// sqlitePoolSettings derives the connection pool tuning for SQLite from cfg:
// the max open / max idle connection counts and the connection max lifetime.
// Unrecognized or invalid override values fall back to the defaults.
func sqlitePoolSettings(cfg Config) (maxOpen, maxIdle int, connMaxLifetime time.Duration) {
	maxOpen = 4
	if cfg.Addr == memoryAddr {
		maxOpen = 1
	}
	if v, ok := cfg.Params["max_open_conns"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxOpen = n
		}
	}
	maxIdle = maxOpen
	if v, ok := cfg.Params["max_idle_conns"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			maxIdle = n
		}
	}
	connMaxLifetime = time.Hour
	if v, ok := cfg.Params["conn_max_lifetime"]; ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			connMaxLifetime = d
		}
	}
	return maxOpen, maxIdle, connMaxLifetime
}

// closeOnErr closes the underlying sql.DB of a *gorm.DB on the error cleanup
// path. The close error is intentionally ignored because the caller already
// holds a more important error to report; surfacing the close error would
// mask the root cause.
func closeOnErr(dbc *gorm.DB) {
	if sqlDB, err := dbc.DB(); err == nil {
		// ignored because: cleanup path on a failed-open gorm.DB; the caller
		// already holds the primary error and a close error here would mask it.
		_ = sqlDB.Close()
	}
}

// applySQLitePragmas applies SQLite3-specific PRAGMA settings read from the
// params map, falling back to sensible defaults when a key is absent or empty.
// All PRAGMA values are validated against an allowlist to prevent injection
// through configuration.
func applySQLitePragmas(dbc *gorm.DB, params map[string]string) error {
	settings := []struct {
		key   string
		value string
	}{
		{pragmaBusyTimeout, paramOrDefault(params, pragmaBusyTimeout, "5000")},
		{pragmaCacheSize, paramOrDefault(params, pragmaCacheSize, "-2000")},
		{pragmaJournalMode, paramOrDefault(params, pragmaJournalMode, "WAL")},
		{pragmaSynchronous, paramOrDefault(params, pragmaSynchronous, "NORMAL")},
	}

	for _, s := range settings {
		val, err := validatePragmaValue(s.key, s.value)
		if err != nil {
			return err
		}
		stmt := fmt.Sprintf("PRAGMA %s = %s", s.key, val)
		if err = dbc.Exec(stmt).Error; err != nil {
			return fmt.Errorf("db: execute pragma %q: %w", stmt, err)
		}
	}

	// foreign_keys is a fixed ON/OFF toggle; no user-controlled value.
	const fkPragma = "PRAGMA foreign_keys = ON"
	if err := dbc.Exec(fkPragma).Error; err != nil {
		return fmt.Errorf("db: execute pragma %q: %w", fkPragma, err)
	}
	return nil
}

// validatePragmaValue validates SQLite PRAGMA values against an allowlist of
// known-safe values to prevent SQL injection through configuration. Numeric
// pragmas must parse as integers; enum-like pragmas must match a known set.
// Returns the normalized value or an error if the value is not recognized.
func validatePragmaValue(key, value string) (string, error) {
	switch key {
	case pragmaBusyTimeout, pragmaCacheSize:
		if _, err := strconv.Atoi(value); err != nil {
			return "", fmt.Errorf("db: invalid %s %q: must be an integer", key, value)
		}
		return value, nil
	case pragmaJournalMode:
		mode := strings.ToUpper(value)
		switch mode {
		case "DELETE", "TRUNCATE", "PERSIST", "MEMORY", "WAL", "OFF":
			return mode, nil
		}
		return "", fmt.Errorf("db: invalid journal_mode %q", value)
	case pragmaSynchronous:
		syn := strings.ToUpper(value)
		switch syn {
		case "OFF", "NORMAL", "FULL", "EXTRA", "0", "1", "2", "3":
			return syn, nil
		}
		return "", fmt.Errorf("db: invalid synchronous %q", value)
	default:
		return "", fmt.Errorf("db: unsupported pragma key %q", key)
	}
}

// paramOrDefault returns the value for key in params, or defaultVal when the
// key is missing or maps to an empty string.
func paramOrDefault(params map[string]string, key, defaultVal string) string {
	if v, ok := params[key]; ok && v != "" {
		return v
	}
	return defaultVal
}
