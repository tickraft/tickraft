// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/user"
)

// EnsureAdminUser ensures that a built-in admin user with the given username
// exists in the database. If the user already exists no action is taken and
// the password is left untouched. When the user does not exist:
//
//   - If pwd is non-empty, it is hashed and used as the admin password.
//   - If pwd is empty, a random 32-character hex password is generated,
//     hashed, and the plaintext form is returned so the caller can log it
//     once for first-login.
//
// The created user has role=2 (admin), status=1 (active).
// Returns the generated plaintext password (empty when the user already
// existed or when an explicit password was supplied).
//
// It lives in pkg/auth (not pkg/user) because it needs both the user model
// and Hash from this package; pkg/user must not import pkg/auth.
func EnsureAdminUser(ctx context.Context, dbc *gorm.DB, username, pwd string) (string, error) {
	if username == "" {
		return "", errors.New("auth: admin username is required")
	}

	// Validate the admin username with the same canonical rule enforced by
	// pkg/user.ValidateUsername (and by Service.Login at authentication time).
	// Failing here prevents the "initialized but cannot log in" bug where a
	// custom admin_username passes EnsureAdminUser but is rejected by the
	// login validator (e.g. hyphens, dots, or length < 3).
	if err := user.ValidateUsername(username); err != nil {
		return "", fmt.Errorf("auth: invalid admin username %q: %w", username, err)
	}

	dbc = dbc.WithContext(ctx)

	var existing user.User
	err := dbc.Where("username = ?", username).First(&existing).Error
	if err == nil {
		// User already exists; do not overwrite the password.
		return "", nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", fmt.Errorf("auth: query admin user: %w", err)
	}

	plainPassword := pwd
	if plainPassword == "" {
		generated, genErr := generateRandomPassword(16)
		if genErr != nil {
			return "", fmt.Errorf("auth: generate admin password: %w", genErr)
		}
		plainPassword = generated
	}

	hash, err := Hash(plainPassword)
	if err != nil {
		return "", fmt.Errorf("auth: hash admin password: %w", err)
	}

	u := user.User{
		Username:     username,
		PasswordHash: hash,
		Role:         2, // admin
		Status:       1, // active
	}

	if err = dbc.Create(&u).Error; err != nil {
		return "", fmt.Errorf("auth: create admin user: %w", err)
	}

	// Only return the plaintext password when it was randomly generated.
	if pwd == "" {
		return plainPassword, nil
	}

	return "", nil
}

// generateRandomPassword returns a random hex-encoded password of the given
// byte length (the resulting string is twice as long as the byte count).
func generateRandomPassword(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
