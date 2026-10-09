// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package contact exposes the notification-only contact directory CRUD
// endpoints.
package contact

import (
	"context"
	"errors"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/contact"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// Handler exposes the contact directory endpoints. It is injected via the
// WithContactService RouteOption and registered on the /api/v1/contacts
// route group. Audit logging lives in the service (see
// contact.WithLogger), mirroring the channel domain.
type Handler struct {
	svc contact.Service
}

// NewHandler creates a new contact Handler backed by the given service.
func NewHandler(svc contact.Service) *Handler {
	return &Handler{svc: svc}
}

// failContactError maps a contact service error onto the HTTP response:
// validation errors become 400, a quota rejection becomes 409 with the
// conflict code, a missing contact becomes 404, everything else falls
// through to the generic errdefs mapping.
func (h *Handler) failContactError(arc *app.RequestContext, err error) {
	var ve *contact.ErrValidation
	if errors.As(err, &ve) {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, ve.Msg)
		return
	}
	if errors.Is(err, contact.ErrQuotaExceeded) {
		httputil.FailWithCode(arc, http.StatusConflict, errdefs.CodeConflict, "quota exceeded")
		return
	}
	if errors.Is(err, contact.ErrNotFound) {
		httputil.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "contact not found")
		return
	}
	if errors.Is(err, contact.ErrTenantRequired) {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"tenant context required")
		return
	}
	httputil.Fail(arc, err)
}

// ListContacts handles GET /api/v1/contacts. Supported query parameters:
// page, size, keyword (substring match on name/email/phone).
func (h *Handler) ListContacts(ctx context.Context, arc *app.RequestContext) {
	ctx = httputil.ScopeTenantContext(ctx, arc)
	page, size, ok := httputil.ParsePaging(arc)
	if !ok {
		return
	}
	filter := contact.ListFilter{
		Keyword: arc.Query("keyword"),
		Page:    page,
		Size:    size,
	}
	items, total, err := h.svc.ListContacts(ctx, filter)
	if err != nil {
		h.failContactError(arc, err)
		return
	}
	httputil.SuccessPage(arc, items, total, page, size)
}

// GetContact handles GET /api/v1/contacts/:id.
func (h *Handler) GetContact(ctx context.Context, arc *app.RequestContext) {
	ctx = httputil.ScopeTenantContext(ctx, arc)
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	c, err := h.svc.GetContact(ctx, id)
	if err != nil {
		h.failContactError(arc, err)
		return
	}
	httputil.Success(arc, c)
}

// CreateContact handles POST /api/v1/contacts.
func (h *Handler) CreateContact(ctx context.Context, arc *app.RequestContext) {
	ctx = httputil.ScopeTenantContext(ctx, arc)
	var req contact.CreateRequest
	if !httputil.BindAndValidate(arc, &req) {
		return
	}
	created, err := h.svc.CreateContact(ctx, &req)
	if err != nil {
		h.failContactError(arc, err)
		return
	}
	httputil.Success(arc, created)
}

// UpdateContact handles PUT /api/v1/contacts/:id.
func (h *Handler) UpdateContact(ctx context.Context, arc *app.RequestContext) {
	ctx = httputil.ScopeTenantContext(ctx, arc)
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	var req contact.UpdateRequest
	if !httputil.BindAndValidate(arc, &req) {
		return
	}
	updated, err := h.svc.UpdateContact(ctx, id, &req)
	if err != nil {
		h.failContactError(arc, err)
		return
	}
	httputil.Success(arc, updated)
}

// DeleteContact handles DELETE /api/v1/contacts/:id.
func (h *Handler) DeleteContact(ctx context.Context, arc *app.RequestContext) {
	ctx = httputil.ScopeTenantContext(ctx, arc)
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	if err := h.svc.DeleteContact(ctx, id); err != nil {
		h.failContactError(arc, err)
		return
	}
	httputil.Success(arc, nil)
}
