// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cert

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestNoopDNSProviderPresent verifies that NoopDNSProvider.Present returns
// ErrDNSChallengeNotConfigured, so a DNS-01 flow without a real provider
// fails fast with a clear error instead of silently succeeding.
func TestNoopDNSProviderPresent(t *testing.T) {
	p := NoopDNSProvider{}
	err := p.Present(context.Background(), "example.com", "token", "value")
	if !errors.Is(err, ErrDNSChallengeNotConfigured) {
		t.Fatalf("Present error = %v, want wrapping %v", err, ErrDNSChallengeNotConfigured)
	}
}

// TestNoopDNSProviderCleanUp verifies that NoopDNSProvider.CleanUp returns
// nil, since cleanup is always safe to skip.
func TestNoopDNSProviderCleanUp(t *testing.T) {
	p := NoopDNSProvider{}
	if err := p.CleanUp(context.Background(), "example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp error = %v, want nil", err)
	}
}

// TestNoopDNSProviderTimeout verifies that NoopDNSProvider.Timeout returns
// (0, 0), signalling the caller to use its own defaults.
func TestNoopDNSProviderTimeout(t *testing.T) {
	p := NoopDNSProvider{}
	interval, timeout := p.Timeout()
	if interval != 0 || timeout != 0 {
		t.Errorf("Timeout = (%v, %v), want (0, 0)", interval, timeout)
	}
}

// TestNewManagerDefaultDNSProvider verifies that NewManager without options
// uses NoopDNSProvider as the default DNS provider.
func TestNewManagerDefaultDNSProvider(t *testing.T) {
	mgr := NewManager()
	if mgr.DNSProvider() == nil {
		t.Fatal("DNSProvider() = nil, want non-nil")
	}
	// The default must be NoopDNSProvider (value type, so compare by type).
	if _, ok := mgr.DNSProvider().(NoopDNSProvider); !ok {
		t.Errorf("DNSProvider() type = %T, want NoopDNSProvider", mgr.DNSProvider())
	}
}

// TestWithDNSProvider verifies that WithDNSProvider injects the given DNS
// provider into the Manager, replacing the NoopDNSProvider default.
func TestWithDNSProvider(t *testing.T) {
	stub := &stubDNSProvider{
		interval: 2 * time.Second,
		timeout:  30 * time.Second,
	}
	mgr := NewManager(WithDNSProvider(stub))
	if mgr.DNSProvider() != stub {
		t.Errorf("DNSProvider() = %T, want %T", mgr.DNSProvider(), stub)
	}
}

// TestWithDNSProviderNil verifies that passing nil to WithDNSProvider keeps
// the NoopDNSProvider default, so a nil injection cannot wipe the default.
func TestWithDNSProviderNil(t *testing.T) {
	mgr := NewManager(WithDNSProvider(nil))
	if _, ok := mgr.DNSProvider().(NoopDNSProvider); !ok {
		t.Errorf("DNSProvider() type = %T, want NoopDNSProvider", mgr.DNSProvider())
	}
}

// TestManagerWithRealProviderEndToEnd verifies the full Manager interface flow:
// a real (stub) provider is injected, Present returns nil, CleanUp returns
// nil, and Timeout returns the configured values. This simulates how an
// extended edition would inject a DNS provider.
func TestManagerWithRealProviderEndToEnd(t *testing.T) {
	stub := &stubDNSProvider{
		interval: 5 * time.Second,
		timeout:  60 * time.Second,
	}
	mgr := NewManager(WithDNSProvider(stub))
	provider := mgr.DNSProvider()

	ctx := context.Background()
	if err := provider.Present(ctx, "example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if err := provider.CleanUp(ctx, "example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	interval, timeout := provider.Timeout()
	if interval != 5*time.Second || timeout != 60*time.Second {
		t.Errorf("Timeout = (%v, %v), want (%v, %v)", interval, timeout, 5*time.Second, 60*time.Second)
	}

	// Verify the stub recorded the Present call.
	if stub.presentCalls != 1 {
		t.Errorf("presentCalls = %d, want 1", stub.presentCalls)
	}
	if stub.cleanupCalls != 1 {
		t.Errorf("cleanupCalls = %d, want 1", stub.cleanupCalls)
	}
}

// stubDNSProvider is a test stub for the DNSProvider interface. It records calls
// and returns configurable values, simulating a real DNS provider injected
// by an extended edition.
type stubDNSProvider struct {
	presentCalls int
	cleanupCalls int
	presentErr   error
	cleanupErr   error
	interval     time.Duration
	timeout      time.Duration
}

// Present satisfies DNSProvider.
func (s *stubDNSProvider) Present(_ context.Context, _, _, _ string) error {
	s.presentCalls++
	return s.presentErr
}

// CleanUp satisfies DNSProvider.
func (s *stubDNSProvider) CleanUp(_ context.Context, _, _, _ string) error {
	s.cleanupCalls++
	return s.cleanupErr
}

// Timeout satisfies DNSProvider.
func (s *stubDNSProvider) Timeout() (time.Duration, time.Duration) {
	return s.interval, s.timeout
}

// Compile-time assertion that stubDNSProvider satisfies DNSProvider.
var _ DNSProvider = (*stubDNSProvider)(nil)
