// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db/errmap"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/pagination"
)

// This file holds the alert domain's two persistence layers:
//
//   - Store (exported): rule persistence (sys_prism_alert_rule). Exported
//     as a concrete type because the service layer and tickraft-x use it
//     directly, including its write-path expression validation.
//   - recordStore (unexported): record persistence (sys_prism_alert_record)
//     behind the RecordStore interface (ports.go), created via
//     NewRecordStore.

// Store is the GORM-backed alert rule persistence layer. It carries a
// Compiler reference so Create and Update can validate expressions
// before they reach the database, guaranteeing that every persisted
// rule is evaluable by the engine.
type Store struct {
	dbc      *gorm.DB
	compiler *Compiler
}

// NewStore creates a Store. The compiler must be non-nil for write-path
// expression validation to function.
func NewStore(dbc *gorm.DB, compiler *Compiler) *Store {
	return &Store{dbc: dbc, compiler: compiler}
}

// Migrate runs AutoMigrate for the Rule table. It is intended to be
// invoked from the application's migration command at startup.
func (s *Store) Migrate(ctx context.Context) error {
	if err := s.dbc.WithContext(ctx).AutoMigrate(&Rule{}); err != nil {
		return fmt.Errorf("migrate alert rule table: %w", err)
	}
	return nil
}

// Create validates the rule expression and, on success, inserts a new
// row. Validation failures are returned wrapped in ErrRuleCompileFailed
// so callers can distinguish validation errors from database errors.
func (s *Store) Create(ctx context.Context, rule *Rule) error {
	if err := s.compiler.Validate(rule.Expression); err != nil {
		return fmt.Errorf("%w: %w", ErrRuleCompileFailed, err)
	}
	if err := s.dbc.WithContext(ctx).Create(rule).Error; err != nil {
		return fmt.Errorf("create rule: %w", err)
	}
	return nil
}

// ruleUpdateColumns lists the user-editable columns touched by Update.
// Runtime state (tenant_id, created_at) is deliberately absent so a
// stale or partial DTO can never clear it.
var ruleUpdateColumns = []string{
	"name", "description", "expression", "enabled", "priority",
	"group_id", "metadata", "updated_at",
}

// Update validates the rule expression and, on success, applies a
// column-level update limited to the user-editable columns. Unlike a
// full Save, tenant_id and created_at are never touched, so a PUT
// built from a partial DTO cannot silently clear them.
func (s *Store) Update(ctx context.Context, rule *Rule) error {
	if err := s.compiler.Validate(rule.Expression); err != nil {
		return fmt.Errorf("%w: %w", ErrRuleCompileFailed, err)
	}
	result := s.dbc.WithContext(ctx).
		Model(&Rule{}).
		Where("id = ?", rule.ID).
		Select(ruleUpdateColumns).
		Updates(rule)
	if result.Error != nil {
		return fmt.Errorf("update rule: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrRuleNotFound
	}
	return nil
}

// ListEnabled returns enabled rules, ordered by priority (descending)
// and then ID (ascending) for deterministic evaluation order. When
// tenantID is positive, the result is scoped to that tenant; a zero
// tenantID returns rules across all tenants, which is the mode used by
// the engine's Reload path (per-event tenant filtering happens at
// evaluation time).
func (s *Store) ListEnabled(ctx context.Context, tenantID int64) ([]Rule, error) {
	var rules []Rule
	query := s.dbc.WithContext(ctx).Where("enabled = ?", true)
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
// descending ID, plus the total count. It is intended for the rule
// CRUD API endpoints. page starts at 1; size is the maximum number of
// items returned. Soft-deleted rows are excluded.
func (s *Store) List(ctx context.Context, page, size int) ([]*Rule, int64, error) {
	page, size = pagination.Clamp(page, size)

	var total int64
	if err := s.dbc.WithContext(ctx).Model(&Rule{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("list rules: %w", err)
	}

	var rules []*Rule
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
func (s *Store) GetByID(ctx context.Context, id int64) (*Rule, error) {
	var m Rule
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
		Delete(&Rule{})
	if result.Error != nil {
		return fmt.Errorf("delete rule: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrRuleNotFound
	}
	return nil
}

// statusColumn is the DB column name shared by the acknowledge/resolve
// update statements. Extracted as a package-level constant to satisfy
// goconst.
const statusColumn = "status"

// recordStore is the GORM-backed implementation of RecordStore,
// accessing the database through a *gorm.DB.
type recordStore struct {
	dbc *gorm.DB
}

// NewRecordStore creates a new RecordStore backed by the given
// *gorm.DB.
//
//nolint:revive // recordStore is deliberately unexported; callers consume it through the exported RecordStore interface
func NewRecordStore(dbc *gorm.DB) *recordStore {
	return &recordStore{dbc: dbc}
}

// Create inserts a new alert record. The ID and CreatedAt are populated by
// the database on success.
func (s *recordStore) Create(ctx context.Context, m *Record) error {
	if m == nil {
		return fmt.Errorf("alert: create record: nil model")
	}
	if err := s.dbc.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("alert: create record: %w", errmap.MapError(err))
	}
	return nil
}

// CreateBatch inserts multiple alert records in a single DB round-trip,
// reducing per-violation INSERT overhead for events carrying multiple
// violations. Empty slices are a no-op.
func (s *recordStore) CreateBatch(ctx context.Context, models []*Record) error {
	if len(models) == 0 {
		return nil
	}
	if err := s.dbc.WithContext(ctx).CreateInBatches(models, 100).Error; err != nil {
		return fmt.Errorf("alert: create records batch: %w", errmap.MapError(err))
	}
	return nil
}

// GetByID retrieves an alert record by its ID. Returns errdefs.ErrNotFound
// when no record with the given ID exists.
func (s *recordStore) GetByID(ctx context.Context, id int64) (*Record, error) {
	var m Record
	if err := s.dbc.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, fmt.Errorf("alert: get record: %w", errmap.MapError(err))
	}
	return &m, nil
}

// List returns a page of alert records matching the filter, ordered by
// descending ID, plus the total count. page starts at 1; size is the maximum
// number of items. A zero-value filter returns all records.
func (s *recordStore) List(ctx context.Context, page, size int, filter RecordFilter) ([]*Record, int64, error) {
	page, size = pagination.Clamp(page, size)

	query := s.dbc.WithContext(ctx).Model(&Record{})
	if filter.Severity != "" {
		query = query.Where("severity = ?", filter.Severity)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if !filter.From.IsZero() {
		query = query.Where("triggered_at >= ?", filter.From)
	}
	if !filter.To.IsZero() {
		query = query.Where("triggered_at <= ?", filter.To)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("alert: list records: %w", errmap.MapError(err))
	}

	var models []*Record
	offset := (page - 1) * size
	if err := query.
		Order("id DESC").
		Offset(offset).
		Limit(size).
		Find(&models).Error; err != nil {
		return nil, 0, fmt.Errorf("alert: list records: %w", errmap.MapError(err))
	}
	return models, total, nil
}

// Acknowledge transitions the alert record identified by id to the
// "acknowledged" status and sets acknowledged_at to the current time. It
// returns the updated record. Returns errdefs.ErrNotFound when no record
// with the given ID exists.
func (s *recordStore) Acknowledge(ctx context.Context, id int64) (*Record, error) {
	now := time.Now()
	result := s.dbc.WithContext(ctx).
		Model(&Record{}).
		Where("id = ?", id).
		Updates(map[string]any{
			statusColumn:      StatusAcknowledged,
			"acknowledged_at": now,
		})
	if result.Error != nil {
		return nil, fmt.Errorf("alert: acknowledge record: %w", errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("alert: acknowledge record: %w", errdefs.ErrNotFound)
	}

	var m Record
	if err := s.dbc.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, fmt.Errorf("alert: acknowledge record: %w", errmap.MapError(err))
	}
	return &m, nil
}

// Resolve transitions the alert record identified by id to the "resolved"
// status and sets resolved_at to the current time. It returns the updated
// record. Returns errdefs.ErrNotFound when no record with the given ID
// exists.
func (s *recordStore) Resolve(ctx context.Context, id int64) (*Record, error) {
	now := time.Now()
	result := s.dbc.WithContext(ctx).
		Model(&Record{}).
		Where("id = ?", id).
		Updates(map[string]any{
			statusColumn:  StatusResolved,
			"resolved_at": now,
		})
	if result.Error != nil {
		return nil, fmt.Errorf("alert: resolve record: %w", errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("alert: resolve record: %w", errdefs.ErrNotFound)
	}

	var m Record
	if err := s.dbc.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, fmt.Errorf("alert: resolve record: %w", errmap.MapError(err))
	}
	return &m, nil
}

// Compile-time assertion that recordStore implements RecordStore.
var _ RecordStore = (*recordStore)(nil)

// Migrate creates or updates the sys_prism_alert_record table schema. It is
// intended to be called once during application startup. The
// sys_prism_alert_rule table is migrated by Store.Migrate (also in this
// file).
func Migrate(ctx context.Context, dbc *gorm.DB) error {
	if err := dbc.WithContext(ctx).AutoMigrate(&Record{}); err != nil {
		return fmt.Errorf("alert: migrate tables: %w", err)
	}
	return nil
}
