// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package errmap

import (
	"errors"
	"fmt"
	"testing"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/errdefs"
)

func TestMapError_Nil(t *testing.T) {
	if err := MapError(nil); err != nil {
		t.Errorf("MapError(nil) = %v, want nil", err)
	}
}

func TestMapError_RecordNotFound(t *testing.T) {
	err := MapError(gorm.ErrRecordNotFound)
	if !errors.Is(err, errdefs.ErrNotFound) {
		t.Errorf("MapError(ErrRecordNotFound) = %v, want errdefs.ErrNotFound", err)
	}
}

func TestMapError_GormTranslatedErrors(t *testing.T) {
	t.Run("gorm duplicated key", func(t *testing.T) {
		err := MapError(gorm.ErrDuplicatedKey)
		if !errors.Is(err, errdefs.ErrConflict) {
			t.Errorf("MapError(ErrDuplicatedKey) = %v, want errdefs.ErrConflict", err)
		}
	})

	t.Run("gorm foreign key violated", func(t *testing.T) {
		err := MapError(gorm.ErrForeignKeyViolated)
		if !errors.Is(err, ErrForeignKeyViolation) {
			t.Errorf("MapError(ErrForeignKeyViolated) = %v, want ErrForeignKeyViolation", err)
		}
	})
}

func TestMapError_SQLiteViolations(t *testing.T) {
	t.Run("sqlite unique violation", func(t *testing.T) {
		err := MapError(fmt.Errorf("UNIQUE constraint failed: users.username"))
		if !errors.Is(err, errdefs.ErrConflict) {
			t.Errorf("MapError(unique violation) = %v, want errdefs.ErrConflict", err)
		}
	})

	t.Run("sqlite foreign key violation", func(t *testing.T) {
		err := MapError(fmt.Errorf("FOREIGN KEY constraint failed"))
		if !errors.Is(err, ErrForeignKeyViolation) {
			t.Errorf("MapError(fk violation) = %v, want ErrForeignKeyViolation", err)
		}
	})

	t.Run("sqlite not null violation", func(t *testing.T) {
		err := MapError(fmt.Errorf("NOT NULL constraint failed: users.username"))
		if !errors.Is(err, ErrNotNullViolation) {
			t.Errorf("MapError(not null violation) = %v, want ErrNotNullViolation", err)
		}
	})

	t.Run("sqlite check violation", func(t *testing.T) {
		err := MapError(fmt.Errorf("CHECK constraint failed: users"))
		if !errors.Is(err, ErrCheckViolation) {
			t.Errorf("MapError(check violation) = %v, want ErrCheckViolation", err)
		}
	})

	t.Run("sqlite undefined table", func(t *testing.T) {
		err := MapError(fmt.Errorf("no such table: nonexistent"))
		if !errors.Is(err, ErrUndefinedTable) {
			t.Errorf("MapError(undefined table) = %v, want ErrUndefinedTable", err)
		}
	})

	t.Run("sqlite undefined column", func(t *testing.T) {
		err := MapError(fmt.Errorf("no such column: nonexistent"))
		if !errors.Is(err, ErrUndefinedColumn) {
			t.Errorf("MapError(undefined column) = %v, want ErrUndefinedColumn", err)
		}
	})
}

func TestMapError_GenericError(t *testing.T) {
	orig := errors.New("connection refused")
	err := MapError(orig)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if !errors.Is(err, orig) {
		t.Errorf("MapError(generic) should wrap original, got %v", err)
	}
}
