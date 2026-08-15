// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package cert provides TLS certificate management primitives for the
// tickraft kernel and downstream editions.
//
// The package exposes a standard-library-only core API for self-signed
// certificate generation (Generate, WriteToDir, ParseLeaf,
// FingerprintSHA256) and a DNS-01 challenge provider interface (DNSProvider)
// with a no-op default. The runtime ships only the interface
// and the no-op default for DNS-01; callers may inject a real DNS
// provider via WithDNSProvider to enable ACME DNS-01 challenge issuance
// without modifying the kernel source.
//
// # Quick Start
//
// Generate an in-memory ECDSA self-signed certificate:
//
//	result, err := cert.Generate(cert.Options{
//	    Domain:  "example.com",
//	    Days:    cert.DefaultDays,
//	    KeyType: cert.DefaultKeyType,
//	})
//	if err != nil {
//	    return err
//	}
//	// result.CertPEM / result.KeyPEM are PEM-encoded bytes.
//
// Generate and write to disk in one call:
//
//	certPath, keyPath, err := cert.WriteToDir(cert.Options{
//	    Domain:  "example.com",
//	    Days:    cert.DefaultDays,
//	    KeyType: cert.DefaultKeyType,
//	}, "./certs")
//
// Print the SHA-256 fingerprint of a PEM-encoded certificate:
//
//	fp, err := cert.FingerprintSHA256(result.CertPEM)
//
// # DNS-01 Challenge Provider Interface
//
// The runtime does not implement DNS-01 challenge issuance.
// The DNSProvider interface and NoopDNSProvider default allow extended
// editions to inject a real DNS provider at startup:
//
//	mgr := cert.NewManager(cert.WithDNSProvider(myDNSProvider))
//
// When no DNS provider is injected, the no-op default returns
// ErrDNSChallengeNotConfigured from Present, so a misconfigured DNS-01
// flow fails fast with a clear error instead of silently succeeding.
//
// # Dependencies
//
// The package depends only on the Go standard library and does not import
// any tickraft internal package, so it is safe for cross-repository reuse
// by downstream editions.
package cert
