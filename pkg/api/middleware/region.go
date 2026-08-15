// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package middleware

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/auth/region"
	"go.uber.org/zap"
)

const (
	// regionHeader is the HTTP header that explicitly specifies the target
	// routing region. It has the highest priority in the three-level routing.
	regionHeader = "X-Tickraft-Region"
	// routeRegionHeader is the response header that records the resolved
	// routing region for client-side inspection and debugging.
	routeRegionHeader = "X-Tickraft-Route-Region"
	// regionCookieName is the name of the HMAC-signed region preference cookie.
	regionCookieName = "tk_region"
	// defaultRegion is used when no region information is available and GeoIP
	// lookup is not configured or returns an empty result.
	defaultRegion = "global"
)

// NewRegionMiddleware returns a Hertz middleware that resolves the request
// routing region using a three-level priority:
//
//  1. X-Tickraft-Region header (highest priority) — used by API/SDK calls and
//     server-side scheduled tasks where the caller explicitly specifies the
//     target region.
//  2. tk_region HMAC-signed cookie — used by browser sessions where the user's
//     region preference is persisted in a tamper-proof cookie.
//  3. GeoIP lookup based on client IP (fallback) — used for first-time visits
//     with no header or cookie.
//
// The resolved region is written to the X-Tickraft-Route-Region response header
// and stored in the request context for downstream handlers via
// httputil.SetRegion.
//
// If keyRotator is non-nil, it is used for cookie verification (supporting key
// rotation with a transition period); otherwise cookieSecret is used directly
// via region.VerifyCookie.
//
// Cookie verification failure does not block the request; the middleware falls
// back to GeoIP lookup. If geoipLookup is nil, the default region "global" is
// used when falling back to GeoIP.
func NewRegionMiddleware(
	cookieSecret []byte,
	keyRotator *region.KeyRotator,
	geoipLookup func(ip string) string,
) app.HandlerFunc {
	return func(ctx context.Context, arc *app.RequestContext) {
		resolved := resolveRegion(arc, cookieSecret, keyRotator, geoipLookup)

		arc.Header(routeRegionHeader, resolved)
		ctx = httputil.SetRegion(ctx, resolved)

		arc.Next(ctx)
	}
}

// resolveRegion determines the routing region using the three-level priority.
func resolveRegion(
	arc *app.RequestContext,
	cookieSecret []byte,
	keyRotator *region.KeyRotator,
	geoipLookup func(ip string) string,
) string {
	// Level 1: X-Tickraft-Region header (highest priority).
	if hdr := string(arc.GetHeader(regionHeader)); hdr != "" {
		return hdr
	}

	// Level 2: tk_region HMAC-signed cookie.
	if cookieValue := string(arc.Request.Header.Cookie(regionCookieName)); cookieValue != "" {
		regionFromCookie, err := verifyRegionCookie(cookieValue, cookieSecret, keyRotator)
		if err == nil && regionFromCookie != "" {
			return regionFromCookie
		}
		// Cookie verification failed; log and fall through to GeoIP.
		zap.L().Debug("region cookie verification failed, falling back to GeoIP",
			zap.Error(err))
	}

	// Level 3: GeoIP lookup based on client IP.
	if geoipLookup != nil {
		clientIP := httputil.GetClientIP(arc)
		if r := geoipLookup(clientIP); r != "" {
			return r
		}
	}

	return defaultRegion
}

// verifyRegionCookie verifies the signed region cookie using either the key
// rotator (if available) or the static cookie secret.
func verifyRegionCookie(cookieValue string, cookieSecret []byte, keyRotator *region.KeyRotator) (string, error) {
	if keyRotator != nil {
		r, _, err := keyRotator.Verify(cookieValue)
		return r, err
	}
	r, _, err := region.VerifyCookie(cookieValue, cookieSecret)
	return r, err
}
