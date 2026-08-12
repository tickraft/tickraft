// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package quota

import (
	"sync"
	"testing"

	pkgquota "github.com/tickraft/tickraft/pkg/quota"
)

// mockProvider is a test Provider that returns configurable ceilings for
// dynamic-switch testing.
type mockProvider struct {
	value int
}

func (m mockProvider) Ceiling(_ pkgquota.Type) int { return m.value }

// TestIntegrationRegisterToCeiling verifies the full lifecycle:
// Register → Ceiling returns CE defaults → SetProvider(mock) → Ceiling
// returns mock value → SetProvider(nil) → Ceiling returns 0.
func TestIntegrationRegisterToCeiling(t *testing.T) {
	t.Cleanup(func() { pkgquota.SetProvider(nil) })

	// 1. Register injects DefaultProvider.
	Register()

	// 2. Verify CE defaults are active.
	if got := pkgquota.Ceiling(pkgquota.TypeDevice); got != CeilingDevice {
		t.Fatalf("after Register, Ceiling(Device) = %d, want %d", got, CeilingDevice)
	}
	if got := pkgquota.Ceiling(pkgquota.TypeRemediation); got != CeilingRemediation {
		t.Fatalf("after Register, Ceiling(Remediation) = %d, want %d", got, CeilingRemediation)
	}

	// 3. Dynamic switch to a mock provider.
	pkgquota.SetProvider(mockProvider{value: 500})
	if got := pkgquota.Ceiling(pkgquota.TypeDevice); got != 500 {
		t.Fatalf("after SetProvider(mock), Ceiling(Device) = %d, want 500", got)
	}

	// 4. Revert to nil (zeroProvider).
	pkgquota.SetProvider(nil)
	if got := pkgquota.Ceiling(pkgquota.TypeDevice); got != 0 {
		t.Fatalf("after SetProvider(nil), Ceiling(Device) = %d, want 0", got)
	}
}

// TestIntegrationSpecAfterRegister verifies that GetSpec returns correct
// metadata after Register.
func TestIntegrationSpecAfterRegister(t *testing.T) {
	t.Cleanup(func() { pkgquota.SetProvider(nil) })

	Register()

	s := pkgquota.GetSpec(pkgquota.TypeDevice)
	if s.Ceiling != CeilingDevice {
		t.Fatalf("GetSpec(Device).Ceiling = %d, want %d", s.Ceiling, CeilingDevice)
	}
	if s.Layer != pkgquota.LayerAsset {
		t.Fatalf("GetSpec(Device).Layer = %d, want LayerAsset", s.Layer)
	}
	if !s.Scalable {
		t.Fatal("GetSpec(Device).Scalable = false, want true")
	}
}

// TestIntegrationConcurrentCeilingAndSetProvider verifies that concurrent
// Ceiling calls and SetProvider calls do not cause data races or panics.
func TestIntegrationConcurrentCeilingAndSetProvider(t *testing.T) {
	t.Cleanup(func() { pkgquota.SetProvider(nil) })

	Register()

	var wg sync.WaitGroup
	providers := []pkgquota.Provider{
		DefaultProvider{},
		mockProvider{value: 100},
		mockProvider{value: 200},
	}

	// 50 goroutines calling Ceiling concurrently.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = pkgquota.Ceiling(pkgquota.TypeDevice)
				_ = pkgquota.Ceiling(pkgquota.TypeProber)
			}
		}()
	}

	// 10 goroutines switching providers concurrently.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pkgquota.SetProvider(providers[idx%len(providers)])
		}(i)
	}

	wg.Wait()
}

// TestIntegrationCeilingAllTypesAfterRegister verifies that every known
// type returns the expected CE value after Register.
func TestIntegrationCeilingAllTypesAfterRegister(t *testing.T) {
	t.Cleanup(func() { pkgquota.SetProvider(nil) })

	Register()

	expected := map[pkgquota.Type]int{
		pkgquota.TypeDevice:                CeilingDevice,
		pkgquota.TypeHost:                  CeilingHost,
		pkgquota.TypeProber:                CeilingProber,
		pkgquota.TypeScheduledTask:         CeilingScheduledTask,
		pkgquota.TypeRemediation:           CeilingRemediation,
		pkgquota.TypeProbeInterval:         CeilingProbeIntervalSeconds,
		pkgquota.TypeScheduledTaskInterval: CeilingScheduledTaskIntervalSeconds,
		pkgquota.TypeDailyEvents:           CeilingDailyEvents,
	}

	for typ, want := range expected {
		if got := pkgquota.Ceiling(typ); got != want {
			t.Errorf("Ceiling(%s) = %d, want %d", typ, got, want)
		}
	}

	// Types not in the CE table should return 0.
	unknownTypes := []pkgquota.Type{
		pkgquota.TypeAsset,
		pkgquota.TypeTeamMember,
		pkgquota.TypeCustomField,
		pkgquota.TypeIngestionMetricTPS,
		pkgquota.TypeIngestionEventTPS,
		pkgquota.TypeConcurrentTasks,
		pkgquota.TypeAPIMinute,
		pkgquota.TypeAPIDaily,
		pkgquota.TypeAPIConcurrent,
	}
	for _, typ := range unknownTypes {
		if got := pkgquota.Ceiling(typ); got != 0 {
			t.Errorf("Ceiling(%s) = %d, want 0 (not configured in CE)", typ, got)
		}
	}
}
