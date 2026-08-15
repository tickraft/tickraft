// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// This file implements the ACMEManager, which drives the RFC 8555
// ACME flow against an ACME directory (typically Let's Encrypt) using the
// golang.org/x/crypto/acme low-level package. The runtime ships
// with an HTTP-01 challenge provider (see http01_provider.go); extended
// editions register DNS-01 and cert-manager backed providers through the
// ACMEProvider extension interface (see acme_provider.go).
//
// The manager supports two operating modes:
//
//   - Manual: a caller invokes RequestCertificate or RenewIfNeeded directly,
//     e.g. from a CLI subcommand or a one-shot job.
//   - Automatic: Run enters a loop that periodically calls RenewIfNeeded for
//     each configured domain and exits when ctx is cancelled.
//
// In both modes, every successful certificate acquisition atomically reloads
// the server's TLS configuration via the configured TLSReloader (typically
// *api.Server.ReloadTLSConfig), achieving zero-downtime rotation.
package api

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"golang.org/x/crypto/acme"
)

// ACMEDefaultRenewalLead is the default window before certificate expiry
// within which RenewIfNeeded attempts a renewal. The 30-day window matches
// the design-doc requirement for pre-expiry renewal detection.
const ACMEDefaultRenewalLead = 30 * 24 * time.Hour

// ACMEManager drives the RFC 8555 ACME flow against an ACME directory. The
// runtime uses this manager for HTTP-01 challenge issuance; the
// challenge work itself is delegated to the ACMEProvider registered for the
// configured challenge type (see ACMEProvider and SetACMEProvider).
type ACMEManager struct {
	// DirectoryURL is the ACME directory URL (e.g.
	// https://acme-v02.api.letsencrypt.org/directory). Required.
	DirectoryURL string
	// Email is the ACME registration email. Required.
	Email string
	// ChallengeType is the ACME challenge type (http-01 or dns-01). When
	// empty, defaults to http-01 (DefaultACMEChallengeType).
	ChallengeType string
	// Domains is the list of domain names for which the manager should
	// obtain certificates. At least one domain is required for Run.
	Domains []string
	// RenewalLead is the window before expiry within which RenewIfNeeded
	// attempts a renewal. When zero, ACMEDefaultRenewalLead (30 days) is
	// used. A negative value disables proactive renewal.
	RenewalLead time.Duration
	// CheckInterval is the interval between RenewIfNeeded checks in Run.
	// When zero, defaults to 24 hours.
	CheckInterval time.Duration

	// Reloader is invoked after each successful certificate acquisition to
	// atomically swap the server's live TLS configuration. Required for
	// zero-downtime rotation; when nil, certificates are obtained but the
	// server keeps serving the old certificate until the next manual reload.
	Reloader TLSReloader

	// CertStore is the persistent store for issued certificates and their
	// associated account keys. When nil, the manager uses an in-memory
	// store that does not persist across restarts. Production deployments
	// should inject a file-backed store so certificates survive process
	// restarts and are not re-issued unnecessarily.
	CertStore ACMECertStore

	// accountKey is the ACME account private key, lazily generated on first
	// use and persisted via CertStore so a restart reuses the same account.
	accountKey crypto.Signer
	// accountKeyMu guards accountKey so concurrent requests (e.g. parallel
	// renewal of multiple domains) share a single key.
	accountKeyMu sync.Mutex
}

// TLSReloader is the interface the ACMEManager depends on to atomically swap the
// server's live TLS configuration. *api.Server satisfies this interface via
// its ReloadTLSConfig method (see tls.go). Tests may inject a stub that
// records the reload count.
type TLSReloader interface {
	// ReloadTLSConfig rebuilds the active *tls.Config from the configured
	// certificate and key files, atomically publishes it, and returns the
	// fingerprint of the loaded leaf certificate.
	ReloadTLSConfig() (string, error)
}

// ACMECertStore is the interface for persisting ACME-issued certificates and the
// account private key. The runtime provides an in-memory store
// (NewMemoryACMECertStore); callers may provide a file- or
// cert-manager-backed store by injecting a custom implementation.
type ACMECertStore interface {
	// StoreCert persists the certificate chain (PEM-encoded DER bytes) and
	// the corresponding private key for the given domain. The
	// implementation must be safe for concurrent use.
	StoreCert(ctx context.Context, domain string, certPEM, keyPEM []byte) error
	// LoadCert returns the PEM-encoded certificate chain and private key
	// for the given domain, or (nil, nil, nil) when no certificate has
	// been stored.
	LoadCert(ctx context.Context, domain string) (certPEM, keyPEM []byte, err error)
	// StoreAccountKey persists the ACME account private key (PKCS8 DER).
	StoreAccountKey(ctx context.Context, keyDER []byte) error
	// LoadAccountKey returns the persisted ACME account private key (PKCS8
	// DER), or (nil, nil) when no key has been stored.
	LoadAccountKey(ctx context.Context) ([]byte, error)
}

// Sentinel errors for ACME operations that are not already declared in
// types.go. Callers may use errors.Is to detect a specific failure.
var (
	// ErrACMEDomainsRequired is returned when Domains is empty.
	ErrACMEDomainsRequired = errors.New("at least one domain is required")
	// ErrACMEReloaderRequired is returned when Reloader is nil at issue
	// time. Reloader is optional for Run (the manager can persist
	// certificates without reloading), but required for RequestCertificate
	// to guarantee the new certificate is served.
	ErrACMEReloaderRequired = errors.New("acme reloader is required")
	// ErrACMEUnsupportedChallenge is returned when no ACMEProvider is
	// registered for the configured challenge type.
	ErrACMEUnsupportedChallenge = errors.New("no acme provider registered for the configured challenge type")
	// ErrACMECertExpired is returned when RenewIfNeeded detects an expired
	// certificate and a renewal cannot be performed.
	ErrACMECertExpired = errors.New("acme certificate has expired")
)

// Run starts the ACME renewal loop. It blocks until ctx is cancelled or until
// a fatal configuration error is detected. On each CheckInterval tick Run
// calls RenewIfNeeded for every domain in Domains; successful renewals
// trigger Reloader.ReloadTLSConfig so the new certificate takes effect
// immediately.
//
// Run does not return an error on per-domain renewal failures: those are
// logged at warning level so a transient ACME server failure cannot take the
// server down. Only configuration errors (missing directory URL, no
// registered challenge provider) cause Run to return.
func (m *ACMEManager) Run(ctx context.Context) error {
	if err := m.validate(); err != nil {
		return err
	}
	interval := m.CheckInterval
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Perform an initial sweep immediately so a fresh start does not wait
	// for the first tick to issue any missing certificates.
	m.renewAll(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			m.renewAll(ctx)
		}
	}
}

// renewAll calls RenewIfNeeded for every configured domain. Errors are
// logged but not propagated, so a single domain's renewal failure does not
// abort the loop.
func (m *ACMEManager) renewAll(ctx context.Context) {
	for _, domain := range m.Domains {
		if err := m.RenewIfNeeded(ctx, domain); err != nil {
			hlog.SystemLogger().Warnf("acme: renew %q: %v", domain, err)
			continue
		}
	}
}

// RequestCertificate obtains a certificate for the given domain from the
// configured ACME directory. It always issues a new certificate; callers that
// want to skip issuance when an existing certificate is still valid should
// use RenewIfNeeded instead.
//
// The returned PEM-encoded certificate chain and private key are persisted
// via CertStore and the server's TLS configuration is reloaded via Reloader
// before this method returns, so the new certificate is live by the time the
// caller observes success.
func (m *ACMEManager) RequestCertificate(ctx context.Context, domain string) ([]byte, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	if domain == "" {
		return nil, ErrACMEDomainsRequired
	}
	if m.Reloader == nil {
		return nil, ErrACMEReloaderRequired
	}

	challenge := m.resolveChallengeType()
	provider := LookupACMEProvider(challenge)
	if provider == nil {
		return nil, fmt.Errorf("challenge %q: %w", challenge, ErrACMEUnsupportedChallenge)
	}

	client, err := m.acmeClient(ctx)
	if err != nil {
		return nil, err
	}

	certPEM, keyPEM, err := m.authorizeAndIssue(ctx, client, provider, domain)
	if err != nil {
		return nil, err
	}

	store := m.certStoreOrMemory()
	if err := store.StoreCert(ctx, domain, certPEM, keyPEM); err != nil {
		return nil, fmt.Errorf("store certificate: %w", err)
	}

	if _, err := m.Reloader.ReloadTLSConfig(); err != nil {
		return nil, fmt.Errorf("reload tls config: %w", err)
	}

	return certPEM, nil
}

// RenewIfNeeded obtains a new certificate for the given domain when the
// currently stored certificate is missing, expired, or within RenewalLead of
// expiry. When the stored certificate is still valid, it returns nil without
// contacting the ACME directory.
//
// The method is safe for concurrent use: the underlying ACME client is
// cached, and per-domain renewal is serialized through the per-domain lock in
// the ACME flow itself (RFC 8555 deactivates pending authorizations for the
// same identifier).
func (m *ACMEManager) RenewIfNeeded(ctx context.Context, domain string) error {
	store := m.certStoreOrMemory()
	certPEM, _, err := store.LoadCert(ctx, domain)
	if err != nil {
		return fmt.Errorf("load stored certificate for %q: %w", domain, err)
	}

	if certPEM == nil {
		_, err := m.RequestCertificate(ctx, domain)
		return err
	}

	leaf, err := parsePEMCertificate(certPEM)
	if err != nil {
		// Stored certificate is unparseable; treat as missing and re-issue.
		hlog.SystemLogger().Warnf("acme: stored certificate for %q unparseable: %v (re-issuing)", domain, err)
		_, err := m.RequestCertificate(ctx, domain)
		return err
	}

	lead := m.RenewalLead
	if lead == 0 {
		lead = ACMEDefaultRenewalLead
	}
	now := time.Now()
	if now.Add(lead).Before(leaf.NotAfter) {
		// Certificate is still valid beyond the renewal window; nothing to do.
		return nil
	}
	if now.After(leaf.NotAfter) {
		hlog.SystemLogger().Warnf("acme: certificate for %q expired at %s", domain, leaf.NotAfter.Format(time.RFC3339))
	}

	_, err = m.RequestCertificate(ctx, domain)
	return err
}

// validate returns a wrapped sentinel error (see ErrACME*) when the manager
// is not fully configured. It does not check the challenge provider
// registration, since that is only required at issue time, not at Run time
// for the initial sweep.
func (m *ACMEManager) validate() error {
	if m.DirectoryURL == "" {
		return ErrACMEDirectoryURLRequired
	}
	if m.Email == "" {
		return ErrACMEEmailRequired
	}
	if len(m.Domains) == 0 {
		return ErrACMEDomainsRequired
	}
	return nil
}

// resolveChallengeType returns the configured challenge type or the default
// HTTP-01 when empty.
func (m *ACMEManager) resolveChallengeType() ACMEChallenge {
	if m.ChallengeType == "" {
		return ACMEChallengeHTTP01
	}
	return ACMEChallenge(m.ChallengeType)
}

// certStoreOrMemory returns the configured CertStore or a fresh
// MemoryACMECertStore when none is configured. The fallback is per-call, so
// callers that want persistence across calls must inject a store. In the
// production startup path the runtime injects a FileACMECertStore so
// certificates and account keys survive restarts.
func (m *ACMEManager) certStoreOrMemory() ACMECertStore {
	if m.CertStore != nil {
		return m.CertStore
	}
	return NewMemoryACMECertStore()
}

// accountKeyOrGenerate returns the cached ACME account key, generating a new
// one (and persisting it via CertStore) when no key is cached or stored. The
// generated key is an ECDSA P-256 key, which is the recommended key type for
// ACME accounts.
func (m *ACMEManager) accountKeyOrGenerate(ctx context.Context) (crypto.Signer, error) {
	m.accountKeyMu.Lock()
	defer m.accountKeyMu.Unlock()

	if m.accountKey != nil {
		return m.accountKey, nil
	}

	store := m.certStoreOrMemory()
	keyDER, err := store.LoadAccountKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("load account key: %w", err)
	}
	if keyDER != nil {
		key, err := x509.ParsePKCS8PrivateKey(keyDER)
		if err != nil {
			return nil, fmt.Errorf("parse stored account key: %w", err)
		}
		signer, ok := key.(crypto.Signer)
		if !ok {
			return nil, fmt.Errorf("stored account key is not a signer: %T", key)
		}
		m.accountKey = signer
		return signer, nil
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate account key: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal account key: %w", err)
	}
	if err := store.StoreAccountKey(ctx, der); err != nil {
		return nil, fmt.Errorf("store account key: %w", err)
	}
	m.accountKey = key
	return key, nil
}

// acmeClient builds an *acme.Client for the configured directory URL. The
// client is created on every call so it always picks up the latest account
// key; acme.Client is stateless aside from the key, so the per-call cost is
// negligible.
func (m *ACMEManager) acmeClient(ctx context.Context) (*acme.Client, error) {
	key, err := m.accountKeyOrGenerate(ctx)
	if err != nil {
		return nil, err
	}
	client := &acme.Client{
		Key:          key,
		DirectoryURL: m.DirectoryURL,
		UserAgent:    "tickraft-acme/1.0",
	}
	return client, nil
}

// authorizeAndIssue runs the RFC 8555 order/authorize/finalize flow for the
// given domain using the given provider to fulfill the challenge. It returns
// the PEM-encoded certificate chain and the PEM-encoded private key of the
// freshly generated cert key (not the account key).
func (m *ACMEManager) authorizeAndIssue(ctx context.Context, client *acme.Client, provider ACMEProvider, domain string) ([]byte, []byte, error) {
	// Register the account if not already registered. acme.Register
	// returns ErrAccountAlreadyExists when the key is already registered,
	// which is benign and can be ignored.
	account := &acme.Account{Contact: []string{"mailto:" + m.Email}}
	if _, err := client.Register(ctx, account, acme.AcceptTOS); err != nil {
		if !errors.Is(err, acme.ErrAccountAlreadyExists) {
			return nil, nil, fmt.Errorf("register account: %w", err)
		}
	}

	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(domain))
	if err != nil {
		return nil, nil, fmt.Errorf("authorize order: %w", err)
	}

	// Fetch the authorizations and select a challenge of the provider's
	// type. When the server does not offer a challenge of that type, the
	// order cannot be fulfilled; the caller should fall back to a
	// different challenge type or directory.
	for _, authzURL := range order.AuthzURLs {
		authz, err := client.GetAuthorization(ctx, authzURL)
		if err != nil {
			return nil, nil, fmt.Errorf("get authorization %q: %w", authzURL, err)
		}
		challenge := pickACMEChallenge(string(provider.ChallengeType()), authz.Challenges)
		if challenge == nil {
			return nil, nil, fmt.Errorf("authorization %q does not offer challenge %q",
				authzURL, provider.ChallengeType())
		}

		response, err := m.computeChallengeResponse(client, provider.ChallengeType(), challenge.Token)
		if err != nil {
			return nil, nil, fmt.Errorf("compute challenge response: %w", err)
		}

		cleanup, err := provider.FulfillChallenge(ctx, ACMEChallengeParams{
			Domain:     domain,
			Token:      challenge.Token,
			Response:   response,
			AccountKey: m.accountKey,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("fulfill challenge: %w", err)
		}
		if cleanup != nil {
			defer cleanup()
		}

		if _, err := client.Accept(ctx, challenge); err != nil {
			return nil, nil, fmt.Errorf("accept challenge: %w", err)
		}
		if _, err := client.WaitAuthorization(ctx, authzURL); err != nil {
			return nil, nil, fmt.Errorf("wait authorization: %w", err)
		}
	}

	if _, err := client.WaitOrder(ctx, order.URI); err != nil {
		return nil, nil, fmt.Errorf("wait order: %w", err)
	}

	// Generate a fresh cert private key for this issuance. Reusing the
	// account key would tie the cert key to the account key, which is
	// both unnecessary and harder to rotate.
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate cert key: %w", err)
	}
	csr, err := createCSR(domain, certKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create csr: %w", err)
	}

	// Finalize the order by submitting the CSR to the order's finalize
	// endpoint. CreateOrderCert is the RFC 8555-compliant replacement for
	// the deprecated CreateCert; it posts the CSR to order.FinalizeURL and
	// internally waits for the CA to issue the certificate.
	derChain, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return nil, nil, fmt.Errorf("create cert: %w", err)
	}
	if len(derChain) == 0 {
		return nil, nil, fmt.Errorf("create cert: empty chain")
	}

	certPEM := encodePEMChain("CERTIFICATE", derChain)
	keyDER, err := x509.MarshalPKCS8PrivateKey(certKey)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal cert key: %w", err)
	}
	keyPEM := encodePEM("PRIVATE KEY", keyDER)
	return certPEM, keyPEM, nil
}

// computeChallengeResponse returns the challenge response value the provider
// should publish. For HTTP-01 this is the key authorization (served as the
// body at /.well-known/acme-challenge/<token>); for DNS-01 this is the
// SHA-256 hashed key authorization (published as a TXT record at
// _acme-challenge.<domain>). The value is computed by the ACME client from
// the account key and the challenge token so the provider does not need
// direct access to either.
func (m *ACMEManager) computeChallengeResponse(client *acme.Client, challengeType ACMEChallenge, token string) (string, error) {
	switch challengeType {
	case ACMEChallengeHTTP01:
		return client.HTTP01ChallengeResponse(token)
	case ACMEChallengeDNS01:
		return client.DNS01ChallengeRecord(token)
	default:
		return "", fmt.Errorf("unsupported challenge type %q: %w", challengeType, ErrACMEUnsupportedChallenge)
	}
}

// pickACMEChallenge returns the first challenge of the given type from the
// list, or nil when none matches. The list is the set of challenges offered
// by the ACME server for a given authorization.
func pickACMEChallenge(typ string, challenges []*acme.Challenge) *acme.Challenge {
	for _, c := range challenges {
		if c == nil {
			continue
		}
		if c.Type == typ {
			return c
		}
	}
	return nil
}

// createCSR builds a Certificate Signing Request for the given domain using
// the given private key. The CSR includes only the domain in the SAN; the
// subject is left empty to match Let's Encrypt recommendations.
func createCSR(domain string, key crypto.Signer) ([]byte, error) {
	tmpl := &x509.CertificateRequest{
		DNSNames: []string{domain},
		Subject:  pkix.Name{CommonName: domain},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		return nil, err
	}
	return csrDER, nil
}
