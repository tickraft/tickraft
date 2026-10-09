// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package middleware

import (
	"context"
	"net/http"
	"runtime"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"

	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// Recovery returns a middleware that catches panics, logs the stack trace,
// and returns a unified 500 error response.
func Recovery() app.HandlerFunc {
	return func(ctx context.Context, arc *app.RequestContext) {
		defer func() {
			if reason := recover(); reason != nil {
				// Log the stack trace
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				hlog.CtxErrorf(ctx, "panic recovered: %v\n%s", reason, buf[:n])

				// Return unified 500 error response
				httputil.FailWithCode(arc, http.StatusInternalServerError,
					errdefs.CodeInternal, "internal error")
			}
		}()
		arc.Next(ctx)
	}
}
