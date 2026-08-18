// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// This file hosts the small PEM / certificate helpers shared by the ACME
// manager and the TLS reload path, plus the in-memory ACMECertStore used as
// the default non-persistent store and by tests.

package api

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
)

// encodePEM PEM-encodes a single DER block with the given block type.
func encodePEM(blockType string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
}

// encodePEMChain PEM-encodes a chain of DER blocks (e.g. a leaf certificate
// followed by its intermediates) into a single PEM byte slice.
func encodePEMChain(blockType string, derChain [][]byte) []byte {
	out := make([]byte, 0, len(derChain))
	for _, der := range derChain {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})...)
	}
	return out
}

// parsePEMCertificate parses the first PEM-encoded certificate from data and
// returns the leaf *x509.Certificate. It returns an error when data contains
// no CERTIFICATE block or the DER bytes cannot be parsed.
func parsePEMCertificate(data []byte) (*x509.Certificate, error) {
	var block *pem.Block
	rest := data
	for {
		block, rest = pem.Decode(rest)
		if block == nil {
			return nil, fmt.Errorf("no certificate PEM block found")
		}
		if block.Type == "CERTIFICATE" {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("parse certificate: %w", err)
			}
			return cert, nil
		}
	}
}

// MemoryACMECertStore is an in-memory implementation of ACMECertStore. It is
// the default store used by ACMEManager when no persistent store is injected,
// and is suitable for tests and ephemeral deployments. Certificates and
// account keys are not persisted across process restarts.
type MemoryACMECertStore struct {
	mu         sync.RWMutex
	certs      map[string][]byte // domain -> PEM-encoded certificate chain
	keys       map[string][]byte // domain -> PEM-encoded private key
	accountKey []byte            // PKCS8 DER
}

// NewMemoryACMECertStore returns a fresh, empty MemoryACMECertStore.
func NewMemoryACMECertStore() *MemoryACMECertStore {
	return &MemoryACMECertStore{
		certs: make(map[string][]byte),
		keys:  make(map[string][]byte),
	}
}

// StoreCert persists the PEM-encoded certificate chain and private key for
// the given domain, replacing any previously stored value.
func (s *MemoryACMECertStore) StoreCert(_ context.Context, domain string, certPEM, keyPEM []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.certs[domain] = append([]byte(nil), certPEM...)
	s.keys[domain] = append([]byte(nil), keyPEM...)
	return nil
}

// LoadCert returns the PEM-encoded certificate chain and private key for the
// given domain, or (nil, nil, nil) when no certificate has been stored.
func (s *MemoryACMECertStore) LoadCert(_ context.Context, domain string) (certPEM, keyPEM []byte, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cert, ok := s.certs[domain]
	if !ok {
		return nil, nil, nil
	}
	key := s.keys[domain]
	return append([]byte(nil), cert...), append([]byte(nil), key...), nil
}

// StoreAccountKey persists the ACME account private key (PKCS8 DER),
// replacing any previously stored value.
func (s *MemoryACMECertStore) StoreAccountKey(_ context.Context, keyDER []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accountKey = append([]byte(nil), keyDER...)
	return nil
}

// LoadAccountKey returns the persisted ACME account private key (PKCS8 DER),
// or (nil, nil) when no key has been stored.
func (s *MemoryACMECertStore) LoadAccountKey(_ context.Context) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.accountKey == nil {
		return nil, nil
	}
	return append([]byte(nil), s.accountKey...), nil
}

// Compile-time assertion that MemoryACMECertStore satisfies ACMECertStore.
var _ ACMECertStore = (*MemoryACMECertStore)(nil)
