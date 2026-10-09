// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package middleware

import (
	"context"
	"encoding/hex"
	"math/rand/v2"

	"github.com/tickraft/tickraft/pkg/api/httputil"

	"github.com/cloudwego/hertz/pkg/app"
)

// RequestID returns a middleware that generates a unique request ID
// and injects it into the context and X-Tickraft-Request-Id response header.
func RequestID() app.HandlerFunc {
	return func(ctx context.Context, arc *app.RequestContext) {
		id := generateRequestID()
		httputil.SetRequestID(arc, id)
		arc.Header(httputil.HeaderRequestID, id)
		arc.Next(ctx)
	}
}

// generateRequestID creates a 32-character hex string using math/rand/v2.
// Request IDs do not carry security guarantees, so a non-crypto RNG is sufficient.
func generateRequestID() string {
	var buf [16]byte
	for i := range buf {
		buf[i] = byte(rand.IntN(256)) //nolint:gosec // non-crypto RNG is fine for request IDs
	}
	return hex.EncodeToString(buf[:])
}
