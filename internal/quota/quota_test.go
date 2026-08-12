// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package quota

import (
	"testing"

	pkgquota "github.com/tickraft/tickraft/pkg/quota"
)

func TestDefaultProviderCeiling(t *testing.T) {
	p := DefaultProvider{}
	tests := []struct {
		name string
		typ  pkgquota.Type
		want int
	}{
		{"Device", pkgquota.TypeDevice, CeilingDevice},
		{"Host", pkgquota.TypeHost, CeilingHost},
		{"Prober", pkgquota.TypeProber, CeilingProber},
		{"ScheduledTask", pkgquota.TypeScheduledTask, CeilingScheduledTask},
		{"Remediation", pkgquota.TypeRemediation, CeilingRemediation},
		{"ProbeInterval", pkgquota.TypeProbeInterval, CeilingProbeIntervalSeconds},
		{"ScheduledTaskInterval", pkgquota.TypeScheduledTaskInterval, CeilingScheduledTaskIntervalSeconds},
		{"DailyEvents", pkgquota.TypeDailyEvents, CeilingDailyEvents},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.Ceiling(tt.typ); got != tt.want {
				t.Errorf("Ceiling(%s) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func TestDefaultProviderUnknownType(t *testing.T) {
	p := DefaultProvider{}
	if got := p.Ceiling(pkgquota.TypeTeamMember); got != 0 {
		t.Errorf("Ceiling(unknown) = %d, want 0", got)
	}
}

func TestDefaultProviderSpec(t *testing.T) {
	p := DefaultProvider{}

	s := p.Spec(pkgquota.TypeDevice)
	if s.Ceiling != CeilingDevice {
		t.Errorf("Spec(Device).Ceiling = %d, want %d", s.Ceiling, CeilingDevice)
	}
	if s.Layer != pkgquota.LayerAsset {
		t.Errorf("Spec(Device).Layer = %d, want LayerAsset", s.Layer)
	}
	if !s.Scalable {
		t.Errorf("Spec(Device).Scalable = false, want true")
	}

	s2 := p.Spec(pkgquota.TypeScheduledTask)
	if s2.Layer != pkgquota.LayerConfig {
		t.Errorf("Spec(ScheduledTask).Layer = %d, want LayerConfig", s2.Layer)
	}

	s3 := p.Spec(pkgquota.TypeDailyEvents)
	if s3.Layer != pkgquota.LayerRuntime {
		t.Errorf("Spec(DailyEvents).Layer = %d, want LayerRuntime", s3.Layer)
	}
}

func TestDefaultProviderImplementsInterfaces(t *testing.T) {
	var _ pkgquota.Provider = DefaultProvider{}
	var _ pkgquota.ProviderWithSpec = DefaultProvider{}
}

func TestRegisterInjectsProvider(t *testing.T) {
	t.Cleanup(func() { pkgquota.SetProvider(nil) })

	pkgquota.SetProvider(nil) // ensure zero state
	Register()

	if got := pkgquota.Ceiling(pkgquota.TypeDevice); got != CeilingDevice {
		t.Errorf("after Register, Ceiling(Device) = %d, want %d", got, CeilingDevice)
	}
	if got := pkgquota.Ceiling(pkgquota.TypeRemediation); got != CeilingRemediation {
		t.Errorf("after Register, Ceiling(Remediation) = %d, want %d", got, CeilingRemediation)
	}
}
