// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package status

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/types"
)

// stubMonitors is a MonitorLister returning canned points.
type stubMonitors struct {
	active  []telemetry.MonitorPoint
	passive []telemetry.MonitorPoint
}

func (s stubMonitors) ListActive(context.Context) ([]telemetry.MonitorPoint, error) {
	return s.active, nil
}

func (s stubMonitors) ListPassive(context.Context) ([]telemetry.MonitorPoint, error) {
	return s.passive, nil
}

// stubProbes is a ProbeReader returning canned latest records.
type stubProbes struct {
	byPoint map[int64]*telemetry.ProbeRecord
}

func (s stubProbes) LatestByPoint(_ context.Context, pointID int64) (*telemetry.ProbeRecord, error) {
	if rec, ok := s.byPoint[pointID]; ok {
		return rec, nil
	}
	return nil, errdefs.ErrNotFound
}

// newTestService opens an in-memory sqlite store, migrates it, and wraps
// it in a service with the given data sources.
func newTestService(t *testing.T, monitors MonitorLister, probes ProbeReader, opts ...Option) Service {
	t.Helper()
	ctx := context.Background()
	gdb, err := db.Open(ctx, db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	store := NewStore(gdb)
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewService(store, monitors, probes, zap.NewNop(), opts...)
}

func TestStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, nil, nil)

	cfg, err := svc.GetConfig(ctx)
	if err != nil {
		t.Fatalf("get seeded config: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("seeded config should be disabled")
	}
	if cfg.Title != "Service Status" {
		t.Fatalf("seeded title = %q", cfg.Title)
	}

	in := &Config{
		Title:   "Acme Status",
		Enabled: true,
		Components: []Component{
			{Name: "API", Description: "public gateway", PointIDs: []int64{1, 2}},
			{Name: "DB"},
		},
	}
	updated, err := svc.UpdateConfig(ctx, in)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !updated.Enabled || updated.Title != "Acme Status" {
		t.Fatalf("updated config = %+v", updated)
	}
	if len(updated.Components) != 2 || len(updated.Components[0].PointIDs) != 2 {
		t.Fatalf("updated components = %+v", updated.Components)
	}

	// A fresh store over the same DB must decode the same shape.
	re, err := svc.GetConfig(ctx)
	if err != nil {
		t.Fatalf("re-get: %v", err)
	}
	if len(re.Components) != 2 || re.Components[1].Name != "DB" {
		t.Fatalf("re-read components = %+v", re.Components)
	}
}

func TestUpdateConfigValidation(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, nil, nil)

	cases := []struct {
		name string
		cfg  *Config
		want string
	}{
		{"nil config", nil, "config is required"},
		{"empty title", &Config{Title: ""}, "title is required"},
		{
			"empty component name",
			&Config{Title: "t", Components: []Component{{Name: "ok"}, {Name: ""}}},
			"components[1]",
		},
		{
			"duplicate component name",
			&Config{Title: "t", Components: []Component{{Name: "a"}, {Name: "a"}}},
			"duplicate",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.UpdateConfig(ctx, tc.cfg)
			var ve *ErrValidation
			if !errors.As(err, &ve) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
			if !strings.Contains(ve.Msg, tc.want) {
				t.Fatalf("msg = %q, want substring %q", ve.Msg, tc.want)
			}
		})
	}
}

func TestPublicViewDisabled(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, nil, nil)
	if _, err := svc.PublicView(ctx); !errors.Is(err, ErrDisabled) {
		t.Fatalf("error = %v, want ErrDisabled", err)
	}
}

func TestPublicViewAggregation(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	monitors := stubMonitors{active: []telemetry.MonitorPoint{
		{ID: 1, Name: "api", Enabled: true, Status: telemetry.MonitorStatusActive},
		{ID: 2, Name: "db", Enabled: true, Status: telemetry.MonitorStatusActive},
		{ID: 3, Name: "cache", Enabled: true, Status: telemetry.MonitorStatusError},
		{ID: 4, Name: "disabled", Enabled: false, Status: telemetry.MonitorStatusActive},
	}}
	probes := stubProbes{byPoint: map[int64]*telemetry.ProbeRecord{
		1: {PointID: 1, Status: types.AssetStatusNormal, StartedAt: now},
		2: {PointID: 2, Status: types.AssetStatusAbnormal, StartedAt: now},
	}}
	svc := newTestService(t, monitors, probes)

	if _, err := svc.UpdateConfig(ctx, &Config{
		Title:   "Status",
		Enabled: true,
		Components: []Component{
			{Name: "Gateway", PointIDs: []int64{1}},
			{Name: "Storage", PointIDs: []int64{2, 3}},
			{Name: "Placeholder"},
		},
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	view, err := svc.PublicView(ctx)
	if err != nil {
		t.Fatalf("public view: %v", err)
	}
	byName := map[string]ComponentStatus{}
	for _, c := range view.Components {
		byName[c.Name] = c
	}
	if got := byName["Gateway"].Status; got != StatusOperational {
		t.Errorf("gateway = %s, want operational", got)
	}
	if got := byName["Gateway"].MonitorCount; got != 1 {
		t.Errorf("gateway monitor count = %d, want 1", got)
	}
	if got := byName["Storage"].Status; got != StatusDegraded {
		t.Errorf("storage = %s, want degraded", got)
	}
	if got := byName["Placeholder"].Status; got != StatusUnknown {
		t.Errorf("placeholder = %s, want unknown", got)
	}
	if _, ok := byName["disabled"]; ok {
		t.Error("disabled point must not render as a component")
	}
	if view.Overall != StatusDegraded {
		t.Errorf("overall = %s, want degraded", view.Overall)
	}
	if !view.UpdatedAt.After(now.Add(-time.Second)) {
		t.Error("view UpdatedAt not set")
	}
}

func TestPublicViewAutoComponents(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	monitors := stubMonitors{active: []telemetry.MonitorPoint{
		{ID: 7, Name: "edge", Enabled: true, Status: telemetry.MonitorStatusActive},
		{ID: 2, Name: "core", Enabled: true, Status: telemetry.MonitorStatusActive},
	}}
	probes := stubProbes{byPoint: map[int64]*telemetry.ProbeRecord{
		2: {PointID: 2, Status: types.AssetStatusNormal, StartedAt: now},
		7: {PointID: 7, Status: types.AssetStatusOffline, StartedAt: now},
	}}
	svc := newTestService(t, monitors, probes)

	if _, err := svc.UpdateConfig(ctx, &Config{Title: "Auto", Enabled: true}); err != nil {
		t.Fatalf("update: %v", err)
	}
	view, err := svc.PublicView(ctx)
	if err != nil {
		t.Fatalf("public view: %v", err)
	}
	if len(view.Components) != 2 {
		t.Fatalf("components = %d, want 2 (one per enabled point)", len(view.Components))
	}
	// Sorted by point ID: core(2) before edge(7).
	if view.Components[0].Name != "core" || view.Components[1].Name != "edge" {
		t.Fatalf("order = [%s, %s], want [core, edge]", view.Components[0].Name, view.Components[1].Name)
	}
	if view.Overall != StatusMajorOutage {
		t.Errorf("overall = %s, want major_outage", view.Overall)
	}
}

func TestPublicViewCacheAndInvalidation(t *testing.T) {
	ctx := context.Background()
	monitors := stubMonitors{active: []telemetry.MonitorPoint{
		{ID: 1, Name: "api", Enabled: true, Status: telemetry.MonitorStatusActive},
	}}
	probes := stubProbes{byPoint: map[int64]*telemetry.ProbeRecord{
		1: {PointID: 1, Status: types.AssetStatusNormal, StartedAt: time.Now()},
	}}
	svc := newTestService(t, monitors, probes, WithCacheTTL(time.Minute))

	if _, err := svc.UpdateConfig(ctx, &Config{Title: "T", Enabled: true}); err != nil {
		t.Fatalf("update: %v", err)
	}
	first, err := svc.PublicView(ctx)
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	second, err := svc.PublicView(ctx)
	if err != nil {
		t.Fatalf("cached render: %v", err)
	}
	if first != second {
		t.Fatal("second call should return the cached view object")
	}

	// An update invalidates the cache despite the long TTL.
	if _, err := svc.UpdateConfig(ctx, &Config{Title: "T2", Enabled: true}); err != nil {
		t.Fatalf("update: %v", err)
	}
	third, err := svc.PublicView(ctx)
	if err != nil {
		t.Fatalf("post-update render: %v", err)
	}
	if third == first {
		t.Fatal("update should invalidate the cached view")
	}
	if third.Title != "T2" {
		t.Fatalf("title = %q, want T2", third.Title)
	}
}

func TestSystemProbeComponents(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, stubMonitors{}, stubProbes{},
		WithSystemProbe(SystemProbe{Name: "Task Engine", Check: func(context.Context) error { return nil }}),
		WithSystemProbe(SystemProbe{Name: "Broken", Check: func(context.Context) error {
			return errors.New("boom")
		}}),
		WithSystemProbe(SystemProbe{Name: "Wired"}),
	)
	if _, err := svc.UpdateConfig(ctx, &Config{Title: "T", Enabled: true}); err != nil {
		t.Fatalf("update: %v", err)
	}
	view, err := svc.PublicView(ctx)
	if err != nil {
		t.Fatalf("public view: %v", err)
	}
	byName := map[string]ComponentStatus{}
	for _, c := range view.Components {
		byName[c.Name] = c
	}
	if got := byName["Task Engine"].Status; got != StatusOperational {
		t.Errorf("task engine = %s, want operational", got)
	}
	if got := byName["Broken"].Status; got != StatusDegraded {
		t.Errorf("broken = %s, want degraded", got)
	}
	if got := byName["Wired"].Status; got != StatusUnknown {
		t.Errorf("nil check = %s, want unknown", got)
	}
	if view.Overall != StatusDegraded {
		t.Errorf("overall = %s, want degraded", view.Overall)
	}
}

func TestAggregateHealth(t *testing.T) {
	cases := []struct {
		states []string
		want   string
	}{
		{nil, StatusUnknown},
		{[]string{StatusUnknown}, StatusUnknown},
		{[]string{StatusOperational}, StatusOperational},
		{[]string{StatusOperational, StatusUnknown}, StatusOperational},
		{[]string{StatusOperational, StatusDegraded}, StatusPartialOutage},
		{[]string{StatusDegraded, StatusMajorOutage}, StatusPartialOutage},
		{[]string{StatusDegraded, StatusDegraded}, StatusDegraded},
		{[]string{StatusMajorOutage, StatusMajorOutage}, StatusMajorOutage},
		{[]string{StatusOperational, StatusDegraded, StatusMajorOutage}, StatusPartialOutage},
	}
	for _, tc := range cases {
		if got := aggregateHealth(tc.states); got != tc.want {
			t.Errorf("aggregateHealth(%v) = %s, want %s", tc.states, got, tc.want)
		}
	}
}

func TestOverallStatusIgnoresUnknown(t *testing.T) {
	comps := []ComponentStatus{
		{Name: "a", Status: StatusOperational},
		{Name: "b", Status: StatusUnknown},
	}
	if got := Overall(comps); got != StatusOperational {
		t.Errorf("overall = %s, want operational", got)
	}
	if got := Overall([]ComponentStatus{{Status: StatusUnknown}}); got != StatusUnknown {
		t.Errorf("all-unknown overall = %s, want unknown", got)
	}
}
