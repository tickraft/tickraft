// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package user

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tickraft/tickraft/pkg/errdefs"
)

// Sentinel validation errors for the user domain.
//
// Each sentinel wraps errdefs.ErrInvalidArgument so that the transport-layer
// error mapper (pkg/api/httputil.mapError) recognizes them uniformly via
// errors.Is and returns 400 instead of falling back to 500.
var (
	// ErrInvalidUsername is returned when username validation fails.
	ErrInvalidUsername = fmt.Errorf("username must be 3-64 characters, only letters, digits "+
		"and underscores: %w", errdefs.ErrInvalidArgument)
	// ErrInvalidPasswordHash is returned when password hash is empty.
	ErrInvalidPasswordHash = fmt.Errorf("password hash must not be empty: %w", errdefs.ErrInvalidArgument)
	// ErrInvalidEmail is returned when email format is invalid.
	ErrInvalidEmail = fmt.Errorf("email format is invalid: %w", errdefs.ErrInvalidArgument)
	// ErrInvalidID is returned when ID is not positive.
	ErrInvalidID = fmt.Errorf("id must be greater than 0: %w", errdefs.ErrInvalidArgument)
	// ErrInvalidAPIKeyName is returned when API key name is empty or too long.
	ErrInvalidAPIKeyName = fmt.Errorf("api key name must be 1-255 characters: %w", errdefs.ErrInvalidArgument)
	// ErrInvalidKeyPrefix is returned when API key prefix is empty.
	ErrInvalidKeyPrefix = fmt.Errorf("key prefix must not be empty: %w", errdefs.ErrInvalidArgument)
	// ErrInvalidKeyHash is returned when API key hash is empty.
	ErrInvalidKeyHash = fmt.Errorf("key hash must not be empty: %w", errdefs.ErrInvalidArgument)
	// ErrInvalidJTI is returned when JTI is empty.
	ErrInvalidJTI = fmt.Errorf("jti must not be empty: %w", errdefs.ErrInvalidArgument)
)

var (
	// NameRegex matches a valid username: 3-64 chars, only letters,
	// digits and underscores. It is the canonical definition shared across
	// the user domain (pkg/user) and the auth domain (pkg/auth).
	NameRegex  = regexp.MustCompile(`^[a-zA-Z0-9_]{3,64}$`)
	emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
)

// ValidateUsername checks that username is 3-64 chars, only letters/digits/underscores.
func ValidateUsername(username string) error {
	if !NameRegex.MatchString(username) {
		return ErrInvalidUsername
	}
	return nil
}

// ValidatePasswordHash checks that password hash is not empty.
func ValidatePasswordHash(hash string) error {
	if hash == "" {
		return ErrInvalidPasswordHash
	}
	return nil
}

// ValidateEmail checks email format if provided; empty string is OK (optional field).
func ValidateEmail(email string) error {
	if email == "" {
		return nil
	}
	if !emailRegex.MatchString(email) {
		return ErrInvalidEmail
	}
	return nil
}

// ValidateID checks that id > 0.
func ValidateID(id int64) error {
	if id <= 0 {
		return ErrInvalidID
	}
	return nil
}

// ValidateAPIKeyName checks that name is 1-255 characters after trimming.
func ValidateAPIKeyName(name string) error {
	trimmed := strings.TrimSpace(name)
	if len(trimmed) < 1 || len(trimmed) > 255 {
		return ErrInvalidAPIKeyName
	}
	return nil
}

// ValidateKeyPrefix checks that keyPrefix is not empty.
func ValidateKeyPrefix(keyPrefix string) error {
	if keyPrefix == "" {
		return ErrInvalidKeyPrefix
	}
	return nil
}

// ValidateKeyHash checks that keyHash is not empty.
func ValidateKeyHash(keyHash string) error {
	if keyHash == "" {
		return ErrInvalidKeyHash
	}
	return nil
}

// ValidateJTI checks that jti is not empty.
func ValidateJTI(jti string) error {
	if jti == "" {
		return ErrInvalidJTI
	}
	return nil
}
