// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileACMECertStore is a file-backed implementation of ACMECertStore. It
// persists issued certificates, private keys, and the ACME account key to a
// directory on disk, surviving process restarts.
//
// Each domain's certificate chain and key are stored as
// <dir>/<domain>.crt.pem and <dir>/<domain>.key.pem respectively. The ACME
// account key is stored as <dir>/account-key.der. Additionally, the most
// recently stored certificate is copied to <dir>/server.crt.pem and
// <dir>/server.key.pem so the TLS server can load it via TLSCertFile /
// TLSKeyFile.
type FileACMECertStore struct {
	mu  sync.Mutex
	dir string
}

// NewFileACMECertStore creates a FileACMECertStore rooted at dir. The
// directory and its parents are created with 0700 permissions when they do
// not already exist.
func NewFileACMECertStore(dir string) (*FileACMECertStore, error) {
	if dir == "" {
		return nil, errors.New("acme cert store: directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("acme cert store: create directory %q: %w", dir, err)
	}
	return &FileACMECertStore{dir: dir}, nil
}

// ServerCertFile returns the path the TLS server should use as its
// TLSCertFile. This file is updated on every successful StoreCert call.
func (s *FileACMECertStore) ServerCertFile() string {
	return filepath.Join(s.dir, "server.crt.pem")
}

// ServerKeyFile returns the path the TLS server should use as its
// TLSKeyFile. This file is updated on every successful StoreCert call.
func (s *FileACMECertStore) ServerKeyFile() string {
	return filepath.Join(s.dir, "server.key.pem")
}

// StoreCert persists the certificate chain and private key for the given
// domain, and copies them to the server cert/key paths so ReloadTLSConfig
// picks up the new certificate immediately.
func (s *FileACMECertStore) StoreCert(_ context.Context, domain string, certPEM, keyPEM []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	certPath := filepath.Join(s.dir, domain+".crt.pem")
	keyPath := filepath.Join(s.dir, domain+".key.pem")
	if err := writeFile(certPath, certPEM); err != nil {
		return err
	}
	if err := writeFile(keyPath, keyPEM); err != nil {
		return err
	}
	// Copy to the server-wide paths so the TLS reload picks them up.
	if err := writeFile(s.ServerCertFile(), certPEM); err != nil {
		return err
	}
	if err := writeFile(s.ServerKeyFile(), keyPEM); err != nil {
		return err
	}
	return nil
}

// LoadCert returns the persisted certificate chain and private key for the
// given domain, or (nil, nil, nil) when no certificate has been stored.
func (s *FileACMECertStore) LoadCert(_ context.Context, domain string) ([]byte, []byte, error) {
	certPEM, err := os.ReadFile(filepath.Join(s.dir, domain+".crt.pem"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("load cert for %q: %w", domain, err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(s.dir, domain+".key.pem"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("load key for %q: %w", domain, err)
	}
	return certPEM, keyPEM, nil
}

// StoreAccountKey persists the ACME account private key (PKCS8 DER).
func (s *FileACMECertStore) StoreAccountKey(_ context.Context, keyDER []byte) error {
	return writeFile(filepath.Join(s.dir, "account-key.der"), keyDER)
}

// LoadAccountKey returns the persisted ACME account private key (PKCS8 DER),
// or (nil, nil) when no key has been stored.
func (s *FileACMECertStore) LoadAccountKey(_ context.Context) ([]byte, error) {
	keyDER, err := os.ReadFile(filepath.Join(s.dir, "account-key.der"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load account key: %w", err)
	}
	return keyDER, nil
}

// writeFile writes data to path with 0600 permissions, creating or
// truncating the file.
func writeFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write file %q: %w", path, err)
	}
	return nil
}

// Compile-time assertion that FileACMECertStore satisfies ACMECertStore.
var _ ACMECertStore = (*FileACMECertStore)(nil)
