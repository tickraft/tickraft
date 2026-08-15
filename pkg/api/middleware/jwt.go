// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/auth/jwt"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// Authorizer checks whether a user holds a specific permission.
// Callers that need RBAC checks provide an implementation (e.g. backed
// by role-permission database tables).
//
// If no authorizer is provided to NewScopedJWTAuth, permission checks
// are skipped and the middleware operates in authentication-only mode.
type Authorizer interface {
	// Authorize returns true if the user identified by userID has been
	// granted the given permission code (e.g. "tenant:write").
	Authorize(ctx context.Context, userID int64, permission string) bool
}

// NewJWTAuth returns a Hertz middleware that validates JWT Bearer tokens using
// the provided JWT manager. If permission is non-empty, the middleware also
// checks that the authenticated user holds that permission via the default
// RBAC policy.
func NewJWTAuth(j *jwt.JWT, permission string) app.HandlerFunc {
	return func(ctx context.Context, arc *app.RequestContext) {
		authHeader := string(arc.GetHeader("Authorization"))
		if authHeader == "" {
			httputil.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeUnauthorized, "missing authorization header")
			arc.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			httputil.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeUnauthorized, "invalid authorization header format")
			arc.Abort()
			return
		}

		token := parts[1]
		claims, err := j.ValidateToken(token, auth.TokenTypeAccess)
		if err != nil {
			writeTokenError(arc, err)
			arc.Abort()
			return
		}

		// jwt.ValidateToken returns *jwt.UserClaims, which is the type
		// expected by httputil.SetUserClaims.
		httputil.SetUserClaims(arc, claims)

		// If a permission is specified, check it.
		if permission != "" {
			if !checkPermission(ctx, claims.Role, permission, "") {
				httputil.FailWithCode(arc, http.StatusForbidden, errdefs.CodeForbidden, "permission denied")
				arc.Abort()
				return
			}
		}

		arc.Next(ctx)
	}
}

// NewScopedJWTAuth returns a Hertz middleware that validates JWT Bearer
// tokens with support for region-based scope isolation and pluggable
// authorization. The middleware performs the following checks in order:
//
//  1. Extracts the Bearer token from the Authorization header.
//  2. Validates the JWT signature, expiry, and blacklist status.
//  3. If scope is non-empty, verifies that the token's region claim
//     equals scope; otherwise returns 403.
//  4. If permission is non-empty and authorizer is non-nil, checks that
//     the user holds the required permission; otherwise returns 403.
//  5. Writes the validated UserClaims to the request context for
//     downstream handlers.
//
// The scope parameter allows callers to restrict access to tokens issued
// for a specific region (e.g. "atlas"). An empty scope disables the
// region check entirely.
//
// Unlike NewJWTAuth which uses the built-in RBAC policy, this variant
// delegates permission checks to the provided Authorizer implementation,
// enabling external repositories to plug in their own authorization logic.
func NewScopedJWTAuth(
	j *jwt.JWT,
	scope string,
	permission string,
	authorizer Authorizer,
) app.HandlerFunc {
	return func(ctx context.Context, arc *app.RequestContext) {
		authHeader := string(arc.GetHeader("Authorization"))
		if authHeader == "" {
			httputil.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeUnauthorized, "missing authorization header")
			arc.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			httputil.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeUnauthorized, "invalid authorization header format")
			arc.Abort()
			return
		}

		token := parts[1]
		claims, err := j.ValidateToken(token, auth.TokenTypeAccess)
		if err != nil {
			writeTokenError(arc, err)
			arc.Abort()
			return
		}

		// Enforce scope isolation: when a scope is specified, only tokens
		// with a matching region claim are accepted.
		if scope != "" && claims.Region != scope {
			httputil.FailWithCode(arc, http.StatusForbidden, errdefs.CodeForbidden, "token scope mismatch")
			arc.Abort()
			return
		}

		// If a permission is required and an authorizer is configured,
		// perform the RBAC check. Fail-closed when the authorizer is set
		// but the check fails.
		if permission != "" && authorizer != nil {
			if !authorizer.Authorize(ctx, claims.UID, permission) {
				httputil.FailWithCode(arc, http.StatusForbidden, errdefs.CodeForbidden, "permission denied")
				arc.Abort()
				return
			}
		}

		httputil.SetUserClaims(arc, claims)
		arc.Next(ctx)
	}
}

// writeTokenError maps a token validation failure to the appropriate HTTP
// response. An expired token yields the dedicated CodeTokenExpired (40101)
// business code so clients can attempt a refresh-token rotation; every other
// failure (bad signature, wrong type, blacklisted) is a plain 40100.
func writeTokenError(arc *app.RequestContext, err error) {
	if errors.Is(err, jwt.ErrTokenExpired) {
		httputil.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeTokenExpired, "token expired")
		return
	}
	httputil.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeUnauthorized, "invalid or expired token")
}
