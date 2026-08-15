// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package api

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// TLS security-baseline and ACME defaults shared by all editions. The
// runtime shares the same TLS configuration schema as extended
// editions; differences live only in the ACME implementation backend.
const (
	// DefaultTLSMinVersion is the default minimum TLS version, aligned with
	// the TLS 1.2+ security baseline shared by all editions.
	DefaultTLSMinVersion = "1.2"

	// DefaultTLSClientAuth is the default client authentication mode, which
	// neither requests nor verifies client certificates.
	DefaultTLSClientAuth = "no_client_cert"

	// DefaultACMEDirectoryURL is the default ACME directory URL pointing at
	// the Let's Encrypt production endpoint.
	DefaultACMEDirectoryURL = "https://acme-v02.api.letsencrypt.org/directory"

	// DefaultACMEChallengeType is the default ACME challenge type. The
	// runtime implements HTTP-01 via golang.org/x/crypto/acme;
	// DNS-01 is provided through an extension.
	DefaultACMEChallengeType = "http-01"
)

// DefaultTLSCipherSuites is the default cipher-suite whitelist shared by all
// editions. Forward-secret (ECDHE) suites are prioritized and static RSA
// key-exchange suites are excluded, satisfying the TLS security baseline. TLS
// 1.3 suites are included because TLS 1.3 ignores the TLS 1.2 cipher-suite
// list and negotiates from its own fixed set.
var DefaultTLSCipherSuites = []string{
	"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
	"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
	"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
	"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
	"TLS_AES_256_GCM_SHA384", // TLS 1.3
	"TLS_AES_128_GCM_SHA256", // TLS 1.3
}

// allowedTLSMinVersions is the set of accepted TLS minimum versions.
var allowedTLSMinVersions = map[string]struct{}{
	"1.2": {},
	"1.3": {},
}

// allowedTLSClientAuthModes is the set of accepted client authentication
// modes, mirroring crypto/tls.ClientAuthType values.
var allowedTLSClientAuthModes = map[string]struct{}{
	"no_client_cert":                 {},
	"request_client_cert":            {},
	"require_any_client_cert":        {},
	"verify_client_cert_if_given":    {},
	"require_and_verify_client_cert": {},
}

// allowedACMEChallengeTypes is the set of accepted ACME challenge types.
var allowedACMEChallengeTypes = map[string]struct{}{
	"http-01": {},
	"dns-01":  {},
}

// Sentinel validation errors. Callers may use errors.Is to detect a specific
// validation failure.
var (
	// ErrTLSMinVersionInvalid is returned when TLSMinVersion is set to a value
	// other than "1.2" or "1.3".
	ErrTLSMinVersionInvalid = errors.New("tls_min_version must be \"1.2\" or \"1.3\"")

	// ErrTLSClientAuthInvalid is returned when TLSClientAuth is set to a value
	// outside the five accepted client authentication modes.
	ErrTLSClientAuthInvalid = errors.New("tls_client_auth must be one of: no_client_cert, request_client_cert, require_any_client_cert, verify_client_cert_if_given, require_and_verify_client_cert")

	// ErrTLSCertRequired is returned when TLS is enabled without ACME but the
	// certificate or key file path is empty.
	ErrTLSCertRequired = errors.New("tls_cert_file and tls_key_file are required when tls is enabled and acme is disabled")

	// ErrACMEEmailRequired is returned when ACME is enabled but the
	// registration email is empty.
	ErrACMEEmailRequired = errors.New("acme email is required when acme is enabled")

	// ErrACMEChallengeInvalid is returned when the ACME challenge type is set
	// to a value other than "http-01" or "dns-01".
	ErrACMEChallengeInvalid = errors.New("acme challenge_type must be \"http-01\" or \"dns-01\"")

	// ErrACMEDirectoryURLRequired is returned when ACME is enabled but the
	// directory URL is empty.
	ErrACMEDirectoryURLRequired = errors.New("acme directory_url is required when acme is enabled")
)

// ServerConfig holds the HTTP server configuration.
type ServerConfig struct {
	// Addr is the listen address, e.g. ":6153".
	Addr string
	// Mode is the running mode: "debug" or "release".
	Mode string
	// ReadTimeout is the maximum duration for reading the entire request.
	ReadTimeout time.Duration
	// WriteTimeout is the maximum duration before timing out writes of the response.
	WriteTimeout time.Duration
	// EnableCORS indicates whether to enable CORS middleware.
	EnableCORS bool
	// AllowedOrigins is the list of origin URLs permitted by CORS.
	// When non-empty, only these origins receive Access-Control-Allow-Origin
	// with credentials. When empty, CORS allows all origins (*) without
	// credentials.
	AllowedOrigins []string
	// EnableLog indicates whether to enable access log middleware.
	EnableLog bool
	// MaxHeaderBytes is the maximum number of bytes the server will read
	// when parsing request headers. A zero or negative value means no limit.
	MaxHeaderBytes int
	// VirtualHosts configures host-based route group dispatch (design doc
	// section 7.7). When nil, VirtualHost dispatch is disabled and all
	// requests route to the default group.
	VirtualHosts *VirtualHostConfig
	// TrustedProxies is a list of trusted proxy CIDRs (design doc chapter 5).
	// When non-empty, the trusted-proxy middleware resolves the real client IP
	// from X-Forwarded-For for requests originating from these CIDRs.
	TrustedProxies []string

	// TLSEnabled toggles TLS termination for the HTTP server. When true the
	// server serves HTTPS on Addr; when false plain HTTP is served. This
	// field and the TLS* fields below share the same schema across all
	// editions.
	TLSEnabled bool `yaml:"tls_enabled" json:"tls_enabled"`
	// TLSCertFile is the PEM-encoded server certificate path. Required when
	// TLSEnabled is true and ACME is disabled.
	TLSCertFile string `yaml:"tls_cert_file" json:"tls_cert_file"`
	// TLSKeyFile is the PEM-encoded server private key path. Required when
	// TLSEnabled is true and ACME is disabled.
	TLSKeyFile string `yaml:"tls_key_file" json:"tls_key_file"`
	// TLSMinVersion is the minimum TLS version: "1.2" or "1.3". Defaults to
	// "1.2" (DefaultTLSMinVersion), aligned with the TLS 1.2+ security
	// baseline shared by all editions.
	TLSMinVersion string `yaml:"tls_min_version" json:"tls_min_version"`
	// TLSCipherSuites is the cipher-suite whitelist. When empty after
	// SetDefaults it falls back to DefaultTLSCipherSuites. Forward-secret
	// suites are prioritized; static RSA key-exchange suites are excluded.
	TLSCipherSuites []string `yaml:"tls_cipher_suites" json:"tls_cipher_suites"`
	// TLSClientCAFile is the PEM-encoded client CA certificate path used for
	// mutual TLS. Empty means no client certificate verification.
	TLSClientCAFile string `yaml:"tls_client_ca_file" json:"tls_client_ca_file"`
	// TLSClientAuth is the client authentication mode: no_client_cert,
	// request_client_cert, require_any_client_cert,
	// verify_client_cert_if_given, or require_and_verify_client_cert. Defaults
	// to no_client_cert (DefaultTLSClientAuth).
	TLSClientAuth string `yaml:"tls_client_auth" json:"tls_client_auth"`

	// ACME configures automatic certificate issuance via the ACME protocol.
	// When ACME.Enabled is true the server obtains certificates from an ACME
	// directory (e.g. Let's Encrypt) instead of relying on statically
	// configured TLSCertFile/TLSKeyFile.
	ACME ACMEConfig `yaml:"acme" json:"acme"`
}

// ACMEConfig configures automatic certificate issuance via the ACME protocol
// (RFC 8555). The runtime implements the HTTP-01 challenge based
// on golang.org/x/crypto/acme; DNS-01 is provided through an extension.
// The configuration schema is shared across all editions.
type ACMEConfig struct {
	// Enabled toggles ACME automatic certificate issuance and renewal.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// DirectoryURL is the ACME directory URL. Defaults to the Let's Encrypt
	// production endpoint (DefaultACMEDirectoryURL). Required when Enabled is
	// true.
	DirectoryURL string `yaml:"directory_url" json:"directory_url"`
	// Email is the ACME registration email. Required when Enabled is true.
	Email string `yaml:"email" json:"email"`
	// ChallengeType is the ACME challenge type: "http-01" or "dns-01". Defaults
	// to "http-01" (DefaultACMEChallengeType). The runtime
	// implements only HTTP-01; DNS-01 is provided through an extension.
	ChallengeType string `yaml:"challenge_type" json:"challenge_type"`
	// Domains is the list of domain names for which the manager should obtain
	// certificates via the ACME protocol. At least one domain is required when
	// Enabled is true and the ACME manager is started by the server. The list
	// is forwarded as-is from pkg/config.ACMEConfig by the start command.
	Domains []string `yaml:"domains" json:"domains"`
}

// SetDefaults populates the TLS and ACME fields of ServerConfig with their
// default values. Fields already set to non-zero values are preserved, so
// callers may invoke SetDefaults on a partially populated config before
// unmarshaling. The TLSCipherSuites default is copied so mutating the
// configured slice does not alias the package-level DefaultTLSCipherSuites.
func (c *ServerConfig) SetDefaults() {
	if c.TLSMinVersion == "" {
		c.TLSMinVersion = DefaultTLSMinVersion
	}
	if len(c.TLSCipherSuites) == 0 {
		c.TLSCipherSuites = append([]string(nil), DefaultTLSCipherSuites...)
	}
	if c.TLSClientAuth == "" {
		c.TLSClientAuth = DefaultTLSClientAuth
	}
	c.ACME.SetDefaults()
}

// SetDefaults populates ACMEConfig with default values. Fields already set to
// non-zero values are preserved.
func (c *ACMEConfig) SetDefaults() {
	if c.DirectoryURL == "" {
		c.DirectoryURL = DefaultACMEDirectoryURL
	}
	if c.ChallengeType == "" {
		c.ChallengeType = DefaultACMEChallengeType
	}
}

// Validate checks the TLS and ACME configuration for correctness and returns
// a wrapped sentinel error (see the ErrTLS* and ErrACME* variables) so callers
// may use errors.Is to detect a specific failure. It returns nil when the
// configuration is valid.
//
// Validation rules:
//   - TLSMinVersion, when non-empty, must be "1.2" or "1.3".
//   - TLSClientAuth, when non-empty, must be one of the five accepted modes.
//   - When TLSEnabled is true and ACME is disabled, TLSCertFile and TLSKeyFile
//     must both be non-empty.
//   - ACME sub-configuration is validated via ACMEConfig.Validate.
func (c *ServerConfig) Validate() error {
	if c.TLSMinVersion != "" {
		if _, ok := allowedTLSMinVersions[c.TLSMinVersion]; !ok {
			return fmt.Errorf("tls_min_version %q: %w", c.TLSMinVersion, ErrTLSMinVersionInvalid)
		}
	}
	if c.TLSClientAuth != "" {
		if _, ok := allowedTLSClientAuthModes[c.TLSClientAuth]; !ok {
			return fmt.Errorf("tls_client_auth %q: %w", c.TLSClientAuth, ErrTLSClientAuthInvalid)
		}
	}
	// When TLS is enabled without ACME, a static certificate and key are
	// required. When ACME is enabled, certificates are obtained automatically.
	if c.TLSEnabled && !c.ACME.Enabled {
		if c.TLSCertFile == "" || c.TLSKeyFile == "" {
			return ErrTLSCertRequired
		}
	}
	if err := c.ACME.Validate(); err != nil {
		return fmt.Errorf("acme: %w", err)
	}
	return nil
}

// Validate checks the ACME configuration for correctness. It returns nil when
// ACME is disabled.
//
// Validation rules:
//   - Email must be non-empty when Enabled is true.
//   - DirectoryURL must be non-empty when Enabled is true.
//   - ChallengeType, when non-empty, must be "http-01" or "dns-01".
func (c *ACMEConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Email == "" {
		return ErrACMEEmailRequired
	}
	if c.DirectoryURL == "" {
		return ErrACMEDirectoryURLRequired
	}
	if c.ChallengeType != "" {
		if _, ok := allowedACMEChallengeTypes[c.ChallengeType]; !ok {
			return fmt.Errorf("challenge_type %q: %w", c.ChallengeType, ErrACMEChallengeInvalid)
		}
	}
	return nil
}

// IsForwardSecretCipherSuite reports whether the given cipher-suite name
// provides forward secrecy. A suite is forward-secret when its key exchange
// uses ECDHE or DHE, or when it is a TLS 1.3 suite (TLS 1.3 mandates forward
// secrecy by design). Static RSA key-exchange suites (TLS_RSA_WITH_*) are not
// forward-secret.
func IsForwardSecretCipherSuite(suite string) bool {
	upper := strings.ToUpper(suite)
	return strings.HasPrefix(upper, "TLS_ECDHE_") ||
		strings.HasPrefix(upper, "TLS_DHE_") ||
		strings.HasPrefix(upper, "TLS_AES_") ||
		strings.HasPrefix(upper, "TLS_CHACHA20_")
}
