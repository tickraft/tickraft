// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/tickraft/tickraft/pkg/api/httputil"
)

// CORS returns a middleware that handles cross-origin requests.
//
// When allowedOrigins is non-empty, only origins in the list are
// permitted; the matched origin is reflected back with
// Access-Control-Allow-Credentials: true so cookies and Authorization
// headers work cross-origin.
//
// When allowedOrigins is empty, the middleware falls back to
// Access-Control-Allow-Origin: * without credentials, which is safe
// for public APIs but does not support authenticated cross-origin
// requests with cookies.
func CORS(allowedOrigins []string) app.HandlerFunc {
	originSet := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[strings.ToLower(strings.TrimSpace(o))] = struct{}{}
	}

	return func(ctx context.Context, arc *app.RequestContext) {
		origin := strings.ToLower(strings.TrimSpace(string(arc.GetHeader("Origin"))))
		if origin == "" {
			arc.Next(ctx)
			return
		}

		// Include all X-Tickraft-* request headers used by the frontend
		// and programmatic clients so cross-origin preflights succeed.
		allowedHeaders := strings.Join([]string{
			"Content-Type",
			"Authorization",
			httputil.HeaderAPIKey,
			httputil.HeaderRequestID,
			"X-Tickraft-Locale",
			"X-Tickraft-Asset-Key",
		}, ",")

		if len(originSet) > 0 {
			// Origin allowlist mode: reflect the origin and allow credentials.
			if _, ok := originSet[origin]; !ok {
				arc.Next(ctx)
				return
			}
			arc.Header("Access-Control-Allow-Origin", origin)
			arc.Header("Access-Control-Allow-Credentials", "true")
		} else {
			// Wildcard mode: allow all origins without credentials.
			arc.Header("Access-Control-Allow-Origin", "*")
		}

		arc.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		arc.Header("Access-Control-Allow-Headers", allowedHeaders)
		arc.Header("Access-Control-Max-Age", "86400")

		// Handle preflight
		if strings.EqualFold(string(arc.Request.Method()), "OPTIONS") {
			arc.Status(http.StatusNoContent)
			arc.Abort()
			return
		}

		arc.Next(ctx)
	}
}
