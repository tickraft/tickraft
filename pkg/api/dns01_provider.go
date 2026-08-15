// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// This file implements the DNS-01 challenge provider adapter that bridges
// pkg/cert.DNSProvider to the ACMEProvider interface. The runtime does
// not ship a concrete DNS provider; callers may implement
// pkg/cert.DNSProvider (e.g. cloudflare, route53, alidns) and register it as
// an ACMEProvider via NewDNS01Provider + SetACMEProvider, enabling ACME
// DNS-01 challenge issuance without modifying the kernel source.
//
// The adapter exists so the ACMEManager (acme.go) can drive the RFC 8555
// flow uniformly for both challenge types: it only interacts with
// ACMEProvider, while the DNS-specific Present/CleanUp/Timeout contract lives
// in pkg/cert.DNSProvider and is shared with the cert.Manager self-sign path.
package api

import (
	"context"
	"time"

	"github.com/tickraft/tickraft/pkg/cert"
)

// DNS01Provider adapts a pkg/cert.DNSProvider to the ACMEProvider interface. It
// is the integration point between the ACME manager (which drives the RFC
// 8555 order/authorize/finalize flow) and the DNS-01 challenge provider interface
// (which publishes and removes the _acme-challenge TXT record).
//
// The runtime does not provide a concrete DNS provider; it only
// ships this adapter and the NoopDNSProvider default from pkg/cert. Extended
// editions inject a real DNS provider by constructing a DNS01Provider around
// their cert.DNSProvider implementation and registering it via
// SetACMEProvider before the server starts:
//
//	api.SetACMEProvider(api.NewDNS01Provider(myDNSProvider))
//
// When the wrapped provider is pkg/cert.NoopDNSProvider, Present returns
// cert.ErrDNSChallengeNotConfigured, so a misconfigured DNS-01 flow fails
// fast with a clear error instead of silently succeeding.
type DNS01Provider struct {
	provider cert.DNSProvider
}

// NewDNS01Provider wraps a pkg/cert.DNSProvider and returns an ACMEProvider
// for the DNS-01 challenge type. The returned provider delegates
// FulfillChallenge to Present and the returned cleanup function to CleanUp,
// matching the contract documented on cert.DNSProvider.
//
// Passing nil falls back to cert.NoopDNSProvider so callers can construct the
// adapter unconditionally; the resulting provider returns
// cert.ErrDNSChallengeNotConfigured from FulfillChallenge, surfacing the
// missing injection as a clear error at challenge time.
func NewDNS01Provider(p cert.DNSProvider) *DNS01Provider {
	if p == nil {
		p = cert.NoopDNSProvider{}
	}
	return &DNS01Provider{provider: p}
}

// ChallengeType returns ACMEChallengeDNS01.
func (p *DNS01Provider) ChallengeType() ACMEChallenge {
	return ACMEChallengeDNS01
}

// FulfillChallenge delegates to cert.DNSProvider.Present, publishing the
// DNS-01 TXT record at _acme-challenge.<domain> with the SHA-256 hashed
// key authorization carried by params.Response. The returned cleanup
// function removes the TXT record via CleanUp; it is safe to call after
// the challenge is validated or after a failure, and is idempotent per
// the cert.DNSProvider contract.
//
// The cleanup function captures the same context passed to FulfillChallenge
// so the caller does not need to provide one at cleanup time. Cleanup errors
// are not propagated (the challenge has already been validated or failed);
// they are surfaced only via the cert.DNSProvider implementation's own
// logging.
func (p *DNS01Provider) FulfillChallenge(ctx context.Context, params ACMEChallengeParams) (func(), error) {
	if err := p.provider.Present(ctx, params.Domain, params.Token, params.Response); err != nil {
		return nil, err
	}
	cleanup := func() {
		_ = p.provider.CleanUp(ctx, params.Domain, params.Token, params.Response)
	}
	return cleanup, nil
}

// Timeout returns the DNS propagation check interval and the overall wait
// timeout, delegating to the wrapped cert.DNSProvider. The ACMEManager uses
// these values to poll DNS until the TXT record is visible or the timeout
// elapses. A (0, 0) return lets the manager use its own defaults.
func (p *DNS01Provider) Timeout() (interval, timeout time.Duration) {
	return p.provider.Timeout()
}

// Compile-time assertion that DNS01Provider satisfies ACMEProvider.
var _ ACMEProvider = (*DNS01Provider)(nil)
