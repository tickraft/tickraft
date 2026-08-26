// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashAndVerify(t *testing.T) {
	password := "test-password-123"
	hash, err := Hash(password)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if hash == "" {
		t.Fatal("Hash() returned empty string")
	}
	if hash == password {
		t.Fatal("Hash() returned plaintext password")
	}

	// Verify correct password
	if err := Verify(hash, password); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	// Verify wrong password
	if err := Verify(hash, "wrong-password"); err == nil {
		t.Fatal("Verify() should fail for wrong password")
	}
}

func TestHashWithCost(t *testing.T) {
	password := "test-password-456"
	hash, err := HashWithCost(password, bcrypt.MinCost)
	if err != nil {
		t.Fatalf("HashWithCost() error = %v", err)
	}
	if err := Verify(hash, password); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}
