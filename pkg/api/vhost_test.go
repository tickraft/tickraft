// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package api

import "testing"

// TestVhostExactMatch verifies exact hostname matching takes priority.
func TestVhostExactMatch(t *testing.T) {
	r := NewVirtualHostRouter(VirtualHostConfig{
		Enabled: true,
		Default: "default-group",
		Hosts: map[string]string{
			"app.tickraft.com": "app-group",
		},
	})

	if got := r.Dispatch("app.tickraft.com"); got != "app-group" {
		t.Fatalf("exact match: got %q, want %q", got, "app-group")
	}
}

// TestVhostWildcardMatch verifies "*.domain" wildcard matching.
func TestVhostWildcardMatch(t *testing.T) {
	r := NewVirtualHostRouter(VirtualHostConfig{
		Enabled: true,
		Default: "default-group",
		Hosts: map[string]string{
			"*.cn.tickraft.com": "cn-group",
		},
	})

	cases := []struct {
		host string
		want string
	}{
		{"api.cn.tickraft.com", "cn-group"},
		{"api.cn.tickraft.com:8080", "cn-group"},
		// Wildcard matches exactly one label.
		{"cn.tickraft.com", "default-group"},
		{"a.b.cn.tickraft.com", "default-group"},
	}
	for _, c := range cases {
		if got := r.Dispatch(c.host); got != c.want {
			t.Fatalf("wildcard match %q: got %q, want %q", c.host, got, c.want)
		}
	}
}

// TestVhostPortStripping verifies that a purely-numeric port suffix is
// stripped before matching.
func TestVhostPortStripping(t *testing.T) {
	r := NewVirtualHostRouter(VirtualHostConfig{
		Enabled: true,
		Default: "default-group",
		Hosts: map[string]string{
			"app.tickraft.com": "app-group",
		},
	})

	if got := r.Dispatch("app.tickraft.com:8080"); got != "app-group" {
		t.Fatalf("port stripping: got %q, want %q", got, "app-group")
	}
	// Non-numeric port suffix is not stripped, so no exact match.
	if got := r.Dispatch("app.tickraft.com:abc"); got != "default-group" {
		t.Fatalf("non-numeric port: got %q, want %q", got, "default-group")
	}
}

// TestVhostIPv6Safety verifies stripPort preserves IPv6 literals both with
// and without a port, and that Dispatch does not corrupt them.
func TestVhostIPv6Safety(t *testing.T) {
	// Direct stripPort assertions.
	cases := map[string]string{
		"[2001:db8::1]:8080": "[2001:db8::1]",
		"[2001:db8::1]":      "[2001:db8::1]",
		// Bare IPv6 literal (no brackets) is left intact.
		"2001:db8::1": "2001:db8::1",
		"::1":         "::1",
	}
	for in, want := range cases {
		if got := stripPort(in); got != want {
			t.Fatalf("stripPort(%q): got %q, want %q", in, got, want)
		}
	}

	// Dispatch with an IPv6 host that has a configured exact match.
	r := NewVirtualHostRouter(VirtualHostConfig{
		Enabled: true,
		Default: "default-group",
		Hosts: map[string]string{
			"[2001:db8::1]": "v6-group",
		},
	})
	if got := r.Dispatch("[2001:db8::1]:8080"); got != "v6-group" {
		t.Fatalf("ipv6 dispatch with port: got %q, want %q", got, "v6-group")
	}
	if got := r.Dispatch("[2001:db8::1]"); got != "v6-group" {
		t.Fatalf("ipv6 dispatch without port: got %q, want %q", got, "v6-group")
	}
}

// TestVhostDefaultFallback verifies unmatched hosts fall back to Default.
func TestVhostDefaultFallback(t *testing.T) {
	r := NewVirtualHostRouter(VirtualHostConfig{
		Enabled: true,
		Default: "default-group",
		Hosts: map[string]string{
			"app.tickraft.com": "app-group",
		},
	})

	if got := r.Dispatch("unknown.tickraft.com"); got != "default-group" {
		t.Fatalf("default fallback: got %q, want %q", got, "default-group")
	}
	if got := r.Dispatch(""); got != "default-group" {
		t.Fatalf("empty host fallback: got %q, want %q", got, "default-group")
	}
}

// TestVhostDisabledZeroOverhead verifies that when Enabled is false Dispatch
// returns Default directly without consulting Hosts.
func TestVhostDisabledZeroOverhead(t *testing.T) {
	r := NewVirtualHostRouter(VirtualHostConfig{
		Enabled: false,
		Default: "default-group",
		Hosts: map[string]string{
			"app.tickraft.com": "app-group",
		},
	})

	// Even an exact-match host must return Default when disabled.
	if got := r.Dispatch("app.tickraft.com"); got != "default-group" {
		t.Fatalf("disabled dispatch: got %q, want %q", got, "default-group")
	}
	if r.Default() != "default-group" {
		t.Fatalf("Default() accessor: got %q", r.Default())
	}
	if r.Enabled() {
		t.Fatalf("Enabled() should be false")
	}
}

// TestVhostExactBeatsWildcard verifies the exact-match priority over
// wildcard patterns when both could apply.
func TestVhostExactBeatsWildcard(t *testing.T) {
	r := NewVirtualHostRouter(VirtualHostConfig{
		Enabled: true,
		Default: "default-group",
		Hosts: map[string]string{
			"*.tickraft.com":   "wild-group",
			"app.tickraft.com": "app-group",
		},
	})

	if got := r.Dispatch("app.tickraft.com"); got != "app-group" {
		t.Fatalf("exact over wildcard: got %q, want %q", got, "app-group")
	}
	if got := r.Dispatch("api.tickraft.com"); got != "wild-group" {
		t.Fatalf("wildcard fallback: got %q, want %q", got, "wild-group")
	}
}
