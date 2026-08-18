// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package system exposes system configuration, runtime info, and
// user-profile endpoints.
package system

import (
	"context"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/api/handler/auth"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// Handler exposes system configuration, info, and user-profile endpoints.
// Profile endpoints (GetProfile/UpdateProfile) delegate to the injected
// auth.Service, making system a thin orchestration layer over auth.
type Handler struct {
	svc     Service
	authSvc auth.Service
}

// NewHandler creates a new system Handler backed by the given services.
// authSvc is required for the profile endpoints (GetProfile/UpdateProfile).
func NewHandler(svc Service, authSvc auth.Service) *Handler {
	return &Handler{svc: svc, authSvc: authSvc}
}

// GetSystemConfig handles GET /api/v1/system/config.
func (h *Handler) GetSystemConfig(ctx context.Context, arc *app.RequestContext) {
	config, err := h.svc.GetConfig(ctx)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, config)
}

// UpdateSystemConfig handles PUT /api/v1/system/config.
func (h *Handler) UpdateSystemConfig(ctx context.Context, arc *app.RequestContext) {
	var req Config
	if !api.BindAndValidate(arc, &req) {
		return
	}
	updated, err := h.svc.UpdateConfig(ctx, &req)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, updated)
}

// GetSystemInfo handles GET /api/v1/system/info.
func (h *Handler) GetSystemInfo(ctx context.Context, arc *app.RequestContext) {
	info, err := h.svc.GetInfo(ctx)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, info)
}

// GetGlobalStats handles GET /api/v1/system/stats.
func (h *Handler) GetGlobalStats(ctx context.Context, arc *app.RequestContext) {
	stats, err := h.svc.GetGlobalStats(ctx)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, stats)
}

// GetProfile handles GET /api/v1/system/profile.
func (h *Handler) GetProfile(ctx context.Context, arc *app.RequestContext) {
	claims, ok := api.GetUserClaims(arc)
	if !ok || claims == nil {
		api.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeUnauthorized, "unauthorized")
		return
	}
	profile, err := h.authSvc.GetProfile(ctx, claims.UID)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, profile)
}

// UpdateProfile handles PUT /api/v1/system/profile.
func (h *Handler) UpdateProfile(ctx context.Context, arc *app.RequestContext) {
	claims, ok := api.GetUserClaims(arc)
	if !ok || claims == nil {
		api.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeUnauthorized, "unauthorized")
		return
	}
	var req auth.UpdateProfileRequest
	if !api.BindAndValidate(arc, &req) {
		return
	}
	profile, err := h.authSvc.UpdateProfile(ctx, claims.UID, &req)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, profile)
}
