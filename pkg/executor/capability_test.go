// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package executor

import "testing"

func TestCapabilityConstants(t *testing.T) {
	tests := []struct {
		name string
		got  Capability
		want Capability
	}{
		{"CapProbe", CapProbe, 1},
		{"CapExec", CapExec, 2},
		{"CapMutate", CapMutate, 4},
		{"CapNotify", CapNotify, 8},
		{"CapWrite", CapWrite, 14},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestCapabilityHasCap(t *testing.T) {
	tests := []struct {
		name         string
		capabilities Capability
		cap          Capability
		want         bool
	}{
		{"probe has probe", CapProbe, CapProbe, true},
		{"probe has exec", CapProbe, CapExec, false},
		{"probe|exec has probe", CapProbe | CapExec, CapProbe, true},
		{"probe|exec has exec", CapProbe | CapExec, CapExec, true},
		{"probe|exec has mutate", CapProbe | CapExec, CapMutate, false},
		{"all has probe", CapProbe | CapExec | CapMutate | CapNotify, CapProbe, true},
		{"all has notify", CapProbe | CapExec | CapMutate | CapNotify, CapNotify, true},
		{"zero has probe", 0, CapProbe, false},
		{"zero has exec", 0, CapExec, false},
		{"write has exec", CapWrite, CapExec, true},
		{"write has mutate", CapWrite, CapMutate, true},
		{"write has notify", CapWrite, CapNotify, true},
		{"write has probe", CapWrite, CapProbe, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasCap(tt.capabilities, tt.cap); got != tt.want {
				t.Errorf("HasCap(%d, %d) = %v, want %v", tt.capabilities, tt.cap, got, tt.want)
			}
		})
	}
}

func TestCapabilityHasWrite(t *testing.T) {
	tests := []struct {
		name         string
		capabilities Capability
		want         bool
	}{
		{"probe only", CapProbe, false},
		{"exec only", CapExec, true},
		{"mutate only", CapMutate, true},
		{"notify only", CapNotify, true},
		{"probe|exec dual", CapProbe | CapExec, true},
		{"probe|mutate dual", CapProbe | CapMutate, true},
		{"probe|notify dual", CapProbe | CapNotify, true},
		{"all", CapProbe | CapExec | CapMutate | CapNotify, true},
		{"zero", 0, false},
		{"capwrite aggregate", CapWrite, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasWrite(tt.capabilities); got != tt.want {
				t.Errorf("HasWrite(%d) = %v, want %v", tt.capabilities, got, tt.want)
			}
		})
	}
}

func TestCapabilityIsDualMode(t *testing.T) {
	tests := []struct {
		name         string
		capabilities Capability
		want         bool
	}{
		{"probe only", CapProbe, false},
		{"exec only", CapExec, false},
		{"mutate only", CapMutate, false},
		{"notify only", CapNotify, false},
		{"probe|exec", CapProbe | CapExec, true},
		{"probe|mutate", CapProbe | CapMutate, true},
		{"probe|notify", CapProbe | CapNotify, true},
		{"probe|exec|mutate|notify", CapProbe | CapExec | CapMutate | CapNotify, true},
		{"exec|mutate (no probe)", CapExec | CapMutate, false},
		{"zero", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDualMode(tt.capabilities); got != tt.want {
				t.Errorf("IsDualMode(%d) = %v, want %v", tt.capabilities, got, tt.want)
			}
		})
	}
}
