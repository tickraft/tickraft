// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/tickraft/tickraft/pkg/cert"
)

// newSelfSignCmd creates the "selfsign" subcommand that generates a self-signed
// TLS certificate and writes the certificate and private key to the output
// directory as <domain>.crt and <domain>.key.
//
// The generated certificate is suitable for development and internal
// deployments. Production deployments should use certificates issued by a
// trusted CA or the ACME auto-issuance feature.
//
// callers may call this function directly to register the
// selfsign subcommand on their own cobra root, or call newCertCmd to obtain
// the full cert parent command and extend it with additional subcommands.
func newSelfSignCmd() *cobra.Command {
	var (
		domain  string
		days    int
		output  string
		keyType string
	)
	cmd := &cobra.Command{
		Use:   "selfsign",
		Short: "Generate a self-signed TLS certificate",
		Long: `Generate a self-signed TLS certificate and private key, writing them as
<domain>.crt and <domain>.key in the output directory.

Suitable for development, testing, and internal deployments. For production,
use certificates issued by a trusted CA or the ACME auto-issuance feature
available in the callers.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSelfSign(domain, days, output, keyType)
		},
	}
	cmd.Flags().StringVar(&domain, "domain", "", "domain name (required, e.g. example.com)")
	cmd.Flags().IntVar(&days, "days", cert.DefaultDays, "certificate validity in days")
	cmd.Flags().StringVar(&output, "output", cert.DefaultOutput, "output directory for certificate and key files")
	cmd.Flags().StringVar(&keyType, "key-type", cert.DefaultKeyType, "key type: rsa or ecdsa")
	if err := cmd.MarkFlagRequired("domain"); err != nil {
		// MarkFlagRequired only fails when the flag does not exist; the flag
		// is defined immediately above so this branch is unreachable.
		panic(fmt.Sprintf("cert selfsign: mark domain flag required: %v", err))
	}
	return cmd
}

// newCertCmd creates the "cert" parent subcommand for certificate management
// with the "selfsign" subcommand already registered. callers may
// call AddCommand on the returned cmd to attach additional subcommands
// (e.g. acme, rotate) without re-implementing the selfsign logic.
func newCertCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cert [command]",
		Short: "Generate and manage TLS certificates",
		Long: `Generate and manage TLS certificates for HTTPS endpoints.

The standalone runtime provides the "selfsign" subcommand for creating
self-signed certificates. The callers may add ACME auto-issuance,
certificate rotation, and other features.`,
	}
	cmd.AddCommand(newSelfSignCmd())
	return cmd
}

// runSelfSign generates a self-signed certificate, writes it to the output
// directory, and prints a summary (cert path, SHA-256 fingerprint, validity
// period) to stdout. It returns a wrapped sentinel error on validation failure
// or a wrapped filesystem/crypto error on I/O or key-generation failure.
func runSelfSign(domain string, days int, output, keyType string) error {
	certPath, _, err := cert.WriteToDir(cert.Options{
		Domain:  domain,
		Days:    days,
		KeyType: keyType,
	}, output)
	if err != nil {
		return err
	}

	// Read the on-disk certificate so the printed fingerprint matches the
	// written bytes exactly.
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("read generated certificate: %w", err)
	}

	printCertSummary(certPath, certPEM)
	return nil
}

// printCertSummary prints the certificate file path, SHA-256 fingerprint, and
// validity period to stdout so the operator can verify the generated
// certificate at a glance. The fingerprint is lowercase hex so it can be
// compared against openssl output without further formatting. If the
// certificate cannot be parsed, a minimal fallback summary containing the cert
// path is printed instead so the operator can still locate the file.
func printCertSummary(certPath string, certPEM []byte) {
	fingerprint, err := cert.FingerprintSHA256(certPEM)
	if err != nil {
		fmt.Printf("certificate generated:\n  %s\n  (fingerprint unavailable: %v)\n", certPath, err)
		return
	}
	leaf, err := cert.ParseLeaf(certPEM)
	if err != nil {
		fmt.Printf("certificate generated:\n  %s\n  (fingerprint unavailable: %v)\n", certPath, err)
		return
	}
	fmt.Printf("certificate generated:\n")
	fmt.Printf("  cert file:  %s\n", certPath)
	fmt.Printf("  fingerprint (SHA-256): %s\n", fingerprint)
	fmt.Printf("  not before: %s\n", leaf.NotBefore.Format(time.RFC3339))
	fmt.Printf("  not after:  %s\n", leaf.NotAfter.Format(time.RFC3339))
}
