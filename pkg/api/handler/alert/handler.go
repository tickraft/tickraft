// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"net/http"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// Handler exposes alert rule and record CRUD endpoints.
// It is injected via the WithAlertService RouteOption and registered on
// the /api/v1/prism/alert route group.
type Handler struct {
	svc Service
}

// NewHandler creates a new alert Handler backed by the given service.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

// ListAlertRules handles GET /api/v1/prism/alert/rules.
func (h *Handler) ListAlertRules(ctx context.Context, arc *app.RequestContext) {
	page, size := httputil.ParsePaging(arc)
	items, total, err := h.svc.ListRules(ctx, page, size)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.SuccessPage(arc, items, total, page, size)
}

// GetAlertRule handles GET /api/v1/prism/alert/rules/:id.
func (h *Handler) GetAlertRule(ctx context.Context, arc *app.RequestContext) {
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

// CreateAlertRule handles POST /api/v1/prism/alert/rules.
func (h *Handler) CreateAlertRule(ctx context.Context, arc *app.RequestContext) {
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
	if len(req.Description) > httputil.MaxDescriptionLength {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "description exceeds maximum length of 1024 characters")
		return
	}
	created, err := h.svc.CreateRule(ctx, &req)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, created)
}

// UpdateAlertRule handles PUT /api/v1/prism/alert/rules/:id.
func (h *Handler) UpdateAlertRule(ctx context.Context, arc *app.RequestContext) {
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
	if len(req.Description) > httputil.MaxDescriptionLength {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "description exceeds maximum length of 1024 characters")
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

// DeleteAlertRule handles DELETE /api/v1/prism/alert/rules/:id.
func (h *Handler) DeleteAlertRule(ctx context.Context, arc *app.RequestContext) {
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

// ListAlertRecords handles GET /api/v1/prism/alert/records. Supported query
// parameters: page, page_size, severity, status, from and to (RFC3339).
func (h *Handler) ListAlertRecords(ctx context.Context, arc *app.RequestContext) {
	page, size := httputil.ParsePaging(arc)
	filter := RecordFilter{
		Severity: arc.Query("severity"),
		Status:   arc.Query("status"),
	}
	if v := arc.Query("from"); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "invalid 'from' timestamp, expected RFC3339 format")
			return
		}
		filter.From = parsed
	}
	if v := arc.Query("to"); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "invalid 'to' timestamp, expected RFC3339 format")
			return
		}
		filter.To = parsed
	}
	items, total, err := h.svc.ListRecords(ctx, page, size, filter)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.SuccessPage(arc, items, total, page, size)
}

// GetAlertRecord handles GET /api/v1/prism/alert/records/:id.
func (h *Handler) GetAlertRecord(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	record, err := h.svc.GetRecord(ctx, id)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, record)
}

// AcknowledgeAlertRecord handles PUT /api/v1/prism/alert/records/:id/acknowledge.
func (h *Handler) AcknowledgeAlertRecord(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	record, err := h.svc.AcknowledgeRecord(ctx, id)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, record)
}

// ResolveAlertRecord handles PUT /api/v1/prism/alert/records/:id/resolve.
func (h *Handler) ResolveAlertRecord(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	record, err := h.svc.ResolveRecord(ctx, id)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, record)
}
