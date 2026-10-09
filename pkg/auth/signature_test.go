// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth

import "testing"

func TestHmacSHA256(t *testing.T) {
	secret := "my-secret-key"
	payload := `{"name":"test"}`
	// Compute expected dynamically: the function must produce consistent output.
	result := HmacSHA256(secret, payload)
	if len(result) != 64 {
		t.Errorf("HmacSHA256() length = %d, want 64", len(result))
	}
	// Same inputs must always produce the same output.
	if HmacSHA256(secret, payload) != result {
		t.Error("HmacSHA256() is not deterministic")
	}
}

func TestHmacSHA256_KnownVector(t *testing.T) {
	// RFC 4231 Test Case 2
	secret := "Jefe"
	payload := "what do ya want for nothing?"
	expected := "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"

	got := HmacSHA256(secret, payload)
	if got != expected {
		t.Errorf("HmacSHA256() = %q, want %q", got, expected)
	}
}

func TestVerifyHmacSHA256_ValidSignature(t *testing.T) {
	secret := "Jefe"
	payload := "what do ya want for nothing?"
	signature := HmacSHA256(secret, payload)

	if !VerifyHmacSHA256(secret, payload, signature) {
		t.Error("VerifyHmacSHA256() = false, want true for valid signature")
	}
}

func TestVerifyHmacSHA256_InvalidSignature(t *testing.T) {
	secret := "Jefe"
	payload := "what do ya want for nothing?"

	if VerifyHmacSHA256(secret, payload, "invalidsignature") {
		t.Error("VerifyHmacSHA256() = true, want false for invalid signature")
	}
}

func TestVerifyHmacSHA256_TamperedPayload(t *testing.T) {
	secret := "Jefe"
	payload := "what do ya want for nothing?"
	signature := HmacSHA256(secret, payload)

	if VerifyHmacSHA256(secret, "tampered payload", signature) {
		t.Error("VerifyHmacSHA256() = true, want false for tampered payload")
	}
}

func TestVerifyHmacSHA256_WrongSecret(t *testing.T) {
	secret := "Jefe"
	payload := "what do ya want for nothing?"
	signature := HmacSHA256(secret, payload)

	if VerifyHmacSHA256("wrong-secret", payload, signature) {
		t.Error("VerifyHmacSHA256() = true, want false for wrong secret")
	}
}

func TestGenerateSignString(t *testing.T) {
	timestamp := int64(1719000000)
	nonce := "abc123"
	body := `{"key":"value"}`

	expected := "1719000000\nabc123\n{\"key\":\"value\"}"
	got := GenerateSignString(timestamp, nonce, body)

	if got != expected {
		t.Errorf("GenerateSignString() = %q, want %q", got, expected)
	}
}

func TestGenerateSignString_EmptyBody(t *testing.T) {
	got := GenerateSignString(0, "nonce", "")
	expected := "0\nnonce\n"

	if got != expected {
		t.Errorf("GenerateSignString() = %q, want %q", got, expected)
	}
}
