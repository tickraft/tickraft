// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cert

import (
	"context"
	"errors"
	"testing"
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
	if err := p.Cleanup(context.Background(), "example.com", "token", "value"); err != nil {
		t.Fatalf("Cleanup error = %v, want nil", err)
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
