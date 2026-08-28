// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package api

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestServerConfigTLSDefaults verifies that SetDefaults populates the TLS
// fields of ServerConfig with the shared security-baseline defaults and that
// the cipher-suite default is a defensive copy (mutating the configured slice
// must not alias the package-level DefaultTLSCipherSuites).
func TestServerConfigTLSDefaults(t *testing.T) {
	var cfg ServerConfig
	cfg.SetDefaults()

	if cfg.TLSMinVersion != DefaultTLSMinVersion {
		t.Errorf("TLSMinVersion: got %q, want %q", cfg.TLSMinVersion, DefaultTLSMinVersion)
	}
	if cfg.TLSClientAuth != DefaultTLSClientAuth {
		t.Errorf("TLSClientAuth: got %q, want %q", cfg.TLSClientAuth, DefaultTLSClientAuth)
	}
	if !reflect.DeepEqual(cfg.TLSCipherSuites, DefaultTLSCipherSuites) {
		t.Errorf("TLSCipherSuites: got %v, want %v", cfg.TLSCipherSuites, DefaultTLSCipherSuites)
	}

	// ACME defaults propagate through ServerConfig.SetDefaults.
	if cfg.ACME.DirectoryURL != DefaultACMEDirectoryURL {
		t.Errorf("ACME.DirectoryURL: got %q, want %q", cfg.ACME.DirectoryURL, DefaultACMEDirectoryURL)
	}
	if cfg.ACME.ChallengeType != DefaultACMEChallengeType {
		t.Errorf("ACME.ChallengeType: got %q, want %q", cfg.ACME.ChallengeType, DefaultACMEChallengeType)
	}
	if cfg.ACME.Enabled {
		t.Errorf("ACME.Enabled: got true, want false")
	}

	// Ensure SetDefaults produced an independent copy so the package-level
	// default slice cannot be mutated through a ServerConfig instance.
	original := append([]string(nil), DefaultTLSCipherSuites...)
	cfg.TLSCipherSuites[0] = "MUTATED"
	if !reflect.DeepEqual(DefaultTLSCipherSuites, original) {
		t.Errorf("SetDefaults aliased DefaultTLSCipherSuites: got %v, want %v", DefaultTLSCipherSuites, original)
	}

	// SetDefaults must preserve caller-provided values.
	cfg2 := ServerConfig{
		TLSMinVersion:   "1.3",
		TLSClientAuth:   "require_and_verify_client_cert",
		TLSCipherSuites: []string{"TLS_AES_256_GCM_SHA384"},
		ACME: ACMEConfig{
			DirectoryURL:  "https://acme-staging-v02.api.letsencrypt.org/directory",
			ChallengeType: "dns-01",
		},
	}
	cfg2.SetDefaults()
	if cfg2.TLSMinVersion != "1.3" {
		t.Errorf("SetDefaults overwrote TLSMinVersion: got %q, want %q", cfg2.TLSMinVersion, "1.3")
	}
	if cfg2.TLSClientAuth != "require_and_verify_client_cert" {
		t.Errorf("SetDefaults overwrote TLSClientAuth: got %q", cfg2.TLSClientAuth)
	}
	if !reflect.DeepEqual(cfg2.TLSCipherSuites, []string{"TLS_AES_256_GCM_SHA384"}) {
		t.Errorf("SetDefaults overwrote TLSCipherSuites: got %v", cfg2.TLSCipherSuites)
	}
	if cfg2.ACME.DirectoryURL != "https://acme-staging-v02.api.letsencrypt.org/directory" {
		t.Errorf("SetDefaults overwrote ACME.DirectoryURL: got %q", cfg2.ACME.DirectoryURL)
	}
	if cfg2.ACME.ChallengeType != "dns-01" {
		t.Errorf("SetDefaults overwrote ACME.ChallengeType: got %q", cfg2.ACME.ChallengeType)
	}
}

// TestServerConfigTLSValidation verifies the TLS and ACME validation rules,
// including valid configurations that pass and invalid configurations that
// return a wrapped sentinel error detectable via errors.Is.
func TestServerConfigTLSValidation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     ServerConfig
		wantErr error
	}{
		{
			name: "valid empty config",
			cfg:  ServerConfig{},
		},
		{
			name: "valid tls 1.2 no client cert",
			cfg:  ServerConfig{TLSMinVersion: "1.2", TLSClientAuth: "no_client_cert"},
		},
		{
			name: "valid tls 1.3 require and verify",
			cfg:  ServerConfig{TLSMinVersion: "1.3", TLSClientAuth: "require_and_verify_client_cert"},
		},
		{
			name: "valid all client auth modes",
			cfg: ServerConfig{
				TLSClientAuth: "request_client_cert",
			},
		},
		{
			name:    "invalid tls min version 1.0",
			cfg:     ServerConfig{TLSMinVersion: "1.0"},
			wantErr: ErrTLSMinVersionInvalid,
		},
		{
			name:    "invalid tls min version ssl3",
			cfg:     ServerConfig{TLSMinVersion: "ssl3"},
			wantErr: ErrTLSMinVersionInvalid,
		},
		{
			name:    "invalid tls client auth mode",
			cfg:     ServerConfig{TLSClientAuth: "bogus_mode"},
			wantErr: ErrTLSClientAuthInvalid,
		},
		{
			name:    "tls enabled without cert and key",
			cfg:     ServerConfig{TLSEnabled: true},
			wantErr: ErrTLSCertRequired,
		},
		{
			name:    "tls enabled with cert but missing key",
			cfg:     ServerConfig{TLSEnabled: true, TLSCertFile: "cert.pem"},
			wantErr: ErrTLSCertRequired,
		},
		{
			name: "tls enabled with cert and key",
			cfg:  ServerConfig{TLSEnabled: true, TLSCertFile: "cert.pem", TLSKeyFile: "key.pem"},
		},
		{
			name: "tls enabled with acme does not require static cert",
			cfg: ServerConfig{
				TLSEnabled: true,
				ACME: ACMEConfig{
					Enabled:       true,
					Email:         "admin@example.com",
					DirectoryURL:  DefaultACMEDirectoryURL,
					ChallengeType: "http-01",
				},
			},
		},
		{
			name: "tls disabled does not require cert even with acme disabled",
			cfg:  ServerConfig{TLSEnabled: false},
		},
		{
			name: "acme disabled ignores email and challenge",
			cfg: ServerConfig{
				ACME: ACMEConfig{Enabled: false, Email: "", ChallengeType: "bogus"},
			},
		},
		{
			name:    "acme enabled without email",
			cfg:     ServerConfig{ACME: ACMEConfig{Enabled: true, Email: "", DirectoryURL: DefaultACMEDirectoryURL}},
			wantErr: ErrACMEEmailRequired,
		},
		{
			name:    "acme enabled without directory url",
			cfg:     ServerConfig{ACME: ACMEConfig{Enabled: true, Email: "admin@example.com", DirectoryURL: ""}},
			wantErr: ErrACMEDirectoryURLRequired,
		},
		{
			name: "acme enabled with dns 01 challenge",
			cfg: ServerConfig{
				ACME: ACMEConfig{
					Enabled:       true,
					Email:         "admin@example.com",
					DirectoryURL:  DefaultACMEDirectoryURL,
					ChallengeType: "dns-01",
				},
			},
		},
		{
			name: "acme enabled with invalid challenge type",
			cfg: ServerConfig{
				ACME: ACMEConfig{
					Enabled:       true,
					Email:         "admin@example.com",
					DirectoryURL:  DefaultACMEDirectoryURL,
					ChallengeType: "tls-01",
				},
			},
			wantErr: ErrACMEChallengeInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
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

// TestACMEConfigDefaults verifies that SetDefaults populates the ACME directory
// URL and challenge type while preserving caller-provided values and the
// Enabled/Email defaults.
func TestACMEConfigDefaults(t *testing.T) {
	var cfg ACMEConfig
	cfg.SetDefaults()

	if cfg.DirectoryURL != DefaultACMEDirectoryURL {
		t.Errorf("DirectoryURL: got %q, want %q", cfg.DirectoryURL, DefaultACMEDirectoryURL)
	}
	if cfg.ChallengeType != DefaultACMEChallengeType {
		t.Errorf("ChallengeType: got %q, want %q", cfg.ChallengeType, DefaultACMEChallengeType)
	}
	if cfg.Enabled {
		t.Errorf("Enabled: got true, want false")
	}
	if cfg.Email != "" {
		t.Errorf("Email: got %q, want empty", cfg.Email)
	}

	// Caller-provided values must be preserved.
	cfg2 := ACMEConfig{
		DirectoryURL:  "https://acme-staging-v02.api.letsencrypt.org/directory",
		ChallengeType: "dns-01",
	}
	cfg2.SetDefaults()
	if cfg2.DirectoryURL != "https://acme-staging-v02.api.letsencrypt.org/directory" {
		t.Errorf("SetDefaults overwrote DirectoryURL: got %q", cfg2.DirectoryURL)
	}
	if cfg2.ChallengeType != "dns-01" {
		t.Errorf("SetDefaults overwrote ChallengeType: got %q", cfg2.ChallengeType)
	}
}

// TestACMEConfigValidation verifies the ACME validation rules in isolation.
func TestACMEConfigValidation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     ACMEConfig
		wantErr error
	}{
		{
			name: "disabled skips validation",
			cfg:  ACMEConfig{Enabled: false, Email: "", ChallengeType: "bogus"},
		},
		{
			name: "enabled http 01 valid",
			cfg: ACMEConfig{
				Enabled:       true,
				Email:         "admin@example.com",
				DirectoryURL:  DefaultACMEDirectoryURL,
				ChallengeType: "http-01",
			},
		},
		{
			name: "enabled dns 01 valid",
			cfg: ACMEConfig{
				Enabled:       true,
				Email:         "admin@example.com",
				DirectoryURL:  DefaultACMEDirectoryURL,
				ChallengeType: "dns-01",
			},
		},
		{
			name: "enabled without challenge type allowed",
			cfg: ACMEConfig{
				Enabled:      true,
				Email:        "admin@example.com",
				DirectoryURL: DefaultACMEDirectoryURL,
			},
		},
		{
			name: "enabled without email",
			cfg: ACMEConfig{
				Enabled:      true,
				Email:        "",
				DirectoryURL: DefaultACMEDirectoryURL,
			},
			wantErr: ErrACMEEmailRequired,
		},
		{
			name: "enabled without directory url",
			cfg: ACMEConfig{
				Enabled: true,
				Email:   "admin@example.com",
			},
			wantErr: ErrACMEDirectoryURLRequired,
		},
		{
			name: "enabled with invalid challenge type",
			cfg: ACMEConfig{
				Enabled:       true,
				Email:         "admin@example.com",
				DirectoryURL:  DefaultACMEDirectoryURL,
				ChallengeType: "tls-01",
			},
			wantErr: ErrACMEChallengeInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
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

// TestDefaultTLSCipherSuites verifies that the shared default cipher-suite
// list contains only forward-secret suites and excludes weak crypto (MD5,
// SHA1, RC4, CBC, and static RSA key exchange).
func TestDefaultTLSCipherSuites(t *testing.T) {
	if len(DefaultTLSCipherSuites) == 0 {
		t.Fatal("DefaultTLSCipherSuites is empty")
	}

	seen := make(map[string]struct{}, len(DefaultTLSCipherSuites))
	for _, suite := range DefaultTLSCipherSuites {
		if _, dup := seen[suite]; dup {
			t.Errorf("duplicate cipher suite %q", suite)
		}
		seen[suite] = struct{}{}

		if !IsForwardSecretCipherSuite(suite) {
			t.Errorf("cipher suite %q is not forward-secret", suite)
		}
		if isWeakCipherSuite(suite) {
			t.Errorf("cipher suite %q contains weak crypto", suite)
		}
	}

	// Sanity-check the helper: static RSA suites must be rejected and TLS 1.3
	// / ECDHE suites must be accepted.
	if IsForwardSecretCipherSuite("TLS_RSA_WITH_AES_128_CBC_SHA256") {
		t.Error("IsForwardSecretCipherSuite accepted static RSA CBC suite")
	}
	if !IsForwardSecretCipherSuite("TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256") {
		t.Error("IsForwardSecretCipherSuite rejected ECDHE GCM suite")
	}
	if !IsForwardSecretCipherSuite("TLS_CHACHA20_POLY1305_SHA256") {
		t.Error("IsForwardSecretCipherSuite rejected TLS 1.3 ChaCha20 suite")
	}
}

// isWeakCipherSuite reports whether the given cipher-suite name contains weak
// cryptography that the TLS security baseline forbids: MD5, SHA1, RC4, CBC
// mode, or a static RSA key exchange (TLS_RSA_WITH_*).
func isWeakCipherSuite(suite string) bool {
	s := strings.ToUpper(suite)
	return strings.Contains(s, "MD5") ||
		strings.Contains(s, "SHA1") ||
		strings.Contains(s, "RC4") ||
		strings.Contains(s, "CBC") ||
		strings.HasPrefix(s, "TLS_RSA_WITH_")
}
