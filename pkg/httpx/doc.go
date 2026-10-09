// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package httpx provides a shared HTTP client factory with a tuned
// connection-pooled transport.
//
// The package exists so that every outbound HTTP client in the codebase
// shares the same sane defaults for connection reuse, idle timeouts and
// TLS handshake budgets. Constructing an [http.Client] directly with
// [net/http.DefaultTransport] or a bare [net/http.Transport] without
// pooled-connection tuning leads to either connection churn (no reuse)
// or unbounded idle connections (no timeout). [NewPoolClient] returns a
// client whose [net/http.Transport] is configured with conservative
// pooling defaults that can be overridden per call site via [Config].
//
// Usage:
//
//	client := httpx.NewPoolClient(httpx.Config{Timeout: 10 * time.Second})
//	resp, err := client.Do(req)
//
// The returned client is safe for concurrent use. Callers that need a
// custom transport (for example to set a proxy) can still build their
// own [net/http.Client] but should prefer [NewTransport] to inherit the
// pooled-transport defaults.
package httpx
