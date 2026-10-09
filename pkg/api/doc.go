// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package api provides the HTTP server, route registration, TLS/ACME
// certificate management, and shared types for the tickraft kernel.
//
// The package is organized around four concerns:
//
//   - Server: a thin wrapper around a Hertz engine that owns the middleware
//     chain, plugin lifecycle, and graceful shutdown. See server.go and
//     plugin.go.
//   - Routing: RouterGroup exposes the subset of Hertz route-registration
//     methods used by plugins and the route registrar. Virtual
//     host dispatch (vhost.go) maps the request Host header to named route
//     groups; single-page application fallback (spa.go) serves the frontend
//     build output.
//   - TLS / ACME: tls.go builds and hot-reloads the *tls.Config; acme.go
//     drives the RFC 8555 ACME flow; the challenge provider interface
//     (acme_provider.go, http01_provider.go, dns01_provider.go) lets
//     callers may inject DNS-01 and cert-manager backed issuance
//     without modifying the kernel source.
//   - Handler subpackage: pkg/api/handler hosts the HTTP
//     handlers (auth, task, alert, system, asset, telemetry, certificates,
//     healthz, i18n) and the in-memory service defaults. The middleware
//     subpackage hosts the built-in middleware chain (recovery, request_id,
//     access_log, cors, locale, jwt, apikey, asset_key, permission,
//     tenant, trusted_proxy).
//
// # Extension Points
//
// The kernel exposes three extension points so extended
// editions can add capabilities without modifying kernel source:
//
//   - Plugin (plugin.go): inject routes, global middleware, and lifecycle
//     hooks via Server.RegisterPlugin.
//   - ACMEProvider (acme_provider.go): inject DNS-01 (and other) ACME
//     challenge providers via SetACMEProvider. The runtime
//     ships only the HTTP-01 provider; DNS-01 is added by wrapping a
//     internal/cert.DNSProvider with NewDNS01Provider.
//   - ACMECertStore (acme.go): inject a persistent store for issued
//     certificates and account keys so they survive process restarts.
//
// # Purity
//
// The package depends only on the Go standard library, hertz, fsnotify,
// golang.org/x/crypto/acme, and other tickraft packages. It
// does not import any third-party observability, RPC, or caching client
// libraries; extended capabilities are added through the extension
// points documented above.
package api
