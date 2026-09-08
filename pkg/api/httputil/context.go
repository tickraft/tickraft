// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package httputil provides shared helpers for API handlers: request-context
// accessors for request IDs, auth principals, regions, and locales; request
// binding and validation; and the unified response envelope with paging
// helpers.
package httputil

import (
	"context"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/auth/jwt"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/user"
)

// Context key constants for storing data in Hertz request context.
const (
	requestIDKey  = "api.request_id"
	userClaimsKey = "api.user_claims"
	apiKeyIDKey   = "api.api_key_id" //nolint:gosec // context key name, not a credential
	clientIPKey   = "api.client_ip"
)

// GetRequestID returns the request ID from context.
func GetRequestID(arc *app.RequestContext) string {
	val, _ := arc.Get(requestIDKey)
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}

// SetRequestID stores the request ID in context.
func SetRequestID(arc *app.RequestContext, id string) {
	arc.Set(requestIDKey, id)
}

// SetUserClaims stores the authenticated user claims in the Hertz request context.
func SetUserClaims(arc *app.RequestContext, claims *jwt.UserClaims) {
	arc.Set(userClaimsKey, claims)
}

// GetUserClaims retrieves the authenticated user claims from the Hertz request context.
func GetUserClaims(arc *app.RequestContext) (*jwt.UserClaims, bool) {
	val, _ := arc.Get(userClaimsKey)
	if claims, ok := val.(*jwt.UserClaims); ok {
		return claims, true
	}
	return nil, false
}

// SetAPIKeyID stores the API key ID in the Hertz request context.
func SetAPIKeyID(arc *app.RequestContext, keyID int64) {
	arc.Set(apiKeyIDKey, keyID)
}

// GetAPIKeyID retrieves the API key ID from the Hertz request context.
func GetAPIKeyID(arc *app.RequestContext) (int64, bool) {
	val, _ := arc.Get(apiKeyIDKey)
	if keyID, ok := val.(int64); ok {
		return keyID, true
	}
	return 0, false
}

// BindAndValidate binds request parameters to obj and validates.
// Returns true on success; on failure, writes a 400 error response automatically.
func BindAndValidate(arc *app.RequestContext, obj any) bool {
	if err := arc.Bind(obj); err != nil {
		FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "invalid request parameters")
		return false
	}
	if err := arc.Validate(obj); err != nil {
		FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, err.Error())
		return false
	}
	return true
}

// SetClientIP stores the resolved real client IP in the request context.
// It is intended for use by the trusted-proxy middleware so downstream
// handlers and loggers read a single, authoritative client IP through
// GetClientIP regardless of proxy headers.
func SetClientIP(arc *app.RequestContext, ip string) {
	arc.Set(clientIPKey, ip)
}

// GetClientIP returns the client's real IP.
//
// Resolution order:
//  1. A value previously stored via SetClientIP (e.g. by the
//     trusted-proxy middleware). This takes precedence so that explicit
//     proxy-aware resolution wins over raw header inspection.
//  2. RemoteAddr.
//
// X-Forwarded-For and X-Real-IP headers are intentionally NOT trusted
// directly: they can be spoofed by clients. Operators who run behind a
// reverse proxy must configure TrustedProxies so the trusted-proxy
// middleware can securely resolve the real client IP.
func GetClientIP(arc *app.RequestContext) string {
	// 1. Authoritative value set by trusted-proxy middleware.
	if val, ok := arc.Get(clientIPKey); ok {
		if s, ok := val.(string); ok && s != "" {
			return s
		}
	}
	// 2. Fall back to RemoteAddr
	return arc.RemoteAddr().String()
}

// regionCtxKey is a context key for storing the resolved routing region.
type regionCtxKey struct{}

// SetRegion stores the resolved region in the request context.
func SetRegion(ctx context.Context, region string) context.Context {
	return context.WithValue(ctx, regionCtxKey{}, region)
}

// GetRegion retrieves the resolved region from the request context.
// Returns an empty string if no region has been set.
func GetRegion(ctx context.Context) string {
	val := ctx.Value(regionCtxKey{})
	if region, ok := val.(string); ok {
		return region
	}
	return ""
}

// localeCtxKey is a context key for storing the request locale parsed from
// the X-Tickraft-Locale header by the locale middleware.
type localeCtxKey struct{}

// SetLocale stores the parsed locale in the request context.
func SetLocale(ctx context.Context, locale i18n.Locale) context.Context {
	return context.WithValue(ctx, localeCtxKey{}, locale)
}

// GetLocale retrieves the locale from the request context. Returns the
// default locale (zh-Hans) if no locale has been set, ensuring callers always
// receive a usable Locale value.
func GetLocale(ctx context.Context) i18n.Locale {
	val := ctx.Value(localeCtxKey{})
	if loc, ok := val.(i18n.Locale); ok {
		return loc
	}
	return i18n.Parse(i18n.DefaultLocale)
}

const userKey = "auth.user"

// SetUser stores the authenticated user in the Hertz request context.
func SetUser(arc *app.RequestContext, u *user.User) {
	arc.Set(userKey, u)
}

// GetUser retrieves the authenticated user from the Hertz request context.
// Returns nil if no user is set.
func GetUser(arc *app.RequestContext) *user.User {
	val, _ := arc.Get(userKey)
	if u, ok := val.(*user.User); ok {
		return u
	}
	return nil
}

// tenantCtxKey is the Go-context key carrying the authenticated tenant ID
// from the request claims. The kernel transports the tenant; whether (and
// how) it is used is decided by the edition's service implementations.
type tenantCtxKey struct{}

// WithTenantContext returns ctx annotated with the given tenant ID.
func WithTenantContext(ctx context.Context, tenantID int64) context.Context {
	return context.WithValue(ctx, tenantCtxKey{}, tenantID)
}

// TenantFromContext extracts the tenant ID stored by WithTenantContext.
// The second return reports whether a tenant is present.
func TenantFromContext(ctx context.Context) (int64, bool) {
	tenantID, ok := ctx.Value(tenantCtxKey{}).(int64)
	return tenantID, ok
}

// ScopeTenantContext returns ctx annotated with the authenticated tenant
// from the request claims (see GetUserClaims), or ctx unchanged when no
// claims are present. Handlers call it once at entry so downstream
// services receive the tenant through the ordinary context.
func ScopeTenantContext(ctx context.Context, arc *app.RequestContext) context.Context {
	if claims, ok := GetUserClaims(arc); ok && claims != nil {
		return WithTenantContext(ctx, claims.TenantID)
	}
	return ctx
}
