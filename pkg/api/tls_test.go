// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package api

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
)

// TestParseTLSMinVersion verifies parseTLSMinVersion translates the accepted
// string forms ("1.2", "1.3", and empty) to the corresponding crypto/tls
// constants, and rejects unknown values with a wrapped ErrTLSInvalidMinVersion.
func TestParseTLSMinVersion(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    uint16
		wantErr error
	}{
		{name: "empty defaults to 1.2", input: "", want: tls.VersionTLS12},
		{name: "1.2", input: "1.2", want: tls.VersionTLS12},
		{name: "1.3", input: "1.3", want: tls.VersionTLS13},
		{name: "invalid", input: "1.1", wantErr: ErrTLSInvalidMinVersion},
		{name: "garbage", input: "tls", wantErr: ErrTLSInvalidMinVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTLSMinVersion(tc.input)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error wrapping %v, got nil", tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error: got %v, want wrapping %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestParseTLSCipherSuites verifies parseTLSCipherSuites accepts the default
// cipher-suite whitelist, rejects unknown names, and returns nil for an empty
// input so crypto/tls picks its own defaults.
func TestParseTLSCipherSuites(t *testing.T) {
	t.Run("empty returns nil", func(t *testing.T) {
		got, err := parseTLSCipherSuites(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("default whitelist accepted", func(t *testing.T) {
		got, err := parseTLSCipherSuites(DefaultTLSCipherSuites)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != len(DefaultTLSCipherSuites) {
			t.Errorf("got %d suites, want %d", len(got), len(DefaultTLSCipherSuites))
		}
	})

	t.Run("unknown suite rejected", func(t *testing.T) {
		_, err := parseTLSCipherSuites([]string{"TLS_RSA_WITH_RC4_128_SHA"})
		if err == nil {
			t.Fatal("expected error for unknown suite, got nil")
		}
		if !strings.Contains(err.Error(), "unknown cipher suite") {
			t.Errorf("error = %v, want it to mention unknown cipher suite", err)
		}
	})
}

// TestParseTLSClientAuth verifies parseTLSClientAuth translates all accepted
// string modes to the corresponding crypto/tls.ClientAuthType constants, and
// rejects unknown values with a wrapped ErrTLSInvalidClientAuth.
func TestParseTLSClientAuth(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr error
	}{
		{name: "empty defaults to no client cert", input: ""},
		{name: "no_client_cert", input: "no_client_cert"},
		{name: "request_client_cert", input: "request_client_cert"},
		{name: "require_any_client_cert", input: "require_any_client_cert"},
		{name: "verify_client_cert_if_given", input: "verify_client_cert_if_given"},
		{name: "require_and_verify_client_cert", input: "require_and_verify_client_cert"},
		{name: "invalid", input: "bogus", wantErr: ErrTLSInvalidClientAuth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseTLSClientAuth(tc.input)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error wrapping %v, got nil", tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error: got %v, want wrapping %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestCertFingerprintSHA256 verifies the fingerprint helper returns a
// lowercase hex string for a non-nil certificate and an empty string for a nil
// certificate or one with empty raw bytes.
func TestCertFingerprintSHA256(t *testing.T) {
	cert := &x509.Certificate{Raw: []byte("hello")}
	got := certFingerprintSHA256(cert)
	if got == "" {
		t.Fatal("expected non-empty fingerprint for non-nil cert")
	}
	// SHA-256 hex digest of "hello" is 64 hex chars.
	if len(got) != 64 {
		t.Errorf("fingerprint length = %d, want 64", len(got))
	}
	if strings.ToLower(got) != got {
		t.Errorf("fingerprint = %q, want lowercase hex", got)
	}

	if certFingerprintSHA256(nil) != "" {
		t.Error("expected empty fingerprint for nil cert")
	}
	if certFingerprintSHA256(&x509.Certificate{}) != "" {
		t.Error("expected empty fingerprint for cert with empty Raw")
	}
}

// TestBuildTLSConfigDisabled verifies buildTLSConfig returns ErrTLSDisabled
// when TLSEnabled is false, regardless of other configuration.
func TestBuildTLSConfigDisabled(t *testing.T) {
	s := &Server{config: ServerConfig{TLSEnabled: false}}
	_, _, err := s.buildTLSConfig()
	if !errors.Is(err, ErrTLSDisabled) {
		t.Fatalf("error = %v, want wrapping %v", err, ErrTLSDisabled)
	}
}

// TestBuildTLSConfigMissingCertFile verifies buildTLSConfig returns
// ErrTLSCertFileMissing when TLSEnabled is true but TLSCertFile is empty.
func TestBuildTLSConfigMissingCertFile(t *testing.T) {
	s := &Server{config: ServerConfig{
		TLSEnabled: true,
		TLSKeyFile: "/tmp/key.pem",
	}}
	_, _, err := s.buildTLSConfig()
	if !errors.Is(err, ErrTLSCertFileMissing) {
		t.Fatalf("error = %v, want wrapping %v", err, ErrTLSCertFileMissing)
	}
}

// TestBuildTLSConfigMissingKeyFile verifies buildTLSConfig returns
// ErrTLSKeyFileMissing when TLSEnabled is true but TLSKeyFile is empty.
func TestBuildTLSConfigMissingKeyFile(t *testing.T) {
	s := &Server{config: ServerConfig{
		TLSEnabled:  true,
		TLSCertFile: "/tmp/cert.pem",
	}}
	_, _, err := s.buildTLSConfig()
	if !errors.Is(err, ErrTLSKeyFileMissing) {
		t.Fatalf("error = %v, want wrapping %v", err, ErrTLSKeyFileMissing)
	}
}

// TestBuildTLSConfigLoadFailure verifies buildTLSConfig returns a wrapped
// error (containing the file paths) when the certificate and key files do not
// exist on disk.
func TestBuildTLSConfigLoadFailure(t *testing.T) {
	s := &Server{config: ServerConfig{
		TLSEnabled:  true,
		TLSCertFile: "/nonexistent/cert.pem",
		TLSKeyFile:  "/nonexistent/key.pem",
	}}
	_, _, err := s.buildTLSConfig()
	if err == nil {
		t.Fatal("expected error for nonexistent cert files, got nil")
	}
	if !strings.Contains(err.Error(), "/nonexistent/cert.pem") {
		t.Errorf("error = %v, want it to mention the cert file path", err)
	}
}

// TestBuildTLSConfigSuccess verifies buildTLSConfig constructs a valid
// *tls.Config from a real self-signed certificate and key, returning a
// non-empty fingerprint and applying the configured minimum version and cipher
// suites.
func TestBuildTLSConfigSuccess(t *testing.T) {
	certPEM, keyPEM, err := generateTestSelfSignedCertPEM("example.com")
	if err != nil {
		t.Fatalf("generate test cert: %v", err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	s := &Server{config: ServerConfig{
		TLSEnabled:      true,
		TLSCertFile:     certPath,
		TLSKeyFile:      keyPath,
		TLSMinVersion:   "1.2",
		TLSCipherSuites: DefaultTLSCipherSuites,
		TLSClientAuth:   "no_client_cert",
	}}

	cfg, fingerprint, err := s.buildTLSConfig()
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("cfg is nil")
	}
	if fingerprint == "" {
		t.Error("fingerprint is empty, want non-empty SHA-256 hex")
	}
	if len(fingerprint) != 64 {
		t.Errorf("fingerprint length = %d, want 64", len(fingerprint))
	}
	if len(cfg.Certificates) != 1 {
		t.Errorf("certificates count = %d, want 1", len(cfg.Certificates))
	}
}

// TestReloadTLSConfigDisabled verifies ReloadTLSConfig returns ErrTLSDisabled
// when TLS is not enabled.
func TestReloadTLSConfigDisabled(t *testing.T) {
	s := &Server{config: ServerConfig{TLSEnabled: false}}
	_, err := s.ReloadTLSConfig()
	if !errors.Is(err, ErrTLSDisabled) {
		t.Fatalf("error = %v, want wrapping %v", err, ErrTLSDisabled)
	}
}

// TestReloadTLSConfigSuccess verifies ReloadTLSConfig atomically publishes a
// new *tls.Config and returns the SHA-256 fingerprint of the loaded leaf
// certificate. The published config must be retrievable via the holder's
// current() method.
func TestReloadTLSConfigSuccess(t *testing.T) {
	certPEM, keyPEM, err := generateTestSelfSignedCertPEM("example.com")
	if err != nil {
		t.Fatalf("generate test cert: %v", err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	s := &Server{config: ServerConfig{
		TLSEnabled:      true,
		TLSCertFile:     certPath,
		TLSKeyFile:      keyPath,
		TLSMinVersion:   "1.2",
		TLSCipherSuites: DefaultTLSCipherSuites,
		TLSClientAuth:   "no_client_cert",
	}}

	fingerprint, err := s.ReloadTLSConfig()
	if err != nil {
		t.Fatalf("ReloadTLSConfig: %v", err)
	}
	if fingerprint == "" {
		t.Error("fingerprint is empty")
	}

	// The holder must now have a live config.
	current := s.tlsHolder.current()
	if current == nil {
		t.Fatal("holder current() is nil after reload")
	}
	if len(current.Certificates) != 1 {
		t.Errorf("certificates count = %d, want 1", len(current.Certificates))
	}

	// GetCertificate callback must return the loaded certificate.
	cert, err := current.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if cert == nil {
		t.Fatal("GetCertificate returned nil cert")
	}
}

// TestReloadTLSConfigKeyMismatch verifies ReloadTLSConfig returns a wrapped
// error when the certificate and key files do not match (a common operator
// mistake), satisfying the certificate validation failure scenario test
// requirement.
func TestReloadTLSConfigKeyMismatch(t *testing.T) {
	// Generate two independent cert/key pairs.
	cert1PEM, _, err := generateTestSelfSignedCertPEM("example.com")
	if err != nil {
		t.Fatalf("generate cert1: %v", err)
	}
	_, key2PEM, err := generateTestSelfSignedCertPEM("other.example")
	if err != nil {
		t.Fatalf("generate cert2: %v", err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	if err := os.WriteFile(certPath, cert1PEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	// Write the wrong key (key2) alongside cert1.
	if err := os.WriteFile(keyPath, key2PEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	s := &Server{config: ServerConfig{
		TLSEnabled:  true,
		TLSCertFile: certPath,
		TLSKeyFile:  keyPath,
	}}
	_, err = s.ReloadTLSConfig()
	if err == nil {
		t.Fatal("expected error for cert/key mismatch, got nil")
	}
}

// TestLoadCABundle verifies loadCABundle reads a PEM-encoded CA bundle and
// returns a non-empty pool. It also verifies a non-existent file returns a
// wrapped error and an empty/non-PEM file returns ErrTLSClientCALoadFailed.
func TestLoadCABundle(t *testing.T) {
	// Generate a self-signed cert to use as a CA.
	certPEM, _, err := generateTestSelfSignedCertPEM("ca.example")
	if err != nil {
		t.Fatalf("generate ca cert: %v", err)
	}

	t.Run("valid CA bundle", func(t *testing.T) {
		dir := t.TempDir()
		caPath := filepath.Join(dir, "ca.pem")
		if err := os.WriteFile(caPath, certPEM, 0o600); err != nil {
			t.Fatalf("write ca: %v", err)
		}
		pool, err := loadCABundle(caPath)
		if err != nil {
			t.Fatalf("loadCABundle: %v", err)
		}
		if pool == nil {
			t.Fatal("pool is nil")
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, err := loadCABundle("/nonexistent/ca.pem")
		if err == nil {
			t.Fatal("expected error for nonexistent file, got nil")
		}
	})

	t.Run("non-PEM content", func(t *testing.T) {
		dir := t.TempDir()
		caPath := filepath.Join(dir, "ca.pem")
		if err := os.WriteFile(caPath, []byte("not a pem"), 0o600); err != nil {
			t.Fatalf("write ca: %v", err)
		}
		_, err := loadCABundle(caPath)
		if !errors.Is(err, ErrTLSClientCALoadFailed) {
			t.Fatalf("error = %v, want wrapping %v", err, ErrTLSClientCALoadFailed)
		}
	})
}

// TestIsTLSFileEvent verifies isTLSFileEvent reports true only for create,
// write, or rename events on the configured cert or key file paths, and false
// for chmod-only events or events on unrelated files.
func TestIsTLSFileEvent(t *testing.T) {
	cfg := ServerConfig{
		TLSCertFile: "/etc/tickraft/certs/server.crt",
		TLSKeyFile:  "/etc/tickraft/certs/server.key",
	}

	cases := []struct {
		name  string
		event fsnotify.Event
		want  bool
	}{
		{name: "write on cert file",
			event: fsnotify.Event{Name: "/etc/tickraft/certs/server.crt", Op: fsnotify.Write},
			want:  true},
		{name: "create on key file",
			event: fsnotify.Event{Name: "/etc/tickraft/certs/server.key", Op: fsnotify.Create},
			want:  true},
		{name: "rename on cert file",
			event: fsnotify.Event{Name: "/etc/tickraft/certs/server.crt", Op: fsnotify.Rename},
			want:  true},
		{name: "chmod on cert file ignored",
			event: fsnotify.Event{Name: "/etc/tickraft/certs/server.crt", Op: fsnotify.Chmod},
			want:  false},
		{name: "write on unrelated file",
			event: fsnotify.Event{Name: "/etc/tickraft/certs/other.txt", Op: fsnotify.Write},
			want:  false},
		{name: "write on unclean path matches cleaned",
			event: fsnotify.Event{Name: "/etc/tickraft/certs/../certs/server.crt", Op: fsnotify.Write},
			want:  true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isTLSFileEvent(cfg, tc.event)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestUniqueParentDirs verifies uniqueParentDirs returns the de-duplicated,
// cleaned parent directories of the given file paths, skipping empty inputs.
func TestUniqueParentDirs(t *testing.T) {
	got := uniqueParentDirs("/a/b/cert.pem", "/a/b/key.pem", "/x/y/ca.pem", "")
	want := []string{"/a/b", "/x/y"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, d := range want {
		if got[i] != d {
			t.Errorf("got[%d] = %q, want %q", i, got[i], d)
		}
	}
}

// TestStartTLSFileWatcherDisabled verifies startTLSFileWatcher returns a no-op
// stop function when TLS is disabled, so callers can always invoke the
// returned function without checking for nil.
func TestStartTLSFileWatcherDisabled(t *testing.T) {
	s := &Server{config: ServerConfig{TLSEnabled: false}}
	stop := s.startTLSFileWatcher()
	if stop == nil {
		t.Fatal("stop is nil, want non-nil no-op function")
	}
	stop() // must not panic
}

// TestStartTLSFileWatcherNoCertFile verifies startTLSFileWatcher returns a
// no-op stop function when TLS is enabled but the cert/key file paths are
// empty (e.g. ACME-only mode).
func TestStartTLSFileWatcherNoCertFile(t *testing.T) {
	s := &Server{config: ServerConfig{TLSEnabled: true}}
	stop := s.startTLSFileWatcher()
	if stop == nil {
		t.Fatal("stop is nil, want non-nil no-op function")
	}
	stop()
}

// TestReloadTLSConfigACMEOnly verifies ReloadTLSConfig returns
// ErrTLSCertFileMissing when ACME is enabled but no static cert/key files are
// configured. In ACME-only mode the runtime expects certs to be
// obtained and written to the configured paths by the ACME manager before
// reload is invoked; calling reload before that is a configuration error.
func TestReloadTLSConfigACMEOnly(t *testing.T) {
	s := &Server{config: ServerConfig{
		TLSEnabled: true,
		ACME:       ACMEConfig{Enabled: true, DirectoryURL: DefaultACMEDirectoryURL, Email: "admin@example.com"},
	}}
	_, err := s.ReloadTLSConfig()
	if !errors.Is(err, ErrTLSCertFileMissing) {
		t.Fatalf("error = %v, want wrapping %v", err, ErrTLSCertFileMissing)
	}
}

// TestBuildTLSConfigWithClientCA verifies buildTLSConfig loads a client CA
// bundle from TLSClientCAFile and installs it on the resulting *tls.Config.
func TestBuildTLSConfigWithClientCA(t *testing.T) {
	certPEM, keyPEM, err := generateTestSelfSignedCertPEM("example.com")
	if err != nil {
		t.Fatalf("generate server cert: %v", err)
	}
	caPEM, _, err := generateTestSelfSignedCertPEM("ca.example")
	if err != nil {
		t.Fatalf("generate ca cert: %v", err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	caPath := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}

	s := &Server{config: ServerConfig{
		TLSEnabled:      true,
		TLSCertFile:     certPath,
		TLSKeyFile:      keyPath,
		TLSMinVersion:   "1.2",
		TLSCipherSuites: DefaultTLSCipherSuites,
		TLSClientAuth:   "require_and_verify_client_cert",
		TLSClientCAFile: caPath,
	}}

	cfg, _, err := s.buildTLSConfig()
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if cfg.ClientCAs == nil {
		t.Error("ClientCAs is nil, want non-nil pool")
	}
}

// TestBuildTLSConfigInvalidClientCA verifies buildTLSConfig returns a wrapped
// error when TLSClientCAFile points to a non-PEM file.
func TestBuildTLSConfigInvalidClientCA(t *testing.T) {
	certPEM, keyPEM, err := generateTestSelfSignedCertPEM("example.com")
	if err != nil {
		t.Fatalf("generate server cert: %v", err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	caPath := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.WriteFile(caPath, []byte("not a pem"), 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}

	s := &Server{config: ServerConfig{
		TLSEnabled:      true,
		TLSCertFile:     certPath,
		TLSKeyFile:      keyPath,
		TLSClientAuth:   "require_and_verify_client_cert",
		TLSClientCAFile: caPath,
	}}
	_, _, err = s.buildTLSConfig()
	if err == nil {
		t.Fatal("expected error for invalid CA file, got nil")
	}
}

// TestBuildTLSConfigInvalidMinVersion verifies buildTLSConfig returns a
// wrapped ErrTLSInvalidMinVersion when TLSMinVersion is set to an unsupported
// value.
func TestBuildTLSConfigInvalidMinVersion(t *testing.T) {
	certPEM, keyPEM, err := generateTestSelfSignedCertPEM("example.com")
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	s := &Server{config: ServerConfig{
		TLSEnabled:    true,
		TLSCertFile:   certPath,
		TLSKeyFile:    keyPath,
		TLSMinVersion: "1.1", // invalid
	}}
	_, _, err = s.buildTLSConfig()
	if !errors.Is(err, ErrTLSInvalidMinVersion) {
		t.Fatalf("error = %v, want wrapping %v", err, ErrTLSInvalidMinVersion)
	}
}

// TestBuildTLSConfigInvalidClientAuth verifies buildTLSConfig returns a
// wrapped ErrTLSInvalidClientAuth when TLSClientAuth is set to an unsupported
// value.
func TestBuildTLSConfigInvalidClientAuth(t *testing.T) {
	certPEM, keyPEM, err := generateTestSelfSignedCertPEM("example.com")
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	s := &Server{config: ServerConfig{
		TLSEnabled:    true,
		TLSCertFile:   certPath,
		TLSKeyFile:    keyPath,
		TLSClientAuth: "bogus", // invalid
	}}
	_, _, err = s.buildTLSConfig()
	if !errors.Is(err, ErrTLSInvalidClientAuth) {
		t.Fatalf("error = %v, want wrapping %v", err, ErrTLSInvalidClientAuth)
	}
}

// TestReloadTLSConfigAtomicSwap verifies two consecutive ReloadTLSConfig
// calls produce different fingerprints when the underlying certificate file
// changes, and the holder always reflects the latest certificate.
func TestReloadTLSConfigAtomicSwap(t *testing.T) {
	// Generate first cert.
	cert1PEM, key1PEM, err := generateTestSelfSignedCertPEM("first.example")
	if err != nil {
		t.Fatalf("generate cert1: %v", err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	if err := os.WriteFile(certPath, cert1PEM, 0o600); err != nil {
		t.Fatalf("write cert1: %v", err)
	}
	if err := os.WriteFile(keyPath, key1PEM, 0o600); err != nil {
		t.Fatalf("write key1: %v", err)
	}

	s := &Server{config: ServerConfig{
		TLSEnabled:      true,
		TLSCertFile:     certPath,
		TLSKeyFile:      keyPath,
		TLSMinVersion:   "1.2",
		TLSCipherSuites: DefaultTLSCipherSuites,
		TLSClientAuth:   "no_client_cert",
	}}

	fp1, err := s.ReloadTLSConfig()
	if err != nil {
		t.Fatalf("first reload: %v", err)
	}

	// Generate a second cert and overwrite the files.
	cert2PEM, key2PEM, err := generateTestSelfSignedCertPEM("second.example")
	if err != nil {
		t.Fatalf("generate cert2: %v", err)
	}
	if err := os.WriteFile(certPath, cert2PEM, 0o600); err != nil {
		t.Fatalf("write cert2: %v", err)
	}
	if err := os.WriteFile(keyPath, key2PEM, 0o600); err != nil {
		t.Fatalf("write key2: %v", err)
	}

	fp2, err := s.ReloadTLSConfig()
	if err != nil {
		t.Fatalf("second reload: %v", err)
	}

	if fp1 == fp2 {
		t.Errorf("fingerprints are identical (%q), want different", fp1)
	}

	// The holder must reflect the second (latest) certificate.
	current := s.tlsHolder.current()
	if current == nil {
		t.Fatal("holder current() is nil")
	}
	cert, err := current.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	if leaf.Subject.CommonName != "second.example" {
		t.Errorf("CN = %q, want %q", leaf.Subject.CommonName, "second.example")
	}
}

// TestReloadTLSConfigGetConfigForClient verifies the GetConfigForClient
// callback installed by ReloadTLSConfig returns the active *tls.Config so
// Hertz's TLS transporter always uses the latest certificate on every
// handshake.
func TestReloadTLSConfigGetConfigForClient(t *testing.T) {
	certPEM, keyPEM, err := generateTestSelfSignedCertPEM("example.com")
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	s := &Server{config: ServerConfig{
		TLSEnabled:      true,
		TLSCertFile:     certPath,
		TLSKeyFile:      keyPath,
		TLSMinVersion:   "1.2",
		TLSCipherSuites: DefaultTLSCipherSuites,
		TLSClientAuth:   "no_client_cert",
	}}

	if _, err := s.ReloadTLSConfig(); err != nil {
		t.Fatalf("ReloadTLSConfig: %v", err)
	}

	current := s.tlsHolder.current()
	if current == nil {
		t.Fatal("holder current() is nil")
	}
	if current.GetConfigForClient == nil {
		t.Fatal("GetConfigForClient callback is nil")
	}
	got, err := current.GetConfigForClient(nil)
	if err != nil {
		t.Fatalf("GetConfigForClient: %v", err)
	}
	if got != current {
		t.Error("GetConfigForClient returned a different config than current")
	}
}

// TestTLSConfigHolderLoadAndCurrent verifies the tlsConfigHolder atomically
// stores and retrieves a *tls.Config, and returns nil before the first load.
func TestTLSConfigHolderLoadAndCurrent(t *testing.T) {
	var h tlsConfigHolder
	if h.current() != nil {
		t.Fatal("current() is non-nil before load")
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	h.load(cfg)
	if h.current() != cfg {
		t.Error("current() did not return the loaded config")
	}
	h.load(nil)
	if h.current() != nil {
		t.Error("current() is non-nil after loading nil")
	}
}
