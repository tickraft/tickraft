// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// This file implements VirtualHost routing
// dispatch (design doc section 7.7): mapping the request Host header to a
// named route group via exact, wildcard, and default fallback matching.

package api

import "strings"

// VirtualHostConfig configures host-based route group dispatch.
//
// When Enabled is true, incoming requests are dispatched to a named route
// group according to the request Host header. Hosts maps a host pattern
// (exact hostname or "*.domain" wildcard) to a registered group name.
// Default names the group used when no Host matches.
type VirtualHostConfig struct {
	// Enabled toggles VirtualHost dispatch. When false, Dispatch returns
	// Default without performing any matching.
	Enabled bool

	// Default is the group name returned when no Host entry matches.
	// It is also returned directly when Enabled is false.
	Default string

	// Hosts maps host patterns to group names. Patterns may be exact
	// (e.g. "app.tickraft.com") or wildcard (e.g. "*.cn.tickraft.com").
	Hosts map[string]string
}

// VirtualHostRouter resolves a request Host header to a registered route
// group name. Matching order: exact match -> wildcard match -> default
// fallback. When disabled, Dispatch short-circuits to Default for zero
// runtime overhead.
type VirtualHostRouter struct {
	hosts        map[string]string
	defaultGroup string
	enabled      bool
}

// NewVirtualHostRouter builds a VirtualHostRouter from the given config.
// A nil Hosts map is handled safely by Dispatch (reads return the zero
// string).
func NewVirtualHostRouter(cfg VirtualHostConfig) *VirtualHostRouter {
	return &VirtualHostRouter{
		hosts:        cfg.Hosts,
		defaultGroup: cfg.Default,
		enabled:      cfg.Enabled,
	}
}

// Enabled reports whether VirtualHost dispatch is active.
func (r *VirtualHostRouter) Enabled() bool {
	return r.enabled
}

// Default returns the configured default group name.
func (r *VirtualHostRouter) Default() string {
	return r.defaultGroup
}

// Dispatch resolves the given Host header value to a registered route group
// name.
//
// Matching order:
//  1. Exact match: the stripped host equals a Hosts key.
//  2. Wildcard match: the stripped host matches a "*.domain" pattern.
//  3. Default fallback: return Default.
//
// When VirtualHost is disabled, Dispatch returns Default immediately
// without any matching work, keeping the disabled path zero-cost.
//
// The host value has its port stripped (when the port suffix is purely
// numeric) before matching. IPv6 literals such as "[2001:db8::1]" and
// "[2001:db8::1]:8080" are preserved correctly (see stripPort).
func (r *VirtualHostRouter) Dispatch(host string) string {
	if !r.enabled {
		return r.defaultGroup
	}

	h := stripPort(host)

	// 1. Exact match.
	if g, ok := r.hosts[h]; ok {
		return g
	}

	// 2. Wildcard match.
	for pattern, group := range r.hosts {
		if matchWildcard(pattern, h) {
			return group
		}
	}

	// 3. Default fallback.
	return r.defaultGroup
}

// stripPort removes a trailing ":<port>" suffix from host when the port
// component is purely numeric. IPv6 literals are preserved:
//
//   - "app.tickraft.com:8080" -> "app.tickraft.com"
//   - "app.tickraft.com"      -> "app.tickraft.com" (unchanged)
//   - "[2001:db8::1]:8080"    -> "[2001:db8::1]"
//   - "[2001:db8::1]"         -> "[2001:db8::1]" (unchanged)
//   - "2001:db8::1"           -> "2001:db8::1" (unchanged; bare IPv6 has
//     multiple colons and is not a host:port pair)
//
// Per RFC 3986, IPv6 addresses in a Host header are wrapped in brackets,
// so bare IPv6 literals without brackets are treated as non-numeric-port
// and returned unchanged.
func stripPort(host string) string {
	// IPv6 literal form: "[addr]" or "[addr]:port".
	if strings.HasPrefix(host, "[") {
		closing := strings.IndexByte(host, ']')
		if closing < 0 {
			// Malformed; leave untouched.
			return host
		}
		rest := host[closing+1:]
		if rest == "" {
			// "[addr]" without port.
			return host
		}
		// "[addr]:port" only when the suffix is ":" followed by digits.
		if strings.HasPrefix(rest, ":") && isAllDigits(rest[1:]) {
			return host[:closing+1]
		}
		return host
	}

	// Non-IPv6 form: strip "host:port" only when there is exactly one
	// colon and the trailing segment is all digits. A bare IPv6 literal
	// (multiple colons) is left intact.
	if strings.Count(host, ":") == 1 {
		if idx := strings.IndexByte(host, ':'); idx >= 0 && isAllDigits(host[idx+1:]) {
			return host[:idx]
		}
	}
	return host
}

// matchWildcard reports whether host matches a DNS wildcard pattern of the
// form "*.domain". A single "*" segment matches exactly one DNS label
// (no dots):
//
//   - "*.cn.tickraft.com" matches "api.cn.tickraft.com"
//   - "*.cn.tickraft.com" does NOT match "cn.tickraft.com" (no label)
//   - "*.cn.tickraft.com" does NOT match "a.b.cn.tickraft.com" (>1 label)
//
// Non-wildcard patterns return false (handled by exact match instead).
func matchWildcard(pattern, host string) bool {
	if !strings.HasPrefix(pattern, "*.") {
		return false
	}
	suffix := pattern[1:] // ".cn.tickraft.com"
	if !strings.HasSuffix(host, suffix) {
		return false
	}
	prefix := host[:len(host)-len(suffix)]
	// prefix must be a single non-empty label (no dots).
	if prefix == "" || strings.Contains(prefix, ".") {
		return false
	}
	return true
}

// isAllDigits reports whether s is non-empty and consists only of ASCII
// digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
