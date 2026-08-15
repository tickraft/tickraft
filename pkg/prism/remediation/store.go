// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package remediation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tickraft/tickraft/pkg/db/errmap"
	"gorm.io/gorm"
)

// ErrRuleNotFound is returned when a remediation rule cannot be located by
// its ID.
var ErrRuleNotFound = errors.New("remediation: rule not found")

// Store is the GORM-backed implementation of RuleStore, accessing the
// database through a *gorm.DB. In addition to the engine-facing methods
// (GetRules, UpdateRuleStatus, UpdateLastRun) required by the RuleStore
// interface, it provides CRUD operations (Migrate, Create, GetByID, List,
// Update, DeleteByID) consumed by the remediation rule API service.
type Store struct {
	dbc *gorm.DB
}

// NewStore creates a new RuleStore backed by the given *gorm.DB.
func NewStore(dbc *gorm.DB) *Store {
	return &Store{dbc: dbc}
}

// Migrate runs AutoMigrate for the Rule and Record tables. It is intended
// to be invoked from the application's migration phase at startup.
func (s *Store) Migrate(ctx context.Context) error {
	if err := s.dbc.WithContext(ctx).AutoMigrate(&Rule{}, &Record{}); err != nil {
		return fmt.Errorf("remediation: migrate rule/record tables: %w", err)
	}
	return nil
}

// Compile-time assertion that *Store satisfies RecordStore.
var _ RecordStore = (*Store)(nil)

// UpsertRecord inserts the record when no row with the same RunID exists,
// or updates the existing row's lifecycle fields (status, error,
// started_at, finished_at) otherwise. The Manager calls this at each
// lifecycle transition of a dispatch.
func (s *Store) UpsertRecord(ctx context.Context, record *Record) error {
	if record == nil {
		return fmt.Errorf("remediation: upsert record: nil model")
	}
	if record.RunID == "" {
		return fmt.Errorf("remediation: upsert record: run_id is required")
	}
	updates := map[string]any{
		"status":      record.Status,
		"error":       record.Error,
		"started_at":  record.StartedAt,
		"finished_at": record.FinishedAt,
	}
	result := s.dbc.WithContext(ctx).
		Model(&Record{}).
		Where("run_id = ?", record.RunID).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("remediation: upsert record: %w", errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		if err := s.dbc.WithContext(ctx).Create(record).Error; err != nil {
			return fmt.Errorf("remediation: create record: %w", errmap.MapError(err))
		}
	}
	return nil
}

// ListRecords returns a page of dispatch records ordered by descending ID,
// plus the total count. A non-empty status filters by exact lifecycle
// status match. limit and offset control the page; callers should clamp
// them before calling.
func (s *Store) ListRecords(ctx context.Context, limit, offset int, status string) ([]*Record, int64, error) {
	q := s.dbc.WithContext(ctx).Model(&Record{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("remediation: count records: %w", errmap.MapError(err))
	}
	if total == 0 {
		return nil, 0, nil
	}
	var records []*Record
	if err := q.
		Order("id DESC").
		Limit(limit).
		Offset(offset).
		Find(&records).Error; err != nil {
		return nil, 0, fmt.Errorf("remediation: list records: %w", errmap.MapError(err))
	}
	return records, total, nil
}

// Create inserts a new remediation rule record. The ID, CreatedAt, and
// UpdatedAt are populated by the database on success.
func (s *Store) Create(ctx context.Context, m *Rule) error {
	if m == nil {
		return fmt.Errorf("remediation: create rule: nil model")
	}
	if err := s.dbc.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("remediation: create rule: %w", errmap.MapError(err))
	}
	return nil
}

// Update saves the remediation rule record. The ID field identifies the
// row to update; CreatedAt is preserved by the caller before invoking
// Update. A RowsAffected count of zero is reported as ErrRuleNotFound.
func (s *Store) Update(ctx context.Context, m *Rule) error {
	if m == nil {
		return fmt.Errorf("remediation: update rule: nil model")
	}
	result := s.dbc.WithContext(ctx).Save(m)
	if result.Error != nil {
		return fmt.Errorf("remediation: update rule: %w", errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrRuleNotFound
	}
	return nil
}

// GetByID retrieves a remediation rule record by its ID. Returns
// ErrRuleNotFound when no record with the given ID exists.
func (s *Store) GetByID(ctx context.Context, id int64) (*Rule, error) {
	var m Rule
	err := s.dbc.WithContext(ctx).First(&m, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRuleNotFound
		}
		return nil, fmt.Errorf("remediation: get rule: %w", errmap.MapError(err))
	}
	return &m, nil
}

// List returns a page of remediation rule records ordered by descending ID,
// plus the total count. page starts at 1; size is the maximum number of
// items returned. Soft-deleted rows are excluded.
func (s *Store) List(ctx context.Context, page, size int) ([]*Rule, int64, error) {
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
	if err := s.dbc.WithContext(ctx).Model(&Rule{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("remediation: list rules: %w", errmap.MapError(err))
	}

	var models []*Rule
	offset := (page - 1) * size
	if err := s.dbc.WithContext(ctx).
		Order("id DESC").
		Offset(offset).
		Limit(size).
		Find(&models).Error; err != nil {
		return nil, 0, fmt.Errorf("remediation: list rules: %w", errmap.MapError(err))
	}
	return models, total, nil
}

// DeleteByID soft-deletes the remediation rule record identified by id. A
// RowsAffected count of zero is reported as ErrRuleNotFound so callers can
// detect the missing-rule case without inspecting the underlying error type.
func (s *Store) DeleteByID(ctx context.Context, id int64) error {
	result := s.dbc.WithContext(ctx).
		Where("id = ?", id).
		Delete(&Rule{})
	if result.Error != nil {
		return fmt.Errorf("remediation: delete rule: %w", errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return ErrRuleNotFound
	}
	return nil
}

// GetRules returns enabled remediation rules for the given tenant, asset,
// and trigger type. Both global rules (asset_id = 0) and asset-scoped rules
// (asset_id = assetID) are returned. Rules are ordered by descending ID so
// more specific (recently created) rules evaluate first.
func (s *Store) GetRules(ctx context.Context, tenantID int64, assetID int64, triggerType string) ([]*Rule, error) {
	var rules []*Rule
	q := s.dbc.WithContext(ctx).
		Where("tenant_id = ? AND trigger_event_type = ? AND enabled = ?",
			tenantID, triggerType, true).
		Where("asset_id = 0 OR asset_id = ?", assetID).
		Order("id DESC")
	if err := q.Find(&rules).Error; err != nil {
		return nil, fmt.Errorf("remediation: get rules: %w", errmap.MapError(err))
	}
	return rules, nil
}

// UpdateRuleStatus updates the rule's operational status and metadata blob.
func (s *Store) UpdateRuleStatus(ctx context.Context, ruleID int64, status string, metadata string) error {
	updates := map[string]any{
		"status":   status,
		"metadata": metadata,
	}
	if err := s.dbc.WithContext(ctx).Model(&Rule{}).
		Where("id = ?", ruleID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("remediation: update rule status: %w", errmap.MapError(err))
	}
	return nil
}

// UpdateLastRun records the last execution timestamp for the rule.
func (s *Store) UpdateLastRun(ctx context.Context, ruleID int64, lastRunAt time.Time) error {
	if err := s.dbc.WithContext(ctx).Model(&Rule{}).
		Where("id = ?", ruleID).
		Update("last_run_at", lastRunAt).Error; err != nil {
		return fmt.Errorf("remediation: update last run: %w", errmap.MapError(err))
	}
	return nil
}
