// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package channel exposes the notification channel CRUD endpoints of the
// prism engine.
package channel

import (
	"context"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/prism/channel"
)

// Handler exposes notification channel CRUD endpoints.
// It is injected via the WithChannelService RouteOption and registered on
// the /api/v1/prism/channels route group.
type Handler struct {
	svc channel.Service
}

// NewHandler creates a new channel Handler backed by the given service.
func NewHandler(svc channel.Service) *Handler {
	return &Handler{svc: svc}
}

// ListChannels handles GET /api/v1/prism/channels.
func (h *Handler) ListChannels(ctx context.Context, arc *app.RequestContext) {
	page, size, ok := httputil.ParsePaging(arc)
	if !ok {
		return
	}
	items, total, err := h.svc.ListChannels(ctx, page, size)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.SuccessPage(arc, items, total, page, size)
}

// GetChannel handles GET /api/v1/prism/channels/:id.
func (h *Handler) GetChannel(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	ch, err := h.svc.GetChannel(ctx, id)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, ch)
}

// CreateChannel handles POST /api/v1/prism/channels.
func (h *Handler) CreateChannel(ctx context.Context, arc *app.RequestContext) {
	var req channel.Channel
	if !httputil.BindAndValidate(arc, &req) {
		return
	}
	if req.Name == "" {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "name is required")
		return
	}
	if len(req.Name) > httputil.MaxNameLength {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"name exceeds maximum length of 255 characters")
		return
	}
	if req.Type == "" {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "type is required")
		return
	}
	if req.Config == "" {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "config is required")
		return
	}
	created, err := h.svc.CreateChannel(ctx, &req)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, created)
}

// UpdateChannel handles PUT /api/v1/prism/channels/:id.
func (h *Handler) UpdateChannel(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	var req channel.Channel
	if !httputil.BindAndValidate(arc, &req) {
		return
	}
	if len(req.Name) > httputil.MaxNameLength {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"name exceeds maximum length of 255 characters")
		return
	}
	req.ID = id
	updated, err := h.svc.UpdateChannel(ctx, id, &req)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, updated)
}

// DeleteChannel handles DELETE /api/v1/prism/channels/:id.
func (h *Handler) DeleteChannel(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	if err := h.svc.DeleteChannel(ctx, id); err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, nil)
}

// TestChannel handles POST /api/v1/prism/channels/:id/test.
func (h *Handler) TestChannel(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	if err := h.svc.TestChannel(ctx, id); err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, nil)
}
