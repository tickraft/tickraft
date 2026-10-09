// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package apikey provides generation, hashing, and validation utilities for API keys.
package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors for API key validation.
var (
	// ErrAPIKeyInvalid is returned when an API key cannot be verified.
	ErrAPIKeyInvalid = errors.New("auth: api key invalid")
	// ErrAPIKeyRevoked is returned when an API key has been revoked.
	ErrAPIKeyRevoked = errors.New("auth: api key revoked")
	// ErrAPIKeyExpired is returned when an API key has passed its expiration time.
	ErrAPIKeyExpired = errors.New("auth: api key expired")
)

// Status constants for API key lifecycle.
const (
	// StatusActive indicates the API key is currently usable.
	StatusActive = 1
	// StatusRevoked indicates the API key has been revoked.
	StatusRevoked = 0
)

// Info represents the metadata of an API key without the raw secret.
type Info struct {
	// ID is the unique identifier of the API key record.
	ID int64
	// Name is the human-readable label of the API key.
	Name string
	// KeyPrefix is the non-secret prefix of the key used for identification.
	KeyPrefix string
	// KeyHash is the hashed representation of the full key for verification.
	KeyHash string
	// Status indicates whether the key is active (1) or revoked (0).
	Status int
	// CreatedAt is the timestamp when the key was created.
	CreatedAt time.Time
	// ExpiredAt is the optional timestamp when the key expires; nil means no expiry.
	ExpiredAt *time.Time
}

// GenerateAPIKey creates a new API key consisting of a human-readable prefix and
// a cryptographically random body. It returns the raw key (to be shown once to the
// caller), its SHA-256 hash (for persistent storage), and the prefix (for display).
func GenerateAPIKey() (rawKey, hash, prefix string, err error) {
	prefixBytes := make([]byte, 2)
	if _, err = rand.Read(prefixBytes); err != nil {
		return "", "", "", fmt.Errorf("apikey: generate prefix: %w", err)
	}
	prefix = "tk_" + hex.EncodeToString(prefixBytes)

	bodyBytes := make([]byte, 32)
	if _, err = rand.Read(bodyBytes); err != nil {
		return "", "", "", fmt.Errorf("apikey: generate body: %w", err)
	}
	body := hex.EncodeToString(bodyBytes)

	rawKey = prefix + body
	hash = HashAPIKey(rawKey)
	return rawKey, hash, prefix, nil
}

// HashAPIKey computes the SHA-256 hash of a raw API key and returns it as a
// hex-encoded string suitable for persistent storage.
func HashAPIKey(rawKey string) string {
	h := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(h[:])
}

// ValidateAPIKey verifies a raw API key against its stored hash and checks both
// the key status and optional expiration. It uses constant-time comparison to
// prevent timing attacks on the hash value.
func ValidateAPIKey(rawKey, storedHash string, status int, expiredAt *time.Time) error {
	computedHash := HashAPIKey(rawKey)
	if subtle.ConstantTimeCompare([]byte(computedHash), []byte(storedHash)) != 1 {
		return ErrAPIKeyInvalid
	}
	if status != StatusActive {
		return ErrAPIKeyRevoked
	}
	if expiredAt != nil && time.Now().After(*expiredAt) {
		return ErrAPIKeyExpired
	}
	return nil
}
