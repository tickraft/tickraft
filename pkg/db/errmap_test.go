// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package db_test

import (
	"errors"
	"fmt"
	"testing"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

func TestMapError_Nil(t *testing.T) {
	if err := db.MapError(nil); err != nil {
		t.Errorf("db.MapError(nil) = %v, want nil", err)
	}
}

func TestMapError_RecordNotFound(t *testing.T) {
	err := db.MapError(gorm.ErrRecordNotFound)
	if !errors.Is(err, errdefs.ErrNotFound) {
		t.Errorf("db.MapError(gorm.ErrRecordNotFound) = %v, want errdefs.ErrNotFound", err)
	}
}

func TestMapError_GormTranslatedErrors(t *testing.T) {
	t.Run("gorm duplicated key", func(t *testing.T) {
		err := db.MapError(gorm.ErrDuplicatedKey)
		if !errors.Is(err, errdefs.ErrConflict) {
			t.Errorf("db.MapError(gorm.ErrDuplicatedKey) = %v, want errdefs.ErrConflict", err)
		}
	})

	t.Run("gorm foreign key violated", func(t *testing.T) {
		err := db.MapError(gorm.ErrForeignKeyViolated)
		if !errors.Is(err, db.ErrForeignKeyViolation) {
			t.Errorf("db.MapError(gorm.ErrForeignKeyViolated) = %v, want db.ErrForeignKeyViolation", err)
		}
	})
}

func TestMapError_SQLiteViolations(t *testing.T) {
	t.Run("sqlite unique violation", func(t *testing.T) {
		err := db.MapError(fmt.Errorf("UNIQUE constraint failed: users.username"))
		if !errors.Is(err, errdefs.ErrConflict) {
			t.Errorf("db.MapError(unique violation) = %v, want errdefs.ErrConflict", err)
		}
	})

	t.Run("sqlite foreign key violation", func(t *testing.T) {
		err := db.MapError(fmt.Errorf("FOREIGN KEY constraint failed"))
		if !errors.Is(err, db.ErrForeignKeyViolation) {
			t.Errorf("db.MapError(fk violation) = %v, want db.ErrForeignKeyViolation", err)
		}
	})

	t.Run("sqlite not null violation", func(t *testing.T) {
		err := db.MapError(fmt.Errorf("NOT NULL constraint failed: users.username"))
		if !errors.Is(err, db.ErrNotNullViolation) {
			t.Errorf("db.MapError(not null violation) = %v, want db.ErrNotNullViolation", err)
		}
	})

	t.Run("sqlite check violation", func(t *testing.T) {
		err := db.MapError(fmt.Errorf("CHECK constraint failed: users"))
		if !errors.Is(err, db.ErrCheckViolation) {
			t.Errorf("db.MapError(check violation) = %v, want db.ErrCheckViolation", err)
		}
	})

	t.Run("sqlite undefined table", func(t *testing.T) {
		err := db.MapError(fmt.Errorf("no such table: nonexistent"))
		if !errors.Is(err, db.ErrUndefinedTable) {
			t.Errorf("db.MapError(undefined table) = %v, want db.ErrUndefinedTable", err)
		}
	})

	t.Run("sqlite undefined column", func(t *testing.T) {
		err := db.MapError(fmt.Errorf("no such column: nonexistent"))
		if !errors.Is(err, db.ErrUndefinedColumn) {
			t.Errorf("db.MapError(undefined column) = %v, want db.ErrUndefinedColumn", err)
		}
	})
}

func TestMapError_GenericError(t *testing.T) {
	orig := errors.New("connection refused")
	err := db.MapError(orig)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if !errors.Is(err, orig) {
		t.Errorf("db.MapError(generic) should wrap original, got %v", err)
	}
}
