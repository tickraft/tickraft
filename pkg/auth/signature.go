// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
)

// HmacSHA256 computes an HMAC-SHA256 using the given secret and payload,
// returning the result as a hex-encoded string.
func HmacSHA256(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyHmacSHA256 computes an HMAC-SHA256 over the payload and compares it
// with the provided signature using constant-time comparison to prevent
// timing attacks. Returns true if the signatures match.
func VerifyHmacSHA256(secret, payload, signature string) bool {
	expected := HmacSHA256(secret, payload)
	expectedBytes, err := hex.DecodeString(expected)
	if err != nil {
		return false
	}
	providedBytes, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(expectedBytes, providedBytes) == 1
}

// GenerateSignString produces the canonical string to be signed from the
// given timestamp, nonce, and request body.
func GenerateSignString(timestamp int64, nonce, body string) string {
	return fmt.Sprintf("%d\n%s\n%s", timestamp, nonce, body)
}
