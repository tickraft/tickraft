// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package apikey

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGenerateAPIKey(t *testing.T) {
	rawKey, hash, prefix, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}

	// Raw key must start with "tk_" prefix.
	if !strings.HasPrefix(rawKey, "tk_") {
		t.Errorf("rawKey = %q, want prefix %q", rawKey, "tk_")
	}

	// Raw key length: "tk_" (3) + 4 (prefix hex) + 64 (body hex) = 71 chars.
	if got, want := len(rawKey), 71; got != want {
		t.Errorf("len(rawKey) = %d, want %d", got, want)
	}

	// Prefix length: "tk_" (3) + 4 (hex) = 7 chars.
	if got, want := len(prefix), 7; got != want {
		t.Errorf("len(prefix) = %d, want %d", got, want)
	}

	// Prefix must be the leading portion of the raw key.
	if !strings.HasPrefix(rawKey, prefix) {
		t.Errorf("rawKey = %q does not start with prefix %q", rawKey, prefix)
	}

	// Hash must be 64-char hex-encoded SHA-256.
	if got, want := len(hash), 64; got != want {
		t.Errorf("len(hash) = %d, want %d", got, want)
	}

	// Hash must match HashAPIKey of the raw key.
	if got := HashAPIKey(rawKey); got != hash {
		t.Errorf("HashAPIKey(rawKey) = %q, want %q", got, hash)
	}

	// Two generated keys must differ.
	//nolint:dogsled // test only needs the service; the other constructor results are intentionally discarded
	rawKey2, _, _, _ := GenerateAPIKey()
	if rawKey == rawKey2 {
		t.Error("two generated keys should not be equal")
	}
}

func TestHashAPIKey(t *testing.T) {
	rawKey := "tk_a1b2" + strings.Repeat("c", 64)
	hash1 := HashAPIKey(rawKey)
	hash2 := HashAPIKey(rawKey)

	// Same input must produce same hash.
	if hash1 != hash2 {
		t.Errorf("HashAPIKey is not deterministic: %q != %q", hash1, hash2)
	}

	// Different input must produce different hash.
	different := "tk_a1b2" + strings.Repeat("d", 64)
	hash3 := HashAPIKey(different)
	if hash1 == hash3 {
		t.Error("different inputs produced the same hash")
	}

	// Output length must be 64 hex chars (SHA-256).
	if got, want := len(hash1), 64; got != want {
		t.Errorf("len(hash) = %d, want %d", got, want)
	}
}

func TestValidateAPIKey_Success(t *testing.T) {
	rawKey, hash, _, _ := GenerateAPIKey()
	err := ValidateAPIKey(rawKey, hash, StatusActive, nil)
	if err != nil {
		t.Fatalf("ValidateAPIKey() error = %v, want nil", err)
	}
}

func TestValidateAPIKey_WrongKey(t *testing.T) {
	rawKey, hash, _, _ := GenerateAPIKey()
	wrongKey := rawKey + "x"
	err := ValidateAPIKey(wrongKey, hash, StatusActive, nil)
	if !errors.Is(err, ErrAPIKeyInvalid) {
		t.Errorf("ValidateAPIKey() error = %v, want %v", err, ErrAPIKeyInvalid)
	}
}

func TestValidateAPIKey_Revoked(t *testing.T) {
	rawKey, hash, _, _ := GenerateAPIKey()
	err := ValidateAPIKey(rawKey, hash, StatusRevoked, nil)
	if !errors.Is(err, ErrAPIKeyRevoked) {
		t.Errorf("ValidateAPIKey() error = %v, want %v", err, ErrAPIKeyRevoked)
	}
}

func TestValidateAPIKey_Expired(t *testing.T) {
	rawKey, hash, _, _ := GenerateAPIKey()
	past := time.Now().Add(-1 * time.Hour)
	err := ValidateAPIKey(rawKey, hash, StatusActive, &past)
	if !errors.Is(err, ErrAPIKeyExpired) {
		t.Errorf("ValidateAPIKey() error = %v, want %v", err, ErrAPIKeyExpired)
	}
}

func TestValidateAPIKey_NilExpiredAt(t *testing.T) {
	rawKey, hash, _, _ := GenerateAPIKey()
	err := ValidateAPIKey(rawKey, hash, StatusActive, nil)
	if err != nil {
		t.Fatalf("ValidateAPIKey() with nil expiredAt error = %v, want nil", err)
	}
}
