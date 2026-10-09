// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package remediation

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/quota"
	"github.com/tickraft/tickraft/pkg/types"
)

// RemediationService implements Service using the
// prism remediation store. The wire shape and the storage shape are the
// same Rule / Record models, so this service only validates, enforces
// quotas, and orchestrates the store — there is no DTO conversion.
// The <Domain>Service name mirrors the convention of the other domain
// service implementations (see pkg/system).
//
//nolint:revive // intentional stutter: mirrors the <Domain>Service convention
type RemediationService struct {
	rules *Store
}

var _ Service = (*RemediationService)(nil)

// NewRemediationService creates a RemediationService backed by the given
// store.
func NewRemediationService(store *Store) *RemediationService {
	return &RemediationService{rules: store}
}

// ListRules returns a page of remediation rules and the total count.
func (s *RemediationService) ListRules(ctx context.Context, page, size int) ([]*Rule, int64, error) {
	page, size = pagination.Clamp(page, size)
	rules, total, err := s.rules.List(ctx, page, size)
	if err != nil {
		return nil, 0, mapRemediationStoreError(err)
	}

	return rules, total, nil
}

// GetRule returns a single remediation rule by ID.
func (s *RemediationService) GetRule(ctx context.Context, id int64) (*Rule, error) {
	m, err := s.rules.GetByID(ctx, id)
	if err != nil {
		return nil, mapRemediationStoreError(err)
	}
	return m, nil
}

// UpdateRule updates an existing remediation rule identified by ID.
func (s *RemediationService) UpdateRule(
	ctx context.Context,
	id int64,
	req *Rule,
) (*Rule, error) {
	if req == nil {
		return nil, errdefs.ErrInvalidRequest
	}
	if err := validateRule(req); err != nil {
		return nil, err
	}
	existing, err := s.rules.GetByID(ctx, id)
	if err != nil {
		return nil, mapRemediationStoreError(err)
	}
	// Runtime state (status, last-run, circuit-breaker counter) and
	// server-assigned lifecycle fields stay server-owned: the store's
	// column-whitelisted Update never writes them, and restoring them
	// here keeps the echoed response truthful. Tenant, metadata, and
	// soft-delete columns are not wire-bindable (json:"-").
	req.ID = existing.ID
	req.CreatedAt = existing.CreatedAt
	req.Status = existing.Status
	req.LastRunAt = existing.LastRunAt
	req.ConsecutiveFailures = existing.ConsecutiveFailures
	if err := s.rules.Update(ctx, req); err != nil {
		return nil, mapRemediationStoreError(err)
	}
	return req, nil
}

// DeleteRule deletes a remediation rule by ID.
func (s *RemediationService) DeleteRule(ctx context.Context, id int64) error {
	if err := s.rules.DeleteByID(ctx, id); err != nil {
		return mapRemediationStoreError(err)
	}
	return nil
}

// ListRecords returns a page of remediation dispatch records and the total
// count, optionally filtered by lifecycle status.
func (s *RemediationService) ListRecords(
	ctx context.Context,
	page, size int,
	status string,
) ([]*Record, int64, error) {
	page, size = pagination.Clamp(page, size)
	records, total, err := s.rules.ListRecords(ctx, page, size, status)
	if err != nil {
		return nil, 0, mapRemediationStoreError(err)
	}

	return records, total, nil
}

// validTriggerEventTypes is the closed set of trigger event types accepted
// by the remediation rule API. They map 1:1 to the event types the
// remediation engine subscribes to.
var validTriggerEventTypes = map[string]struct{}{
	string(TriggerMetric):       {},
	string(TriggerLog):          {},
	string(TriggerStatusChange): {},
}

// validExecutorTypes is the closed set of executor types accepted by the
// remediation rule API. They must match the operator names registered with
// the remediation engine (local, webhook, http).
var validExecutorTypes = map[string]struct{}{
	string(types.ExecutorLocal):   {},
	string(types.ExecutorWebhook): {},
	string(types.ExecutorHTTP):    {},
}

// validateRule checks the closed-set fields of a remediation rule request
// and pre-compiles both expression surfaces so a bad expression is rejected
// with a 400 at the entry point rather than silently never matching at
// runtime (fix for D-06):
//
//  1. the trigger condition `expression` is compiled and sample-evaluated
//     against the RemediationEnv contract;
//  2. the optional "expression" key inside `executor_config` JSON is
//     compiled and sample-evaluated against the executor's ExecutionEnv
//     contract.
func validateRule(r *Rule) error {
	if _, ok := validTriggerEventTypes[r.TriggerEventType]; !ok {
		return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
			"triggerEventType must be one of: metric, log, status_change")
	}
	if _, ok := validExecutorTypes[r.ExecutorType]; !ok {
		return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
			"executorType must be one of: local, webhook, http")
	}
	if r.Cooldown < 0 {
		return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
			"cooldown must be non-negative")
	}
	if r.CircuitBreakerThreshold < 0 {
		return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
			"circuitBreakerThreshold must be non-negative")
	}
	if r.Expression != "" {
		if err := ValidateExpression(r.Expression); err != nil {
			return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
				"invalid expression: "+errdefs.InnermostMessage(err))
		}
	}
	if exprStr := executor.ConfigExpression(r.ExecutorConfig); exprStr != "" {
		if err := executor.ValidateExpression(exprStr); err != nil {
			return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
				"invalid executor judgment expression: "+errdefs.InnermostMessage(err))
		}
	}
	return nil
}

// CreateRule creates a new remediation rule from the given request.
func (s *RemediationService) CreateRule(
	ctx context.Context,
	req *Rule,
) (*Rule, error) {
	if req == nil {
		return nil, errdefs.ErrInvalidRequest
	}
	if req.Name == "" || req.TriggerEventType == "" || req.ExecutorType == "" {
		return nil, errdefs.ErrInvalidRequest
	}
	if err := validateRule(req); err != nil {
		return nil, err
	}
	// Enforce the remediation rule count quota before inserting.
	if ceiling := quota.Ceiling(quota.TypeRemediation); ceiling > 0 {
		_, total, err := s.rules.List(ctx, 1, 1)
		if err != nil {
			return nil, mapRemediationStoreError(err)
		}
		if total >= int64(ceiling) {
			return nil, errdefs.NewServiceError(
				http.StatusConflict, errdefs.CodeConflict,
				fmt.Sprintf("remediation rule quota exceeded: maximum %d rules", ceiling),
			)
		}
	}
	// Runtime state is server-owned from birth: clear any client-bound
	// values so the store's insert applies the column defaults (status
	// 'active', zero breaker count, no last-run).
	req.Status = ""
	req.LastRunAt = nil
	req.ConsecutiveFailures = 0
	if err := s.rules.Create(ctx, req); err != nil {
		return nil, mapRemediationStoreError(err)
	}
	return req, nil
}

// mapRemediationStoreError translates a remediation store error into a
// handler-level ServiceError suitable for the API response layer.
func mapRemediationStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrRuleNotFound) {
		return errdefs.ErrRemediationRuleNotFound
	}
	if errors.Is(err, errdefs.ErrNotFound) {
		return errdefs.ErrRemediationRuleNotFound
	}
	return errdefs.NewServiceError(http.StatusInternalServerError, errdefs.CodeInternal, err.Error())
}
