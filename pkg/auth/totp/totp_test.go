// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package totp

import (
	"strings"
	"testing"
	"time"
)

// TestGenerateCodeRFC6238 verifies the TOTP algorithm against the RFC 6238
// Appendix B test vectors. The RFC publishes 8-digit codes; the 6-digit codes
// below are the truncated value mod 10^6, which is the format used by
// authenticator apps.
func TestGenerateCodeRFC6238(t *testing.T) {
	// The RFC 6238 SHA-1 seed is the 20-byte ASCII string "12345678901234567890".
	key := []byte("12345678901234567890")

	tests := []struct {
		name    string
		counter int64
		want    string
	}{
		{"t=59", 0x1, "287082"},
		{"t=1111111109", 0x23523ec, "081804"},
		{"t=1111111111", 0x23523ed, "050471"},
		{"t=1234567890", 0x273ef07, "005924"},
		{"t=2000000000", 0x3f940aa, "279037"},
		{"t=20000000000", 0x27bc86aa, "353130"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateCode(key, tt.counter)
			if got != tt.want {
				t.Fatalf("counter=%d: got %q, want %q", tt.counter, got, tt.want)
			}
		})
	}
}

// TestValidateRoundTrip verifies that a freshly generated code validates
// against the same secret.
func TestValidateRoundTrip(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	if secret == "" {
		t.Fatal("GenerateSecret returned empty secret")
	}

	key, err := base32Encoding.DecodeString(strings.ToUpper(secret))
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	code := generateCode(key, time.Now().Unix()/timeStep)
	if !Validate(secret, code) {
		t.Fatalf("Validate rejected a valid code %q for secret %q", code, secret)
	}
}

// TestValidateRejectsBadSecret ensures a malformed secret is rejected without
// panicking.
func TestValidateRejectsBadSecret(t *testing.T) {
	if Validate("not-base32!!!", "123456") {
		t.Fatal("Validate accepted a malformed secret")
	}
}

// TestValidateRejectsWrongLength ensures a non-6-digit code is rejected.
func TestValidateRejectsWrongLength(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	if Validate(secret, "12345") {
		t.Fatal("Validate accepted a 5-digit code")
	}
}

// TestGenerateURI checks the otpauth URI format.
func TestGenerateURI(t *testing.T) {
	uri := GenerateURI("JBSWY3DPEHPK3PXP", "alice", "Tickraft")
	if !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatalf("URI prefix = %q, want otpauth://totp/", uri)
	}
	if !strings.Contains(uri, "secret=JBSWY3DPEHPK3PXP") {
		t.Fatalf("URI missing secret: %s", uri)
	}
	if !strings.Contains(uri, "issuer=Tickraft") {
		t.Fatalf("URI missing issuer: %s", uri)
	}
}

// TestQRCodeReturnsURI verifies QRCode returns the URI bytes.
func TestQRCodeReturnsURI(t *testing.T) {
	uri := "otpauth://totp/Tickraft:bob?secret=JBSWY3DPEHPK3PXP"
	b, err := QRCode(uri)
	if err != nil {
		t.Fatalf("QRCode: %v", err)
	}
	if string(b) != uri {
		t.Fatalf("QRCode = %q, want %q", string(b), uri)
	}
}
