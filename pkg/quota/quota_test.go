// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package quota

import (
	"sync"
	"testing"
)

func TestTypeConstants(t *testing.T) {
	tests := []struct {
		name string
		t    Type
		want string
	}{
		{"TypeAsset", TypeAsset, "asset"},
		{"TypeDevice", TypeDevice, "device"},
		{"TypeHost", TypeHost, "host"},
		{"TypeTeamMember", TypeTeamMember, "team_member"},
		{"TypeCustomField", TypeCustomField, "custom_field"},
		{"TypeScheduledTask", TypeScheduledTask, "scheduled_task"},
		{"TypeProber", TypeProber, "prober"},
		{"TypeRemediation", TypeRemediation, "remediation"},
		{"TypeProbeInterval", TypeProbeInterval, "probe_interval"},
		{"TypeScheduledTaskInterval", TypeScheduledTaskInterval, "scheduled_task_interval"},
		{"TypeDailyEvents", TypeDailyEvents, "daily_events"},
		{"TypeIngestionMetricTPS", TypeIngestionMetricTPS, "ingestion_metric_tps"},
		{"TypeIngestionEventTPS", TypeIngestionEventTPS, "ingestion_event_tps"},
		{"TypeConcurrentTasks", TypeConcurrentTasks, "concurrent_tasks"},
		{"TypeAPIMinute", TypeAPIMinute, "api_minute"},
		{"TypeAPIDaily", TypeAPIDaily, "api_daily"},
		{"TypeAPIConcurrent", TypeAPIConcurrent, "api_concurrent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.t) != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.t, tt.want)
			}
		})
	}
}

func TestTypeAliases(t *testing.T) {
	if TypeTask != TypeScheduledTask {
		t.Errorf("TypeTask (%s) should alias TypeScheduledTask (%s)", TypeTask, TypeScheduledTask)
	}
	if TypeHTTPInterval != TypeProbeInterval {
		t.Errorf("TypeHTTPInterval (%s) should alias TypeProbeInterval (%s)", TypeHTTPInterval, TypeProbeInterval)
	}
}

func TestLayerOf(t *testing.T) {
	assetTypes := []Type{TypeAsset, TypeDevice, TypeHost, TypeTeamMember, TypeCustomField}
	configTypes := []Type{TypeScheduledTask, TypeProber, TypeRemediation, TypeProbeInterval, TypeScheduledTaskInterval}
	runtimeTypes := []Type{TypeDailyEvents, TypeIngestionMetricTPS, TypeIngestionEventTPS, TypeConcurrentTasks, TypeAPIMinute, TypeAPIDaily, TypeAPIConcurrent}

	for _, typ := range assetTypes {
		if got := LayerOf(typ); got != LayerAsset {
			t.Errorf("LayerOf(%s) = %d, want LayerAsset (%d)", typ, got, LayerAsset)
		}
	}
	for _, typ := range configTypes {
		if got := LayerOf(typ); got != LayerConfig {
			t.Errorf("LayerOf(%s) = %d, want LayerConfig (%d)", typ, got, LayerConfig)
		}
	}
	for _, typ := range runtimeTypes {
		if got := LayerOf(typ); got != LayerRuntime {
			t.Errorf("LayerOf(%s) = %d, want LayerRuntime (%d)", typ, got, LayerRuntime)
		}
	}
	if got := LayerOf(Type("unknown")); got != LayerUnknown {
		t.Errorf("LayerOf(unknown) = %d, want LayerUnknown (%d)", got, LayerUnknown)
	}
}

func TestIsScalable(t *testing.T) {
	scalable := []Type{TypeAsset, TypeDevice, TypeHost, TypeTeamMember, TypeScheduledTask, TypeProber, TypeRemediation, TypeDailyEvents}
	notScalable := []Type{TypeProbeInterval, TypeScheduledTaskInterval, TypeIngestionMetricTPS, TypeIngestionEventTPS, TypeConcurrentTasks, TypeAPIMinute, TypeAPIDaily, TypeAPIConcurrent}

	for _, typ := range scalable {
		if !IsScalable(typ) {
			t.Errorf("IsScalable(%s) = false, want true", typ)
		}
	}
	for _, typ := range notScalable {
		if IsScalable(typ) {
			t.Errorf("IsScalable(%s) = true, want false", typ)
		}
	}
}

// staticProvider is a test Provider that returns a fixed value for all
// types.
type staticProvider struct {
	value int
}

func (s staticProvider) Ceiling(Type) int { return s.value }

func TestSetProviderAndCeiling(t *testing.T) {
	t.Cleanup(func() { SetProvider(nil) })

	SetProvider(staticProvider{value: 42})
	if got := Ceiling(TypeDevice); got != 42 {
		t.Fatalf("Ceiling(TypeDevice) = %d, want 42", got)
	}

	SetProvider(staticProvider{value: 100})
	if got := Ceiling(TypeProber); got != 100 {
		t.Fatalf("Ceiling(TypeProber) = %d, want 100", got)
	}
}

func TestSetProviderNil(t *testing.T) {
	SetProvider(staticProvider{value: 50})
	t.Cleanup(func() { SetProvider(nil) })

	if got := Ceiling(TypeDevice); got != 50 {
		t.Fatalf("Ceiling before nil = %d, want 50", got)
	}

	SetProvider(nil)
	if got := Ceiling(TypeDevice); got != 0 {
		t.Fatalf("Ceiling after nil = %d, want 0 (zeroProvider)", got)
	}
}

func TestActiveProvider(t *testing.T) {
	t.Cleanup(func() { SetProvider(nil) })

	p := staticProvider{value: 7}
	SetProvider(p)
	ap := ActiveProvider()
	if ap.Ceiling(TypeDevice) != 7 {
		t.Fatalf("ActiveProvider().Ceiling = %d, want 7", ap.Ceiling(TypeDevice))
	}
}

// specProvider is a test Provider that implements ProviderWithSpec.
type specProvider struct{}

func (specProvider) Ceiling(Type) int { return 999 }

func (specProvider) Spec(t Type) Spec {
	return Spec{
		Ceiling:  999,
		Layer:    LayerOf(t),
		Scalable: IsScalable(t),
	}
}

func TestGetSpecWithSpecProvider(t *testing.T) {
	t.Cleanup(func() { SetProvider(nil) })

	SetProvider(specProvider{})
	s := GetSpec(TypeDevice)
	if s.Ceiling != 999 {
		t.Fatalf("GetSpec(TypeDevice).Ceiling = %d, want 999", s.Ceiling)
	}
	if s.Layer != LayerAsset {
		t.Fatalf("GetSpec(TypeDevice).Layer = %d, want LayerAsset", s.Layer)
	}
	if !s.Scalable {
		t.Fatalf("GetSpec(TypeDevice).Scalable = false, want true")
	}
}

func TestGetSpecWithoutSpecProvider(t *testing.T) {
	t.Cleanup(func() { SetProvider(nil) })

	SetProvider(staticProvider{value: 55})
	s := GetSpec(TypeProber)
	if s.Ceiling != 55 {
		t.Fatalf("GetSpec(TypeProber).Ceiling = %d, want 55", s.Ceiling)
	}
	if s.Layer != LayerConfig {
		t.Fatalf("GetSpec(TypeProber).Layer = %d, want LayerConfig", s.Layer)
	}
	if !s.Scalable {
		t.Fatalf("GetSpec(TypeProber).Scalable = false, want true")
	}
}

func TestProviderInterfaceCompliance(t *testing.T) {
	var _ Provider = specProvider{}
	var _ Provider = staticProvider{}
	var _ Provider = zeroProvider{}
	var _ ProviderWithSpec = specProvider{}
}

func TestConcurrentSetProviderAndCeiling(t *testing.T) {
	t.Cleanup(func() { SetProvider(nil) })

	var wg sync.WaitGroup
	providers := []Provider{
		staticProvider{value: 10},
		staticProvider{value: 20},
		staticProvider{value: 30},
	}

	// Concurrent SetProvider + Ceiling calls
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			SetProvider(providers[idx%len(providers)])
		}(i)
		go func() {
			defer wg.Done()
			// Should never panic and should return one of the
			// provider values or 0 (race between SetProvider and read).
			_ = Ceiling(TypeDevice)
		}()
	}
	wg.Wait()
}
