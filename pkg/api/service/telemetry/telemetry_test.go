// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/tickraft/tickraft/pkg/api/handler"
	telemetryhandler "github.com/tickraft/tickraft/pkg/api/handler/telemetry"
	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/telemetry"
)

// newProbeTestService opens an in-memory store seeded with one active
// enabled point, one passive enabled point, and one disabled active point.
func newProbeTestService(t *testing.T) (svc *Service, probed *[]int64) {
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
	if err := telemetry.Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := telemetry.NewMonitorStore(dbc)
	seed := []telemetry.MonitorPoint{
		{ID: 1, Name: "active", Mode: telemetry.ModeActive, Type: "icmp", Enabled: true},
		{ID: 2, Name: "passive", Mode: telemetry.ModePassive, Type: "webhook", Enabled: true},
		{ID: 3, Name: "disabled", Mode: telemetry.ModeActive, Type: "tcp", Enabled: false},
	}
	for i := range seed {
		if err := store.Create(context.Background(), &seed[i]); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	probed = &[]int64{}
	svc = NewService(store, nil, WithProbeTrigger(func(_ context.Context, pointID int64) error {
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
	if err := telemetry.Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := telemetry.NewMonitorStore(dbc)
	point := telemetry.MonitorPoint{ID: 1, Name: "active", Mode: telemetry.ModeActive, Type: "icmp", Enabled: true}
	if err := store.Create(context.Background(), &point); err != nil {
		t.Fatalf("seed: %v", err)
	}
	svc := NewService(store, nil)
	_, err = svc.ProbeNow(context.Background(), 1)
	var svcErr *handler.ServiceError
	if !errors.As(err, &svcErr) || svcErr.HTTPStatus() != http.StatusServiceUnavailable {
		t.Fatalf("ProbeNow without trigger: got %v, want 503 service error", err)
	}
}

func TestProbeNowUnknownPoint(t *testing.T) {
	svc, _ := newProbeTestService(t)
	_, err := svc.ProbeNow(context.Background(), 999)
	if !errors.Is(err, telemetryhandler.ErrTelemetryTaskNotFound) {
		t.Fatalf("ProbeNow unknown: got %v, want ErrTelemetryTaskNotFound", err)
	}
}

// assertBadRequest verifies the error is a 400 service error.
func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	var svcErr *handler.ServiceError
	if !errors.As(err, &svcErr) || svcErr.HTTPStatus() != http.StatusBadRequest {
		t.Fatalf("got %v, want 400 service error", err)
	}
}
