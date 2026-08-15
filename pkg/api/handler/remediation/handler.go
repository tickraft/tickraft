// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package remediation

import (
	"context"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// Handler exposes remediation rule CRUD endpoints.
// It is injected via the WithRemediationRuleService RouteOption and
// registered on the /api/v1/prism/remediation/rules route group.
type Handler struct {
	svc Service
}

// NewHandler creates a new remediation Handler backed by the given service.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

// ListRemediationRules handles GET /api/v1/prism/remediation/rules.
func (h *Handler) ListRemediationRules(ctx context.Context, arc *app.RequestContext) {
	page, size := httputil.ParsePaging(arc)
	items, total, err := h.svc.ListRules(ctx, page, size)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.SuccessPage(arc, items, total, page, size)
}

// GetRemediationRule handles GET /api/v1/prism/remediation/rules/:id.
func (h *Handler) GetRemediationRule(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	rule, err := h.svc.GetRule(ctx, id)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, rule)
}

// CreateRemediationRule handles POST /api/v1/prism/remediation/rules.
func (h *Handler) CreateRemediationRule(ctx context.Context, arc *app.RequestContext) {
	var req Rule
	if !api.BindAndValidate(arc, &req) {
		return
	}
	if req.Name == "" {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "name is required")
		return
	}
	if len(req.Name) > httputil.MaxNameLength {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "name exceeds maximum length of 255 characters")
		return
	}
	if req.TriggerEventType == "" {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "triggerEventType is required")
		return
	}
	if req.ExecutorType == "" {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "executorType is required")
		return
	}
	created, err := h.svc.CreateRule(ctx, &req)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, created)
}

// UpdateRemediationRule handles PUT /api/v1/prism/remediation/rules/:id.
func (h *Handler) UpdateRemediationRule(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	var req Rule
	if !api.BindAndValidate(arc, &req) {
		return
	}
	if len(req.Name) > httputil.MaxNameLength {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "name exceeds maximum length of 255 characters")
		return
	}
	req.ID = id
	updated, err := h.svc.UpdateRule(ctx, id, &req)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, updated)
}

// DeleteRemediationRule handles DELETE /api/v1/prism/remediation/rules/:id.
func (h *Handler) DeleteRemediationRule(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	if err := h.svc.DeleteRule(ctx, id); err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, nil)
}

// ListRemediationRecords handles GET /api/v1/prism/remediation/records.
// Supported query parameters: page, page_size, status (lifecycle status
// filter: triggered/started/completed/skipped/failed).
func (h *Handler) ListRemediationRecords(ctx context.Context, arc *app.RequestContext) {
	page, size := httputil.ParsePaging(arc)
	status := string(arc.Query("status"))
	items, total, err := h.svc.ListRecords(ctx, page, size, status)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.SuccessPage(arc, items, total, page, size)
}
