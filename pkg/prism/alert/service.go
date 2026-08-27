// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"errors"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/pagination"
)

// ReloadFunc hot-reloads the rule engine after a rule mutation. Returning
// an error only logs a warning: the rule is already persisted and the
// engine keeps its previous rule set. A nil ReloadFunc disables hot reload
// (mutations still persist; the engine falls back to its polling reload).
type ReloadFunc func(ctx context.Context) error

// AlertService implements Service using the prism rule engine
// and persistent rule/record stores. The wire shape and the storage shape
// are the same Rule / Record models, so this service only
// orchestrates stores and the engine reload — there is no DTO conversion.
// The <Domain>Service name mirrors the convention of the other domain
// service implementations (see pkg/system).
//
//nolint:revive // intentional stutter: mirrors the <Domain>Service convention
type AlertService struct {
	rules   *Store
	records RecordStore
	reload  ReloadFunc
}

var _ Service = (*AlertService)(nil)

// NewAlertService creates an AlertService backed by the given rule store,
// record store, and rule engine. A nil ruleEngine disables hot reload.
func NewAlertService(ruleStore *Store, recordStore RecordStore, ruleEngine *Engine) *AlertService {
	var reload ReloadFunc
	if ruleEngine != nil {
		reload = func(ctx context.Context) error { return ruleEngine.Reload(ctx, ruleStore) }
	}
	return NewAlertServiceFunc(ruleStore, recordStore, reload)
}

// NewAlertServiceFunc creates an AlertService with a custom reload hook.
// Extended editions use it to propagate rule changes through their own
// bus instead of reloading the in-process engine directly.
func NewAlertServiceFunc(ruleStore *Store, recordStore RecordStore, reload ReloadFunc) *AlertService {
	return &AlertService{
		rules:   ruleStore,
		records: recordStore,
		reload:  reload,
	}
}

// ListRules returns a page of alert rules and the total count.
func (s *AlertService) ListRules(ctx context.Context, page, size int) ([]*Rule, int64, error) {
	page, size = pagination.Clamp(page, size)
	rules, total, err := s.rules.List(ctx, page, size)
	if err != nil {
		return nil, 0, mapRuleStoreError(err)
	}

	return rules, total, nil
}

// GetRule returns a single alert rule by ID.
func (s *AlertService) GetRule(ctx context.Context, id int64) (*Rule, error) {
	m, err := s.rules.GetByID(ctx, id)
	if err != nil {
		return nil, mapRuleStoreError(err)
	}
	return m, nil
}

// CreateRule creates a new alert rule from the given request.
func (s *AlertService) CreateRule(ctx context.Context, req *Rule) (*Rule, error) {
	if req == nil {
		return nil, errdefs.ErrInvalidRequest
	}
	if req.Name == "" || req.Expression == "" {
		return nil, errdefs.ErrInvalidRequest
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
func (s *AlertService) UpdateRule(ctx context.Context, id int64, req *Rule) (*Rule, error) {
	if req == nil {
		return nil, errdefs.ErrInvalidRequest
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
	filter RecordFilter,
) ([]*Record, int64, error) {
	page, size = pagination.Clamp(page, size)
	records, total, err := s.records.List(ctx, page, size, filter)
	if err != nil {
		return nil, 0, mapRecordStoreError(err)
	}

	return records, total, nil
}

// GetRecord returns a single alert record by ID.
func (s *AlertService) GetRecord(ctx context.Context, id int64) (*Record, error) {
	record, err := s.records.GetByID(ctx, id)
	if err != nil {
		return nil, mapRecordStoreError(err)
	}
	return record, nil
}

// AcknowledgeRecord transitions the alert record to "acknowledged" status.
func (s *AlertService) AcknowledgeRecord(ctx context.Context, id int64) (*Record, error) {
	record, err := s.records.Acknowledge(ctx, id)
	if err != nil {
		return nil, mapRecordStoreError(err)
	}
	return record, nil
}

// ResolveRecord transitions the alert record to "resolved" status.
func (s *AlertService) ResolveRecord(ctx context.Context, id int64) (*Record, error) {
	record, err := s.records.Resolve(ctx, id)
	if err != nil {
		return nil, mapRecordStoreError(err)
	}
	return record, nil
}

// reloadRules invokes the reload hook when one is wired.
func (s *AlertService) reloadRules(ctx context.Context) error {
	if s.reload == nil || s.rules == nil {
		return nil
	}
	return s.reload(ctx)
}

// mapRuleStoreError translates a rule store error into a handler-level
// ServiceError suitable for the API response layer. Expression
// validation failures map to 400 with the innermost (expr-lang)
// diagnostic so the client can display the position-annotated message.
func mapRuleStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrRuleNotFound) {
		return errdefs.ErrRuleNotFound
	}
	if errors.Is(err, errdefs.ErrNotFound) {
		return errdefs.ErrRuleNotFound
	}
	if errors.Is(err, ErrRuleCompileFailed) {
		return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
			"invalid expression: "+errdefs.InnermostMessage(err))
	}
	return errdefs.NewServiceError(http.StatusInternalServerError, errdefs.CodeInternal, err.Error())
}

// mapRecordStoreError translates an alert record store error into a
// handler-level ServiceError suitable for the API response layer.
func mapRecordStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errdefs.ErrNotFound) {
		return errdefs.ErrRecordNotFound
	}
	return errdefs.NewServiceError(http.StatusInternalServerError, errdefs.CodeInternal, err.Error())
}
