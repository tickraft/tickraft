// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package db provides database connectivity, driver-specific optimizations,
// and error normalization for GORM-based data access.
//
// The runtime ships only the SQLite3 driver. Additional drivers
// may be registered via the Register SPI at process startup; this
// package is unaware of those drivers and never imports their dependencies.
//
// This package focuses on three concerns:
//   - Connection management: Open returns a *gorm.DB that callers own and
//     must close via Close (or the underlying sql.DB.Close obtained from
//     gorm.DB.DB()).
//   - Driver-specific optimizations: SQLite3 PRAGMA tuning
//   - DSN parsing and serialization: Parse and Config.DSN
//
// Error normalization is provided by MapError (errmap.go in this
// package). Cross-domain shared sentinels live in pkg/errdefs.
//
// Data operations (CRUD, queries, transactions) are performed directly with
// GORM via the *gorm.DB instance returned by Open. Caching is decoupled from
// this package and owned by the caller (see pkg/cache).
//
// The database driver is inferred from the DSN scheme:
//   - sqlite://, sqlite3://, or a bare path with no scheme → sqlite3
//
// Config.Params is a generic map of driver-specific parameters, either parsed
// from the DSN query string by Parse or set directly by the caller before
// calling Open. For sqlite3 the recognized keys are: busy_timeout, cache_size,
// journal_mode, synchronous, max_open_conns, max_idle_conns, conn_max_lifetime.
// Params is optional; sensible defaults apply when a key is absent.
//
// The Config fields divide responsibilities as follows: Address carries the
// database location (a file path for SQLite, or "host:port" for network
// databases); Credential holds the username/password (left zero-valued for
// SQLite, populated by the caller for network databases); Params carries
// driver-specific parameters, and for network databases the database name via
// Params["database"].
//
// # Quick Start
//
// Open a SQLite3 database by constructing a Config directly. The Credential
// field is left zero-valued for SQLite; for network databases it is populated
// by the caller from the DSN userinfo.
//
//	dbc, err := db.Open(ctx, db.Config{
//	    Driver: "sqlite3",
//	    Addr:   "tickraft.db",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer func() { _ = db.Close(dbc) }()
//
// Or parse a DSN string into a Config first:
//
//	cfg, err := db.Parse("sqlite://tickraft.db")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	dbc, err := db.Open(ctx, cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # Custom Configuration
//
// SQLite3 with custom PRAGMA settings and connection pool tuning:
//
//	dbc, err := db.Open(ctx, db.Config{
//	    Driver: "sqlite3",
//	    Addr:   "/var/lib/tickraft/tickraft.db",
//	    Params: map[string]string{
//	        "busy_timeout":      "10000",
//	        "cache_size":        "-4000",
//	        "journal_mode":      "WAL",
//	        "synchronous":       "NORMAL",
//	        "max_open_conns":    "4",
//	        "max_idle_conns":    "4",
//	        "conn_max_lifetime": "1h",
//	    },
//	})
//
// # Data Operations
//
// Use GORM directly for all data operations:
//
//	// Create
//	user := model.User{Username: "alice", Role: 1}
//	err := dbc.WithContext(ctx).Create(&user).Error
//	if err != nil {
//	    return db.MapError(err)
//	}
//
//	// Read
//	var user model.User
//	err = dbc.WithContext(ctx).First(&user, id).Error
//	if err != nil {
//	    return db.MapError(err)
//	}
//
//	// List with filters and pagination
//	var users []model.User
//	var total int64
//	query := dbc.WithContext(ctx).Model(&model.User{})
//	query = query.Where("role = ?", 1)
//	query.Count(&total)
//	query = query.Order("id DESC").Offset(0).Limit(20)
//	query.Find(&users)
//
//	// Update
//	result := dbc.WithContext(ctx).Model(&model.User{}).
//		Where("id = ?", id).Updates(map[string]interface{}{"name": "bob"})
//	if result.Error != nil {
//	    return db.MapError(result.Error)
//	}
//
//	// Delete
//	result = dbc.WithContext(ctx).Where("id = ?", id).Delete(&model.User{})
//	if result.Error != nil {
//	    return db.MapError(result.Error)
//	}
//
// # Transactions
//
// Use GORM's native transaction API:
//
//	err := dbc.Transaction(func(tx *gorm.DB) error {
//	    if err := tx.Create(&user).Error; err != nil {
//	        return err // auto rollback
//	    }
//	    if err := tx.Create(&log).Error; err != nil {
//	        return err // auto rollback
//	    }
//	    return nil // auto commit
//	})
//
// # Error Handling
//
// Use db.MapError to normalize driver-specific errors. Cross-domain
// outcomes are mapped to the shared sentinels in pkg/errdefs:
//
//	err := dbc.WithContext(ctx).First(&user, id).Error
//	if err != nil {
//	    mapped := db.MapError(err)
//	    if errors.Is(mapped, errdefs.ErrNotFound) {
//	        // handle not found
//	    } else if errors.Is(mapped, errdefs.ErrConflict) {
//	        // handle duplicate
//	    }
//	}
//
// # DSN Parsing and Serialization
//
// Parse converts a DSN string into a Config with Driver, Address, and Params
// populated. It recognizes the "sqlite" and "sqlite3" schemes as well as bare
// file paths (e.g. "tickraft.db"). In-memory databases (":memory:") are
// explicitly rejected with ErrMemoryNotSupported. Query parameters in the DSN
// are parsed into Config.Params. For network databases the database name is
// carried in Params["database"]; Parse handles only SQLite3,
// so this convention is observed by downstream network drivers:
//
//	cfg, err := db.Parse("sqlite:///var/lib/tickraft.db?busy_timeout=10000&journal_mode=WAL")
//	// cfg.Driver == "sqlite3"
//	// cfg.Addr == "/var/lib/tickraft.db"
//	// cfg.Params == {"busy_timeout":"10000", "journal_mode":"WAL"}
//
// Config.DSN generates a canonical DSN string from a Config, sorting params
// by key for deterministic output. This is the inverse of Parse and is useful
// for logging or persisting a normalized connection string:
//
//	cfg := db.Config{
//	    Driver: "sqlite3",
//	    Addr:   "/var/lib/tickraft.db",
//	    Params: map[string]string{"busy_timeout": "10000"},
//	}
//	dsn := cfg.DSN() // "sqlite3:///var/lib/tickraft.db?busy_timeout=10000"
//
// SplitURLQuery splits a DSN body (the part after "scheme://") into its
// address component and a params map parsed from the query string. It returns
// nil params when the input contains no "?" separator:
//
//	addr, params := db.SplitURLQuery("/var/lib/tickraft.db?journal_mode=WAL")
//	// addr == "/var/lib/tickraft.db"
//	// params == {"journal_mode":"WAL"}
//
// # SPI: Registering Additional Drivers
//
// Additional database drivers can be registered at process startup via the
// Register function. The runtime ships only the SQLite3 driver;
// downstream repositories may register their own drivers via init() side
// effects before calling Open.
package db
