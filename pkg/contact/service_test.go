// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package contact

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/quota"
)

// newTestDB opens an in-memory SQLite database with the contact table
// migrated.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	ctx := context.Background()
	gdb, err := db.Open(ctx, db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, e := gdb.DB(); e == nil {
			_ = sqlDB.Close()
		}
	})
	if err := NewStore(gdb).Migrate(ctx); err != nil {
		t.Fatalf("migrate contact table: %v", err)
	}
	return gdb
}

// stubProvider is a quota.Provider test double returning a fixed contact
// ceiling and zero for every other type.
type stubProvider struct{ ceiling int }

func (p stubProvider) Ceiling(t quota.Type) int {
	if t == quota.TypeContact {
		return p.ceiling
	}
	return 0
}

// withCeiling installs a stub provider for the test's lifetime. Cleanup
// reverts to the zero provider (unlimited), which is the state this
// package's tests start from.
func withCeiling(t *testing.T, ceiling int) {
	t.Helper()
	quota.SetProvider(stubProvider{ceiling: ceiling})
	t.Cleanup(func() { quota.SetProvider(nil) })
}

// TestCreateContactQuotaCeiling verifies seat enforcement through the quota
// SPI: with a ceiling of 2 the third creation fails with ErrQuotaExceeded,
// and a ceiling of 0 (provider contract: not configured) is unlimited.
func TestCreateContactQuotaCeiling(t *testing.T) {
	gdb := newTestDB(t)
	svc := NewContactService(NewStore(gdb))
	ctx := context.Background()

	withCeiling(t, 2)
	for i := range 2 {
		if _, err := svc.CreateContact(ctx, &CreateRequest{
			Name:  "Alice",
			Email: "alice@example.com",
		}); err != nil {
			t.Fatalf("create under ceiling %d: %v", i, err)
		}
	}
	_, err := svc.CreateContact(ctx, &CreateRequest{Name: "Bob", Email: "bob@example.com"})
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("create at ceiling: expected ErrQuotaExceeded, got %v", err)
	}

	// Update is not seat-consuming: renaming an existing contact must work
	// at the ceiling.
	if _, err := svc.UpdateContact(ctx, 1, &CreateRequest{Name: "Renamed", Email: "alice@example.com"}); err != nil {
		t.Fatalf("update at ceiling: %v", err)
	}

	// A delete frees the seat again.
	if err := svc.DeleteContact(ctx, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.CreateContact(ctx, &CreateRequest{Name: "Backfill", Email: "back@example.com"}); err != nil {
		t.Fatalf("create after freeing a seat: %v", err)
	}
}

// TestCreateContactUnlimitedWhenNotConfigured verifies the provider
// contract: a ceiling of 0 means the type is not enforced, so creations are
// unlimited.
func TestCreateContactUnlimitedWhenNotConfigured(t *testing.T) {
	gdb := newTestDB(t)
	svc := NewContactService(NewStore(gdb))
	ctx := context.Background()

	withCeiling(t, 0)
	for i := range 5 {
		req := &CreateRequest{Name: "Alice", Phone: "13800000000"}
		if _, err := svc.CreateContact(ctx, req); err != nil {
			t.Fatalf("create %d with no ceiling: %v", i, err)
		}
	}
	if n, err := svc.CountContacts(ctx); err != nil || n != 5 {
		t.Fatalf("count: expected 5, got %d (err=%v)", n, err)
	}
}

// TestContactValidation exercises the payload contract in contactOf: name
// required, one of email/phone required, email must be a plain valid
// address, length bounds.
func TestContactValidation(t *testing.T) {
	gdb := newTestDB(t)
	svc := NewContactService(NewStore(gdb))
	ctx := context.Background()

	cases := []struct {
		name string
		req  CreateRequest
	}{
		{"empty name", CreateRequest{Email: "a@example.com"}},
		{"name too long", CreateRequest{Name: string(make([]byte, 256)), Email: "a@example.com"}},
		{"no channel", CreateRequest{Name: "X"}},
		{"invalid email", CreateRequest{Name: "X", Email: "not-an-email"}},
		{"display-form email", CreateRequest{Name: "X", Email: "Alice <a@example.com>"}},
		{"email too long", CreateRequest{Name: "X", Email: "a@" + string(make([]byte, 250)) + ".com"}},
		{"phone too long", CreateRequest{Name: "X", Phone: string(make([]byte, 65))}},
		{"remark too long", CreateRequest{Name: "X", Phone: "1", Remark: string(make([]byte, 256))}},
	}
	for _, tc := range cases {
		_, err := svc.CreateContact(ctx, &tc.req)
		var ve *ErrValidation
		if !errors.As(err, &ve) {
			t.Fatalf("%s: expected ErrValidation, got %v", tc.name, err)
		}
	}

	// Whitespace-only fields are trimmed to empty before the checks above
	// run, so a blank name with a valid email is still rejected.
	_, err := svc.CreateContact(ctx, &CreateRequest{Name: "   ", Email: "a@example.com"})
	var ve *ErrValidation
	if !errors.As(err, &ve) {
		t.Fatalf("whitespace name: expected ErrValidation, got %v", err)
	}
}

// TestStoreTenantScoping verifies the tenant seam: a resolver-backed store
// scopes List/Count/Get/Update/Delete to the resolved tenant, stamps Create
// with it, and fails closed when the context carries no tenant.
func TestStoreTenantScoping(t *testing.T) {
	gdb := newTestDB(t)
	ctx := context.Background()

	type tenantKey struct{}
	resolve := func(ctx context.Context) (int64, bool) {
		id, ok := ctx.Value(tenantKey{}).(int64)
		return id, ok
	}
	s := NewTenantStore(gdb, resolve)

	tenantA := context.WithValue(ctx, tenantKey{}, int64(1))
	tenantB := context.WithValue(ctx, tenantKey{}, int64(2))

	a := &Contact{Name: "A-owner", Email: "a@example.com"}
	if err := s.Create(tenantA, a); err != nil {
		t.Fatalf("create for tenant A: %v", err)
	}
	if a.TenantID != 1 {
		t.Fatalf("create did not stamp tenant: got %d", a.TenantID)
	}

	// Tenant B sees none of tenant A's rows.
	items, total, err := s.List(tenantB, ListFilter{Page: 1, Size: 20})
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("tenant B list: expected isolation, got total=%d items=%d err=%v", total, len(items), err)
	}
	if _, err := s.Get(tenantB, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tenant B get: expected ErrNotFound, got %v", err)
	}
	if err := s.Delete(tenantB, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tenant B delete: expected ErrNotFound, got %v", err)
	}

	// Tenant A still sees and mutates its row.
	if _, err := s.Get(tenantA, a.ID); err != nil {
		t.Fatalf("tenant A get: %v", err)
	}
	if err := s.Update(tenantA, &Contact{ID: a.ID, Name: "A-owner-2", Email: "a2@example.com"}); err != nil {
		t.Fatalf("tenant A update: %v", err)
	}
	if n, err := s.Count(tenantA); err != nil || n != 1 {
		t.Fatalf("tenant A count: expected 1, got %d (err=%v)", n, err)
	}

	// A context without a tenant fails closed instead of degrading to an
	// unscoped query.
	if _, _, err := s.List(ctx, ListFilter{Page: 1, Size: 20}); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("unscoped list: expected ErrTenantRequired, got %v", err)
	}
	if err := s.Create(ctx, &Contact{Name: "X", Email: "x@example.com"}); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("unscoped create: expected ErrTenantRequired, got %v", err)
	}
}
