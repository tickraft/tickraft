// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httpclient

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Direct connection (no proxy): region-aware timeouts
// ---------------------------------------------------------------------------

func TestNew_DirectCN(t *testing.T) {
	client, err := New(Config{Region: RegionCN})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.Timeout != defaultCNTimeout {
		t.Errorf("timeout: got %v, want %v", client.Timeout, defaultCNTimeout)
	}
	// Direct connection uses the pooled transport (*http.Transport) with
	// TLS 1.2 enforced; no custom proxy is configured.
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport: expected *http.Transport, got %T", client.Transport)
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("TLSClientConfig MinVersion: got %v, want TLS 1.2", tr.TLSClientConfig)
	}
}

func TestNew_DirectGlobal(t *testing.T) {
	client, err := New(Config{Region: RegionGlobal})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.Timeout != defaultGlobalTimeout {
		t.Errorf("timeout: got %v, want %v", client.Timeout, defaultGlobalTimeout)
	}
}

func TestNew_EmptyConfigDefaultsToGlobal(t *testing.T) {
	client, err := New(Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.Timeout != defaultGlobalTimeout {
		t.Errorf("timeout: got %v, want %v", client.Timeout, defaultGlobalTimeout)
	}
}

func TestNew_CustomTimeoutOverridesRegion(t *testing.T) {
	client, err := New(Config{Timeout: 5 * time.Second, Region: RegionCN})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.Timeout != 5*time.Second {
		t.Errorf("timeout: got %v, want %v", client.Timeout, 5*time.Second)
	}
}

// ---------------------------------------------------------------------------
// Proxy configuration
// ---------------------------------------------------------------------------

func TestNew_HTTPProxy(t *testing.T) {
	proxy := "http://proxy.example.com:8080"
	client, err := New(Config{ProxyURL: proxy, Region: RegionGlobal})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.Timeout != defaultGlobalTimeout {
		t.Errorf("timeout: got %v, want %v", client.Timeout, defaultGlobalTimeout)
	}
	verifyProxy(t, client, proxy)
}

func TestNew_HTTPSProxy(t *testing.T) {
	proxy := "https://proxy.example.com:8443"
	client, err := New(Config{ProxyURL: proxy, Region: RegionCN})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	verifyProxy(t, client, proxy)
}

func TestNew_Socks5Proxy(t *testing.T) {
	proxy := "socks5://proxy.example.com:1080"
	client, err := New(Config{ProxyURL: proxy, Region: RegionGlobal})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	verifyProxy(t, client, proxy)
}

// ---------------------------------------------------------------------------
// Error handling
// ---------------------------------------------------------------------------

func TestNew_InvalidProxyURL(t *testing.T) {
	// An unclosed IPv6 literal forces a url.Parse failure.
	_, err := New(Config{ProxyURL: "http://[::1"})
	if err == nil {
		t.Fatal("expected error for invalid proxy url")
	}
	if !errors.Is(err, ErrInvalidProxyURL) {
		t.Errorf("error should wrap ErrInvalidProxyURL, got %v", err)
	}
}

func TestNew_UnsupportedProxyScheme(t *testing.T) {
	_, err := New(Config{ProxyURL: "ftp://proxy.example.com:21"})
	if err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
	if !errors.Is(err, ErrUnsupportedProxyScheme) {
		t.Errorf("error should wrap ErrUnsupportedProxyScheme, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// verifyProxy asserts that client.Transport is an *http.Transport whose
// Proxy function returns the configured proxy URL for an arbitrary
// request.
func verifyProxy(t *testing.T, client *http.Client, proxyURL string) {
	t.Helper()
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport: expected *http.Transport, got %T", client.Transport)
	}
	if tr.Proxy == nil {
		t.Fatal("transport Proxy function is nil")
	}
	req := &http.Request{URL: &url.URL{Scheme: "http", Host: "example.com"}}
	got, err := tr.Proxy(req)
	if err != nil {
		t.Fatalf("Proxy() returned error: %v", err)
	}
	if got.String() != proxyURL {
		t.Errorf("proxy url: got %q, want %q", got.String(), proxyURL)
	}
	if tr.TLSClientConfig == nil {
		t.Error("TLSClientConfig should be set when a proxy is configured")
	}
}

// ---------------------------------------------------------------------------
// Proxy bypass (NO_PROXY-style matching)
// ---------------------------------------------------------------------------

func TestBypass(t *testing.T) {
	entries := []string{"corp.example.com", "intranet", " 10.0.0.1 "}
	cases := []struct {
		host string
		want bool
	}{
		{"corp.example.com", true},      // exact match
		{"mail.corp.example.com", true}, // domain suffix
		{"notcorp.example.com", false},  // partial suffix is not a match
		{"evilcorp.example.com", false}, // suffix must respect label boundary
		{"intranet", true},              // bare hostname
		{"10.0.0.1", true},              // IP with padded entry
		{"10.0.0.2", false},             // different IP
		{"", false},                     // empty host never matches
		{"example.com", false},          // unrelated host
	}
	for _, tc := range cases {
		if got := Bypass(tc.host, entries); got != tc.want {
			t.Errorf("Bypass(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
	if Bypass("corp.example.com", nil) {
		t.Error("empty bypass list must never match")
	}
	if Bypass("corp.example.com", []string{"  ", ""}) {
		t.Error("blank entries must be ignored")
	}
}

func TestNew_ProxyBypassRoutesMatchingHostDirect(t *testing.T) {
	proxy := "http://proxy.example.com:8080"
	client, err := New(Config{
		ProxyURL:    proxy,
		ProxyBypass: []string{"intranet.local"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport: expected *http.Transport, got %T", client.Transport)
	}

	bypassed := &http.Request{URL: &url.URL{Scheme: "http", Host: "mail.intranet.local"}}
	got, err := tr.Proxy(bypassed)
	if err != nil {
		t.Fatalf("Proxy() returned error: %v", err)
	}
	if got != nil {
		t.Errorf("bypassed host should connect directly, got proxy %q", got.String())
	}

	direct := &http.Request{URL: &url.URL{Scheme: "http", Host: "example.com"}}
	got, err = tr.Proxy(direct)
	if err != nil {
		t.Fatalf("Proxy() returned error: %v", err)
	}
	if got == nil || got.String() != proxy {
		t.Errorf("non-bypassed host should use proxy, got %v", got)
	}
}

func TestValidateProxyURL(t *testing.T) {
	for _, valid := range []string{"", "http://proxy:8080", "https://proxy:8443", "socks5://proxy:1080"} {
		if err := ValidateProxyURL(valid); err != nil {
			t.Errorf("ValidateProxyURL(%q) = %v, want nil", valid, err)
		}
	}
	for _, invalid := range []string{"ftp://proxy:21", "not a url", "http://[::1"} {
		if err := ValidateProxyURL(invalid); err == nil {
			t.Errorf("ValidateProxyURL(%q) = nil, want error", invalid)
		}
	}
}
