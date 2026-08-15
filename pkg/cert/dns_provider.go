// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cert

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors for DNS-01 challenge operations. Callers may use errors.Is
// to detect a specific failure.
var (
	// ErrDNSChallengeNotConfigured is returned by NoopDNSProvider.Present to
	// indicate that no real DNS provider has been injected. The runtime
	// does not implement DNS-01 challenge issuance; callers
	// inject a real DNS provider via WithDNSProvider.
	ErrDNSChallengeNotConfigured = errors.New("dns-01 challenge provider not configured")
)

// DNSProvider is the interface for DNS-01 challenge providers. The runtime
// provides only the NoopDNSProvider default; callers
// inject a real DNS provider (e.g. cloudflare, route53, alidns) via
// WithDNSProvider to enable ACME DNS-01 challenge issuance.
//
// The interface abstracts only the two operations that differ between DNS
// providers: publishing a TXT record for challenge validation and removing
// it after validation completes. The shared ACME manager (in pkg/api)
// drives the rest of the RFC 8555 flow and delegates TXT record management
// to the configured provider.
//
// Implementations must be safe for concurrent use: the ACME manager may
// renew certificates for multiple domains in parallel.
type DNSProvider interface {
	// Present publishes a DNS TXT record for the given domain with the
	// given value, fulfilling the DNS-01 challenge. The domain is the
	// FQDN being authorized (e.g. "example.com"); the token is the ACME
	// challenge token; the value is the SHA-256 hashed key authorization
	// that the ACME server expects to find at
	// _acme-challenge.<domain>.
	Present(ctx context.Context, domain, token, value string) error

	// CleanUp removes the DNS TXT record previously published by Present.
	// It is called after the challenge is validated (or after a failure),
	// so the provider can remove the temporary TXT record. CleanUp must
	// be idempotent: calling it multiple times for the same domain/token
	// pair must not return an error.
	CleanUp(ctx context.Context, domain, token, value string) error

	// Timeout returns the DNS propagation check interval and the overall
	// wait timeout. The ACME manager polls DNS at each interval until the
	// TXT record is visible or the timeout elapses. Implementations may
	// return (0, 0) to use the manager defaults.
	Timeout() (interval, timeout time.Duration)
}

// NoopDNSProvider is the no-op DNSProvider. It is the default used by
// Manager when no DNS provider is injected via WithDNSProvider.
//
// Present returns ErrDNSChallengeNotConfigured so a misconfigured DNS-01
// flow fails fast with a clear error instead of silently succeeding
// without publishing a TXT record. CleanUp returns nil because cleanup is
// always safe to skip. Timeout returns (0, 0) so the caller uses its own
// defaults.
//
// The runtime does not implement DNS-01 challenge issuance.
type NoopDNSProvider struct{}

// Present returns ErrDNSChallengeNotConfigured, indicating that no real DNS
// provider has been injected.
func (NoopDNSProvider) Present(_ context.Context, _, _, _ string) error {
	return ErrDNSChallengeNotConfigured
}

// CleanUp is a no-op and returns nil. Cleanup is always safe to skip.
func (NoopDNSProvider) CleanUp(_ context.Context, _, _, _ string) error {
	return nil
}

// Timeout returns zero values for both interval and timeout, signalling the
// caller to use its own defaults.
func (NoopDNSProvider) Timeout() (time.Duration, time.Duration) {
	return 0, 0
}

// Compile-time assertion that NoopDNSProvider satisfies DNSProvider.
var _ DNSProvider = NoopDNSProvider{}

// Manager holds certificate management configuration, including the DNS-01
// challenge provider interface. The runtime uses the NoopDNSProvider
// default; callers may inject a real DNS provider via WithDNSProvider.
//
// Manager is the holder: it decouples the DNS-01 challenge provider
// implementation from the ACME manager that consumes it. callers
// construct a Manager with their DNS provider and expose it to the ACME
// flow at startup, without modifying the kernel source.
type Manager struct {
	dnsProvider DNSProvider
}

// Option configures a Manager.
type Option func(*Manager)

// WithDNSProvider sets the DNS-01 challenge provider. Pass nil to keep the
// NoopDNSProvider default.
//
// The runtime does not provide a real DNS provider. Extended
// editions inject one via this option to enable ACME DNS-01 challenge
// issuance:
//
//	mgr := cert.NewManager(cert.WithDNSProvider(myDNSProvider))
func WithDNSProvider(p DNSProvider) Option {
	return func(m *Manager) {
		if p == nil {
			return
		}
		m.dnsProvider = p
	}
}

// NewManager creates a new Manager with the given options. When no DNS
// provider option is supplied, the NoopDNSProvider default is used.
func NewManager(opts ...Option) *Manager {
	m := &Manager{dnsProvider: NoopDNSProvider{}}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// DNSProvider returns the configured DNS-01 challenge provider. When no
// provider was injected via WithDNSProvider, it returns NoopDNSProvider{}.
func (m *Manager) DNSProvider() DNSProvider {
	return m.dnsProvider
}
