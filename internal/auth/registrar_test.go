// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tickraft/tickraft/internal/auth"
	authcore "github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/user"
)

// ---------------------------------------------------------------------------
// Registrar tests
// ---------------------------------------------------------------------------

func newTestRegistrarStore(t *testing.T) user.Store {
	t.Helper()
	dbc := newTestDB(t)
	return user.NewStore(dbc)
}

func TestRegistrar_Register(t *testing.T) {
	users := newTestRegistrarStore(t)
	reg := auth.NewRegistrar(users, 4)

	ctx := context.Background()
	u, err := reg.Register(ctx, "testuser", "password123", "test@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if u.Username != "testuser" {
		t.Errorf("expected username=testuser, got %s", u.Username)
	}
	if u.Role != authcore.RoleDeveloper {
		t.Errorf("expected role=%d, got %d", authcore.RoleDeveloper, u.Role)
	}
}

func TestRegistrar_DuplicateUser(t *testing.T) {
	users := newTestRegistrarStore(t)
	// Create an existing user via the store directly.
	_, err := users.Create(context.Background(), "existinguser", "hash", "", int64(authcore.RoleDeveloper))
	if err != nil {
		t.Fatalf("create existing user: %v", err)
	}

	reg := auth.NewRegistrar(users, 4)

	ctx := context.Background()
	_, err = reg.Register(ctx, "existinguser", "password123", "test@example.com")
	if !errors.Is(err, authcore.ErrUserExists) {
		t.Errorf("expected ErrUserExists, got %v", err)
	}
}
