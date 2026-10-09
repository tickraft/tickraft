// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package api

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"
)

// TestACMEManagerValidate verifies the ACMEManager.validate checks the
// required DirectoryURL, Email, and Domains fields, returning the
// corresponding sentinel error (see ErrACME*) when any is missing.
func TestACMEManagerValidate(t *testing.T) {
	cases := []struct {
		name    string
		mgr     *ACMEManager
		wantErr error
	}{
		{
			name: "empty directory url",
			mgr: &ACMEManager{
				Email:   "admin@example.com",
				Domains: []string{"example.com"},
			},
			wantErr: ErrACMEDirectoryURLRequired,
		},
		{
			name: "empty email",
			mgr: &ACMEManager{
				DirectoryURL: DefaultACMEDirectoryURL,
				Domains:      []string{"example.com"},
			},
			wantErr: ErrACMEEmailRequired,
		},
		{
			name: "empty domains",
			mgr: &ACMEManager{
				DirectoryURL: DefaultACMEDirectoryURL,
				Email:        "admin@example.com",
			},
			wantErr: ErrACMEDomainsRequired,
		},
		{
			name: "valid configuration",
			mgr: &ACMEManager{
				DirectoryURL: DefaultACMEDirectoryURL,
				Email:        "admin@example.com",
				Domains:      []string{"example.com"},
			},
			wantErr: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.mgr.validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error wrapping %v, got nil", tc.wantErr)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error: got %v, want wrapping %v", err, tc.wantErr)
			}
		})
	}
}

// TestACMEManagerResolveChallengeType verifies the challenge-type resolver
// returns HTTP-01 when the configured type is empty, and returns the
// configured type otherwise.
func TestACMEManagerResolveChallengeType(t *testing.T) {
	cases := []struct {
		name string
		mgr  *ACMEManager
		want ACMEChallenge
	}{
		{
			name: "empty defaults to http 01",
			mgr:  &ACMEManager{},
			want: ACMEChallengeHTTP01,
		},
		{
			name: "http 01 preserved",
			mgr:  &ACMEManager{ChallengeType: "http-01"},
			want: ACMEChallengeHTTP01,
		},
		{
			name: "dns 01 preserved",
			mgr:  &ACMEManager{ChallengeType: "dns-01"},
			want: ACMEChallengeDNS01,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.mgr.resolveChallengeType(); got != tc.want {
				t.Errorf("resolveChallengeType = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestACMEManagerRequestCertificateValidation verifies that
// RequestCertificate surfaces the validate sentinel errors before contacting
// the ACME directory, and that it requires a reloader (ErrACMEReloaderRequired)
// even when validation passes.
func TestACMEManagerRequestCertificateValidation(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name    string
		mgr     *ACMEManager
		domain  string
		wantErr error
	}{
		{
			name: "missing directory url",
			mgr: &ACMEManager{
				Email:    "admin@example.com",
				Domains:  []string{"example.com"},
				Reloader: &stubTLSReloader{fingerprint: "fp"},
			},
			domain:  "example.com",
			wantErr: ErrACMEDirectoryURLRequired,
		},
		{
			name: "missing email",
			mgr: &ACMEManager{
				DirectoryURL: DefaultACMEDirectoryURL,
				Domains:      []string{"example.com"},
				Reloader:     &stubTLSReloader{fingerprint: "fp"},
			},
			domain:  "example.com",
			wantErr: ErrACMEEmailRequired,
		},
		{
			name: "missing domains",
			mgr: &ACMEManager{
				DirectoryURL: DefaultACMEDirectoryURL,
				Email:        "admin@example.com",
				Reloader:     &stubTLSReloader{fingerprint: "fp"},
			},
			domain:  "example.com",
			wantErr: ErrACMEDomainsRequired,
		},
		{
			name: "valid config but missing reloader",
			mgr: &ACMEManager{
				DirectoryURL: DefaultACMEDirectoryURL,
				Email:        "admin@example.com",
				Domains:      []string{"example.com"},
			},
			domain:  "example.com",
			wantErr: ErrACMEReloaderRequired,
		},
		{
			name: "empty domain with otherwise valid config",
			mgr: &ACMEManager{
				DirectoryURL: DefaultACMEDirectoryURL,
				Email:        "admin@example.com",
				Domains:      []string{"example.com"},
				Reloader:     &stubTLSReloader{fingerprint: "fp"},
			},
			domain:  "",
			wantErr: ErrACMEDomainsRequired,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.mgr.RequestCertificate(ctx, tc.domain)
			if err == nil {
				t.Fatalf("expected error wrapping %v, got nil", tc.wantErr)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error: got %v, want wrapping %v", err, tc.wantErr)
			}
		})
	}
}

// TestACMEManagerRunValidation verifies that Run returns the validate error
// before starting the renewal loop.
func TestACMEManagerRunValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := &ACMEManager{} // empty: missing DirectoryURL, Email, Domains
	err := mgr.Run(ctx)
	if !errors.Is(err, ErrACMEDirectoryURLRequired) {
		t.Fatalf("Run error = %v, want wrapping %v", err, ErrACMEDirectoryURLRequired)
	}
}

// TestACMEManagerRunCancelledContext verifies that Run with a valid
// configuration returns nil when the context is cancelled before the first
// tick fires. The initial sweep runs synchronously but RenewIfNeeded returns
// its error via hlog (not propagated), so the loop reaches the select and
// observes ctx.Done.
func TestACMEManagerRunCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel before calling Run so the initial sweep's RenewIfNeeded fails
	// (no provider can issue, but the error is logged, not propagated) and
	// the loop's select immediately observes ctx.Done.
	cancel()

	mgr := &ACMEManager{
		DirectoryURL:  DefaultACMEDirectoryURL,
		Email:         "admin@example.com",
		Domains:       []string{"example.com"},
		CheckInterval: 100 * time.Millisecond,
	}
	err := mgr.Run(ctx)
	if err != nil {
		t.Fatalf("Run with cancelled context: got %v, want nil", err)
	}
}

// stubTLSReloader is a test stub for the TLSReloader interface. It returns the
// configured fingerprint and error on each ReloadTLSConfig call, and records
// the call count so tests can assert the manager invoked the reloader.
type stubTLSReloader struct {
	fingerprint string
	err         error
	calls       int
}

// ReloadTLSConfig satisfies the TLSReloader interface.
func (s *stubTLSReloader) ReloadTLSConfig() (string, error) {
	s.calls++
	return s.fingerprint, s.err
}

// Compile-time assertion that stubTLSReloader satisfies TLSReloader.
var _ TLSReloader = (*stubTLSReloader)(nil)

// TestHTTP01ProviderFulfillChallenge verifies that FulfillChallenge stores the
// challenge response keyed by token and returns a non-nil cleanup function.
// Calling the cleanup removes the token from the store.
func TestHTTP01ProviderFulfillChallenge(t *testing.T) {
	ctx := context.Background()
	p := NewHTTP01Provider()

	params := ACMEChallengeParams{
		Domain:   "example.com",
		Token:    "token-abc",
		Response: "response-key-auth",
	}
	cleanup, err := p.FulfillChallenge(ctx, params)
	if err != nil {
		t.Fatalf("FulfillChallenge: %v", err)
	}
	if cleanup == nil {
		t.Fatal("cleanup is nil, want non-nil")
	}

	// Token must be present right after FulfillChallenge.
	p.mu.RLock()
	_, ok := p.tokens["token-abc"]
	p.mu.RUnlock()
	if !ok {
		t.Fatal("token not stored after FulfillChallenge")
	}

	cleanup()

	// Token must be removed after cleanup.
	p.mu.RLock()
	_, ok = p.tokens["token-abc"]
	p.mu.RUnlock()
	if ok {
		t.Fatal("token still present after cleanup")
	}
}

// TestHTTP01ProviderFulfillChallengeEmptyToken verifies that an empty token
// is rejected with a non-nil error so the ACME flow fails fast rather than
// storing a response under the empty key.
func TestHTTP01ProviderFulfillChallengeEmptyToken(t *testing.T) {
	ctx := context.Background()
	p := NewHTTP01Provider()

	_, err := p.FulfillChallenge(ctx, ACMEChallengeParams{
		Domain:   "example.com",
		Token:    "",
		Response: "resp",
	})
	if err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error = %q, want it to mention token", err.Error())
	}
}

// TestHTTP01ProviderChallengeType verifies the provider reports HTTP-01 as
// its challenge type so the ACME manager can correctly select it from the
// authorization's challenge list.
func TestHTTP01ProviderChallengeType(t *testing.T) {
	p := NewHTTP01Provider()
	if got := p.ChallengeType(); got != ACMEChallengeHTTP01 {
		t.Errorf("ChallengeType = %q, want %q", got, ACMEChallengeHTTP01)
	}
}

// TestHTTP01ProviderSweep verifies that Sweep removes only expired tokens,
// leaving non-expired tokens in the store.
func TestHTTP01ProviderSweep(t *testing.T) {
	p := NewHTTP01Provider()

	// Store two tokens: one expired, one fresh.
	now := time.Now()
	p.mu.Lock()
	p.tokens["expired"] = http01Entry{
		response:  "r1",
		expiresAt: now.Add(-time.Minute), // expired
	}
	p.tokens["fresh"] = http01Entry{
		response:  "r2",
		expiresAt: now.Add(time.Hour), // fresh
	}
	p.mu.Unlock()

	p.Sweep(now)

	p.mu.RLock()
	_, hasExpired := p.tokens["expired"]
	_, hasFresh := p.tokens["fresh"]
	p.mu.RUnlock()

	if hasExpired {
		t.Error("expired token still present after Sweep")
	}
	if !hasFresh {
		t.Error("fresh token removed by Sweep")
	}
}

// TestACMEProviderRegistry verifies the SetACMEProvider / LookupACMEProvider
// pair: a registered provider is retrievable by its challenge type, a nil
// provider is a no-op, and re-registering replaces the previous provider.
func TestACMEProviderRegistry(t *testing.T) {
	// Save the original HTTP-01 provider registered by init so the test
	// restores the global state on exit.
	original := LookupACMEProvider(ACMEChallengeHTTP01)
	defer SetACMEProvider(original)

	// nil must be a no-op (no panic, no change).
	SetACMEProvider(nil)
	if got := LookupACMEProvider(ACMEChallengeHTTP01); got != original {
		t.Errorf("SetACMEProvider(nil) replaced the registry: got %T, want %T", got, original)
	}

	// Registering a new provider replaces the original.
	stub := &stubACMEProvider{challenge: ACMEChallengeHTTP01}
	SetACMEProvider(stub)
	got := LookupACMEProvider(ACMEChallengeHTTP01)
	if got != stub {
		t.Errorf("LookupACMEProvider = %T, want %T", got, stub)
	}

	// Looking up an unregistered challenge type returns nil.
	if got := LookupACMEProvider(ACMEChallengeDNS01); got != nil {
		t.Errorf("LookupACMEProvider(dns-01) = %T, want nil", got)
	}
}

// stubACMEProvider is a test stub for the ACMEProvider interface.
type stubACMEProvider struct {
	challenge ACMEChallenge
	calls     int
	cleanup   func()
	err       error
}

// ChallengeType satisfies ACMEProvider.
func (s *stubACMEProvider) ChallengeType() ACMEChallenge {
	return s.challenge
}

// FulfillChallenge satisfies ACMEProvider. It returns the configured cleanup
// function (or a no-op when nil) so tests can verify the manager invokes it.
func (s *stubACMEProvider) FulfillChallenge(_ context.Context, _ ACMEChallengeParams) (func(), error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if s.cleanup != nil {
		return s.cleanup, nil
	}
	return func() {}, nil
}

// Compile-time assertion that stubACMEProvider satisfies ACMEProvider.
var _ ACMEProvider = (*stubACMEProvider)(nil)

// TestMemoryACMECertStoreRoundTrip verifies the in-memory store can store
// and load a certificate + private key pair, and that LoadCert returns
// (nil, nil, nil) for an unknown domain.
func TestMemoryACMECertStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryACMECertStore()

	// Unknown domain returns nil, nil, nil.
	certPEM, keyPEM, err := s.LoadCert(ctx, "unknown.example")
	if err != nil {
		t.Fatalf("LoadCert unknown domain: %v", err)
	}
	if certPEM != nil || keyPEM != nil {
		t.Errorf("LoadCert unknown domain: cert=%v key=%v, want both nil", certPEM, keyPEM)
	}

	// Store and reload a certificate.
	wantCert := []byte("-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n")
	wantKey := []byte("-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----\n")
	if err := s.StoreCert(ctx, "example.com", wantCert, wantKey); err != nil {
		t.Fatalf("StoreCert: %v", err)
	}
	gotCert, gotKey, err := s.LoadCert(ctx, "example.com")
	if err != nil {
		t.Fatalf("LoadCert: %v", err)
	}
	if !bytes.Equal(gotCert, wantCert) {
		t.Errorf("cert: got %q, want %q", gotCert, wantCert)
	}
	if !bytes.Equal(gotKey, wantKey) {
		t.Errorf("key: got %q, want %q", gotKey, wantKey)
	}

	// Re-storing replaces the previous value.
	newCert := []byte("-----BEGIN CERTIFICATE-----\nnew\n-----END CERTIFICATE-----\n")
	if err := s.StoreCert(ctx, "example.com", newCert, wantKey); err != nil {
		t.Fatalf("StoreCert (replace): %v", err)
	}
	gotCert, _, err = s.LoadCert(ctx, "example.com")
	if err != nil {
		t.Fatalf("LoadCert (after replace): %v", err)
	}
	if !bytes.Equal(gotCert, newCert) {
		t.Errorf("cert after replace: got %q, want %q", gotCert, newCert)
	}
}

// TestMemoryACMECertStoreAccountKey verifies the in-memory store can store
// and load the ACME account key, and that LoadAccountKey returns (nil, nil)
// when no key has been stored.
func TestMemoryACMECertStoreAccountKey(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryACMECertStore()

	// No key stored returns nil, nil.
	keyDER, err := s.LoadAccountKey(ctx)
	if err != nil {
		t.Fatalf("LoadAccountKey empty: %v", err)
	}
	if keyDER != nil {
		t.Errorf("LoadAccountKey empty: got %v, want nil", keyDER)
	}

	// Store and reload.
	want := []byte("fake-account-key-der")
	if err := s.StoreAccountKey(ctx, want); err != nil {
		t.Fatalf("StoreAccountKey: %v", err)
	}
	got, err := s.LoadAccountKey(ctx)
	if err != nil {
		t.Fatalf("LoadAccountKey: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("account key: got %q, want %q", got, want)
	}
}

// TestParsePEMCertificate verifies the PEM certificate parser accepts a
// well-formed PEM certificate block and rejects malformed input with a
// descriptive error.
func TestParsePEMCertificate(t *testing.T) {
	certPEM, _, err := generateTestSelfSignedCertPEM("example.com")
	if err != nil {
		t.Fatalf("generate test cert: %v", err)
	}

	leaf, err := parsePEMCertificate(certPEM)
	if err != nil {
		t.Fatalf("parsePEMCertificate: %v", err)
	}
	if leaf == nil {
		t.Fatal("leaf is nil")
	}
	if leaf.Subject.CommonName != "example.com" {
		t.Errorf("CN = %q, want %q", leaf.Subject.CommonName, "example.com")
	}

	// Malformed input: no PEM block.
	if _, err := parsePEMCertificate([]byte("not a pem")); err == nil {
		t.Error("expected error for non-PEM input, got nil")
	}

	// Malformed input: PEM block but not a CERTIFICATE.
	nonCertPEM := encodePEM("PRIVATE KEY", []byte("not a cert"))
	if _, err := parsePEMCertificate(nonCertPEM); err == nil {
		t.Error("expected error for non-CERTIFICATE PEM block, got nil")
	}
}

// TestEncodePEMChain verifies encodePEMChain produces a PEM byte slice with
// one block per DER input, in order.
func TestEncodePEMChain(t *testing.T) {
	der1 := []byte("der-1")
	der2 := []byte("der-2")
	out := encodePEMChain("CERTIFICATE", [][]byte{der1, der2})
	if !strings.Contains(string(out), "BEGIN CERTIFICATE") {
		t.Errorf("output does not contain CERTIFICATE header: %q", out)
	}
	// The output must contain both DERs (the count of BEGIN markers equals
	// the number of blocks).
	if got := strings.Count(string(out), "BEGIN CERTIFICATE"); got != 2 {
		t.Errorf("block count = %d, want 2", got)
	}
}

// generateTestSelfSignedCertPEM generates a self-signed certificate and
// private key for the given domain using ECDSA P-256. It returns the
// PEM-encoded certificate and private key. This helper is local to the test
// package so the parsePEMCertificate test can exercise the parser against a
// real certificate without depending on the cmd package.
func generateTestSelfSignedCertPEM(domain string) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   domain,
			Organization: []string{"tickraft-test"},
		},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{domain},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}
