// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// This file implements the trusted-proxy middleware (design doc chapter 5):
// resolving the real client IP from X-Forwarded-For only when the direct
// peer is inside a configured CIDR allowlist.

package middleware

import (
	"context"
	"net"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"

	"github.com/tickraft/tickraft/pkg/api/httputil"
)

// xForwardedFor is the canonical header carrying the original client IP
// chain established by forward proxies.
const xForwardedFor = "X-Forwarded-For"

// NewTrustedProxyMiddleware returns a middleware that resolves the real
// client IP based on a trusted-proxy CIDR allowlist.
//
// Resolution rules:
//   - The direct peer address is taken from arc.RemoteAddr().
//   - If the peer IP is inside one of cidrs (trusted proxy), the first
//     (leftmost) IP from the X-Forwarded-For header is used as the real
//     client IP.
//   - If the peer IP is NOT in cidrs (untrusted), X-Forwarded-For is
//     ignored and the peer IP itself is used.
//   - The resolved IP is stored in the request context via
//     httputil.SetClientIP so downstream handlers and loggers read it
//     through httputil.GetClientIP.
//
// Invalid CIDR entries are logged and skipped at construction time; the
// middleware never panics. An empty cidrs list means no proxy is trusted,
// so the peer IP is always used.
func NewTrustedProxyMiddleware(cidrs []string) app.HandlerFunc {
	nets := parseCIDRs(cidrs)
	return func(ctx context.Context, arc *app.RequestContext) {
		peerIP := hostFromAddr(arc.RemoteAddr().String())
		realIP := peerIP

		if cidrContains(nets, peerIP) {
			if xff := arc.GetHeader(xForwardedFor); len(xff) > 0 {
				if ip := firstXFF(string(xff)); ip != "" {
					realIP = ip
				}
			}
		}

		httputil.SetClientIP(arc, realIP)
		arc.Next(ctx)
	}
}

// parseCIDRs parses a list of CIDR strings into net.IPNet entries. Invalid
// entries are logged at error level and skipped; the returned slice may be
// empty if no entry is valid.
func parseCIDRs(cidrs []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		// net.ParseCIDR accepts both "10.0.0.0/8" and a bare IP with
		// full mask. Single IPs are normalized to /32 or /128 by the
		// caller; here we rely on the caller to supply proper CIDRs.
		_, ipNet, err := net.ParseCIDR(c)
		if err != nil {
			hlog.Errorf("trusted_proxy: invalid cidr %q: %v", c, err)
			continue
		}
		nets = append(nets, ipNet)
	}
	return nets
}

// cidrContains reports whether ipStr is a valid IP contained in any of the
// networks. Returns false if ipStr is not parseable as an IP.
func cidrContains(nets []*net.IPNet, ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// hostFromAddr extracts the host portion from an address that may be
// "host:port" or "[ipv6]:port". It returns the original string when the
// address cannot be split (for example, a bare hostname or IPv6 literal
// without a port).
func hostFromAddr(addr string) string {
	if addr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// firstXFF returns the first (leftmost) IP from an X-Forwarded-For header
// value, trimmed of surrounding whitespace. Returns an empty string when
// the header value is empty or contains only whitespace.
func firstXFF(xff string) string {
	parts := strings.SplitN(xff, ",", 2)
	return strings.TrimSpace(parts[0])
}
