// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package prism

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/tickraft/tickraft/pkg/api/handler"
	"github.com/tickraft/tickraft/pkg/api/handler/remediation"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
	prismremediation "github.com/tickraft/tickraft/pkg/prism/remediation"
	"github.com/tickraft/tickraft/pkg/quota"
)

// RemediationService implements remediation.Service using the
// prism remediation store.
type RemediationService struct {
	store *prismremediation.Store
}

// NewRemediationService creates a RemediationService backed by the given
// store.
func NewRemediationService(store *prismremediation.Store) *RemediationService {
	return &RemediationService{store: store}
}

// ListRules returns a page of remediation rules and the total count.
func (s *RemediationService) ListRules(ctx context.Context, page, size int) ([]remediation.Rule, int64, error) {
	page, size = httputil.ClampPaging(page, size)
	models, total, err := s.store.List(ctx, page, size)
	if err != nil {
		return nil, 0, mapRemediationStoreError(err)
	}
	rules := make([]remediation.Rule, 0, len(models))
	for _, m := range models {
		rules = append(rules, remediationModelToHandler(m))
	}
	return rules, total, nil
}

// GetRule returns a single remediation rule by ID.
func (s *RemediationService) GetRule(ctx context.Context, id int64) (*remediation.Rule, error) {
	m, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, mapRemediationStoreError(err)
	}
	h := remediationModelToHandler(m)
	return &h, nil
}

// UpdateRule updates an existing remediation rule identified by ID.
func (s *RemediationService) UpdateRule(ctx context.Context, id int64, req *remediation.Rule) (*remediation.Rule, error) {
	if req == nil {
		return nil, handler.ErrInvalidRequest
	}
	if err := validateRule(req); err != nil {
		return nil, err
	}
	existing, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, mapRemediationStoreError(err)
	}
	m := remediationHandlerToModel(req)
	m.ID = existing.ID
	m.CreatedAt = existing.CreatedAt
	if err := s.store.Update(ctx, m); err != nil {
		return nil, mapRemediationStoreError(err)
	}
	h := remediationModelToHandler(m)
	return &h, nil
}

// DeleteRule deletes a remediation rule by ID.
func (s *RemediationService) DeleteRule(ctx context.Context, id int64) error {
	if err := s.store.DeleteByID(ctx, id); err != nil {
		return mapRemediationStoreError(err)
	}
	return nil
}

// ListRecords returns a page of remediation dispatch records and the total
// count, optionally filtered by lifecycle status.
func (s *RemediationService) ListRecords(ctx context.Context, page, size int, status string) ([]remediation.Record, int64, error) {
	page, size = httputil.ClampPaging(page, size)
	models, total, err := s.store.ListRecords(ctx, size, (page-1)*size, status)
	if err != nil {
		return nil, 0, mapRemediationStoreError(err)
	}
	records := make([]remediation.Record, 0, len(models))
	for _, m := range models {
		records = append(records, remediationRecordToHandler(m))
	}
	return records, total, nil
}

// validTriggerEventTypes is the closed set of trigger event types accepted
// by the remediation rule API. They map 1:1 to the event types the
// remediation engine subscribes to.
var validTriggerEventTypes = map[string]struct{}{
	string(prismremediation.TriggerMetric):       {},
	string(prismremediation.TriggerLog):          {},
	string(prismremediation.TriggerStatusChange): {},
}

// validExecutorTypes is the closed set of executor types accepted by the
// remediation rule API. They must match the operator names registered with
// the remediation engine (local, webhook, http).
var validExecutorTypes = map[string]struct{}{
	"local":   {},
	"webhook": {},
	"http":    {},
}

// validateRule checks the closed-set fields of a remediation rule request.
func validateRule(r *remediation.Rule) error {
	if _, ok := validTriggerEventTypes[r.TriggerEventType]; !ok {
		return handler.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
			"triggerEventType must be one of: metric, log, status_change")
	}
	if _, ok := validExecutorTypes[r.ExecutorType]; !ok {
		return handler.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
			"executorType must be one of: local, webhook, http")
	}
	if r.Cooldown < 0 {
		return handler.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
			"cooldown must be non-negative")
	}
	if r.CircuitBreakerThreshold < 0 {
		return handler.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
			"circuitBreakerThreshold must be non-negative")
	}
	return nil
}

// CreateRule creates a new remediation rule from the given request.
func (s *RemediationService) CreateRule(ctx context.Context, req *remediation.Rule) (*remediation.Rule, error) {
	if req == nil {
		return nil, handler.ErrInvalidRequest
	}
	if req.Name == "" || req.TriggerEventType == "" || req.ExecutorType == "" {
		return nil, handler.ErrInvalidRequest
	}
	if err := validateRule(req); err != nil {
		return nil, err
	}
	// Enforce the remediation rule count quota before inserting.
	if ceiling := quota.Ceiling(quota.TypeRemediation); ceiling > 0 {
		_, total, err := s.store.List(ctx, 1, 1)
		if err != nil {
			return nil, mapRemediationStoreError(err)
		}
		if total >= int64(ceiling) {
			return nil, handler.NewServiceError(
				http.StatusConflict, errdefs.CodeConflict,
				fmt.Sprintf("remediation rule quota exceeded: maximum %d rules", ceiling),
			)
		}
	}
	m := remediationHandlerToModel(req)
	if err := s.store.Create(ctx, m); err != nil {
		return nil, mapRemediationStoreError(err)
	}
	h := remediationModelToHandler(m)
	return &h, nil
}

// remediationRecordToHandler converts a prismremediation.Record persistence
// model into the handler-layer Record DTO.
func remediationRecordToHandler(m *prismremediation.Record) remediation.Record {
	return remediation.Record{
		ID:         m.ID,
		RuleID:     m.RuleID,
		RuleName:   m.RuleName,
		AssetID:    m.AssetID,
		AssetKey:   m.AssetKey,
		RunID:      m.RunID,
		Trigger:    m.Trigger,
		Status:     m.Status,
		Error:      m.Error,
		StartedAt:  m.StartedAt,
		FinishedAt: m.FinishedAt,
		CreatedAt:  m.CreatedAt,
	}
}

// remediationModelToHandler converts a prismremediation.Rule persistence model
// into the handler-layer Rule DTO.
func remediationModelToHandler(m *prismremediation.Rule) remediation.Rule {
	return remediation.Rule{
		ID:                      m.ID,
		Name:                    m.Name,
		Description:             m.Description,
		AssetID:                 m.AssetID,
		TriggerEventType:        m.TriggerEventType,
		ConditionExpr:           m.ConditionExpr,
		ExecutorType:            m.ExecutorType,
		ExecutorConfig:          m.ExecutorConfig,
		Cooldown:                m.Cooldown,
		CircuitBreakerThreshold: m.CircuitBreakerThreshold,
		Enabled:                 m.Enabled,
		Status:                  m.Status,
		LastRunAt:               m.LastRunAt,
		CreatedAt:               m.CreatedAt,
		UpdatedAt:               m.UpdatedAt,
	}
}

// remediationHandlerToModel converts a handler-layer Rule DTO into
// a prismremediation.Rule persistence model ready for Create/Update.
func remediationHandlerToModel(r *remediation.Rule) *prismremediation.Rule {
	return &prismremediation.Rule{
		Name:                    r.Name,
		Description:             r.Description,
		AssetID:                 r.AssetID,
		TriggerEventType:        r.TriggerEventType,
		ConditionExpr:           r.ConditionExpr,
		ExecutorType:            r.ExecutorType,
		ExecutorConfig:          r.ExecutorConfig,
		Cooldown:                r.Cooldown,
		CircuitBreakerThreshold: r.CircuitBreakerThreshold,
		Enabled:                 r.Enabled,
	}
}

// mapRemediationStoreError translates a remediation store error into a
// handler-level ServiceError suitable for the API response layer.
func mapRemediationStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, prismremediation.ErrRuleNotFound) {
		return handler.ErrRemediationRuleNotFound
	}
	if errors.Is(err, errdefs.ErrNotFound) {
		return handler.ErrRemediationRuleNotFound
	}
	return handler.NewServiceError(http.StatusInternalServerError, errdefs.CodeInternal, err.Error())
}
