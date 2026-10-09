// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/types"
)

// newProbeTestService opens an in-memory store seeded with one active
// enabled point, one passive enabled point, and one disabled active point.
func newProbeTestService(t *testing.T) (svc *TelemetryService, probed *[]int64) {
	t.Helper()
	dbc, err := db.Open(context.Background(), db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := dbc.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := NewMonitorStore(dbc)
	seed := []MonitorPoint{
		{ID: 1, Name: "active", Mode: ModeActive, Type: "icmp", Enabled: true},
		{ID: 2, Name: "passive", Mode: ModePassive, Type: "webhook", Enabled: true},
		{ID: 3, Name: "disabled", Mode: ModeActive, Type: "tcp", Enabled: false},
	}
	for i := range seed {
		if err := store.Create(context.Background(), &seed[i]); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	probed = &[]int64{}
	svc = NewTelemetryService(store, nil, WithProbeTrigger(func(_ context.Context, pointID int64) error {
		*probed = append(*probed, pointID)
		return nil
	}))
	return
}

func TestProbeNowDispatchesActiveEnabledPoint(t *testing.T) {
	svc, called := newProbeTestService(t)
	point, err := svc.ProbeNow(context.Background(), 1)
	if err != nil {
		t.Fatalf("ProbeNow: %v", err)
	}
	if point.ID != 1 {
		t.Fatalf("returned point id = %d, want 1", point.ID)
	}
	if len(*called) != 1 || (*called)[0] != 1 {
		t.Fatalf("trigger calls = %v, want [1]", *called)
	}
}

func TestProbeNowRejectsPassivePoint(t *testing.T) {
	svc, called := newProbeTestService(t)
	_, err := svc.ProbeNow(context.Background(), 2)
	assertBadRequest(t, err)
	if len(*called) != 0 {
		t.Fatalf("trigger calls = %v, want none", *called)
	}
}

func TestProbeNowRejectsDisabledPoint(t *testing.T) {
	svc, called := newProbeTestService(t)
	_, err := svc.ProbeNow(context.Background(), 3)
	assertBadRequest(t, err)
	if len(*called) != 0 {
		t.Fatalf("trigger calls = %v, want none", *called)
	}
}

func TestProbeNowWithoutTriggerIsUnavailable(t *testing.T) {
	dbc, err := db.Open(context.Background(), db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := dbc.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := NewMonitorStore(dbc)
	point := MonitorPoint{ID: 1, Name: "active", Mode: ModeActive, Type: "icmp", Enabled: true}
	if err := store.Create(context.Background(), &point); err != nil {
		t.Fatalf("seed: %v", err)
	}
	svc := NewTelemetryService(store, nil)
	_, err = svc.ProbeNow(context.Background(), 1)
	var svcErr *errdefs.ServiceError
	if !errors.As(err, &svcErr) || svcErr.HTTPStatus() != http.StatusServiceUnavailable {
		t.Fatalf("ProbeNow without trigger: got %v, want 503 service error", err)
	}
}

func TestProbeNowUnknownPoint(t *testing.T) {
	svc, _ := newProbeTestService(t)
	_, err := svc.ProbeNow(context.Background(), 999)
	if !errors.Is(err, ErrMonitorNotFound) {
		t.Fatalf("ProbeNow unknown: got %v, want ErrMonitorNotFound", err)
	}
}

// assertBadRequest verifies the error is a 400 service error.
func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	var svcErr *errdefs.ServiceError
	if !errors.As(err, &svcErr) || svcErr.HTTPStatus() != http.StatusBadRequest {
		t.Fatalf("got %v, want 400 service error", err)
	}
}

// newAssetValidatedService opens an in-memory store plus a migrated kernel
// asset store (sys_asset) wired into the telemetry service through
// WithAssetGetter, so the create-time asset-binding validation runs against
// real rows rather than a stub.
func newAssetValidatedService(t *testing.T) (svc *TelemetryService, assets asset.Store) {
	t.Helper()
	dbc, err := db.Open(context.Background(), db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := dbc.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assets = asset.NewStore(dbc)
	if err := assets.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate asset store: %v", err)
	}
	store := NewMonitorStore(dbc)
	svc = NewTelemetryService(store, nil, WithAssetGetter(assets))
	return svc, assets
}

// TestCreateMonitorAssetValidation covers the create-time asset-binding
// validation on the monitor point path (spec asset.md §17, ALIGN-V3-011):
// both passive and active points reject an asset_id that references a
// missing asset row with 400 "asset_id references an unknown asset"; a
// binding to an existing asset passes and persists; asset_id 0 keeps its
// "unbound" meaning and always passes; and a service without the getter
// wired skips the check.
func TestCreateMonitorAssetValidation(t *testing.T) {
	svc, assets := newAssetValidatedService(t)

	known := &asset.Asset{AssetType: types.AssetTypeHost, AssetKey: "host-1", Name: "host-1"}
	if err := assets.Create(context.Background(), known); err != nil {
		t.Fatalf("seed asset: %v", err)
	}

	t.Run("active point unknown asset rejected", func(t *testing.T) {
		_, err := svc.CreateMonitor(context.Background(), &MonitorPoint{
			Name: "active-bad", Mode: ModeActive, Type: "icmp", Enabled: true,
			AssetID: known.ID + 9999,
		})
		assertBadRequest(t, err)
		var svcErr *errdefs.ServiceError
		if !errors.As(err, &svcErr) || !strings.Contains(svcErr.Error(), "asset_id references an unknown asset") {
			t.Fatalf("error message = %v, want unknown-asset wording", err)
		}
	})

	t.Run("passive point unknown asset rejected", func(t *testing.T) {
		_, err := svc.CreateMonitor(context.Background(), &MonitorPoint{
			Name: "passive-bad", Mode: ModePassive, Type: "webhook", Enabled: true,
			AssetID: known.ID + 9999,
		})
		assertBadRequest(t, err)
	})

	t.Run("valid asset accepted", func(t *testing.T) {
		created, err := svc.CreateMonitor(context.Background(), &MonitorPoint{
			Name: "active-ok", Mode: ModeActive, Type: "icmp", Enabled: true,
			AssetID: known.ID,
		})
		if err != nil {
			t.Fatalf("create bound point: %v", err)
		}
		if created.AssetID != known.ID {
			t.Fatalf("binding not persisted: asset_id = %d, want %d", created.AssetID, known.ID)
		}
	})

	t.Run("zero asset id means unbound", func(t *testing.T) {
		created, err := svc.CreateMonitor(context.Background(), &MonitorPoint{
			Name: "unbound", Mode: ModeActive, Type: "tcp", Enabled: true,
		})
		if err != nil {
			t.Fatalf("create unbound point: %v", err)
		}
		if created.AssetID != 0 {
			t.Fatalf("asset_id = %d, want 0", created.AssetID)
		}
	})

	t.Run("nil getter skips validation", func(t *testing.T) {
		dbc, err := db.Open(context.Background(), db.Config{Driver: "sqlite3", Addr: ":memory:"})
		if err != nil {
			t.Fatalf("open db: %v", err)
		}
		t.Cleanup(func() {
			if sqlDB, err := dbc.DB(); err == nil {
				_ = sqlDB.Close()
			}
		})
		if err := Migrate(context.Background(), dbc); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		legacy := NewTelemetryService(NewMonitorStore(dbc), nil)
		if _, err := legacy.CreateMonitor(context.Background(), &MonitorPoint{
			Name: "legacy", Mode: ModeActive, Type: "icmp", Enabled: true, AssetID: 9999,
		}); err != nil {
			t.Fatalf("unwired service must accept dangling binding: %v", err)
		}
	})
}

// TestUpdateMonitorAssetValidation covers the asset-binding validation on the
// monitor update path (spec asset.md §17, ALIGN-V3-011): rebinding to a
// missing asset row is rejected with 400 "asset_id references an unknown
// asset", rebinding to an existing asset and clearing the binding (asset_id 0)
// both succeed, and a service without the getter wired skips the check.
func TestUpdateMonitorAssetValidation(t *testing.T) {
	svc, assets := newAssetValidatedService(t)

	known := &asset.Asset{AssetType: types.AssetTypeHost, AssetKey: "host-upd", Name: "host-upd"}
	if err := assets.Create(context.Background(), known); err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	point, err := svc.CreateMonitor(context.Background(), &MonitorPoint{
		Name: "bound", Mode: ModeActive, Type: "icmp", Enabled: true,
		AssetID: known.ID,
	})
	if err != nil {
		t.Fatalf("seed point: %v", err)
	}

	t.Run("rebind to unknown asset rejected", func(t *testing.T) {
		_, err := svc.UpdateMonitor(context.Background(), point.ID, &MonitorPoint{
			Name: "bound", Mode: ModeActive, Type: "icmp", Enabled: true,
			AssetID: known.ID + 9999,
		})
		assertBadRequest(t, err)
		var svcErr *errdefs.ServiceError
		if !errors.As(err, &svcErr) || !strings.Contains(svcErr.Error(), "asset_id references an unknown asset") {
			t.Fatalf("error message = %v, want unknown-asset wording", err)
		}
		// The stored binding is untouched by the rejected update.
		stored, err := svc.GetMonitor(context.Background(), point.ID)
		if err != nil {
			t.Fatalf("get point: %v", err)
		}
		if stored.AssetID != known.ID {
			t.Fatalf("binding changed on rejected update: asset_id = %d, want %d", stored.AssetID, known.ID)
		}
	})

	t.Run("rebind to valid asset accepted", func(t *testing.T) {
		target := &asset.Asset{AssetType: types.AssetTypeHost, AssetKey: "host-upd-2", Name: "host-upd-2"}
		if err := assets.Create(context.Background(), target); err != nil {
			t.Fatalf("seed target asset: %v", err)
		}
		updated, err := svc.UpdateMonitor(context.Background(), point.ID, &MonitorPoint{
			Name: "rebound", Mode: ModeActive, Type: "icmp", Enabled: true,
			AssetID: target.ID,
		})
		if err != nil {
			t.Fatalf("rebind point: %v", err)
		}
		if updated.AssetID != target.ID {
			t.Fatalf("rebind not persisted: asset_id = %d, want %d", updated.AssetID, target.ID)
		}
	})

	t.Run("clear binding with zero asset id", func(t *testing.T) {
		updated, err := svc.UpdateMonitor(context.Background(), point.ID, &MonitorPoint{
			Name: "unbound", Mode: ModeActive, Type: "icmp", Enabled: true,
		})
		if err != nil {
			t.Fatalf("clear binding: %v", err)
		}
		if updated.AssetID != 0 {
			t.Fatalf("asset_id = %d, want 0", updated.AssetID)
		}
	})

	t.Run("nil getter skips validation", func(t *testing.T) {
		dbc, err := db.Open(context.Background(), db.Config{Driver: "sqlite3", Addr: ":memory:"})
		if err != nil {
			t.Fatalf("open db: %v", err)
		}
		t.Cleanup(func() {
			if sqlDB, err := dbc.DB(); err == nil {
				_ = sqlDB.Close()
			}
		})
		if err := Migrate(context.Background(), dbc); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		store := NewMonitorStore(dbc)
		seed := MonitorPoint{ID: 1, Name: "legacy", Mode: ModeActive, Type: "icmp", Enabled: true, AssetID: 777}
		if err := store.Create(context.Background(), &seed); err != nil {
			t.Fatalf("seed legacy point: %v", err)
		}
		legacy := NewTelemetryService(store, nil)
		if _, err := legacy.UpdateMonitor(context.Background(), seed.ID, &MonitorPoint{
			Name: "legacy", Mode: ModeActive, Type: "icmp", Enabled: true, AssetID: 9999,
		}); err != nil {
			t.Fatalf("unwired service must accept dangling rebind: %v", err)
		}
	})
}
