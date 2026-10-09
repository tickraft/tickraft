// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/prism/channel/tracking"
)

// tenantResolverStub resolves tenants from a mutable value so tests can
// flip the "current tenant" between calls.
type tenantResolverStub struct {
	tenantID int64
	present  bool
}

func (r *tenantResolverStub) resolve(context.Context) (int64, bool) {
	return r.tenantID, r.present
}

// newTenantStores opens a test DB with tenant-scoped Store and
// DeliveryStore sharing a resolver stub.
func newTenantStores(t *testing.T) (*Store, *DeliveryStore, *tenantResolverStub) {
	t.Helper()
	gdb := newStoreTestDB(t)
	resolver := &tenantResolverStub{}
	return NewTenantStore(gdb, testEncryptionKey, resolver.resolve),
		NewTenantDeliveryStore(gdb, resolver.resolve), resolver
}

func TestTenantStore_Isolation(t *testing.T) {
	store, _, resolver := newTenantStores(t)
	ctx := context.Background()

	// Seed one channel per tenant.
	for _, tenantID := range []int64{1, 2} {
		resolver.tenantID, resolver.present = tenantID, true
		ch := newChannel("ch", "webhook", `{"url":"https://example.test/h"}`, true)
		if err := store.Create(ctx, &ch); err != nil {
			t.Fatalf("create for tenant %d: %v", tenantID, err)
		}
		if ch.TenantID != tenantID {
			t.Fatalf("create stamped tenant %d, want %d", ch.TenantID, tenantID)
		}
	}

	resolver.tenantID, resolver.present = 1, true
	listed, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].TenantID != 1 {
		t.Fatalf("list returned %v for tenant 1, want exactly one tenant-1 row", listed)
	}
	enabled, err := store.ListEnabled(ctx)
	if err != nil || len(enabled) != 1 {
		t.Fatalf("list enabled tenant 1: %v (rows %d), want 1 row", err, len(enabled))
	}

	// Tenant 2 cannot see, update, or delete tenant 1's row.
	resolver.tenantID, resolver.present = 2, true
	other := listed[0]
	if _, err := store.Get(ctx, other.ID); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("cross-tenant get: got %v, want ErrChannelNotFound", err)
	}
	other.Name = "hijacked"
	if err := store.Update(ctx, &other); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("cross-tenant update: got %v, want ErrChannelNotFound", err)
	}
	if err := store.Delete(ctx, other.ID); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("cross-tenant delete: got %v, want ErrChannelNotFound", err)
	}

	// Tenant 1 still sees its row intact.
	resolver.tenantID, resolver.present = 1, true
	kept, err := store.Get(ctx, other.ID)
	if err != nil || kept.Name != "ch" {
		t.Fatalf("owner re-read after cross-tenant attempts: %v name=%q", err, kept.Name)
	}

	// TouchLastUsedAt is lenient: tenant-less contexts still update by id.
	resolver.present = false
	if err := store.TouchLastUsedAt(ctx, other.ID, time.Now()); err != nil {
		t.Fatalf("tenant-less touch: %v", err)
	}
}

func TestTenantStore_MissingTenantRequired(t *testing.T) {
	store, _, resolver := newTenantStores(t)
	ctx := context.Background()
	resolver.present = false

	ch := newChannel("ch", "webhook", `{"url":"https://example.test/h"}`, true)
	if err := store.Create(ctx, &ch); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("create without tenant: got %v, want ErrTenantRequired", err)
	}
	if _, err := store.List(ctx); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("list without tenant: got %v, want ErrTenantRequired", err)
	}
	if _, err := store.ListEnabled(ctx); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("list enabled without tenant: got %v, want ErrTenantRequired", err)
	}
	if _, err := store.Get(ctx, 1); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("get without tenant: got %v, want ErrTenantRequired", err)
	}
	if err := store.Delete(ctx, 1); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("delete without tenant: got %v, want ErrTenantRequired", err)
	}
}

func TestTenantDeliveryStore_Scoping(t *testing.T) {
	channelStore, delivery, resolver := newTenantStores(t)
	ctx := context.Background()

	// Seed one channel and one delivery record per tenant.
	ids := make(map[int64]int64)
	for _, tenantID := range []int64{1, 2} {
		resolver.tenantID, resolver.present = tenantID, true
		ch := newChannel("ch", "webhook", `{"url":"https://example.test/h"}`, true)
		if err := channelStore.Create(ctx, &ch); err != nil {
			t.Fatalf("create channel tenant %d: %v", tenantID, err)
		}
		ids[tenantID] = ch.ID
		if err := delivery.Record(ctx, tracking.DeliveryRecord{
			Identity: tracking.Identity{ChannelID: ch.ID, ChannelName: "ch", ChannelType: "webhook"},
			Status:   tracking.StatusSuccess,
			Event:    trackingEvent(),
			SentAt:   time.Now().UTC(),
		}); err != nil {
			t.Fatalf("record tenant %d: %v", tenantID, err)
		}
	}

	// Record without a tenant context stores a system-level (tenant 0) row.
	resolver.present = false
	if err := delivery.Record(ctx, tracking.DeliveryRecord{
		Identity: tracking.Identity{ChannelName: "env", ChannelType: "webhook"},
		Status:   tracking.StatusSuccess,
		Event:    trackingEvent(),
		SentAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("record without tenant: %v", err)
	}

	// List is tenant-filtered.
	resolver.tenantID, resolver.present = 1, true
	rows, total, err := delivery.List(ctx, DeliveryListParams{Page: 1, Size: 10})
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("list tenant 1: %v total=%d rows=%d, want 1/1", err, total, len(rows))
	}
	keyset, err := delivery.ListKeyset(ctx, DeliveryListParams{Size: 10})
	if err != nil || keyset.Total != 1 || len(keyset.Items) != 1 {
		t.Fatalf("keyset tenant 1: %v total=%d rows=%d, want 1/1", err, keyset.Total, len(keyset.Items))
	}

	// Cross-tenant access behaves as not found.
	resolver.tenantID, resolver.present = 2, true
	if _, err := delivery.Get(ctx, rows[0].ID); !errors.Is(err, errdefs.ErrNotFound) {
		t.Fatalf("cross-tenant get: got %v, want ErrNotFound", err)
	}
	rec := rows[0]
	rec.Status = "failed"
	if err := delivery.UpdateAttempt(ctx, &rec); err != nil {
		t.Fatalf("cross-tenant update attempt: %v", err)
	}
	// The owner still sees the original outcome.
	resolver.tenantID, resolver.present = 1, true
	kept, err := delivery.Get(ctx, rows[0].ID)
	if err != nil || kept.Status != "success" {
		t.Fatalf("owner re-read: %v status=%q, want success", err, kept.Status)
	}

	// Owner update attempt lands.
	kept.Status = "failed"
	if err := delivery.UpdateAttempt(ctx, kept); err != nil {
		t.Fatalf("owner update attempt: %v", err)
	}

	// Missing tenant on strict paths fails closed.
	resolver.present = false
	if _, _, err := delivery.List(ctx, DeliveryListParams{Page: 1, Size: 10}); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("list without tenant: got %v, want ErrTenantRequired", err)
	}
	if _, err := delivery.ListKeyset(ctx, DeliveryListParams{Size: 10}); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("keyset without tenant: got %v, want ErrTenantRequired", err)
	}
	if _, err := delivery.Get(ctx, 1); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("get without tenant: got %v, want ErrTenantRequired", err)
	}
}
