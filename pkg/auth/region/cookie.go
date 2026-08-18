// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package region implements signed region cookies that carry the region a
// request originated from, together with HMAC key rotation for verifying
// cookies signed under older keys.
package region

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sentinel errors for region cookie operations.
var (
	// ErrInvalidCookie is returned when a cookie value is malformed.
	ErrInvalidCookie = errors.New("region: invalid cookie format")
	// ErrCookieTampered is returned when a cookie's HMAC signature does not match.
	ErrCookieTampered = errors.New("region: cookie signature mismatch")
)

const (
	// DefaultRotationInterval is the recommended interval between key
	// rotations (90 days).
	DefaultRotationInterval = 90 * 24 * time.Hour
	// DefaultTransitionPeriod is the recommended duration after a rotation
	// during which both the current and previous keys are accepted (24 hours).
	DefaultTransitionPeriod = 24 * time.Hour
)

// SignCookie creates a signed region cookie value.
//
// The format is: {region}:{timestamp}:{hmac}
// HMAC is computed with SHA-256 over the message "{region}:{timestamp}" using
// the provided secret.
func SignCookie(region string, timestamp int64, secret []byte) string {
	message := fmt.Sprintf("%s:%d", region, timestamp)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s:%d:%s", region, timestamp, signature)
}

// VerifyCookie validates a signed region cookie and returns the region and
// timestamp embedded in it.
//
// It parses the "{region}:{timestamp}:{hmac}" format, recomputes the HMAC, and
// compares it in constant time using hmac.Equal. It returns ErrInvalidCookie
// for malformed values and ErrCookieTampered when the signature does not match.
func VerifyCookie(signed string, secret []byte) (region string, timestamp int64, err error) {
	parts := strings.Split(signed, ":")
	if len(parts) != 3 {
		return "", 0, ErrInvalidCookie
	}

	region = parts[0]
	timestamp, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("region: invalid timestamp: %w", err)
	}
	providedSig := parts[2]

	message := fmt.Sprintf("%s:%d", region, timestamp)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(message))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(providedSig), []byte(expectedSig)) {
		return "", 0, ErrCookieTampered
	}

	return region, timestamp, nil
}

// KeyRotator manages HMAC key rotation with a transition period during which
// both the current and previous keys are accepted.
//
// The rotationInterval field records the recommended interval between
// rotations (see DefaultRotationInterval), and transitionPeriod records how
// long after a rotation the previous key remains valid (see
// DefaultTransitionPeriod). The lastRotation timestamp tracks when the current
// key became active.
type KeyRotator struct {
	current          []byte
	previous         []byte
	rotationInterval time.Duration
	transitionPeriod time.Duration
	lastRotation     time.Time
	mu               sync.RWMutex
}

// NewKeyRotator creates a KeyRotator with the given current and previous keys.
// The current key is treated as freshly rotated (lastRotation = now), so the
// previous key is accepted throughout the transitionPeriod. The keys are
// copied to prevent external mutation.
func NewKeyRotator(current, previous []byte, rotationInterval, transitionPeriod time.Duration) *KeyRotator {
	return &KeyRotator{
		current:          append([]byte(nil), current...),
		previous:         append([]byte(nil), previous...),
		rotationInterval: rotationInterval,
		transitionPeriod: transitionPeriod,
		lastRotation:     time.Now(),
	}
}

// Sign signs the region cookie with the current key.
func (kr *KeyRotator) Sign(region string, timestamp int64) string {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	return SignCookie(region, timestamp, kr.current)
}

// Verify validates the signed cookie. It tries the current key first; if that
// fails and the rotator is still within the transition period, it retries with
// the previous key. Returns the error from the current key when both attempts
// fail.
func (kr *KeyRotator) Verify(signed string) (region string, timestamp int64, err error) {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	// Try the current key first.
	region, timestamp, err = VerifyCookie(signed, kr.current)
	if err == nil {
		return region, timestamp, nil
	}

	// During the transition period, accept cookies signed with the previous key.
	if len(kr.previous) > 0 && time.Since(kr.lastRotation) <= kr.transitionPeriod {
		r, ts, errPrev := VerifyCookie(signed, kr.previous)
		if errPrev == nil {
			return r, ts, nil
		}
	}

	return "", 0, err
}

// Rotate promotes the current key to previous, installs newCurrent as the
// active key, and resets the lastRotation timestamp so the transition period
// begins anew. The previous previous key is discarded.
func (kr *KeyRotator) Rotate(newCurrent []byte) {
	kr.mu.Lock()
	defer kr.mu.Unlock()
	kr.previous = kr.current
	kr.current = append([]byte(nil), newCurrent...)
	kr.lastRotation = time.Now()
}
