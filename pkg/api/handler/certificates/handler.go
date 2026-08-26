// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package certificates implements the certificate reload endpoint
// (POST /api/v1/system/certificates/reload). The handler reuses the JWT
// middleware registered by routes.go (see WithJWTAuth) and the live
// certificate-reload machinery exposed by *api.Server (see ReloadTLSConfig in
// pkg/api/tls.go). It is intentionally small: all cryptographic work lives in
// the api package so the handler package stays free of crypto imports and
// remains easy to mock in tests.
package certificates

import (
	"context"
	"fmt"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// Reloader is the interface the certificate reload handler depends on.
// *api.Server satisfies this interface via its ReloadTLSConfig method; tests
// may inject a stub that returns a deterministic fingerprint.
//
// The method returns the SHA-256 fingerprint of the newly loaded leaf
// certificate (lowercase hex) so the handler can return it to the operator
// for verification. ReloadTLSConfig is the only method the handler needs, so
// the interface is single-method to keep mocking trivial.
type Reloader interface {
	// ReloadTLSConfig rebuilds the active *tls.Config from the configured
	// certificate and key files, atomically publishes it, and returns the
	// fingerprint of the loaded leaf certificate.
	ReloadTLSConfig() (string, error)
}

// Handler exposes the certificate-management endpoints under
// /api/v1/system/certificates. The runtime only implements the
// reload endpoint; callers may extend the route group with additional
// endpoints (issue, rotate, list) via the extension interface in pkg/api/handler/route_option.go.
type Handler struct {
	reloader Reloader
}

// NewHandler creates a Handler backed by the given
// reloader. The reloader must be non-nil; passing nil returns an error so
// the wiring mistake surfaces at startup rather than on the first request.
func NewHandler(reloader Reloader) (*Handler, error) {
	if reloader == nil {
		return nil, fmt.Errorf("certificates: %w: reloader must not be nil", errdefs.ErrInvalidArgument)
	}
	return &Handler{reloader: reloader}, nil
}

// reloadResponse is the response body for the reload endpoint. Fingerprint is
// the SHA-256 fingerprint of the newly loaded leaf certificate, expressed as
// a lowercase hex string so it can be compared against openssl output without
// further formatting.
type reloadResponse struct {
	Fingerprint string `json:"fingerprint"`
}

// Reload handles POST /api/v1/system/certificates/reload. It rebuilds the
// active TLS configuration from the configured certificate and key files and
// returns the SHA-256 fingerprint of the newly loaded leaf certificate.
//
// The endpoint is JWT-protected (registered under the system route group with
// the JWT middleware applied by routes.go) so only authenticated operators can
// trigger a reload. Authorization (which role may reload) is delegated to the
// permission middleware configured at route-registration time.
//
// Failures are surfaced as the standard httputil.Response envelope with the
// underlying error wrapped by ReloadTLSConfig (see ErrTLS* and ErrACME* in
// pkg/api/types.go and pkg/api/tls.go).
func (h *Handler) Reload(ctx context.Context, arc *app.RequestContext) {
	fingerprint, err := h.reloader.ReloadTLSConfig()
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, reloadResponse{Fingerprint: fingerprint})
}
