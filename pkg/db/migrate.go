// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/auth/password"
	"github.com/tickraft/tickraft/pkg/user"
)

// AutoMigrate automatically creates or updates database tables for the
// auth models managed by pkg/auth (TokenBlacklist) and pkg/user
// (User, APIKey).
//
// Extended models are migrated by the downstream repository's own
// initialization path so that pkg/db remains free of extended
// dependencies and importable by downstream repositories.
//
// Scheduler-specific tables (sys_schedule_task, sys_schedule_log) are owned
// by pkg/scheduler and must be migrated by the scheduler's own
// initialization path. Remediation tables (sys_prism_remediation_rule,
// sys_prism_remediation_record) are owned by pkg/prism/remediation and
// migrated by its own Migrate function (which also drops the pre-rename
// legacy tables). This keeps pkg/db free of prism dependencies so it
// remains importable by downstream repositories.
func AutoMigrate(ctx context.Context, dbc *gorm.DB) error {
	return dbc.WithContext(ctx).AutoMigrate(
		&user.User{},
		&user.APIKey{},
		&auth.TokenBlacklist{},
	)
}

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
func EnsureAdminUser(ctx context.Context, db *gorm.DB, username, pwd string) (string, error) {
	if username == "" {
		return "", errors.New("db: admin username is required")
	}

	// Validate the admin username with the same canonical rule enforced by
	// pkg/user.ValidateUsername (and by Service.Login at authentication time).
	// Failing here prevents the "initialized but cannot log in" bug where a
	// custom admin_username passes EnsureAdminUser but is rejected by the
	// login validator (e.g. hyphens, dots, or length < 3).
	if err := user.ValidateUsername(username); err != nil {
		return "", fmt.Errorf("db: invalid admin username %q: %w", username, err)
	}

	db = db.WithContext(ctx)

	var existing user.User
	err := db.Where("username = ?", username).First(&existing).Error
	if err == nil {
		// User already exists; do not overwrite the password.
		return "", nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", fmt.Errorf("db: query admin user: %w", err)
	}

	plainPassword := pwd
	if plainPassword == "" {
		generated, genErr := generateRandomPassword(16)
		if genErr != nil {
			return "", fmt.Errorf("db: generate admin password: %w", genErr)
		}
		plainPassword = generated
	}

	hash, err := password.Hash(plainPassword)
	if err != nil {
		return "", fmt.Errorf("db: hash admin password: %w", err)
	}

	u := user.User{
		Username:     username,
		PasswordHash: hash,
		Role:         2, // admin
		Status:       1, // active
	}

	if err = db.Create(&u).Error; err != nil {
		return "", fmt.Errorf("db: create admin user: %w", err)
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
