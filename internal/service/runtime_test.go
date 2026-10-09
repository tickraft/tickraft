// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package service

import (
	"testing"

	"go.uber.org/zap"
)

// TestImplicitAccount_Defaults verifies that the runtime's implicit
// in-memory account has the documented default values: a personal account
// (Type=1) owned by the admin user (OwnerID=1) with active status. The
// implicit account is never persisted to the database; it exists only to
// give the admin user a conceptual account context that mirrors the
// extended edition's Account row.
func TestImplicitAccount_Defaults(t *testing.T) {
	t.Parallel()

	implicit := &account{
		ID:      1,
		OwnerID: 1,
		Type:    1,
		Name:    "admin",
		Status:  1,
	}
	rt := &runtime{
		logger:          zap.NewNop(),
		implicitAccount: implicit,
	}

	got := rt.ImplicitAccount()
	if got == nil {
		t.Fatal("ImplicitAccount() returned nil; expected non-nil implicit account")
	}
	if got.ID != 1 {
		t.Errorf("implicit account ID = %d, want 1", got.ID)
	}
	if got.OwnerID != 1 {
		t.Errorf("implicit account OwnerID = %d, want 1", got.OwnerID)
	}
	if got.Type != 1 {
		t.Errorf("implicit account Type = %d, want 1 (personal)", got.Type)
	}
	if got.Status != 1 {
		t.Errorf("implicit account Status = %d, want 1 (active)", got.Status)
	}
}

// TestImplicitAccount_NilWhenUnset verifies that ImplicitAccount returns nil
// for a runtime that was not initialized with an implicit account. This
// guards against nil-pointer dereferences in callers that run before
// initRuntime completes.
func TestImplicitAccount_NilWhenUnset(t *testing.T) {
	t.Parallel()

	rt := &runtime{logger: zap.NewNop()}
	if got := rt.ImplicitAccount(); got != nil {
		t.Errorf("ImplicitAccount() = %v, want nil for uninitialized runtime", got)
	}
}
