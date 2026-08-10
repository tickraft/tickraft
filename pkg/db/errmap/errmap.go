// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package errmap contains the database-specific error sentinel variables
// and the MapError helper that translates GORM and driver-specific errors
// into these sentinels.
//
// Cross-domain shared sentinels (ErrNotFound, ErrConflict) live in
// pkg/errdefs and are returned by MapError directly; this package only
// defines sentinels that are specific to database operations (driver
// errors, constraint violations, schema errors).
//
// It is a leaf package (no imports from tickraft/tickraft other than
// pkg/errdefs) so that domain packages whose models are referenced by
// pkg/db/migrate.go (e.g. pkg/user, pkg/auth) can use MapError without
// creating an import cycle with pkg/db.
package errmap

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tickraft/tickraft/pkg/errdefs"
	"gorm.io/gorm"
)

// Sentinel errors specific to database operations. Cross-domain shared
// sentinels (ErrNotFound, ErrConflict) are intentionally NOT redefined
// here; callers should use errdefs.ErrNotFound and errdefs.ErrConflict
// directly.
var (
	// ErrUnsupportedDriver is returned when the database driver is not registered.
	ErrUnsupportedDriver = errors.New("db: unsupported driver")
	// ErrDSNRequired is returned when the DSN is empty.
	ErrDSNRequired = errors.New("db: dsn is required")
	// ErrDriverRequired is returned when the driver is empty.
	ErrDriverRequired = errors.New("db: driver is required")
	// ErrMemoryNotSupported is returned when a memory database is requested.
	ErrMemoryNotSupported = errors.New("db: memory databases are not supported")
	// ErrForeignKeyViolation is returned when a foreign key constraint is violated.
	ErrForeignKeyViolation = errors.New("db: foreign key violation")
	// ErrNotNullViolation is returned when a NOT NULL constraint is violated.
	ErrNotNullViolation = errors.New("db: not-null violation")
	// ErrCheckViolation is returned when a CHECK constraint is violated.
	ErrCheckViolation = errors.New("db: check violation")
	// ErrUndefinedTable is returned when the referenced table does not exist.
	ErrUndefinedTable = errors.New("db: undefined table")
	// ErrUndefinedColumn is returned when the referenced column does not exist.
	ErrUndefinedColumn = errors.New("db: undefined column")
)

// MapError maps GORM and driver-specific errors to sentinel error variables.
//
// Cross-domain outcomes are mapped to the shared sentinels in pkg/errdefs:
//   - record-not-found      -> errdefs.ErrNotFound
//   - duplicate-key          -> errdefs.ErrConflict
//
// Database-specific outcomes (foreign key, not-null, check, schema errors)
// are mapped to the corresponding errmap.Err* sentinels.
//
// The runtime ships only the SQLite3 driver; for SQLite it falls
// back to substring matching on the error message because the go-sqlite3
// driver reports constraint violations via human-readable messages rather than
// a stable typed error. GORM translated errors (when TranslateError is enabled
// on gorm.Config) are also handled.
//
// Drivers that enable TranslateError get uniform error mapping without
// re-implementing MapError.
func MapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errdefs.ErrNotFound
	}

	// GORM translated errors (when TranslateError is enabled on gorm.Config).
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return errdefs.ErrConflict
	}
	if errors.Is(err, gorm.ErrForeignKeyViolated) {
		return ErrForeignKeyViolation
	}

	// SQLite errors: string-matching fallback. The go-sqlite3 driver reports
	// constraint violations and schema errors via human-readable messages
	// rather than a stable typed error, so substring matching is the only
	// option here.
	msg := err.Error()
	switch {
	case isSQLiteUniqueViolation(msg):
		return errdefs.ErrConflict
	case isSQLiteForeignKeyViolation(msg):
		return ErrForeignKeyViolation
	case isSQLiteNotNullViolation(msg):
		return ErrNotNullViolation
	case isSQLiteCheckViolation(msg):
		return ErrCheckViolation
	case isSQLiteUndefinedTable(msg):
		return ErrUndefinedTable
	case isSQLiteUndefinedColumn(msg):
		return ErrUndefinedColumn
	}

	return fmt.Errorf("db: %w", err)
}

// isSQLiteUniqueViolation checks if the error message indicates a SQLite unique constraint violation.
func isSQLiteUniqueViolation(msg string) bool {
	return strings.Contains(msg, "UNIQUE constraint failed")
}

// isSQLiteForeignKeyViolation checks if the error message indicates a SQLite foreign key violation.
func isSQLiteForeignKeyViolation(msg string) bool {
	return strings.Contains(msg, "FOREIGN KEY constraint failed")
}

// isSQLiteNotNullViolation checks if the error message indicates a SQLite NOT NULL constraint violation.
func isSQLiteNotNullViolation(msg string) bool {
	return strings.Contains(msg, "NOT NULL constraint failed")
}

// isSQLiteCheckViolation checks if the error message indicates a SQLite CHECK constraint violation.
func isSQLiteCheckViolation(msg string) bool {
	return strings.Contains(msg, "CHECK constraint failed")
}

// isSQLiteUndefinedTable checks if the error message indicates a missing table.
func isSQLiteUndefinedTable(msg string) bool {
	return strings.Contains(msg, "no such table:")
}

// isSQLiteUndefinedColumn checks if the error message indicates a missing column.
func isSQLiteUndefinedColumn(msg string) bool {
	return strings.Contains(msg, "no such column:")
}
