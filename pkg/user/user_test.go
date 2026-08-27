// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package user

import (
	"errors"
	"strings"
	"testing"

	"github.com/tickraft/tickraft/pkg/errdefs"
)

// TestValidateUsername verifies the canonical username rule: 3-64 chars,
// only letters, digits, and underscores. This is the single source of truth
// shared by pkg/auth.EnsureAdminUser, pkg/config AuthConfig.validate, and
// Service.Login's validateUsername — a divergence between any of them causes
// the "initialized but cannot log in" bug.
func TestValidateUsername(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Valid usernames.
		{"default_admin", "admin", false},
		{"with_underscore", "admin_user", false},
		{"letters_and_digits", "user123", false},
		{"underscore_only_after_letters", "u_1", false},
		{"min_length_3", "abc", false},
		{"max_length_64", strings.Repeat("a", 64), false},
		{"digits_only", "123456", false},

		// Invalid usernames.
		{"empty", "", true},
		{"too_short_2_chars", "ab", true},
		{"too_long_65_chars", strings.Repeat("a", 65), true},
		{"hyphen", "admin-user", true},
		{"dot", "admin.user", true},
		{"space", "admin user", true},
		{"at_sign", "admin@user", true},
		{"unicode_letter", "ädmın", true},
		{"newline", "admin\nuser", true},
		{"tab", "admin\tuser", true},
		{"plus", "user+1", true},
		{"slash", "user/1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateUsername(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateUsername(%q) expected error, got nil", tc.input)
				}
				if !errors.Is(err, ErrInvalidUsername) {
					t.Errorf("ValidateUsername(%q) error does not wrap ErrInvalidUsername: %v", tc.input, err)
				}
				if !errors.Is(err, errdefs.ErrInvalidArgument) {
					t.Errorf("ValidateUsername(%q) error does not wrap errdefs.ErrInvalidArgument: %v", tc.input, err)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateUsername(%q) unexpected error: %v", tc.input, err)
			}
		})
	}
}

// TestUsernameRegex_CanonicalBoundary verifies the boundary lengths (3 and 64)
// of the canonical username regex directly, documenting the exact policy.
func TestUsernameRegex_CanonicalBoundary(t *testing.T) {
	if !NameRegex.MatchString(strings.Repeat("a", 3)) {
		t.Error("NameRegex should match a 3-char string")
	}
	if !NameRegex.MatchString(strings.Repeat("a", 64)) {
		t.Error("NameRegex should match a 64-char string")
	}
	if NameRegex.MatchString(strings.Repeat("a", 2)) {
		t.Error("NameRegex should NOT match a 2-char string")
	}
	if NameRegex.MatchString(strings.Repeat("a", 65)) {
		t.Error("NameRegex should NOT match a 65-char string")
	}
}
