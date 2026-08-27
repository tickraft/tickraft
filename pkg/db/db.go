// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package db

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"

	"gorm.io/gorm"
)

// Shared driver and address literals used across the package.
const (
	// driverSQLite3 is the canonical driver name for the builtin SQLite3
	// driver registered at init time.
	driverSQLite3 = "sqlite3"
	// memoryAddr is the special SQLite address that selects a private
	// in-memory database; rejected by Parse, allowed for direct test Configs.
	memoryAddr = ":memory:"
)

// Credential holds the authentication credentials for a database connection.
// Zero-valued for SQLite; for network databases Parse populates it from DSN
// userinfo.
type Credential struct {
	Username string
	Password string
}

// Config holds the database configuration. The Driver, Address, Credential,
// and Params fields are populated by Parse; callers may also set them
// directly before passing the Config to Open.
type Config struct {
	// Driver is the database driver name parsed from the DSN scheme
	// (e.g. "sqlite3").
	Driver string
	// Addr is the database address/location. For SQLite it is a file path
	// (e.g. "/var/lib/tickraft/tickraft.db"); for network databases it is
	// the "host:port" form without the database name.
	Addr string
	// Credential holds the username/password for network databases. It is
	// left zero-valued for SQLite.
	Credential Credential
	// Params is a map of driver-specific parameters parsed from the DSN
	// query string or set directly by the caller. For network databases
	// Params["database"] carries the database name; for sqlite3 the
	// recognized keys are: busy_timeout, cache_size, journal_mode,
	// synchronous, max_open_conns, max_idle_conns, conn_max_lifetime.
	Params map[string]string
}

// Parse fully parses the given DSN into a Config with Driver, Address, and
// Params populated. Callers may further modify the returned Config before
// passing it to Open.
//
// Only SQLite3 DSNs are recognized: the "sqlite" or "sqlite3" scheme, or a
// bare path with no scheme. Any other scheme returns ErrUnsupportedDriver.
// Credential is left zero-valued; Parse does not interpret
// DSN userinfo.
//
// A DSN without "://" (e.g. a bare file path like "tickraft.db") is treated
// as a SQLite3 DSN.
func Parse(dsn string) (Config, error) {
	if dsn == "" {
		return Config{}, ErrDSNRequired
	} else if strings.Contains(dsn, memoryAddr) {
		return Config{}, ErrMemoryNotSupported
	}

	if !strings.Contains(dsn, "://") {
		return Config{Driver: driverSQLite3, Addr: dsn}, nil
	}

	parts := strings.SplitN(dsn, "://", 2)
	scheme, rest := parts[0], parts[1]
	switch scheme {
	case "sqlite", driverSQLite3:
		address, params := SplitURLQuery(rest)
		if address == "" {
			return Config{}, ErrDSNRequired
		}
		return Config{Driver: driverSQLite3, Addr: address, Params: params}, nil
	default:
		return Config{}, fmt.Errorf("%w: scheme %q", ErrUnsupportedDriver, scheme)
	}
}

// SplitURLQuery splits a DSN body (the part after "scheme://") into the
// address component and a params map parsed from the query string. Returns
// nil params when the input contains no "?" separator.
func SplitURLQuery(s string) (address string, params map[string]string) {
	idx := strings.Index(s, "?")
	if idx < 0 {
		return s, nil
	}
	address = s[:idx]
	queryPart := s[idx+1:]
	if queryPart == "" {
		return address, nil
	}
	params = make(map[string]string)
	for pair := range strings.SplitSeq(queryPart, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		key, err := url.QueryUnescape(k)
		if err != nil {
			continue
		}
		val, err := url.QueryUnescape(v)
		if err != nil {
			continue
		}
		params[key] = val
	}
	if len(params) == 0 {
		return address, nil
	}
	return address, params
}

// DSN generates a canonical DSN string from the Config's Driver, Address,
// Credential, and Params. The reconstruction is driver-agnostic: it emits
// "scheme://[user[:pass]@]address[/database]?params". Credential.Username
// (and Credential.Password when non-empty) are placed in the userinfo
// segment. Params["database"], when present, is rendered as the path segment
// "/database" and excluded from the query string; all remaining Params are
// sorted by key and url.QueryEscape'd into the query string. Returns an
// empty string when Address is empty. Params are sorted by key for
// deterministic output.
func (c Config) DSN() string {
	if c.Addr == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString(c.Driver)
	b.WriteString("://")

	if c.Credential.Username != "" {
		b.WriteString(c.Credential.Username)
		if c.Credential.Password != "" {
			b.WriteString(":")
			b.WriteString(c.Credential.Password)
		}
		b.WriteString("@")
	}

	b.WriteString(c.Addr)

	if dbName, ok := c.Params["database"]; ok && dbName != "" {
		b.WriteString("/")
		b.WriteString(dbName)
	}

	if len(c.Params) > 0 {
		keys := make([]string, 0, len(c.Params))
		for k := range c.Params {
			if k == "database" {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			sep := "?"
			if i > 0 {
				sep = "&"
			}
			b.WriteString(sep)
			b.WriteString(url.QueryEscape(k))
			b.WriteString("=")
			b.WriteString(url.QueryEscape(c.Params[k]))
		}
	}

	return b.String()
}

// Opener creates a *gorm.DB from the given Config.
type Opener func(ctx context.Context, cfg Config) (*gorm.DB, error)

var (
	driversMu sync.RWMutex
	drivers   = make(map[string]Opener)
)

// Register registers a database driver opener under the given name.
// Returns an error if a driver with the same name is already registered.
func Register(name string, opener Opener) error {
	driversMu.Lock()
	defer driversMu.Unlock()
	if _, exists := drivers[name]; exists {
		return fmt.Errorf("db: driver %q already registered", name)
	}
	drivers[name] = opener
	return nil
}

// Open opens a database connection described by cfg and returns the
// resulting *gorm.DB. The driver is selected from cfg.Driver, which must be
// set; callers that have a raw DSN string should use Parse first to obtain
// a populated Config.
//
// The runtime ships only the SQLite3 driver; additional drivers
// may be registered via the Register SPI.
func Open(ctx context.Context, cfg Config) (*gorm.DB, error) {
	if cfg.Driver == "" {
		return nil, ErrDriverRequired
	}
	driversMu.RLock()
	opener, ok := drivers[cfg.Driver]
	driversMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedDriver, cfg.Driver)
	}
	return opener(ctx, cfg)
}

// Close closes the underlying connection pool of a *gorm.DB instance. It
// retrieves the *sql.DB from GORM and closes it. The *gorm.DB must not be
// used after Close returns. Passing a nil *gorm.DB is a no-op.
//
// Callers should typically use this via defer:
//
//	dbc, err := db.Open(ctx, cfg)
//	if err != nil { ... }
//	defer func() { _ = db.Close(dbc) }()
func Close(dbc *gorm.DB) error {
	if dbc == nil {
		return nil
	}
	sqlDB, err := dbc.DB()
	if err != nil {
		return fmt.Errorf("db: get underlying sql.DB for close: %w", err)
	}
	if err = sqlDB.Close(); err != nil {
		return fmt.Errorf("db: close sql.DB: %w", err)
	}
	return nil
}

func init() {
	if err := Register(driverSQLite3, openSQLite); err != nil {
		panic(err) // init-time registration failure is a programming error
	}
	// Additional drivers may be registered by downstream repositories via
	// Register(). The runtime ships only SQLite3.
}
