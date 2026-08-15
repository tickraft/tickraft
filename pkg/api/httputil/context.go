// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

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
	apiKeyIDKey   = "api.api_key_id"
	clientIPKey   = "api.client_ip"
)

// GetRequestID returns the request ID from context.
func GetRequestID(c *app.RequestContext) string {
	val, _ := c.Get(requestIDKey)
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}

// SetRequestID stores the request ID in context.
func SetRequestID(c *app.RequestContext, id string) {
	c.Set(requestIDKey, id)
}

// SetUserClaims stores the authenticated user claims in the Hertz request context.
func SetUserClaims(c *app.RequestContext, claims *jwt.UserClaims) {
	c.Set(userClaimsKey, claims)
}

// GetUserClaims retrieves the authenticated user claims from the Hertz request context.
func GetUserClaims(c *app.RequestContext) (*jwt.UserClaims, bool) {
	val, _ := c.Get(userClaimsKey)
	if claims, ok := val.(*jwt.UserClaims); ok {
		return claims, true
	}
	return nil, false
}

// SetAPIKeyID stores the API key ID in the Hertz request context.
func SetAPIKeyID(c *app.RequestContext, keyID int64) {
	c.Set(apiKeyIDKey, keyID)
}

// GetAPIKeyID retrieves the API key ID from the Hertz request context.
func GetAPIKeyID(c *app.RequestContext) (int64, bool) {
	val, _ := c.Get(apiKeyIDKey)
	if keyID, ok := val.(int64); ok {
		return keyID, true
	}
	return 0, false
}

// BindAndValidate binds request parameters to obj and validates.
// Returns true on success; on failure, writes a 400 error response automatically.
func BindAndValidate(c *app.RequestContext, obj interface{}) bool {
	if err := c.Bind(obj); err != nil {
		FailWithCode(c, http.StatusBadRequest, errdefs.CodeBadRequest, "invalid request parameters")
		return false
	}
	if err := c.Validate(obj); err != nil {
		FailWithCode(c, http.StatusBadRequest, errdefs.CodeBadRequest, err.Error())
		return false
	}
	return true
}

// SetClientIP stores the resolved real client IP in the request context.
// It is intended for use by the trusted-proxy middleware so downstream
// handlers and loggers read a single, authoritative client IP through
// GetClientIP regardless of proxy headers.
func SetClientIP(c *app.RequestContext, ip string) {
	c.Set(clientIPKey, ip)
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
func GetClientIP(c *app.RequestContext) string {
	// 1. Authoritative value set by trusted-proxy middleware.
	if val, ok := c.Get(clientIPKey); ok {
		if s, ok := val.(string); ok && s != "" {
			return s
		}
	}
	// 2. Fall back to RemoteAddr
	return c.RemoteAddr().String()
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
func SetUser(ctx *app.RequestContext, u *user.User) {
	ctx.Set(userKey, u)
}

// GetUser retrieves the authenticated user from the Hertz request context.
// Returns nil if no user is set.
func GetUser(ctx *app.RequestContext) *user.User {
	val, _ := ctx.Get(userKey)
	if u, ok := val.(*user.User); ok {
		return u
	}
	return nil
}
