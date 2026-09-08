// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package httpclient builds *http.Client instances for the
// notification channels with region-aware timeouts and optional proxy
// support. Channels targeting domestic endpoints (e.g. Feishu) use a
// shorter default timeout, while international channels (e.g. Discord,
// Telegram) use a longer default that can be further tuned or routed
// through a proxy for reachability.
package httpclient

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tickraft/tickraft/pkg/httpx"
)

// Region selects the default timeout policy for a client.
type Region string

const (
	// RegionCN applies the domestic default timeout (10s).
	RegionCN Region = "cn"
	// RegionGlobal applies the international default timeout (15s).
	RegionGlobal Region = "global"
)

// Default timeouts applied when Config.Timeout is not set. Domestic
// endpoints are expected to be low-latency; international endpoints use a
// longer default to tolerate cross-border round trips.
const (
	defaultCNTimeout     = 10 * time.Second
	defaultGlobalTimeout = 15 * time.Second
)

// Sentinel errors returned by New. Use errors.Is to distinguish failure
// causes.
var (
	// ErrInvalidProxyURL is returned when the configured ProxyURL cannot be
	// parsed.
	ErrInvalidProxyURL = errors.New("httpclient: invalid proxy url")
	// ErrUnsupportedProxyScheme is returned when the configured ProxyURL
	// uses a scheme other than http, https, or socks5.
	ErrUnsupportedProxyScheme = errors.New("httpclient: unsupported proxy scheme")
)

// Config configures the HTTP client built by New.
type Config struct {
	// ProxyURL is an optional proxy URL. Supported schemes are http,
	// https, and socks5. When empty the client connects directly.
	ProxyURL string
	// ProxyBypass is a NO_PROXY-style list of hostnames and domain
	// suffixes whose requests must bypass ProxyURL and connect directly.
	// An entry matches a request host exactly or as a domain suffix, so
	// "corp.example.com" matches "mail.corp.example.com". Empty or blank
	// entries are ignored. Effective only when ProxyURL is set.
	ProxyBypass []string
	// Timeout is the total request timeout. When zero or negative a
	// region-aware default is applied.
	Timeout time.Duration
	// Region selects the default timeout policy when Timeout is not set.
	// RegionCN yields a 10s default; any other value (including the empty
	// string) yields the 15s global default.
	Region Region
}

// New builds an *http.Client from cfg. The client always uses a pooled
// transport sourced from [httpx.NewTransport] so that idle connections
// are reused across channels. When ProxyURL is empty the pooled defaults
// apply directly. When ProxyURL is set it is parsed and must use the
// http, https, or socks5 scheme; the resulting transport inherits the
// pooled-transport defaults and routes requests through the proxy,
// except for hosts matching ProxyBypass, which connect directly.
func New(cfg Config) (*http.Client, error) {
	timeout := resolveTimeout(cfg)

	transportCfg := httpx.Config{
		Timeout:   timeout,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}

	if cfg.ProxyURL == "" {
		return httpx.NewPoolClient(transportCfg), nil
	}

	parsed, err := url.Parse(cfg.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidProxyURL, err)
	}
	if !isSupportedProxyScheme(parsed.Scheme) {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedProxyScheme, parsed.Scheme)
	}

	// Inherit the pooled-transport defaults (idle conn caps, TLS
	// handshake budget, dial timeout) and layer the proxy on top.
	transport := httpx.NewTransport(transportCfg)
	if len(cfg.ProxyBypass) > 0 {
		transport.Proxy = func(req *http.Request) (*url.URL, error) {
			if Bypass(req.URL.Hostname(), cfg.ProxyBypass) {
				// Returning nil, nil is the net/http proxy-function
				// contract for "connect directly".
				//nolint:nilnil // ProxyFunc contract: nil proxy = direct connection
				return nil, nil
			}
			return parsed, nil
		}
	} else {
		transport.Proxy = http.ProxyURL(parsed)
	}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}

// ValidateProxyURL reports whether proxyURL is empty or a well-formed
// proxy URL with a supported scheme (http, https, socks5). Channel
// configs expose a user-facing proxy_url field and call this from their
// Validate so a malformed value fails at the API boundary instead of at
// channel build time.
func ValidateProxyURL(proxyURL string) error {
	if proxyURL == "" {
		return nil
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidProxyURL, err)
	}
	if !isSupportedProxyScheme(parsed.Scheme) {
		return fmt.Errorf("%w: %q", ErrUnsupportedProxyScheme, parsed.Scheme)
	}
	return nil
}

// Bypass reports whether host matches any entry in the NO_PROXY-style
// bypass list. An entry matches when it equals the host or is a domain
// suffix of it ("corp.example.com" matches "mail.corp.example.com").
// Blank entries are ignored; an empty list never matches.
func Bypass(host string, entries []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, entry := range entries {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if host == entry || strings.HasSuffix(host, "."+entry) {
			return true
		}
	}
	return false
}

// resolveTimeout returns the effective timeout: an explicit positive
// Timeout takes precedence, otherwise the region default is used.
func resolveTimeout(cfg Config) time.Duration {
	if cfg.Timeout > 0 {
		return cfg.Timeout
	}
	if cfg.Region == RegionCN {
		return defaultCNTimeout
	}
	return defaultGlobalTimeout
}

// isSupportedProxyScheme reports whether scheme is one of the proxy
// schemes accepted by New.
func isSupportedProxyScheme(scheme string) bool {
	switch scheme {
	case "http", "https", "socks5":
		return true
	}
	return false
}
