// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db/errmap"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// TemplateStore provides CRUD operations for telemetry templates backed by
// a GORM database. All methods map low-level driver errors to the db
// sentinel errors (ErrNotFound, ErrDuplicateKey) so callers can use
// errors.Is for consistent handling.
type TemplateStore struct {
	dbc *gorm.DB
}

// NewTemplateStore creates a new TemplateStore backed by the given GORM
// database.
func NewTemplateStore(dbc *gorm.DB) *TemplateStore {
	return &TemplateStore{dbc: dbc}
}

// List returns templates filtered by an optional category. When category
// is empty, all templates are returned ordered by ascending ID.
func (s *TemplateStore) List(ctx context.Context, category string) ([]Template, error) {
	query := s.dbc.WithContext(ctx).Model(&Template{})
	if category != "" {
		query = query.Where("category = ?", category)
	}
	var templates []Template
	if err := query.Order("id ASC").Find(&templates).Error; err != nil {
		return nil, fmt.Errorf("telemetry: list templates: %w", errmap.MapError(err))
	}
	return templates, nil
}

// GetByID retrieves a single template by its ID. It returns
// errdefs.ErrNotFound when no template exists with the given ID.
func (s *TemplateStore) GetByID(ctx context.Context, id int64) (*Template, error) {
	var t Template
	if err := s.dbc.WithContext(ctx).First(&t, id).Error; err != nil {
		return nil, fmt.Errorf("telemetry: get template %d: %w", id, errmap.MapError(err))
	}
	return &t, nil
}

// Create inserts a new template. The caller is responsible for populating
// all required fields. A duplicate name surfaces as errdefs.ErrConflict.
func (s *TemplateStore) Create(ctx context.Context, t *Template) error {
	if err := s.dbc.WithContext(ctx).Create(t).Error; err != nil {
		return fmt.Errorf("telemetry: create template: %w", errmap.MapError(err))
	}
	return nil
}

// Update saves all fields of an existing template. The ID field identifies
// the row to update. It returns errdefs.ErrNotFound when the ID does not
// exist.
func (s *TemplateStore) Update(ctx context.Context, t *Template) error {
	result := s.dbc.WithContext(ctx).Save(t)
	if result.Error != nil {
		return fmt.Errorf("telemetry: update template %d: %w", t.ID, errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("telemetry: update template %d: %w", t.ID, errdefs.ErrNotFound)
	}
	return nil
}

// Delete permanently removes a template by ID. It returns
// errdefs.ErrNotFound when the ID does not exist.
func (s *TemplateStore) Delete(ctx context.Context, id int64) error {
	result := s.dbc.WithContext(ctx).Delete(&Template{}, id)
	if result.Error != nil {
		return fmt.Errorf("telemetry: delete template %d: %w", id, errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("telemetry: delete template %d: %w", id, errdefs.ErrNotFound)
	}
	return nil
}

// ListBuiltin returns only built-in templates ordered by ascending ID.
func (s *TemplateStore) ListBuiltin(ctx context.Context) ([]Template, error) {
	var templates []Template
	if err := s.dbc.WithContext(ctx).
		Where("is_builtin = ?", true).
		Order("id ASC").
		Find(&templates).Error; err != nil {
		return nil, fmt.Errorf("telemetry: list builtin templates: %w", errmap.MapError(err))
	}
	return templates, nil
}
