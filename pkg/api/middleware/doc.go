// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package middleware provides the built-in HTTP middleware for the tickraft
// API server.
//
// The middleware is applied by [*api.Server.Start] in a fixed order:
//
//	RequestID -> AccessLog (when enabled) -> Recovery -> CORS (when enabled)
//	-> TrustedProxy (when configured) -> Locale -> Plugin middlewares
//	-> route-group middleware
//
// Each middleware is stateless (except for the trusted-proxy CIDR allowlist,
// which is parsed once at construction time) and safe for concurrent use.
//
// # Panic isolation
//
// [Recovery] catches panics from any downstream handler, logs the stack
// trace, and returns a unified 500 response so a panic in one handler never
// takes the server down. The healthz handler's probeCache helper also
// recovers from panics in case a remote cache implementation panics on a
// broken connection.
//
// # Authentication
//
// [NewJWTAuth], [NewAPIKeyAuth], and [NewAssetKeyMiddleware] validate JWT
// bearer tokens, API keys, and asset keys respectively. Each stores the
// resolved identity in the request context via the httputil helpers so
// downstream handlers and services can access it without re-parsing the
// credential.
//
// # Authorization
//
// [RequirePermission] checks the authenticated user's role against the
// default RBAC policy. The middleware fails closed (403), never fail-open,
// so a misconfigured policy cannot accidentally grant access.
//
// # Purity
//
// The middleware package depends only on the Go standard library, hertz,
// and the tickraft packages (pkg/auth, pkg/i18n). It does not import any
// third-party observability, RPC, or caching client libraries.
package middleware
