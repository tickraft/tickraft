// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// Store is the GORM-backed rule persistence layer. It carries a
// Compiler reference so Create and Update can validate expressions
// before they reach the database, guaranteeing that every persisted
// rule is evaluable by the engine.
type Store struct {
	dbc      *gorm.DB
	compiler *Compiler
}

// NewStore creates a Store. The compiler must be non-nil for
// write-path expression validation to function.
func NewStore(dbc *gorm.DB, compiler *Compiler) *Store {
	return &Store{dbc: dbc, compiler: compiler}
}

// Migrate runs AutoMigrate for the Record table. It is intended
// to be invoked from the application's migration command at startup.
func (s *Store) Migrate(ctx context.Context) error {
	if err := s.dbc.WithContext(ctx).AutoMigrate(&Record{}); err != nil {
		return fmt.Errorf("migrate rule table: %w", err)
	}
	return nil
}

// Create compile-checks the rule expression and, on success, inserts
// a new row. Compile failures are returned wrapped in
// ErrRuleCompileFailed so callers can distinguish validation errors
// from database errors.
func (s *Store) Create(ctx context.Context, rule *Record) error {
	if _, err := s.compiler.Compile(Scene(rule.Scene), rule.Expression); err != nil {
		return fmt.Errorf("validate expression: %w", err)
	}
	if err := s.dbc.WithContext(ctx).Create(rule).Error; err != nil {
		return fmt.Errorf("create rule: %w", err)
	}
	return nil
}

// Update compile-checks the rule expression and, on success, saves
// the rule. See Create for error semantics.
func (s *Store) Update(ctx context.Context, rule *Record) error {
	if _, err := s.compiler.Compile(Scene(rule.Scene), rule.Expression); err != nil {
		return fmt.Errorf("validate expression: %w", err)
	}
	if err := s.dbc.WithContext(ctx).Save(rule).Error; err != nil {
		return fmt.Errorf("update rule: %w", err)
	}
	return nil
}

// Delete soft-deletes the rule identified by (id, tenantID). A
// RowsAffected count of zero is reported as ErrRuleNotFound so
// callers can detect the missing-rule case without inspecting the
// underlying error type.
func (s *Store) Delete(ctx context.Context, id, tenantID int64) error {
	result := s.dbc.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&Record{})
	if result.Error != nil {
		return fmt.Errorf("delete rule: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrRuleNotFound
	}
	return nil
}

// Get retrieves the rule identified by (id, tenantID). A
// gorm.ErrRecordNotFound is mapped to ErrRuleNotFound; other database
// errors are wrapped with a "get rule" prefix.
func (s *Store) Get(ctx context.Context, id, tenantID int64) (*Record, error) {
	var m Record
	err := s.dbc.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRuleNotFound
		}
		return nil, fmt.Errorf("get rule: %w", err)
	}
	return &m, nil
}

// ListEnabled returns enabled rules for the given scene, ordered by
// priority (descending) and then ID (ascending) for deterministic
// evaluation order. When tenantID is positive, the result is scoped
// to that tenant; a zero tenantID returns rules across all tenants,
// which is the mode used by the engine's Reload path.
func (s *Store) ListEnabled(ctx context.Context, tenantID int64, scene Scene) ([]Record, error) {
	var rules []Record
	query := s.dbc.WithContext(ctx).Where("enabled = ? AND scene = ?", true, scene)
	if tenantID > 0 {
		query = query.Where("tenant_id = ?", tenantID)
	}
	query = query.Order("priority DESC, id ASC")
	if err := query.Find(&rules).Error; err != nil {
		return nil, fmt.Errorf("list enabled rules: %w", err)
	}
	return rules, nil
}

// List returns a page of all rules (enabled and disabled) ordered by
// descending ID, plus the total count. It is intended for the rule CRUD
// API endpoints. page starts at 1; size is the maximum number of items
// returned. Soft-deleted rows are excluded.
func (s *Store) List(ctx context.Context, page, size int) ([]*Record, int64, error) {
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}

	var total int64
	if err := s.dbc.WithContext(ctx).Model(&Record{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("list rules: %w", err)
	}

	var rules []*Record
	offset := (page - 1) * size
	if err := s.dbc.WithContext(ctx).
		Order("id DESC").
		Offset(offset).
		Limit(size).
		Find(&rules).Error; err != nil {
		return nil, 0, fmt.Errorf("list rules: %w", err)
	}
	return rules, total, nil
}

// GetByID retrieves a rule by its ID without tenant scoping. It is
// intended for the rule CRUD API endpoints where the caller already
// holds the rule ID. A gorm.ErrRecordNotFound is mapped to
// ErrRuleNotFound; other database errors are wrapped with a "get rule"
// prefix.
func (s *Store) GetByID(ctx context.Context, id int64) (*Record, error) {
	var m Record
	err := s.dbc.WithContext(ctx).First(&m, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRuleNotFound
		}
		return nil, fmt.Errorf("get rule: %w", err)
	}
	return &m, nil
}

// DeleteByID soft-deletes the rule identified by id without tenant
// scoping. It is intended for the rule CRUD API endpoints. A
// RowsAffected count of zero is reported as ErrRuleNotFound so callers
// can detect the missing-rule case without inspecting the underlying
// error type.
func (s *Store) DeleteByID(ctx context.Context, id int64) error {
	result := s.dbc.WithContext(ctx).
		Where("id = ?", id).
		Delete(&Record{})
	if result.Error != nil {
		return fmt.Errorf("delete rule: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrRuleNotFound
	}
	return nil
}
