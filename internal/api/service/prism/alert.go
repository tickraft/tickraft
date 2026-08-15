// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package prism

import (
	"go.uber.org/zap"

	"context"
	"errors"
	"net/http"

	"github.com/tickraft/tickraft/pkg/api/handler"
	"github.com/tickraft/tickraft/pkg/api/handler/alert"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
	prismalert "github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/rule"
)

// AlertService implements alert.Service using the prism rule engine
// and persistent rule/record stores.
type AlertService struct {
	ruleStore   *rule.Store
	ruleEngine  *rule.Engine
	recordStore prismalert.RecordStore
}

// NewAlertService creates an AlertService backed by the given rule store,
// record store, and rule engine.
func NewAlertService(ruleStore *rule.Store, recordStore prismalert.RecordStore, ruleEngine *rule.Engine) *AlertService {
	return &AlertService{
		ruleStore:   ruleStore,
		recordStore: recordStore,
		ruleEngine:  ruleEngine,
	}
}

// ListRules returns a page of alert rules and the total count.
func (s *AlertService) ListRules(ctx context.Context, page, size int) ([]alert.Rule, int64, error) {
	page, size = httputil.ClampPaging(page, size)
	models, total, err := s.ruleStore.List(ctx, page, size)
	if err != nil {
		return nil, 0, mapRuleStoreError(err)
	}
	rules := make([]alert.Rule, 0, len(models))
	for _, m := range models {
		rules = append(rules, ruleModelToHandler(m))
	}
	return rules, total, nil
}

// GetRule returns a single alert rule by ID.
func (s *AlertService) GetRule(ctx context.Context, id int64) (*alert.Rule, error) {
	m, err := s.ruleStore.GetByID(ctx, id)
	if err != nil {
		return nil, mapRuleStoreError(err)
	}
	h := ruleModelToHandler(m)
	return &h, nil
}

// CreateRule creates a new alert rule from the given request.
func (s *AlertService) CreateRule(ctx context.Context, req *alert.Rule) (*alert.Rule, error) {
	if req == nil {
		return nil, handler.ErrInvalidRequest
	}
	if req.Name == "" || req.Scene == "" || req.Expression == "" {
		return nil, handler.ErrInvalidRequest
	}
	m := ruleHandlerToModel(req)
	if err := s.ruleStore.Create(ctx, m); err != nil {
		return nil, mapRuleStoreError(err)
	}
	// best-effort: rule persisted; reload failure keeps the engine on
	// the previous rule set, so log it rather than failing the request.
	if err := s.reloadRules(ctx); err != nil {
		zap.L().Warn("alert rule engine reload failed after create",
			zap.Int64("rule_id", m.ID), zap.Error(err))
	}
	h := ruleModelToHandler(m)
	return &h, nil
}

// UpdateRule updates an existing alert rule identified by ID.
func (s *AlertService) UpdateRule(ctx context.Context, id int64, req *alert.Rule) (*alert.Rule, error) {
	if req == nil {
		return nil, handler.ErrInvalidRequest
	}
	existing, err := s.ruleStore.GetByID(ctx, id)
	if err != nil {
		return nil, mapRuleStoreError(err)
	}
	m := ruleHandlerToModel(req)
	m.ID = existing.ID
	m.CreatedAt = existing.CreatedAt
	if err := s.ruleStore.Update(ctx, m); err != nil {
		return nil, mapRuleStoreError(err)
	}
	// best-effort: rule updated; reload failure keeps the engine on
	// the previous rule set, so log it rather than failing the request.
	if err := s.reloadRules(ctx); err != nil {
		zap.L().Warn("alert rule engine reload failed after update",
			zap.Int64("rule_id", m.ID), zap.Error(err))
	}
	h := ruleModelToHandler(m)
	return &h, nil
}

// DeleteRule deletes an alert rule by ID.
func (s *AlertService) DeleteRule(ctx context.Context, id int64) error {
	if err := s.ruleStore.DeleteByID(ctx, id); err != nil {
		return mapRuleStoreError(err)
	}
	// best-effort: rule deleted; reload failure keeps the engine on
	// the previous rule set, so log it rather than failing the request.
	if err := s.reloadRules(ctx); err != nil {
		zap.L().Warn("alert rule engine reload failed after delete",
			zap.Int64("rule_id", id), zap.Error(err))
	}
	return nil
}

// ListRecords returns a page of alert records matching the filter and the
// total count.
func (s *AlertService) ListRecords(ctx context.Context, page, size int, filter alert.RecordFilter) ([]alert.Record, int64, error) {
	page, size = httputil.ClampPaging(page, size)
	storeFilter := prismalert.RecordFilter{
		Severity: filter.Severity,
		Status:   filter.Status,
		From:     filter.From,
		To:       filter.To,
	}
	models, total, err := s.recordStore.List(ctx, page, size, storeFilter)
	if err != nil {
		return nil, 0, mapRecordStoreError(err)
	}
	records := make([]alert.Record, 0, len(models))
	for _, m := range models {
		records = append(records, recordModelToHandler(*m))
	}
	return records, total, nil
}

// GetRecord returns a single alert record by ID.
func (s *AlertService) GetRecord(ctx context.Context, id int64) (*alert.Record, error) {
	m, err := s.recordStore.GetByID(ctx, id)
	if err != nil {
		return nil, mapRecordStoreError(err)
	}
	h := recordModelToHandler(*m)
	return &h, nil
}

// AcknowledgeRecord transitions the alert record to "acknowledged" status.
func (s *AlertService) AcknowledgeRecord(ctx context.Context, id int64) (*alert.Record, error) {
	m, err := s.recordStore.Acknowledge(ctx, id)
	if err != nil {
		return nil, mapRecordStoreError(err)
	}
	h := recordModelToHandler(*m)
	return &h, nil
}

// ResolveRecord transitions the alert record to "resolved" status.
func (s *AlertService) ResolveRecord(ctx context.Context, id int64) (*alert.Record, error) {
	m, err := s.recordStore.Resolve(ctx, id)
	if err != nil {
		return nil, mapRecordStoreError(err)
	}
	h := recordModelToHandler(*m)
	return &h, nil
}

// reloadRules reloads the rule engine from the store when both are non-nil.
func (s *AlertService) reloadRules(ctx context.Context) error {
	if s.ruleEngine == nil || s.ruleStore == nil {
		return nil
	}
	return s.ruleEngine.Reload(ctx, s.ruleStore)
}

// ruleModelToHandler converts a rule.Record persistence model into the
// handler-layer Rule DTO.
func ruleModelToHandler(m *rule.Record) alert.Rule {
	return alert.Rule{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		Scene:       m.Scene,
		Expression:  m.Expression,
		Priority:    m.Priority,
		Enabled:     m.Enabled,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
}

// ruleHandlerToModel converts a handler-layer Rule DTO into a
// rule.Record persistence model ready for Create/Update.
func ruleHandlerToModel(r *alert.Rule) *rule.Record {
	return &rule.Record{
		Name:        r.Name,
		Description: r.Description,
		Scene:       r.Scene,
		Expression:  r.Expression,
		Priority:    r.Priority,
		Enabled:     r.Enabled,
	}
}

// recordModelToHandler converts a prismalert.Record persistence model into
// the handler-layer Record DTO, defaulting severity to "warning" when
// empty.
func recordModelToHandler(m prismalert.Record) alert.Record {
	severity := m.Severity
	if severity == "" {
		severity = "warning"
	}
	return alert.Record{
		ID:             m.ID,
		RuleID:         m.RuleID,
		RuleName:       m.RuleName,
		Severity:       severity,
		Value:          m.Value,
		Status:         m.Status,
		Message:        m.Message,
		FiredAt:        m.TriggeredAt,
		AcknowledgedAt: m.AcknowledgedAt,
		ResolvedAt:     m.ResolvedAt,
	}
}

// mapRuleStoreError translates a rule store error into a handler-level
// ServiceError suitable for the API response layer.
func mapRuleStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, rule.ErrRuleNotFound) {
		return handler.ErrRuleNotFound
	}
	if errors.Is(err, errdefs.ErrNotFound) {
		return handler.ErrRuleNotFound
	}
	return handler.NewServiceError(http.StatusInternalServerError, errdefs.CodeInternal, err.Error())
}

// mapRecordStoreError translates an alert record store error into a
// handler-level ServiceError suitable for the API response layer.
func mapRecordStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errdefs.ErrNotFound) {
		return handler.ErrRecordNotFound
	}
	return handler.NewServiceError(http.StatusInternalServerError, errdefs.CodeInternal, err.Error())
}
