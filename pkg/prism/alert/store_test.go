// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/db"
)

// setupStore opens an in-memory SQLite database and migrates the alert
// rule table, returning the store and a cleanup function.
func setupStore(t *testing.T) (store *Store, cleanup func()) {
	t.Helper()
	ctx := context.Background()
	gdb, err := db.Open(ctx, db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store = NewStore(gdb, NewCompiler())
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cleanup = func() {
		if sqlDB, e := gdb.DB(); e == nil {
			_ = sqlDB.Close()
		}
	}
	return store, cleanup
}

// mustCreate inserts a valid rule, failing the test on error.
func mustCreate(t *testing.T, store *Store, rule *Rule) {
	t.Helper()
	if err := store.Create(context.Background(), rule); err != nil {
		t.Fatalf("create rule %q: %v", rule.Name, err)
	}
}

// TestStoreCreateValidatesExpression pins the write-path validation
// gate: expressions are compiled against the AlertEnv contract before
// they reach the database.
func TestStoreCreateValidatesExpression(t *testing.T) {
	store, cleanup := setupStore(t)
	defer cleanup()

	invalid := []string{
		`metrics["cpu"] >`,        // syntax error
		`unknownvar > 1`,          // unknown name
		`asset.naem == "web-1"`,   // domain field typo
		`metrics["cpu"] == "hot"`, // type mismatch on comparison
	}
	for _, expression := range invalid {
		err := store.Create(context.Background(), &Rule{Name: "bad", Expression: expression})
		if !errors.Is(err, ErrRuleCompileFailed) {
			t.Errorf("Create(%q) err = %v, want ErrRuleCompileFailed", expression, err)
		}
	}

	// Nothing was persisted.
	rules, total, err := store.List(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 0 || len(rules) != 0 {
		t.Errorf("invalid rules were persisted: total=%d", total)
	}
}

// TestStoreUpdateValidatesExpression pins the same gate on Update.
func TestStoreUpdateValidatesExpression(t *testing.T) {
	store, cleanup := setupStore(t)
	defer cleanup()

	rule := &Rule{Name: "r", Expression: `metrics["cpu"] > 90`, Enabled: true}
	mustCreate(t, store, rule)

	err := store.Update(context.Background(), &Rule{ID: rule.ID, Name: "r", Expression: `oops(`})
	if !errors.Is(err, ErrRuleCompileFailed) {
		t.Errorf("Update err = %v, want ErrRuleCompileFailed", err)
	}
}

// TestStoreUpdateColumnLevel pins the D-01/D-02 fix: Update touches only
// the user-editable columns, so tenant_id and created_at survive a PUT
// built from a partial DTO that carries different values.
func TestStoreUpdateColumnLevel(t *testing.T) {
	store, cleanup := setupStore(t)
	defer cleanup()

	original := &Rule{TenantID: 5, Name: "r", Expression: `metrics["cpu"] > 90`, Enabled: true}
	mustCreate(t, store, original)
	createdAt := original.CreatedAt

	// A stale/partial DTO attempts to rewrite tenant and lifecycle fields.
	update := &Rule{
		ID:         original.ID,
		TenantID:   999,
		Name:       "r2",
		Expression: `metrics["cpu"] > 95`,
		Enabled:    true,
		CreatedAt:  time.Now().Add(time.Hour),
	}
	if err := store.Update(context.Background(), update); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := store.GetByID(context.Background(), original.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.TenantID != 5 {
		t.Errorf("TenantID = %d, want 5 (never touched by Update)", got.TenantID)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt = %v, want %v (never touched by Update)", got.CreatedAt, createdAt)
	}
	if got.Name != "r2" || got.Expression != `metrics["cpu"] > 95` {
		t.Errorf("editable columns not applied: name=%q expression=%q", got.Name, got.Expression)
	}
}

// TestStoreUpdateMissing verifies the RowsAffected contract.
func TestStoreUpdateMissing(t *testing.T) {
	store, cleanup := setupStore(t)
	defer cleanup()

	err := store.Update(context.Background(), &Rule{ID: 424242, Name: "x", Expression: "true"})
	if !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("Update missing err = %v, want ErrRuleNotFound", err)
	}
}

// TestStoreGetAndDelete covers the ID-addressed GetByID/DeleteByID
// variants, including sentinel mapping.
func TestStoreGetAndDelete(t *testing.T) {
	store, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	rule := &Rule{TenantID: 5, Name: "r", Expression: "true", Enabled: true}
	mustCreate(t, store, rule)

	if _, err := store.GetByID(ctx, rule.ID); err != nil {
		t.Errorf("GetByID: %v", err)
	}
	if err := store.DeleteByID(ctx, rule.ID); err != nil {
		t.Fatalf("DeleteByID: %v", err)
	}
	if _, err := store.GetByID(ctx, rule.ID); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("GetByID after delete err = %v, want ErrRuleNotFound", err)
	}
	if err := store.DeleteByID(ctx, rule.ID); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("DeleteByID twice err = %v, want ErrRuleNotFound", err)
	}
}

// TestStoreListEnabled covers the engine's load path: only enabled
// rules are returned, ordered by priority descending then id ascending,
// and metadata round-trips through the tolerantjson serializer.
func TestStoreListEnabled(t *testing.T) {
	store, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	mustCreate(t, store, &Rule{
		Name: "low", Expression: "true", Enabled: true, Priority: 1,
		Metadata: map[string]string{"team": "ops"},
	})
	mustCreate(t, store, &Rule{Name: "high", Expression: "true", Enabled: true, Priority: 10})
	mustCreate(t, store, &Rule{Name: "disabled", Expression: "true", Enabled: false})
	mustCreate(t, store, &Rule{Name: "no-meta", Expression: "true", Enabled: true, Priority: 5})

	rules, err := store.ListEnabled(ctx, 0)
	if err != nil {
		t.Fatalf("ListEnabled: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("ListEnabled returned %d rules, want 3 (disabled excluded)", len(rules))
	}
	wantOrder := []string{"high", "no-meta", "low"}
	for i, rule := range rules {
		if rule.Name != wantOrder[i] {
			t.Errorf("rules[%d].Name = %q, want %q (priority DESC, id ASC)", i, rule.Name, wantOrder[i])
		}
	}

	// Metadata round-trips: the map persists and reloads as-is.
	for _, rule := range rules {
		switch rule.Name {
		case "low":
			if rule.Metadata["team"] != "ops" {
				t.Errorf("low: Metadata = %v, want team=ops", rule.Metadata)
			}
		case "no-meta":
			if rule.Metadata != nil {
				t.Errorf("no-meta: Metadata = %v, want nil", rule.Metadata)
			}
		}
	}
}

// TestStoreListEnabledTenantScope verifies that a positive tenantID
// scopes the listing while zero returns rules across tenants.
func TestStoreListEnabledTenantScope(t *testing.T) {
	store, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	mustCreate(t, store, &Rule{TenantID: 5, Name: "scoped", Expression: "true", Enabled: true})
	mustCreate(t, store, &Rule{TenantID: 7, Name: "other", Expression: "true", Enabled: true})

	scoped, err := store.ListEnabled(ctx, 5)
	if err != nil {
		t.Fatalf("ListEnabled(5): %v", err)
	}
	if len(scoped) != 1 || scoped[0].Name != "scoped" {
		t.Errorf("tenant 5 listing = %+v, want only the scoped rule", scoped)
	}

	all, err := store.ListEnabled(ctx, 0)
	if err != nil {
		t.Fatalf("ListEnabled(0): %v", err)
	}
	if len(all) != 2 {
		t.Errorf("cross-tenant listing = %d rules, want 2", len(all))
	}
}

// TestStoreListPagination covers the CRUD listing contract: page starts
// at 1, size defaults and clamps, and the total count covers all rows.
func TestStoreListPagination(t *testing.T) {
	store, cleanup := setupStore(t)
	defer cleanup()

	for range 3 {
		mustCreate(t, store, &Rule{Name: "r", Expression: "true", Enabled: true})
	}

	rules, total, err := store.List(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(rules) != 2 {
		t.Errorf("page size 2 returned %d rules", len(rules))
	}

	// Out-of-range page yields an empty slice, not an error.
	rules, _, err = store.List(context.Background(), 5, 2)
	if err != nil {
		t.Fatalf("list page 5: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("out-of-range page returned %d rules, want 0", len(rules))
	}
}
