// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package middleware

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/tickraft/tickraft/pkg/api/httputil"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
)

// TestParseCIDRsValid verifies that valid CIDR strings are parsed into
// net.IPNet entries in the same order they were supplied.
func TestParseCIDRsValid(t *testing.T) {
	cidrs := []string{"10.0.0.0/8", "192.168.0.0/16", "127.0.0.0/8"}
	nets := parseCIDRs(cidrs)
	if len(nets) != 3 {
		t.Fatalf("len(nets) = %d, want 3", len(nets))
	}
	if nets[0].String() != "10.0.0.0/8" {
		t.Errorf("nets[0] = %q, want 10.0.0.0/8", nets[0].String())
	}
	if nets[1].String() != "192.168.0.0/16" {
		t.Errorf("nets[1] = %q, want 192.168.0.0/16", nets[1].String())
	}
	if nets[2].String() != "127.0.0.0/8" {
		t.Errorf("nets[2] = %q, want 127.0.0.0/8", nets[2].String())
	}
}

// TestParseCIDRsInvalidSkipped verifies that invalid CIDR entries are skipped
// while valid ones before and after them are retained.
func TestParseCIDRsInvalidSkipped(t *testing.T) {
	cidrs := []string{"10.0.0.0/8", "not-a-cidr", "192.168.0.0/16", "300.1.2.3/24"}
	nets := parseCIDRs(cidrs)
	if len(nets) != 2 {
		t.Fatalf("len(nets) = %d, want 2 (invalid entries skipped)", len(nets))
	}
	if nets[0].String() != "10.0.0.0/8" {
		t.Errorf("nets[0] = %q, want 10.0.0.0/8", nets[0].String())
	}
	if nets[1].String() != "192.168.0.0/16" {
		t.Errorf("nets[1] = %q, want 192.168.0.0/16", nets[1].String())
	}
}

// TestParseCIDRsEmpty verifies that an empty input yields an empty (non-nil)
// slice and never panics.
func TestParseCIDRsEmpty(t *testing.T) {
	nets := parseCIDRs(nil)
	if len(nets) != 0 {
		t.Fatalf("len(nets) = %d, want 0", len(nets))
	}
	nets = parseCIDRs([]string{})
	if len(nets) != 0 {
		t.Fatalf("len(nets) = %d, want 0", len(nets))
	}
}

// TestCIDRContains verifies containment checks for both IPv4 inside a CIDR
// and outside it, plus an unparseable IP which must return false.
func TestCIDRContains(t *testing.T) {
	nets := parseCIDRs([]string{"10.0.0.0/8", "127.0.0.0/8"})

	cases := []struct {
		name   string
		ipStr  string
		expect bool
	}{
		{"inside first cidr", "10.1.2.3", true},
		{"inside second cidr", "127.0.0.1", true},
		{"outside all cidrs", "192.168.1.1", false},
		{"public ip outside", "203.0.113.5", false},
		{"invalid ip", "not-an-ip", false},
		{"empty ip", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cidrContains(nets, c.ipStr)
			if got != c.expect {
				t.Errorf("cidrContains(%q) = %v, want %v", c.ipStr, got, c.expect)
			}
		})
	}
}

// TestCIDRContainsEmptyNets verifies that an empty net slice always returns
// false (no proxy is trusted).
func TestCIDRContainsEmptyNets(t *testing.T) {
	if cidrContains(nil, "10.0.0.1") {
		t.Error("cidrContains(nil, ...) = true, want false")
	}
	if cidrContains([]*net.IPNet{}, "127.0.0.1") {
		t.Error("cidrContains(empty, ...) = true, want false")
	}
}

// TestHostFromAddr verifies extraction of the host portion from host:port,
// [ipv6]:port, and bare addresses that cannot be split.
func TestHostFromAddr(t *testing.T) {
	cases := []struct {
		name   string
		addr   string
		expect string
	}{
		{"ipv4 with port", "1.2.3.4:5678", "1.2.3.4"},
		{"loopback with port", "127.0.0.1:8080", "127.0.0.1"},
		{"ipv6 with port", "[::1]:8080", "::1"},
		{"ipv6 full with port", "[2001:db8::1]:443", "2001:db8::1"},
		{"bare ipv4 no port", "1.2.3.4", "1.2.3.4"},
		{"bare ipv6 no port", "::1", "::1"},
		{"empty string", "", ""},
		{"hostname no port", "example.com", "example.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := hostFromAddr(c.addr)
			if got != c.expect {
				t.Errorf("hostFromAddr(%q) = %q, want %q", c.addr, got, c.expect)
			}
		})
	}
}

// TestFirstXFF verifies that the leftmost IP in an X-Forwarded-For header
// value is extracted and trimmed of surrounding whitespace.
func TestFirstXFF(t *testing.T) {
	cases := []struct {
		name   string
		xff    string
		expect string
	}{
		{"single ip", "203.0.113.5", "203.0.113.5"},
		{"two ips", "203.0.113.5, 10.0.0.1", "203.0.113.5"},
		{"three ips", "203.0.113.5, 10.0.0.1, 192.168.1.1", "203.0.113.5"},
		{"leading whitespace", "  203.0.113.5  , 10.0.0.1", "203.0.113.5"},
		{"empty string", "", ""},
		{"only whitespace", "   ", ""},
		{"trailing comma", "203.0.113.5,", "203.0.113.5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := firstXFF(c.xff)
			if got != c.expect {
				t.Errorf("firstXFF(%q) = %q, want %q", c.xff, got, c.expect)
			}
		})
	}
}

// TestNewTrustedProxyMiddlewareDoesNotPanic verifies that middleware
// construction never panics regardless of input, including malformed CIDRs
// and empty slices.
func TestNewTrustedProxyMiddlewareDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewTrustedProxyMiddleware panicked: %v", r)
		}
	}()
	_ = NewTrustedProxyMiddleware(nil)
	_ = NewTrustedProxyMiddleware([]string{})
	_ = NewTrustedProxyMiddleware([]string{"not-a-cidr"})
	_ = NewTrustedProxyMiddleware([]string{"10.0.0.0/8", "garbage", "127.0.0.0/8"})
}

// newTrustedProxyTestEngine builds a route engine with the given middleware
// and a handler that records the resolved client IP via httputil.GetClientIP.
func newTrustedProxyTestEngine(mw app.HandlerFunc) (engine *route.Engine, clientIP *string) {
	var recorded string
	engine = route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(mw)
	engine.GET("/", func(ctx context.Context, arc *app.RequestContext) {
		recorded = httputil.GetClientIP(arc)
		httputil.Success(arc, nil)
	})
	return engine, &recorded
}

// TestTrustedProxyResolvesXFF verifies that when the direct peer is inside a
// trusted CIDR, the leftmost X-Forwarded-For entry is used as the real
// client IP. The ut.PerformRequest helper creates a synthetic request whose
// RemoteAddr resolves to "0.0.0.0:0", so we configure the middleware to
// trust 0.0.0.0/8 which contains that peer IP.
func TestTrustedProxyResolvesXFF(t *testing.T) {
	mw := NewTrustedProxyMiddleware([]string{"0.0.0.0/8"})
	engine, recorded := newTrustedProxyTestEngine(mw)

	w := ut.PerformRequest(engine, "GET", "/", nil,
		ut.Header{Key: "X-Forwarded-For", Value: "203.0.113.5, 10.0.0.1"})

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if *recorded != "203.0.113.5" {
		t.Errorf("clientIP = %q, want %q (leftmost XFF entry)", *recorded, "203.0.113.5")
	}
}

// TestTrustedProxyIgnoresXFFWhenPeerUntrusted verifies that when the direct
// peer is NOT in the trusted CIDR, the X-Forwarded-For header is ignored
// and the peer IP itself is used. The synthetic peer is "0.0.0.0"; trusting
// only 127.0.0.0/8 makes the peer untrusted.
func TestTrustedProxyIgnoresXFFWhenPeerUntrusted(t *testing.T) {
	mw := NewTrustedProxyMiddleware([]string{"127.0.0.0/8"})
	engine, recorded := newTrustedProxyTestEngine(mw)

	w := ut.PerformRequest(engine, "GET", "/", nil,
		ut.Header{Key: "X-Forwarded-For", Value: "203.0.113.5, 10.0.0.1"})

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	// The peer IP should be used, NOT the XFF value.
	if *recorded == "203.0.113.5" {
		t.Errorf("clientIP = %q, XFF should be ignored for untrusted peer", *recorded)
	}
	if !strings.Contains(*recorded, "0.0.0.0") {
		t.Errorf("clientIP = %q, want to contain the peer IP %q", *recorded, "0.0.0.0")
	}
}

// TestTrustedProxyEmptyCIDRsUsesPeer verifies that an empty CIDR list means
// no proxy is trusted, so the peer IP is always used regardless of any
// X-Forwarded-For header.
func TestTrustedProxyEmptyCIDRsUsesPeer(t *testing.T) {
	mw := NewTrustedProxyMiddleware(nil)
	engine, recorded := newTrustedProxyTestEngine(mw)

	w := ut.PerformRequest(engine, "GET", "/", nil,
		ut.Header{Key: "X-Forwarded-For", Value: "203.0.113.5"})

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if *recorded == "203.0.113.5" {
		t.Errorf("clientIP = %q, XFF should be ignored when no CIDRs are trusted", *recorded)
	}
	if !strings.Contains(*recorded, "0.0.0.0") {
		t.Errorf("clientIP = %q, want to contain the peer IP %q", *recorded, "0.0.0.0")
	}
}
