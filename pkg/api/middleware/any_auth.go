// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package middleware

import (
	"context"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/auth/apikey"
	"github.com/tickraft/tickraft/pkg/auth/jwt"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// NewAnyAuth returns a Hertz middleware that accepts either a JWT Bearer
// token (via the Authorization header) or an API key (via the
// X-Tickraft-API-Key header). It dispatches to the appropriate
// authentication path based on which header is present:
//
//   - Authorization header → JWT validation via [NewJWTAuth].
//   - X-Tickraft-API-Key header → API key validation via [NewAPIKeyAuth].
//   - Neither header → 401 Unauthorized.
//
// This middleware enables programmatic API consumers to authenticate
// with an API key while interactive users continue to use JWT tokens,
// without requiring two separate middleware chains.
func NewAnyAuth(j *jwt.JWT, keyGetter func(ctx context.Context, keyHash string) (*apikey.Info, error)) app.HandlerFunc {
	jwtMW := NewJWTAuth(j, "")
	apiKeyMW := NewAPIKeyAuth(keyGetter, "")

	return func(ctx context.Context, arc *app.RequestContext) {
		authHeader := string(arc.GetHeader("Authorization"))
		if authHeader != "" {
			jwtMW(ctx, arc)
			return
		}

		apiKeyHeader := string(arc.GetHeader(httputil.HeaderAPIKey))
		if apiKeyHeader != "" {
			apiKeyMW(ctx, arc)
			return
		}

		httputil.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeUnauthorized, "missing authentication")
		arc.Abort()
	}
}
