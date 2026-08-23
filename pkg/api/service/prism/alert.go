// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package prism provides the API service implementations that
// bridge the handler layer with the prism rule engine and its persistent
// rule and record stores.
package prism

import (
	"context"
	"errors"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/api/handler"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// AlertService implements alerthandler.Service using the prism rule engine
// and persistent rule/record stores. The wire shape and the storage shape
// are the same alert.Rule / alert.Record models, so this service only
// orchestrates stores and the engine reload — there is no DTO conversion.
type AlertService struct {
	rules      *alert.Store
	records    alert.RecordStore
	ruleEngine *alert.Engine
}

// NewAlertService creates an AlertService backed by the given rule store,
// record store, and rule engine.
func NewAlertService(ruleStore *alert.Store, recordStore alert.RecordStore, ruleEngine *alert.Engine) *AlertService {
	return &AlertService{
		rules:      ruleStore,
		records:    recordStore,
		ruleEngine: ruleEngine,
	}
}

// ListRules returns a page of alert rules and the total count.
func (s *AlertService) ListRules(ctx context.Context, page, size int) ([]*alert.Rule, int64, error) {
	page, size = pagination.Clamp(page, size)
	rules, total, err := s.rules.List(ctx, page, size)
	if err != nil {
		return nil, 0, mapRuleStoreError(err)
	}

	return rules, total, nil
}

// GetRule returns a single alert rule by ID.
func (s *AlertService) GetRule(ctx context.Context, id int64) (*alert.Rule, error) {
	m, err := s.rules.GetByID(ctx, id)
	if err != nil {
		return nil, mapRuleStoreError(err)
	}
	return m, nil
}

// CreateRule creates a new alert rule from the given request.
func (s *AlertService) CreateRule(ctx context.Context, req *alert.Rule) (*alert.Rule, error) {
	if req == nil {
		return nil, handler.ErrInvalidRequest
	}
	if req.Name == "" || req.Expression == "" {
		return nil, handler.ErrInvalidRequest
	}
	// Server-assigned fields are dropped from the request so a client
	// cannot pick its own row ID or timestamps; Create back-fills ID,
	// CreatedAt, and UpdatedAt after the insert.
	req.ID = 0
	req.CreatedAt = time.Time{}
	req.UpdatedAt = time.Time{}
	if err := s.rules.Create(ctx, req); err != nil {
		return nil, mapRuleStoreError(err)
	}
	// best-effort: rule persisted; reload failure keeps the engine on
	// the previous rule set, so log it rather than failing the request.
	if err := s.reloadRules(ctx); err != nil {
		zap.L().Warn("alert rule engine reload failed after create",
			zap.Int64("rule_id", req.ID), zap.Error(err))
	}
	return req, nil
}

// UpdateRule updates an existing alert rule identified by ID.
func (s *AlertService) UpdateRule(ctx context.Context, id int64, req *alert.Rule) (*alert.Rule, error) {
	if req == nil {
		return nil, handler.ErrInvalidRequest
	}
	existing, err := s.rules.GetByID(ctx, id)
	if err != nil {
		return nil, mapRuleStoreError(err)
	}
	// Server-assigned lifecycle fields stay server-assigned: the request
	// may carry client-echoed id/created_at, so restore both from the
	// existing row. Tenant columns are not wire-bindable (json:"-") and
	// the store's column-whitelisted Update never touches them.
	req.ID = existing.ID
	req.CreatedAt = existing.CreatedAt
	if err = s.rules.Update(ctx, req); err != nil {
		return nil, mapRuleStoreError(err)
	}
	// best-effort: rule updated; reload failure keeps the engine on
	// the previous rule set, so log it rather than failing the request.
	if err = s.reloadRules(ctx); err != nil {
		zap.L().Warn("alert rule engine reload failed after update",
			zap.Int64("rule_id", req.ID), zap.Error(err))
	}
	return req, nil
}

// DeleteRule deletes an alert rule by ID.
func (s *AlertService) DeleteRule(ctx context.Context, id int64) error {
	if err := s.rules.DeleteByID(ctx, id); err != nil {
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
	filter alert.RecordFilter,
) ([]*alert.Record, int64, error) {
	page, size = pagination.Clamp(page, size)
	records, total, err := s.records.List(ctx, page, size, filter)
	if err != nil {
		return nil, 0, mapRecordStoreError(err)
	}

	return records, total, nil
}

// GetRecord returns a single alert record by ID.
func (s *AlertService) GetRecord(ctx context.Context, id int64) (*alert.Record, error) {
	record, err := s.records.GetByID(ctx, id)
	if err != nil {
		return nil, mapRecordStoreError(err)
	}
	return record, nil
}

// AcknowledgeRecord transitions the alert record to "acknowledged" status.
func (s *AlertService) AcknowledgeRecord(ctx context.Context, id int64) (*alert.Record, error) {
	record, err := s.records.Acknowledge(ctx, id)
	if err != nil {
		return nil, mapRecordStoreError(err)
	}
	return record, nil
}

// ResolveRecord transitions the alert record to "resolved" status.
func (s *AlertService) ResolveRecord(ctx context.Context, id int64) (*alert.Record, error) {
	record, err := s.records.Resolve(ctx, id)
	if err != nil {
		return nil, mapRecordStoreError(err)
	}
	return record, nil
}

// reloadRules reloads the rule engine from the store when both are non-nil.
func (s *AlertService) reloadRules(ctx context.Context) error {
	if s.ruleEngine == nil || s.rules == nil {
		return nil
	}
	return s.ruleEngine.Reload(ctx, s.rules)
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
