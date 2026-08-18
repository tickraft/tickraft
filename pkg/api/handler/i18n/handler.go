// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package i18n exposes locale listing endpoints backed by the shared i18n
// registry, letting the frontend discover available languages before
// authentication.
package i18n

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/i18n"
)

// Handler exposes the i18n Registry via HTTP endpoints. It is
// registered at /api/v1/i18n and provides locale listing for frontend
// language switchers and validation.
type Handler struct {
	registry i18n.Registry
}

// NewHandler creates a Handler backed by the given Registry.
// The Registry must be non-nil; callers should ensure it is populated
// with locale bundles before the server starts accepting requests.
func NewHandler(r i18n.Registry) *Handler {
	return &Handler{registry: r}
}

// ListLocales handles GET /api/v1/i18n/locales.
//
// Returns all registered locales as a JSON array of LocaleInfo objects,
// sorted by tag. This endpoint is public (no JWT required) so the
// frontend can discover available locales before authentication, e.g.
// on the login page language switcher.
//
// callers may extend the locale list transparently: when
// extended locale bundles (zh-Hant, en-GB, ar, ja, de, fr, es, ru, ko)
// are registered via Registry.Register at startup, they automatically
// appear in the response without any handler modification.
func (h *Handler) ListLocales(ctx context.Context, arc *app.RequestContext) {
	if h.registry == nil {
		api.Success(arc, []i18n.LocaleInfo{})
		return
	}
	locales := h.registry.List()
	api.Success(arc, locales)
}
