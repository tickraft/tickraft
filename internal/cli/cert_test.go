// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cli

import (
	"testing"
)

// TestNewCertCmd verifies newCertCmd() delegates to
// cert.NewCertCmd(): the returned command has Use="cert [command]" and exactly one
// "selfsign" subcommand. The core generation logic and its tests live in
// internal/cert.
func TestNewCertCmd(t *testing.T) {
	cmd := newCertCmd()
	if cmd.Use != "cert [command]" {
		t.Errorf("Use = %q, want %q", cmd.Use, "cert [command]")
	}
	sub := cmd.Commands()
	if len(sub) != 1 {
		t.Fatalf("subcommand count = %d, want 1", len(sub))
	}
	if sub[0].Use != "selfsign" {
		t.Errorf("subcommand Use = %q, want %q", sub[0].Use, "selfsign")
	}
}
