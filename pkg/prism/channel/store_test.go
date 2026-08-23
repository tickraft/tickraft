// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/db"
)

func setupStore(t *testing.T) (store *Store, cleanup func()) {
	t.Helper()
	ctx := context.Background()
	gdb, err := db.Open(ctx, db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store = NewStore(gdb)
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

// TestStoreUpdatePreservesEngineState pins the Update column whitelist:
// a PUT-shaped model (client-bound fields, zero last_used_at) must never
// reset the engine-owned last_used_at bookkeeping (the guardrail class
// the model-layering redesign introduced alongside direct model binding).
func TestStoreUpdatePreservesEngineState(t *testing.T) {
	store, cleanup := setupStore(t)
	defer cleanup()
	ctx := context.Background()

	created := &Channel{Name: "hook", Type: "webhook", Config: `{"url":"https://example.test"}`, Enabled: true}
	if err := store.Create(ctx, created); err != nil {
		t.Fatalf("create: %v", err)
	}

	used := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	if err := store.TouchLastUsedAt(ctx, created.ID, used); err != nil {
		t.Fatalf("touch last_used_at: %v", err)
	}

	// PUT-shaped update: renamed and disabled, last_used_at left zero.
	updated := &Channel{
		ID:      created.ID,
		Name:    "hook2",
		Type:    "webhook",
		Config:  `{"url":"https://example.test/v2"}`,
		Enabled: false,
	}
	if err := store.Update(ctx, updated); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := store.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "hook2" || got.Enabled {
		t.Errorf("editable fields not applied: name=%q enabled=%v", got.Name, got.Enabled)
	}
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(used) {
		t.Errorf("last_used_at = %v, want preserved %v (engine-owned, never reset by PUT)", got.LastUsedAt, used)
	}
	if got.CreatedAt.IsZero() {
		t.Error("created_at was cleared by update")
	}
}

// TestStoreUpdateNotFound verifies the not-found mapping of a missing row.
func TestStoreUpdateNotFound(t *testing.T) {
	store, cleanup := setupStore(t)
	defer cleanup()

	err := store.Update(context.Background(), &Channel{ID: 99999, Name: "x", Type: "webhook", Config: "{}"})
	if !errors.Is(err, ErrChannelNotFound) {
		t.Errorf("Update on missing row = %v, want ErrChannelNotFound", err)
	}
}
