// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package middleware

import (
	"context"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// assetKeyHeader is the HTTP header used to authenticate telemetry report
// endpoints. The value must be a valid asset key issued by the system.
const assetKeyHeader = "X-Tickraft-Asset-Key"

// NewAssetKeyMiddleware returns a middleware that validates the
// X-Tickraft-Asset-Key header for telemetry report authentication.
//
// The getter function checks whether a asset key is valid. It returns
// (true, nil) if the key is valid, (false, nil) if the key is unknown, and
// (false, err) if an internal error occurs during validation.
//
// Behaviour:
//   - Header missing: 401 with code 40102 (CodeAssetKeyMissing).
//   - Header invalid (getter returns false): 403 with code 40301
//     (CodeAssetKeyInvalid).
//   - Header valid: the request proceeds to the next handler.
//   - Internal error from getter: 500 with code 50000 (CodeInternal).
func NewAssetKeyMiddleware(getter func(ctx context.Context, key string) (bool, error)) app.HandlerFunc {
	return func(ctx context.Context, arc *app.RequestContext) {
		key := string(arc.GetHeader(assetKeyHeader))
		if key == "" {
			httputil.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeAssetKeyMissing, "missing asset key")
			arc.Abort()
			return
		}

		valid, err := getter(ctx, key)
		if err != nil {
			httputil.FailWithCode(arc, http.StatusInternalServerError, errdefs.CodeInternal, "asset key validation error")
			arc.Abort()
			return
		}

		if !valid {
			httputil.FailWithCode(arc, http.StatusForbidden, errdefs.CodeAssetKeyInvalid, "invalid asset key")
			arc.Abort()
			return
		}

		arc.Next(ctx)
	}
}
