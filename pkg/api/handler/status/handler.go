// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package status exposes the public status page endpoint and the status
// configuration management endpoints.
package status

import (
	"context"
	"errors"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/status"
)

// Handler exposes the status page endpoints. The public endpoint is
// registered without authentication; the configuration endpoints are
// registered behind the JWT middleware with the wildcard permission.
type Handler struct {
	svc status.Service
}

// NewHandler creates a status Handler backed by the given service.
func NewHandler(svc status.Service) *Handler {
	return &Handler{svc: svc}
}

// failStatusError maps a status service error onto the HTTP response: a
// disabled page becomes 404 (indistinguishable from a missing route), a
// validation error becomes 400, everything else falls through to the
// generic errdefs mapping.
func failStatusError(arc *app.RequestContext, err error) {
	var ve *status.ErrValidation
	if errors.As(err, &ve) {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, ve.Msg)
		return
	}
	if errors.Is(err, status.ErrDisabled) {
		httputil.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "status page not found")
		return
	}
	httputil.Fail(arc, err)
}

// Public handles GET /api/v1/status. It is an intentionally public
// endpoint (no auth middleware): it serves the aggregated status view and
// returns 404 when the page is not enabled.
func (h *Handler) Public(ctx context.Context, arc *app.RequestContext) {
	view, err := h.svc.PublicView(ctx)
	if err != nil {
		failStatusError(arc, err)
		return
	}
	httputil.Success(arc, view)
}

// GetConfig handles GET /api/v1/status/config.
func (h *Handler) GetConfig(ctx context.Context, arc *app.RequestContext) {
	cfg, err := h.svc.GetConfig(ctx)
	if err != nil {
		failStatusError(arc, err)
		return
	}
	httputil.Success(arc, cfg)
}

// UpdateConfig handles PUT /api/v1/status/config.
func (h *Handler) UpdateConfig(ctx context.Context, arc *app.RequestContext) {
	var req status.Config
	if !httputil.BindAndValidate(arc, &req) {
		return
	}
	updated, err := h.svc.UpdateConfig(ctx, &req)
	if err != nil {
		failStatusError(arc, err)
		return
	}
	httputil.Success(arc, updated)
}
