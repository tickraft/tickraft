// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package readyz exposes the /readyz readiness endpoint. It runs every
// configured dependency checker in parallel with a per-check timeout and
// returns HTTP 503 when any checker fails, so load balancers can route
// traffic away from instances that are alive but not yet ready.
package readyz

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/cache"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// readyCheckTimeout bounds each individual dependency probe so a hung
// dependency cannot block the readiness endpoint. A short timeout ensures
// the probe fails fast rather than holding the health-check loop.
const readyCheckTimeout = 2 * time.Second

// readyzCheckKey is a sentinel cache key used to verify cache responsiveness
// without producing side effects on real cache entries.
const readyzCheckKey = "__readyz_ping__"

// JSON field name and ready status value of the readiness response payload.
const (
	statusKey   = "status"
	statusReady = "ready"
)

// DependencyChecker probes a single dependency and reports whether it is
// ready to serve traffic. Implementations must be safe for concurrent use
// because Handler runs every checker in parallel.
type DependencyChecker interface {
	// Name returns the dependency identifier used as the key in the checks
	// map of the readiness response.
	Name() string
	// Check verifies the dependency is reachable within the given context.
	// The context carries a per-check timeout so slow dependencies fail fast.
	Check(ctx context.Context) error
}

// Handler exposes the /readyz endpoint. It runs every configured
// DependencyChecker in parallel with a per-check timeout and returns HTTP
// 200 when all checks pass, or 503 when any check fails. Dependencies that
// are nil (not configured) are skipped, allowing the handler to degrade
// gracefully in deployments that do not enable caching.
//
// Separation from /healthz:
//   - /healthz is the liveness probe: "is the process alive?" It returns 200
//     as long as the process can answer, so orchestrators do not restart the
//     instance on a transient dependency blip.
//   - /readyz is the readiness probe: "can the server accept traffic?" It
//     returns 503 when a dependency is unreachable so a load balancer can
//     route traffic away from an instance that is alive but not yet ready.
type Handler struct {
	checkers []DependencyChecker
}

// NewHandler creates a Handler that probes the given database and
// cache. Either argument may be nil, in which case the corresponding
// dependency check is skipped. This mirrors healthz.NewHandler so the two
// probes can be wired identically at startup.
func NewHandler(dbc *gorm.DB, c *cache.LRUCache) *Handler {
	h := &Handler{}
	if dc := NewDatabaseChecker(dbc); dc != nil {
		h.checkers = append(h.checkers, dc)
	}
	if cc := NewCacheChecker(c); cc != nil {
		h.checkers = append(h.checkers, cc)
	}
	return h
}

// NewHandlerWithCheckers creates a Handler from an explicit list
// of DependencyChecker values. Nil checkers are ignored. This constructor
// is intended for tests and callers that need custom probes beyond
// the DB+cache pair.
func NewHandlerWithCheckers(checkers ...DependencyChecker) *Handler {
	cs := make([]DependencyChecker, 0, len(checkers))
	for _, c := range checkers {
		if c == nil {
			continue
		}
		cs = append(cs, c)
	}
	return &Handler{checkers: cs}
}

// checkResult holds the outcome of a single dependency probe.
type checkResult struct {
	status    string
	latencyMs float64
}

// Ready handles GET /readyz. It probes all configured dependencies in
// parallel and returns 200 with status details when ready, or 503 with the
// failure list when any dependency is not ready.
//
// This is an intentionally public infrastructure endpoint: it must not
// depend on any auth context or business service wiring, so it is
// registered on the root route group without middleware.
func (h *Handler) Ready(ctx context.Context, arc *app.RequestContext) {
	results := make(map[string]checkResult, len(h.checkers))
	ready := true

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range h.checkers {
		wg.Add(1)
		go func(c DependencyChecker) {
			defer wg.Done()
			checkCtx, cancel := context.WithTimeout(ctx, readyCheckTimeout)
			defer cancel()

			start := time.Now()
			err := c.Check(checkCtx)
			latency := float64(time.Since(start).Microseconds()) / 1000.0

			res := checkResult{status: "up", latencyMs: latency}
			if err != nil {
				res.status = "down"
			}

			mu.Lock()
			results[c.Name()] = res
			if err != nil {
				ready = false
			}
			mu.Unlock()
		}(c)
	}
	wg.Wait()

	checks := make(map[string]any, len(results))
	for name, res := range results {
		checks[name] = map[string]any{
			statusKey:    res.status,
			"latency_ms": res.latencyMs,
		}
	}

	if ready {
		httputil.Success(arc, map[string]any{
			statusKey: statusReady,
			"checks":  checks,
		})
		return
	}

	httputil.FailWithData(arc, http.StatusServiceUnavailable, errdefs.CodeInternal, "not ready", map[string]any{
		statusKey: "not_ready",
		"checks":  checks,
	})
}

// DefaultReady is the fallback handler used when no Handler is
// injected via WithReadyHandler. It returns 200 with {"status":"ready"}
// without performing any dependency checks, preserving a trivially-ready
// behavior for deployments that do not wire concrete checkers.
func DefaultReady(ctx context.Context, arc *app.RequestContext) {
	httputil.Success(arc, map[string]string{statusKey: statusReady})
}

// DatabaseChecker probes a SQL database via SELECT 1.
type DatabaseChecker struct {
	dbc *gorm.DB
}

// NewDatabaseChecker creates a DatabaseChecker. Returns nil when db is nil
// so the caller can pass the result directly to NewHandler without an
// extra nil guard.
func NewDatabaseChecker(dbc *gorm.DB) *DatabaseChecker {
	if dbc == nil {
		return nil
	}
	return &DatabaseChecker{dbc: dbc}
}

// Name returns "database".
func (c *DatabaseChecker) Name() string { return "database" }

// Check executes SELECT 1 against the underlying database.
func (c *DatabaseChecker) Check(ctx context.Context) error {
	return c.dbc.WithContext(ctx).Exec("SELECT 1").Error
}

// CacheChecker probes the LRU cache via a Has sentinel key.
type CacheChecker struct {
	cache *cache.LRUCache
}

// NewCacheChecker creates a CacheChecker. Returns nil when c is nil.
func NewCacheChecker(c *cache.LRUCache) *CacheChecker {
	if c == nil {
		return nil
	}
	return &CacheChecker{cache: c}
}

// Name returns "cache".
func (c *CacheChecker) Name() string { return "cache" }

// Check verifies cache responsiveness via a Has probe on a sentinel key. A
// panic from the cache implementation is treated as a failure.
func (c *CacheChecker) Check(ctx context.Context) error {
	if !probeCache(ctx, c.cache) {
		return errors.New("cache probe failed")
	}
	return nil
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
	c.Has(ctx, readyzCheckKey)
	return true
}
