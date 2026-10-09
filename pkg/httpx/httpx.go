// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httpx

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// Default connection-pool tuning applied by [NewPoolClient] and
// [NewTransport] when the corresponding [Config] field is zero. The
// values follow the recommendations from the net/http documentation and
// are sized for a typical multi-tenant control plane that fans out to
// many upstream endpoints.
const (
	// DefaultMaxIdleConns is the global cap on idle connections across
	// all hosts. It bounds the total file-descriptor usage of the
	// process.
	DefaultMaxIdleConns = 100
	// DefaultMaxIdleConnsPerHost is the per-host idle connection cap.
	// The net/http default is 2, which is too low for high-fanout
	// outbound traffic and causes excessive TLS handshakes; 10 keeps a
	// small warm pool per host without overcommitting.
	DefaultMaxIdleConnsPerHost = 10
	// DefaultIdleConnTimeout is how long an idle connection stays in
	// the pool before being closed. Matches the net/http default.
	DefaultIdleConnTimeout = 90 * time.Second
	// DefaultTLSHandshakeTimeout is the maximum time to wait for a TLS
	// handshake.
	DefaultTLSHandshakeTimeout = 10 * time.Second
	// DefaultExpectContinueTimeout is the maximum time to wait for a
	// server's first response headers after fully writing the request
	// headers if the request has an "Expect: 100-continue" header.
	DefaultExpectContinueTimeout = 1 * time.Second
	// DefaultDialTimeout is the maximum amount of time a dial will wait
	// for a connect to complete.
	DefaultDialTimeout = 30 * time.Second
	// DefaultResponseHeaderTimeout is the maximum time to wait for a
	// response header from the server after sending the request.
	DefaultResponseHeaderTimeout = 0
	// DefaultTimeout is the end-to-end request timeout applied to the
	// [http.Client] when [Config.Timeout] is zero.
	DefaultTimeout = 30 * time.Second
)

// Config configures the pooled HTTP client built by [NewPoolClient].
// All fields are optional; zero values fall back to the package
// defaults documented above.
type Config struct {
	// Timeout is the end-to-end request timeout applied to the
	// returned [http.Client]. Zero or negative selects
	// [DefaultTimeout].
	Timeout time.Duration
	// MaxIdleConns caps the total idle connections across all hosts.
	// Zero or negative selects [DefaultMaxIdleConns].
	MaxIdleConns int
	// MaxIdleConnsPerHost caps the idle connections per host. Zero or
	// negative selects [DefaultMaxIdleConnsPerHost].
	MaxIdleConnsPerHost int
	// IdleConnTimeout is how long an idle connection remains in the
	// pool. Zero or negative selects [DefaultIdleConnTimeout].
	IdleConnTimeout time.Duration
	// TLSHandshakeTimeout bounds the TLS handshake. Zero or negative
	// selects [DefaultTLSHandshakeTimeout].
	TLSHandshakeTimeout time.Duration
	// DialTimeout bounds the TCP dial. Zero or negative selects
	// [DefaultDialTimeout].
	DialTimeout time.Duration
	// TLSConfig is applied to the transport. When nil a default
	// [tls.Config] with [tls.VersionTLS12] as the minimum version is
	// used. Callers that need InsecureSkipVerify (for self-signed
	// probes) may set it here.
	TLSConfig *tls.Config
}

// NewTransport builds a pooled [*http.Transport] configured from cfg.
// The returned transport is safe for concurrent use and may be shared
// across multiple [*http.Client] values.
//
// Use [NewPoolClient] when a ready-to-use [*http.Client] is needed;
// use [NewTransport] when composing with a custom [*http.Client] (for
// example to set a per-request timeout via context only).
func NewTransport(cfg Config) *http.Transport {
	resolve := func(v, def int) int {
		if v > 0 {
			return v
		}
		return def
	}
	resolveDur := func(v, def time.Duration) time.Duration {
		if v > 0 {
			return v
		}
		return def
	}

	tlsConfig := cfg.TLSConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	dialer := &net.Dialer{
		Timeout:   resolveDur(cfg.DialTimeout, DefaultDialTimeout),
		KeepAlive: 30 * time.Second,
	}

	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          resolve(cfg.MaxIdleConns, DefaultMaxIdleConns),
		MaxIdleConnsPerHost:   resolve(cfg.MaxIdleConnsPerHost, DefaultMaxIdleConnsPerHost),
		IdleConnTimeout:       resolveDur(cfg.IdleConnTimeout, DefaultIdleConnTimeout),
		TLSHandshakeTimeout:   resolveDur(cfg.TLSHandshakeTimeout, DefaultTLSHandshakeTimeout),
		ExpectContinueTimeout: DefaultExpectContinueTimeout,
		ForceAttemptHTTP2:     true,
		DialContext:           dialer.DialContext,
		TLSClientConfig:       tlsConfig,
	}
}

// NewPoolClient builds an [*http.Client] backed by a pooled transport
// configured from cfg. The returned client is safe for concurrent use
// and should be reused across requests rather than recreated per call.
//
// When cfg.Timeout is zero or negative [DefaultTimeout] is applied. To
// disable the client-level timeout entirely (for example to rely on
// per-request context deadlines) pass a negative Timeout and clear the
// returned client's Timeout field:
//
//	c := httpx.NewPoolClient(httpx.Config{Timeout: -1})
//	c.Timeout = 0
func NewPoolClient(cfg Config) *http.Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	return &http.Client{
		Transport: NewTransport(cfg),
		Timeout:   timeout,
	}
}
