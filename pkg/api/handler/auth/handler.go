// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package auth exposes the authentication and API-key management
// endpoints.
package auth

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// Handler exposes authentication and API-key management endpoints.
// It is injected via the WithAuthService RouteOption and registered on
// the /api/v1/auth route group.
type Handler struct {
	svc Service
}

// NewHandler creates a new auth Handler backed by the given service.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

// loginRequest is the request body for the login endpoint.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// refreshRequest is the request body for the refresh endpoint.
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// logoutRequest is the optional request body for the logout endpoint.
// The client may include the refresh token so it can be revoked server-side.
type logoutRequest struct {
	RefreshToken string `json:"refresh_token,omitempty"`
}

// changePasswordRequest is the request body for the change-password endpoint.
type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// createAPIKeyRequest is the request body for the create-apikey endpoint.
type createAPIKeyRequest struct {
	Name      string     `json:"name"`
	ExpiredAt *time.Time `json:"expired_at,omitempty"`
}

// apiKeyData is the response data for the create-apikey endpoint.
type apiKeyData struct {
	RawKey   string     `json:"raw_key"`
	ID       int64      `json:"id"`
	Name     string     `json:"name"`
	Prefix   string     `json:"key_prefix"`
	Status   int        `json:"status"`
	CreateAt time.Time  `json:"created_at"`
	ExpireAt *time.Time `json:"expired_at,omitempty"`
}

// Login handles POST /api/v1/auth/login.
func (h *Handler) Login(ctx context.Context, c *app.RequestContext) {
	var req loginRequest
	if !httputil.BindAndValidate(c, &req) {
		return
	}

	if req.Username == "" || req.Password == "" {
		httputil.FailWithCode(c, 400, errdefs.CodeBadRequest, "username and password are required")
		return
	}

	tokenPair, err := h.svc.Login(ctx, req.Username, req.Password)
	if err != nil {
		httputil.Fail(c, err)
		return
	}

	httputil.Success(c, tokenPair)
}

// Logout handles POST /api/v1/auth/logout.
func (h *Handler) Logout(ctx context.Context, c *app.RequestContext) {
	claims, ok := httputil.GetUserClaims(c)
	if !ok || claims == nil {
		httputil.FailWithCode(c, 401, errdefs.CodeUnauthorized, "unauthorized")
		return
	}

	// The request body is optional: the client may send the refresh token
	// for server-side revocation. A missing body must not fail the logout,
	// so the bind runs best-effort — BindAndValidate's hard 400 only suits
	// required-body endpoints.
	var req logoutRequest
	_ = c.Bind(&req)

	if err := h.svc.Logout(ctx, claims.JTI, claims.ExpiresAt, req.RefreshToken); err != nil {
		httputil.Fail(c, err)
		return
	}

	httputil.Success(c, nil)
}

// Refresh handles POST /api/v1/auth/refresh.
func (h *Handler) Refresh(ctx context.Context, c *app.RequestContext) {
	var req refreshRequest
	if !httputil.BindAndValidate(c, &req) {
		return
	}

	if req.RefreshToken == "" {
		httputil.FailWithCode(c, 400, errdefs.CodeBadRequest, "refresh_token is required")
		return
	}

	tokenPair, err := h.svc.RefreshToken(ctx, req.RefreshToken)
	if err != nil {
		httputil.Fail(c, err)
		return
	}

	// Refresh responses carry only the token pair; the remaining TokenPair
	// fields are omitempty and zero, so the wire shape is unchanged.
	httputil.Success(c, tokenPair)
}

// ChangePassword handles PUT /api/v1/auth/password.
func (h *Handler) ChangePassword(ctx context.Context, c *app.RequestContext) {
	claims, ok := httputil.GetUserClaims(c)
	if !ok || claims == nil {
		httputil.FailWithCode(c, 401, errdefs.CodeUnauthorized, "unauthorized")
		return
	}

	var req changePasswordRequest
	if !httputil.BindAndValidate(c, &req) {
		return
	}

	if req.OldPassword == "" || req.NewPassword == "" {
		httputil.FailWithCode(c, 400, errdefs.CodeBadRequest, "old_password and new_password are required")
		return
	}

	if err := h.svc.ChangePassword(ctx, claims.UID, req.OldPassword, req.NewPassword, claims.JTI); err != nil {
		httputil.Fail(c, err)
		return
	}

	httputil.Success(c, nil)
}

// CreateAPIKey handles POST /api/v1/auth/apikeys. Only administrators may
// create API keys; the route also carries an admin-level RequirePermission
// middleware, this check defends in depth for other mount points.
func (h *Handler) CreateAPIKey(ctx context.Context, c *app.RequestContext) {
	claims, ok := httputil.GetUserClaims(c)
	if !ok || claims == nil {
		httputil.FailWithCode(c, 401, errdefs.CodeUnauthorized, "unauthorized")
		return
	}
	if claims.Role < auth.RoleAdmin {
		httputil.FailWithCode(c, 403, errdefs.CodeForbidden, "admin role required")
		return
	}

	var req createAPIKeyRequest
	if !httputil.BindAndValidate(c, &req) {
		return
	}

	if req.Name == "" {
		httputil.FailWithCode(c, 400, errdefs.CodeBadRequest, "name is required")
		return
	}
	if len(req.Name) > 100 {
		httputil.FailWithCode(c, 400, errdefs.CodeBadRequest, "name must be 100 characters or less")
		return
	}

	rawKey, info, err := h.svc.CreateAPIKey(ctx, req.Name, req.ExpiredAt)
	if err != nil {
		httputil.Fail(c, err)
		return
	}

	httputil.Success(c, apiKeyData{
		RawKey:   rawKey,
		ID:       info.ID,
		Name:     info.Name,
		Prefix:   info.KeyPrefix,
		Status:   info.Status,
		CreateAt: info.CreatedAt,
		ExpireAt: info.ExpiredAt,
	})
}

// ListAPIKeys handles GET /api/v1/auth/apikeys. Supported query parameters:
// page, size and keyword (substring match on name).
func (h *Handler) ListAPIKeys(ctx context.Context, c *app.RequestContext) {
	page, size, ok := httputil.ParsePaging(c)
	if !ok {
		return
	}
	keys, total, err := h.svc.ListAPIKeys(ctx, page, size, c.Query("keyword"))
	if err != nil {
		httputil.Fail(c, err)
		return
	}

	httputil.SuccessPage(c, keys, total, page, size)
}

// RevokeAPIKey handles DELETE /api/v1/auth/apikeys/:id.
func (h *Handler) RevokeAPIKey(ctx context.Context, c *app.RequestContext) {
	id, ok := httputil.ParseID(c)
	if !ok {
		return
	}

	if err := h.svc.RevokeAPIKey(ctx, id); err != nil {
		httputil.Fail(c, err)
		return
	}

	httputil.Success(c, nil)
}
