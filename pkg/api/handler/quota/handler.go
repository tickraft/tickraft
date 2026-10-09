// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package quota exposes the read-only quota usage aggregate that backs the
// dashboard near-limit hints: one row per count-based quota type with the
// current usage, the ceiling from the active Provider, and the ratio.
package quota

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/api/httputil"
	pkgquota "github.com/tickraft/tickraft/pkg/quota"
)

// UsageItem is one row of the usage view. Ratio is used/ceiling when a
// ceiling is configured (> 0) and 0 otherwise (unlimited types carry no
// meaningful ratio).
type UsageItem struct {
	Type    string  `json:"type"`
	Used    int64   `json:"used"`
	Ceiling int     `json:"ceiling"`
	Ratio   float64 `json:"ratio"`
}

// UsageSource binds a quota type to a live counter. Count implementations
// should be cheap point queries (store COUNT or a 1×1 list total), matching
// the counting basis the enforcement points use for the same type.
type UsageSource struct {
	Type  pkgquota.Type
	Count func(ctx context.Context) (int64, error)
}

// Handler exposes the quota usage endpoint. It owns no storage: the
// composition root injects one UsageSource per reportable type, so the
// endpoint stays a pure aggregation over services that already exist.
type Handler struct {
	sources []UsageSource
	logger  *zap.Logger
}

// NewHandler creates a quota Handler over the given sources. Sources with a
// nil Count are skipped.
func NewHandler(logger *zap.Logger, sources ...UsageSource) *Handler {
	usable := make([]UsageSource, 0, len(sources))
	for _, s := range sources {
		if s.Count != nil {
			usable = append(usable, s)
		}
	}
	return &Handler{sources: usable, logger: logger}
}

// Usage handles GET /api/v1/quota/usage. A source whose count fails is
// logged and omitted (the same warn-and-continue contract as the system
// stats aggregate) so one unavailable store never hides the other rows.
func (h *Handler) Usage(ctx context.Context, arc *app.RequestContext) {
	items := make([]UsageItem, 0, len(h.sources))
	for _, s := range h.sources {
		used, err := s.Count(ctx)
		if err != nil {
			h.logger.Warn("quota usage: count failed",
				zap.String("type", string(s.Type)), zap.Error(err))
			continue
		}
		ceiling := pkgquota.Ceiling(s.Type)
		item := UsageItem{Type: string(s.Type), Used: used, Ceiling: ceiling}
		if ceiling > 0 {
			item.Ratio = float64(used) / float64(ceiling)
		}
		items = append(items, item)
	}
	httputil.Success(arc, items)
}
