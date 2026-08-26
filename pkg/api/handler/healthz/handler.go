// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package healthz exposes the /healthz liveness endpoint. The handler probes
// the configured database and cache dependencies and returns HTTP 503 when
// any of them is unhealthy.
package healthz

import (
	"context"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/cache"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// healthzCheckKey is a sentinel cache key used to verify cache responsiveness
// without producing side effects on real cache entries.
const healthzCheckKey = "__healthz_ping__"

// Handler exposes the /healthz endpoint with dependency probing. It
// checks the database (via SELECT 1) and the cache (via a Has probe) and
// returns HTTP 503 when any configured dependency is unhealthy. Dependencies
// that are nil (not configured) are skipped, allowing the handler to degrade
// gracefully in deployments that do not enable caching.
type Handler struct {
	dbc   *gorm.DB
	cache *cache.LRUCache
}

// NewHandler creates a Handler that probes the given database
// and cache. Either argument may be nil, in which case the corresponding
// dependency check is skipped.
func NewHandler(dbc *gorm.DB, c *cache.LRUCache) *Handler {
	return &Handler{dbc: dbc, cache: c}
}

// Healthz handles GET /healthz. It probes all configured dependencies and
// returns 200 with status details when healthy, or 503 with the failure list
// when any dependency is unhealthy.
//
// This is an intentionally public infrastructure endpoint: it must not depend
// on any auth context or business service wiring, so it is registered on the
// root route group without middleware.
func (h *Handler) Healthz(ctx context.Context, arc *app.RequestContext) {
	checks := make(map[string]string)
	healthy := true

	if h.dbc != nil {
		if err := h.dbc.WithContext(ctx).Exec("SELECT 1").Error; err != nil {
			checks["db"] = "failed: " + err.Error()
			healthy = false
		} else {
			checks["db"] = "ok"
		}
	}

	if h.cache != nil {
		if probeCache(ctx, h.cache) {
			checks["cache"] = "ok"
		} else {
			checks["cache"] = "failed"
			healthy = false
		}
	}

	if healthy {
		httputil.Success(arc, map[string]any{
			"status": "ok",
			"checks": checks,
		})
		return
	}

	httputil.FailWithData(arc, http.StatusServiceUnavailable, errdefs.CodeInternal, "unhealthy", map[string]any{
		"status": "unhealthy",
		"checks": checks,
	})
}

// probeCache verifies cache responsiveness by invoking Has on a sentinel key.
// It recovers from panics as a defensive measure. A non-panic return is
// treated as healthy.
func probeCache(ctx context.Context, c *cache.LRUCache) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()
	c.Has(ctx, healthzCheckKey)
	return true
}

// DefaultHealthz is the fallback handler used when no Handler is
// injected via WithHealthzHandler. It returns 200 with {"status":"ok"}
// without performing any dependency checks, preserving the
// default behavior for deployments that do not wire a concrete handler.
func DefaultHealthz(ctx context.Context, arc *app.RequestContext) {
	httputil.Success(arc, map[string]string{"status": "ok"})
}
