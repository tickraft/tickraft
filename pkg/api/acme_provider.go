// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// This file defines the ACME provider extension interface. The
// runtime implements only HTTP-01 challenge via the ACMEManager
// type in acme.go; callers may implement DNS-01 and cert-manager
// backed issuance by registering a custom ACMEProvider.

package api

import (
	"context"
	"crypto"
	"sync"
)

// ACMEChallenge is the ACME challenge type. The runtime
// implements HTTP-01; DNS-01 is provided through an extension.
type ACMEChallenge string

const (
	// ACMEChallengeHTTP01 is the HTTP-01 challenge type. The
	// ACMEManager implements this challenge based on
	// golang.org/x/crypto/acme.
	ACMEChallengeHTTP01 ACMEChallenge = "http-01"

	// ACMEChallengeDNS01 is the DNS-01 challenge type. The runtime
	// does not implement DNS-01; callers may implement it
	// by registering a custom ACMEProvider.
	ACMEChallengeDNS01 ACMEChallenge = "dns-01"
)

// ACMEProvider is the extension interface for ACME challenge providers. The runtime
// provides a default HTTP-01 implementation (see ACMEManager in
// acme.go); callers may register DNS-01 and cert-manager backed
// providers by calling SetACMEProvider before the server starts.
//
// The interface is intentionally narrow: it abstracts only the
// challenge-response step that differs between providers. The shared
// ACMEManager drives the rest of the RFC 8555 flow (directory discovery,
// account registration, order creation, certificate finalization) and
// delegates challenge fulfillment to the configured provider.
type ACMEProvider interface {
	// ChallengeType returns the challenge type this provider fulfills
	// (ACMEChallengeHTTP01 or ACMEChallengeDNS01).
	ChallengeType() ACMEChallenge

	// FulfillChallenge fulfills the given ACME challenge for the given
	// domain. It is called after the ACMEManager creates an order and
	// selects a challenge of this provider's type from the authorization.
	// The returned cleanup function is invoked after the challenge is
	// validated (or after a failure), so the provider can remove any
	// temporary state (e.g. a DNS record or an HTTP token).
	//
	// Implementations must be safe for concurrent use: the ACMEManager
	// may renew certificates for multiple domains in parallel.
	FulfillChallenge(ctx context.Context, p ACMEChallengeParams) (cleanup func(), err error)
}

// ACMEChallengeParams bundles the inputs an ACMEProvider needs to fulfill a
// challenge. It is passed by value to discourage implementations from
// retaining references to mutable ACME client state.
type ACMEChallengeParams struct {
	// Domain is the domain name being authorized.
	Domain string
	// Token is the challenge token provided by the ACME server.
	Token string
	// Response is the challenge response value the provider should publish.
	// For HTTP-01 this is the key authorization (served as the body at
	// /.well-known/acme-challenge/<token>); for DNS-01 this is the
	// SHA-256 hashed key authorization (published as a TXT record at
	// _acme-challenge.<domain>). The value is computed by the ACMEManager
	// from the account key and the challenge token so the provider does
	// not need direct access to the ACME client.
	Response string
	// AccountKey is the ACME account key, exposed for providers that need
	// to sign additional artifacts (e.g. external account binding). Most
	// providers can ignore this field.
	AccountKey crypto.Signer
}

// acmeProviderRegistry holds the registered ACMEProvider instances keyed by
// their challenge type. The registry is package-private; callers interact
// with it through SetACMEProvider and LookupACMEProvider, both of which are
// safe for concurrent use.
var acmeProviderRegistry = struct {
	mu        sync.RWMutex
	providers map[ACMEChallenge]ACMEProvider
}{
	providers: map[ACMEChallenge]ACMEProvider{},
}

// SetACMEProvider registers an ACMEProvider, replacing any previously
// registered provider for the same challenge type. It is safe for concurrent
// use and is intended to be called once at startup before the server begins
// serving traffic. Calling it after the server has started is allowed but
// will not affect in-flight challenges.
//
// Passing nil is a no-op.
func SetACMEProvider(p ACMEProvider) {
	if p == nil {
		return
	}
	acmeProviderRegistry.mu.Lock()
	defer acmeProviderRegistry.mu.Unlock()
	acmeProviderRegistry.providers[p.ChallengeType()] = p
}

// LookupACMEProvider returns the registered ACMEProvider for the given
// challenge type, or nil if none has been registered. It is safe for
// concurrent use.
func LookupACMEProvider(challenge ACMEChallenge) ACMEProvider {
	acmeProviderRegistry.mu.RLock()
	defer acmeProviderRegistry.mu.RUnlock()
	return acmeProviderRegistry.providers[challenge]
}
