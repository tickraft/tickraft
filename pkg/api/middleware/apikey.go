// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/auth/apikey"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// NewAPIKeyAuth returns a Hertz middleware that validates API keys from the
// X-Tickraft-API-Key header. The keyGetter function looks up the key information by its
// SHA-256 hash. If permission is non-empty, the middleware also checks that
// permission via the default RBAC policy.
func NewAPIKeyAuth(keyGetter func(ctx context.Context, keyHash string) (*apikey.Info, error), permission string) app.HandlerFunc {
	return func(ctx context.Context, arc *app.RequestContext) {
		rawKey := string(arc.GetHeader(httputil.HeaderAPIKey))
		if rawKey == "" {
			httputil.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeUnauthorized, "missing api key")
			arc.Abort()
			return
		}

		// Compute SHA-256 hash of the raw key.
		h := sha256.Sum256([]byte(rawKey))
		keyHash := hex.EncodeToString(h[:])

		// Look up the key in storage.
		info, err := keyGetter(ctx, keyHash)
		if err != nil {
			httputil.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeUnauthorized, "invalid api key")
			arc.Abort()
			return
		}

		// Basic validation (hash match, status, expiry).
		if err = apikey.ValidateAPIKey(rawKey, info.KeyHash, info.Status, info.ExpiredAt); err != nil {
			switch {
			case errors.Is(err, apikey.ErrAPIKeyRevoked):
				httputil.FailWithCode(arc, http.StatusForbidden, errdefs.CodeForbidden, "api key revoked")
			case errors.Is(err, apikey.ErrAPIKeyExpired):
				httputil.FailWithCode(arc, http.StatusForbidden, errdefs.CodeForbidden, "api key expired")
			default:
				httputil.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeUnauthorized, "invalid api key")
			}
			arc.Abort()
			return
		}

		// Store key ID in context.
		httputil.SetAPIKeyID(arc, info.ID)

		// If a permission is specified, check it.
		if permission != "" {
			if !checkPermission(ctx, 0, permission, "") {
				httputil.FailWithCode(arc, http.StatusForbidden, errdefs.CodeForbidden, "permission denied")
				arc.Abort()
				return
			}
		}

		arc.Next(ctx)
	}
}
