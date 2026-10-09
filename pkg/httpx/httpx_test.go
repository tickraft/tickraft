// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httpx

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"
)

func TestNewPoolClient_Defaults(t *testing.T) {
	c := NewPoolClient(Config{})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.Timeout != DefaultTimeout {
		t.Fatalf("expected default timeout %v, got %v", DefaultTimeout, c.Timeout)
	}

	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", c.Transport)
	}
	if tr.MaxIdleConns != DefaultMaxIdleConns {
		t.Fatalf("expected MaxIdleConns %d, got %d", DefaultMaxIdleConns, tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != DefaultMaxIdleConnsPerHost {
		t.Fatalf("expected MaxIdleConnsPerHost %d, got %d", DefaultMaxIdleConnsPerHost, tr.MaxIdleConnsPerHost)
	}
	if tr.IdleConnTimeout != DefaultIdleConnTimeout {
		t.Fatalf("expected IdleConnTimeout %v, got %v", DefaultIdleConnTimeout, tr.IdleConnTimeout)
	}
	if tr.TLSHandshakeTimeout != DefaultTLSHandshakeTimeout {
		t.Fatalf("expected TLSHandshakeTimeout %v, got %v", DefaultTLSHandshakeTimeout, tr.TLSHandshakeTimeout)
	}
	if tr.TLSClientConfig == nil {
		t.Fatal("expected non-nil TLSClientConfig")
	}
	if tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected TLS 1.2 minimum, got %x", tr.TLSClientConfig.MinVersion)
	}
	if !tr.ForceAttemptHTTP2 {
		t.Fatal("expected ForceAttemptHTTP2=true")
	}
}

func TestNewPoolClient_Overrides(t *testing.T) {
	cfg := Config{
		Timeout:             7 * time.Second,
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 25,
		IdleConnTimeout:     2 * time.Minute,
		TLSHandshakeTimeout: 3 * time.Second,
		DialTimeout:         5 * time.Second,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	c := NewPoolClient(cfg)
	if c.Timeout != 7*time.Second {
		t.Fatalf("expected 7s timeout, got %v", c.Timeout)
	}
	tr := c.Transport.(*http.Transport)
	if tr.MaxIdleConns != 200 {
		t.Fatalf("expected MaxIdleConns 200, got %d", tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != 25 {
		t.Fatalf("expected MaxIdleConnsPerHost 25, got %d", tr.MaxIdleConnsPerHost)
	}
	if tr.IdleConnTimeout != 2*time.Minute {
		t.Fatalf("expected IdleConnTimeout 2m, got %v", tr.IdleConnTimeout)
	}
	if !tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("expected InsecureSkipVerify=true from custom TLSConfig")
	}
}

func TestNewTransport_NegativeValuesFallBack(t *testing.T) {
	tr := NewTransport(Config{
		MaxIdleConns:        -1,
		MaxIdleConnsPerHost: -5,
		IdleConnTimeout:     -time.Second,
	})
	if tr.MaxIdleConns != DefaultMaxIdleConns {
		t.Fatalf("expected fallback to default, got %d", tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != DefaultMaxIdleConnsPerHost {
		t.Fatalf("expected fallback to default, got %d", tr.MaxIdleConnsPerHost)
	}
	if tr.IdleConnTimeout != DefaultIdleConnTimeout {
		t.Fatalf("expected fallback to default, got %v", tr.IdleConnTimeout)
	}
}
