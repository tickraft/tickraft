// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package middleware

import (
	"context"
	"time"

	"github.com/tickraft/tickraft/pkg/api/httputil"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

// AccessLog returns a middleware that logs each request's method, path,
// status code, duration, client IP, and request ID.
func AccessLog() app.HandlerFunc {
	return func(ctx context.Context, arc *app.RequestContext) {
		start := time.Now()

		arc.Next(ctx)

		duration := time.Since(start)
		hlog.CtxInfof(ctx, "[%s] %s %d %v %s request_id=%s",
			string(arc.Request.Method()),
			string(arc.Request.URI().Path()),
			arc.Response.StatusCode(),
			duration,
			httputil.GetClientIP(arc),
			httputil.GetRequestID(arc),
		)
	}
}
