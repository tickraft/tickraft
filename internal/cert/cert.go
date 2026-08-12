// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cert

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Default flag values for the self-sign command. Chosen so that running
// `tickraft cert selfsign --domain example.com` produces a usable certificate
// without any further configuration.
const (
	// DefaultDays is the default certificate validity period in days.
	DefaultDays = 365
	// DefaultKeyType is the default private key type ("rsa" or "ecdsa").
	DefaultKeyType = "ecdsa"
	// DefaultOutput is the default output directory for generated cert/key files.
	DefaultOutput = "./certs"
	// RSABits is the RSA key size in bits used when KeyType is "rsa".
	RSABits = 2048
)

// Sentinel errors for self-sign failures. Callers may use errors.Is to detect
// a specific failure.
var (
	// ErrDomainRequired is returned when the Domain option is empty.
	ErrDomainRequired = errors.New("domain is required")
	// ErrUnsupportedKeyType is returned when KeyType is not "rsa" or "ecdsa".
	ErrUnsupportedKeyType = errors.New("unsupported key type (must be rsa or ecdsa)")
	// ErrInvalidDays is returned when Days is not positive.
	ErrInvalidDays = errors.New("days must be positive")
)

// Options configures self-signed certificate generation.
type Options struct {
	// Domain is used as the certificate CommonName and SAN value. It may be a
	// hostname (placed in DNSNames) or an IP literal (placed in IPAddresses).
	Domain string
	// Days is the certificate validity period in days; must be positive.
	Days int
	// KeyType selects the private key algorithm: "rsa" or "ecdsa".
	KeyType string
}

// Result holds the PEM-encoded certificate and private key produced by Generate.
type Result struct {
	CertPEM []byte
	KeyPEM  []byte
}

// Generate validates opts, generates a self-signed certificate and private key,
// and returns them PEM-encoded. It returns a wrapped sentinel error
// (ErrDomainRequired, ErrInvalidDays, or ErrUnsupportedKeyType) on validation
// failure, or a wrapped crypto error on key/certificate generation failure.
func Generate(opts Options) (*Result, error) {
	if opts.Domain == "" {
		return nil, ErrDomainRequired
	}
	if opts.Days <= 0 {
		return nil, fmt.Errorf("days must be positive, got %d: %w", opts.Days, ErrInvalidDays)
	}
	if opts.KeyType != "rsa" && opts.KeyType != "ecdsa" {
		return nil, fmt.Errorf("key-type %q: %w", opts.KeyType, ErrUnsupportedKeyType)
	}

	notBefore := time.Now().Add(-time.Minute) // small clock-skew tolerance
	notAfter := notBefore.AddDate(0, 0, opts.Days)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   opts.Domain,
			Organization: []string{"tickraft self-signed"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		DNSNames:              []string{opts.Domain},
	}
	if ip := net.ParseIP(opts.Domain); ip != nil {
		template.IPAddresses = []net.IP{ip}
	}

	key, err := generatePrivateKey(opts.KeyType)
	if err != nil {
		return nil, err
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, publicKey(key), key)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return &Result{CertPEM: certPEM, KeyPEM: keyPEM}, nil
}

// WriteToDir generates a self-signed certificate via Generate and writes the
// certificate and private key to outputDir as <domain>.crt (mode 0644) and
// <domain>.key (mode 0600). It creates outputDir (including parents) when it
// does not exist. It returns the full paths of the written certificate and key
// files.
func WriteToDir(opts Options, outputDir string) (certPath, keyPath string, err error) {
	result, err := Generate(opts)
	if err != nil {
		return "", "", err
	}

	if err = os.MkdirAll(outputDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create output directory %q: %w", outputDir, err)
	}

	certPath = filepath.Join(outputDir, opts.Domain+".crt")
	keyPath = filepath.Join(outputDir, opts.Domain+".key")
	if err = writePEM(certPath, result.CertPEM, 0o644); err != nil {
		return "", "", fmt.Errorf("write certificate: %w", err)
	}
	if err = writePEM(keyPath, result.KeyPEM, 0o600); err != nil {
		return "", "", fmt.Errorf("write private key: %w", err)
	}
	return certPath, keyPath, nil
}

// ParseLeaf parses the first CERTIFICATE PEM block from certPEM and returns the
// leaf *x509.Certificate. It returns a descriptive error when the PEM data
// contains no CERTIFICATE block or the DER bytes cannot be parsed.
func ParseLeaf(certPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("no CERTIFICATE PEM block found")
	}
	return x509.ParseCertificate(block.Bytes)
}

// FingerprintSHA256 parses the first CERTIFICATE PEM block from certPEM and
// returns the lowercase hex SHA-256 digest of the leaf certificate's raw DER
// bytes. It returns a wrapped error if the PEM block cannot be parsed.
func FingerprintSHA256(certPEM []byte) (string, error) {
	leaf, err := ParseLeaf(certPEM)
	if err != nil {
		return "", fmt.Errorf("parse leaf certificate: %w", err)
	}
	sum := sha256.Sum256(leaf.Raw)
	return hex.EncodeToString(sum[:]), nil
}

// generatePrivateKey generates a new private key of the requested type. RSA
// keys use a RSABits modulus (sufficient for the TLS 1.2+ security baseline);
// ECDSA keys use the P-256 curve which is widely supported and matches the
// ECDHE-ECDSA cipher suites in the default whitelist.
func generatePrivateKey(keyType string) (any, error) {
	switch keyType {
	case "rsa":
		key, err := rsa.GenerateKey(rand.Reader, RSABits)
		if err != nil {
			return nil, fmt.Errorf("generate rsa key: %w", err)
		}
		return key, nil
	case "ecdsa":
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generate ecdsa key: %w", err)
		}
		return key, nil
	default:
		return nil, fmt.Errorf("key-type %q: %w", keyType, ErrUnsupportedKeyType)
	}
}

// publicKey returns the public counterpart of a private key. Supports the key
// types that generatePrivateKey can produce (RSA and ECDSA) plus ed25519 for
// future extension.
func publicKey(key any) any {
	switch k := key.(type) {
	case *rsa.PrivateKey:
		return &k.PublicKey
	case *ecdsa.PrivateKey:
		return &k.PublicKey
	case ed25519.PrivateKey:
		return k.Public()
	default:
		return nil
	}
}

// writePEM writes PEM-encoded data to the given path with the requested file
// mode. The file is created or truncated; existing permissions are not
// preserved.
func writePEM(path string, data []byte, mode os.FileMode) error {
	return os.WriteFile(path, data, mode)
}
