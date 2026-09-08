// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package totp implements the Time-based One-Time Password (TOTP) algorithm
// defined in RFC 6238 using only the Go standard library.
//
// The implementation supports 6-digit codes with a 30-second time step and
// SHA-1 HMAC, which is the most widely deployed TOTP configuration and the
// default used by authenticator apps such as Google Authenticator and
// Microsoft Authenticator. Validation allows a ±1 time-step drift to
// tolerate minor clock skew between the client and the server.
//
// The CE kernel itself does not surface MFA flows; this package is kernel
// base consumed by tickraft-x, whose auth service issues and verifies TOTP
// secrets for MFA-enabled logins.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP mandates HMAC-SHA1
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// timeStep is the TOTP time step in seconds (RFC 6238 default).
	timeStep = 30
	// codeDigits is the number of digits in the TOTP code.
	codeDigits = 6
	// secretBytes is the length of the random secret in bytes (160 bits,
	// the recommended key length for SHA-1 TOTP).
	secretBytes = 20
	// allowedDrift is the number of time steps (before and after the
	// current one) accepted during validation to tolerate clock drift.
	allowedDrift = 1
)

// base32Encoding is the unpadded Base32 encoding used for TOTP secrets, as
// specified by RFC 4648. Authenticator apps accept unpadded Base32 secrets.
var base32Encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateSecret generates a random TOTP secret (base32 encoded, 20 bytes)
// using a cryptographically secure random source.
func GenerateSecret() (string, error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("totp: generate secret: %w", err)
	}
	return base32Encoding.EncodeToString(buf), nil
}

// Validate checks if the given TOTP code is valid for the secret. It allows
// ±1 time step (30 seconds) for clock drift. The comparison is constant-time
// to mitigate timing attacks. Returns false if the secret cannot be decoded
// or the code does not match.
func Validate(secret, code string) bool {
	if len(code) != codeDigits {
		return false
	}
	key, err := base32Encoding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return false
	}
	counter := time.Now().Unix() / timeStep
	for drift := -allowedDrift; drift <= allowedDrift; drift++ {
		expected := generateCode(key, counter+int64(drift))
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// GenerateURI creates the otpauth:// URI for QR code generation. The URI
// follows the format used by Google Authenticator and compatible apps:
//
//	otpauth://totp/<issuer>:<account>?secret=<secret>&issuer=<issuer>&algorithm=SHA1&digits=6&period=30
//
// The frontend renders this URI as a QR code that authenticator apps scan.
func GenerateURI(secret, account, issuer string) string {
	label := account
	if issuer != "" {
		label = issuer + ":" + account
	}
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", strconv.Itoa(codeDigits))
	q.Set("period", strconv.Itoa(timeStep))
	return "otpauth://totp/" + url.PathEscape(label) + "?" + q.Encode()
}

// QRCode returns the otpauth URI as bytes for client-side QR rendering.
//
// Server-side PNG generation would require a QR encoding library which the
// runtime does not depend on (standard-library-only constraint).
// The frontend is expected to render the returned URI into a QR code using a
// client-side library. Callers should prefer GenerateURI for the plain URI;
// this helper exists for APIs that expect a byte payload.
func QRCode(uri string) ([]byte, error) {
	return []byte(uri), nil
}

// generateCode computes the TOTP code for the given secret key and time
// counter using HMAC-SHA1 and dynamic truncation as specified in RFC 6238.
func generateCode(key []byte, counter int64) string {
	// TOTP counters derive from unix time; negative only pre-1970.
	counter = max(counter, 0)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(counter))

	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	// Dynamic truncation: the low nibble of the last byte is the offset
	// into the HMAC sum. Take 4 bytes at that offset and mask the high bit.
	offset := int(sum[len(sum)-1] & 0x0f)
	bin := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	mod := uint32(1)
	for range codeDigits {
		mod *= 10
	}
	otp := bin % mod

	return fmt.Sprintf("%0*d", codeDigits, otp)
}
