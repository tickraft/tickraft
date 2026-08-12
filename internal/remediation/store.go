// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package remediation

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Store defines the persistence operations for remediation rules and
// dispatch records. Implementations must be safe for concurrent use.
//
// Not-found conditions are reported as gorm.ErrRecordNotFound, wrapped via
// fmt.Errorf("...: %w", err) so callers can test for them with
// errors.Is(err, gorm.ErrRecordNotFound).
type Store interface {
	// CreateRule inserts a new rule. The ID and timestamps are populated
	// by the database on success.
	CreateRule(ctx context.Context, rule *Rule) error
	// UpdateRule saves all fields of the given rule. The ID identifies the
	// row to update; all other columns are overwritten. Returns
	// gorm.ErrRecordNotFound when no rule with the given ID exists.
	UpdateRule(ctx context.Context, rule *Rule) error
	// DeleteRule permanently removes a rule by its ID. Returns
	// gorm.ErrRecordNotFound when no rule with the given ID exists.
	DeleteRule(ctx context.Context, id int64) error
	// GetRule retrieves a rule by its ID. Returns gorm.ErrRecordNotFound
	// when no rule with the given ID exists.
	GetRule(ctx context.Context, id int64) (*Rule, error)
	// ListRules returns a page of rules ordered by descending ID, plus the
	// total count. limit and offset control the page; callers should clamp
	// them before calling.
	ListRules(ctx context.Context, limit, offset int) ([]*Rule, int64, error)
	// ListEnabledRules returns all rules with Enabled=true, ordered by
	// ascending ID. The Manager uses this on startup to load the active
	// rule set.
	ListEnabledRules(ctx context.Context) ([]*Rule, error)

	// CreateRecord inserts a new dispatch record. The ID and CreatedAt are
	// populated by the database on success.
	CreateRecord(ctx context.Context, record *Record) error
	// UpdateRecord saves all fields of the given record. The ID identifies
	// the row to update; all other columns are overwritten. Returns
	// gorm.ErrRecordNotFound when no record with the given ID exists.
	UpdateRecord(ctx context.Context, record *Record) error
	// GetRecord retrieves a dispatch record by its ID. Returns
	// gorm.ErrRecordNotFound when no record with the given ID exists.
	GetRecord(ctx context.Context, id int64) (*Record, error)
	// ListRecords returns a page of dispatch records ordered by descending
	// ID, plus the total count.
	ListRecords(ctx context.Context, limit, offset int) ([]*Record, int64, error)
	// ListRecentRecords returns records for the given rule and asset key
	// created at or after the since timestamp, ordered by descending
	// StartedAt. The Manager uses this for cooldown checks. When assetKey
	// is empty, records for all assets are returned.
	ListRecentRecords(ctx context.Context, ruleID int64, assetKey string, since time.Time) ([]*Record, error)
}

// store is the GORM-backed implementation of Store. It accesses the
// database through a *gorm.DB and is safe for concurrent use because
// *gorm.DB is.
type store struct {
	dbc *gorm.DB
}

// NewStore creates a Store backed by the given *gorm.DB.
func NewStore(dbc *gorm.DB) Store {
	return &store{dbc: dbc}
}

// Migrate creates or updates the remediation database tables
// (sys_remediation_rule, sys_remediation_record). It is intended to be
// called once during application startup, after db.AutoMigrate has created
// the core auth tables. Keeping this function separate from db.AutoMigrate
// ensures pkg/db remains free of internal/ dependencies and importable by
// downstream repositories.
func Migrate(ctx context.Context, dbc *gorm.DB) error {
	if err := dbc.WithContext(ctx).AutoMigrate(&Rule{}, &Record{}); err != nil {
		return fmt.Errorf("remediation: auto migrate: %w", err)
	}
	return nil
}

// CreateRule inserts a new rule. The ID and timestamps are populated by the
// database on success.
func (s *store) CreateRule(ctx context.Context, rule *Rule) error {
	if rule == nil {
		return fmt.Errorf("remediation: create rule: nil model")
	}
	if err := s.dbc.WithContext(ctx).Create(rule).Error; err != nil {
		return fmt.Errorf("remediation: create rule: %w", err)
	}
	return nil
}

// UpdateRule saves all fields of the given rule. The ID identifies the row
// to update; all other columns are overwritten. Returns
// gorm.ErrRecordNotFound when no rule with the given ID exists.
//
// The existence check is performed explicitly because GORM's Save upserts
// when the primary key is absent from the table, which would otherwise
// mask a not-found condition as a successful update.
func (s *store) UpdateRule(ctx context.Context, rule *Rule) error {
	if rule == nil {
		return fmt.Errorf("remediation: update rule: nil model")
	}
	if rule.ID == 0 {
		return fmt.Errorf("remediation: update rule: id is required")
	}
	var existing Rule
	if err := s.dbc.WithContext(ctx).Select("id").First(&existing, rule.ID).Error; err != nil {
		return fmt.Errorf("remediation: update rule: %w", err)
	}
	if err := s.dbc.WithContext(ctx).Save(rule).Error; err != nil {
		return fmt.Errorf("remediation: update rule: %w", err)
	}
	return nil
}

// DeleteRule permanently removes a rule by its ID. Returns
// gorm.ErrRecordNotFound when no rule with the given ID exists.
func (s *store) DeleteRule(ctx context.Context, id int64) error {
	result := s.dbc.WithContext(ctx).Delete(&Rule{}, id)
	if result.Error != nil {
		return fmt.Errorf("remediation: delete rule: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("remediation: delete rule: %w", gorm.ErrRecordNotFound)
	}
	return nil
}

// GetRule retrieves a rule by its ID. Returns gorm.ErrRecordNotFound when
// no rule with the given ID exists.
func (s *store) GetRule(ctx context.Context, id int64) (*Rule, error) {
	var rule Rule
	if err := s.dbc.WithContext(ctx).First(&rule, id).Error; err != nil {
		return nil, fmt.Errorf("remediation: get rule: %w", err)
	}
	return &rule, nil
}

// ListRules returns a page of rules ordered by descending ID, plus the
// total count. limit and offset control the page; callers should clamp
// them before calling.
func (s *store) ListRules(ctx context.Context, limit, offset int) ([]*Rule, int64, error) {
	var total int64
	if err := s.dbc.WithContext(ctx).Model(&Rule{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("remediation: list rules: %w", err)
	}
	var rules []*Rule
	if err := s.dbc.WithContext(ctx).
		Order("id DESC").
		Offset(offset).
		Limit(limit).
		Find(&rules).Error; err != nil {
		return nil, 0, fmt.Errorf("remediation: list rules: %w", err)
	}
	return rules, total, nil
}

// ListEnabledRules returns all rules with Enabled=true, ordered by ascending
// ID. The Manager uses this on startup to load the active rule set.
func (s *store) ListEnabledRules(ctx context.Context) ([]*Rule, error) {
	var rules []*Rule
	if err := s.dbc.WithContext(ctx).
		Where("enabled = ?", true).
		Order("id ASC").
		Find(&rules).Error; err != nil {
		return nil, fmt.Errorf("remediation: list enabled rules: %w", err)
	}
	return rules, nil
}

// CreateRecord inserts a new dispatch record. The ID and CreatedAt are
// populated by the database on success.
func (s *store) CreateRecord(ctx context.Context, record *Record) error {
	if record == nil {
		return fmt.Errorf("remediation: create record: nil model")
	}
	if err := s.dbc.WithContext(ctx).Create(record).Error; err != nil {
		return fmt.Errorf("remediation: create record: %w", err)
	}
	return nil
}

// UpdateRecord saves all fields of the given record. The ID identifies the
// row to update; all other columns are overwritten. Returns
// gorm.ErrRecordNotFound when no record with the given ID exists.
//
// The existence check is performed explicitly because GORM's Save upserts
// when the primary key is absent from the table, which would otherwise
// mask a not-found condition as a successful update.
func (s *store) UpdateRecord(ctx context.Context, record *Record) error {
	if record == nil {
		return fmt.Errorf("remediation: update record: nil model")
	}
	if record.ID == 0 {
		return fmt.Errorf("remediation: update record: id is required")
	}
	var existing Record
	if err := s.dbc.WithContext(ctx).Select("id").First(&existing, record.ID).Error; err != nil {
		return fmt.Errorf("remediation: update record: %w", err)
	}
	if err := s.dbc.WithContext(ctx).Save(record).Error; err != nil {
		return fmt.Errorf("remediation: update record: %w", err)
	}
	return nil
}

// GetRecord retrieves a dispatch record by its ID. Returns
// gorm.ErrRecordNotFound when no record with the given ID exists.
func (s *store) GetRecord(ctx context.Context, id int64) (*Record, error) {
	var record Record
	if err := s.dbc.WithContext(ctx).First(&record, id).Error; err != nil {
		return nil, fmt.Errorf("remediation: get record: %w", err)
	}
	return &record, nil
}

// ListRecords returns a page of dispatch records ordered by descending ID,
// plus the total count.
func (s *store) ListRecords(ctx context.Context, limit, offset int) ([]*Record, int64, error) {
	var total int64
	if err := s.dbc.WithContext(ctx).Model(&Record{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("remediation: list records: %w", err)
	}
	var records []*Record
	if err := s.dbc.WithContext(ctx).
		Order("id DESC").
		Offset(offset).
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, 0, fmt.Errorf("remediation: list records: %w", err)
	}
	return records, total, nil
}

// ListRecentRecords returns records for the given rule and asset key
// created at or after the since timestamp, ordered by descending
// StartedAt. The Manager uses this for cooldown checks. When assetKey is
// empty, records for all assets are returned.
func (s *store) ListRecentRecords(ctx context.Context, ruleID int64, assetKey string, since time.Time) ([]*Record, error) {
	var records []*Record
	q := s.dbc.WithContext(ctx).
		Where("rule_id = ?", ruleID).
		Where("created_at >= ?", since)
	if assetKey != "" {
		q = q.Where("asset_key = ?", assetKey)
	}
	if err := q.Order("started_at DESC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("remediation: list recent records: %w", err)
	}
	return records, nil
}

// Compile-time assertion that store implements Store.
var _ Store = (*store)(nil)
