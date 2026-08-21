// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package prism provides the internal API service implementations that
// bridge the handler layer with the prism rule engine and its persistent
// rule and record stores.
package prism

import (
	"go.uber.org/zap"

	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/tickraft/tickraft/pkg/api/handler"
	alerthandler "github.com/tickraft/tickraft/pkg/api/handler/alert"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/types"
)

// AlertService implements alerthandler.Service using the prism rule engine
// and persistent rule/record stores.
type AlertService struct {
	ruleStore   *alert.Store
	ruleEngine  *alert.Engine
	recordStore alert.RecordStore
}

// NewAlertService creates an AlertService backed by the given rule store,
// record store, and rule engine.
func NewAlertService(ruleStore *alert.Store, recordStore alert.RecordStore, ruleEngine *alert.Engine) *AlertService {
	return &AlertService{
		ruleStore:   ruleStore,
		recordStore: recordStore,
		ruleEngine:  ruleEngine,
	}
}

// ListRules returns a page of alert rules and the total count.
func (s *AlertService) ListRules(ctx context.Context, page, size int) ([]alerthandler.Rule, int64, error) {
	page, size = pagination.Clamp(page, size)
	models, total, err := s.ruleStore.List(ctx, page, size)
	if err != nil {
		return nil, 0, mapRuleStoreError(err)
	}
	rules := make([]alerthandler.Rule, 0, len(models))
	for _, m := range models {
		rules = append(rules, ruleModelToHandler(m))
	}
	return rules, total, nil
}

// GetRule returns a single alert rule by ID.
func (s *AlertService) GetRule(ctx context.Context, id int64) (*alerthandler.Rule, error) {
	m, err := s.ruleStore.GetByID(ctx, id)
	if err != nil {
		return nil, mapRuleStoreError(err)
	}
	h := ruleModelToHandler(m)
	return &h, nil
}

// CreateRule creates a new alert rule from the given request.
func (s *AlertService) CreateRule(ctx context.Context, req *alerthandler.Rule) (*alerthandler.Rule, error) {
	if req == nil {
		return nil, handler.ErrInvalidRequest
	}
	if req.Name == "" || req.Expression == "" {
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
func (s *AlertService) UpdateRule(ctx context.Context, id int64, req *alerthandler.Rule) (*alerthandler.Rule, error) {
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
	if err = s.ruleStore.Update(ctx, m); err != nil {
		return nil, mapRuleStoreError(err)
	}
	// best-effort: rule updated; reload failure keeps the engine on
	// the previous rule set, so log it rather than failing the request.
	if err = s.reloadRules(ctx); err != nil {
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
func (s *AlertService) ListRecords(
	ctx context.Context,
	page, size int,
	filter alerthandler.RecordFilter,
) ([]alerthandler.Record, int64, error) {
	page, size = pagination.Clamp(page, size)
	storeFilter := alert.RecordFilter{
		Severity: filter.Severity,
		Status:   filter.Status,
		From:     filter.From,
		To:       filter.To,
	}
	models, total, err := s.recordStore.List(ctx, page, size, storeFilter)
	if err != nil {
		return nil, 0, mapRecordStoreError(err)
	}
	records := make([]alerthandler.Record, 0, len(models))
	for _, m := range models {
		records = append(records, recordModelToHandler(*m))
	}
	return records, total, nil
}

// GetRecord returns a single alert record by ID.
func (s *AlertService) GetRecord(ctx context.Context, id int64) (*alerthandler.Record, error) {
	m, err := s.recordStore.GetByID(ctx, id)
	if err != nil {
		return nil, mapRecordStoreError(err)
	}
	h := recordModelToHandler(*m)
	return &h, nil
}

// AcknowledgeRecord transitions the alert record to "acknowledged" status.
func (s *AlertService) AcknowledgeRecord(ctx context.Context, id int64) (*alerthandler.Record, error) {
	m, err := s.recordStore.Acknowledge(ctx, id)
	if err != nil {
		return nil, mapRecordStoreError(err)
	}
	h := recordModelToHandler(*m)
	return &h, nil
}

// ResolveRecord transitions the alert record to "resolved" status.
func (s *AlertService) ResolveRecord(ctx context.Context, id int64) (*alerthandler.Record, error) {
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

// ruleModelToHandler converts a alert.Rule persistence model into the
// handler-layer Rule DTO.
func ruleModelToHandler(m *alert.Rule) alerthandler.Rule {
	return alerthandler.Rule{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		Expression:  m.Expression,
		Priority:    m.Priority,
		GroupID:     m.GroupID,
		Metadata:    decodeRuleMetadata(m.Metadata),
		Enabled:     m.Enabled,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
}

// ruleHandlerToModel converts a handler-layer Rule DTO into a
// alert.Rule persistence model ready for Create/Update. Tenant and
// lifecycle fields are deliberately not mapped: the store's column-level
// Update never touches them, and Create assigns them server-side.
func ruleHandlerToModel(r *alerthandler.Rule) *alert.Rule {
	return &alert.Rule{
		Name:        r.Name,
		Description: r.Description,
		Expression:  r.Expression,
		Priority:    r.Priority,
		GroupID:     r.GroupID,
		Metadata:    encodeRuleMetadata(r.Metadata),
		Enabled:     r.Enabled,
	}
}

// decodeRuleMetadata parses a rule's JSON metadata blob into the DTO's
// map form. Malformed or empty metadata yields nil.
func decodeRuleMetadata(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	var metadata map[string]string
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil || len(metadata) == 0 {
		return nil
	}
	return metadata
}

// encodeRuleMetadata serializes the DTO's metadata map into the JSON
// blob persisted on the rule. A nil or empty map yields the empty
// string.
func encodeRuleMetadata(metadata map[string]string) string {
	if len(metadata) == 0 {
		return ""
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		// map[string]string is always marshalable; unreachable in practice.
		return ""
	}
	return string(raw)
}

// recordModelToHandler converts a alert.Record persistence model into
// the handler-layer Record DTO, defaulting severity to "warning" when
// empty.
func recordModelToHandler(m alert.Record) alerthandler.Record {
	severity := m.Severity
	if severity == "" {
		severity = string(types.SeverityWarning)
	}
	return alerthandler.Record{
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
// ServiceError suitable for the API response layer. Expression
// validation failures map to 400 with the innermost (expr-lang)
// diagnostic so the client can display the position-annotated message.
func mapRuleStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, alert.ErrRuleNotFound) {
		return handler.ErrRuleNotFound
	}
	if errors.Is(err, errdefs.ErrNotFound) {
		return handler.ErrRuleNotFound
	}
	if errors.Is(err, alert.ErrRuleCompileFailed) {
		return handler.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
			"invalid expression: "+innermostMessage(err))
	}
	return handler.NewServiceError(http.StatusInternalServerError, errdefs.CodeInternal, err.Error())
}

// innermostMessage walks the wrap chain and returns the leaf error's
// message, discarding this package's sentinel prefixes.
func innermostMessage(err error) string {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return err.Error()
		}
		err = next
	}
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
