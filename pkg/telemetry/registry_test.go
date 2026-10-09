// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"testing"

	"github.com/tickraft/tickraft/pkg/types"
)

// mockProcessor implements Processor for testing.
type mockProcessor struct {
	typeVal types.AssetType
}

func (m *mockProcessor) Type() types.AssetType { return m.typeVal }
func (m *mockProcessor) Process(_ context.Context, _ *Telemetry) (*ProcessResult, error) {
	return &ProcessResult{}, nil
}
func (m *mockProcessor) OnTimeout(_ context.Context, _ int64) error { return nil }

// --- ProcessorRegistry tests ---

func TestProcessorRegistryRegisterAndLookup(t *testing.T) {
	reg := NewProcessorRegistry()
	proc := &mockProcessor{typeVal: types.AssetTypeHost}

	if err := reg.Register(proc); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	got, err := reg.Lookup(types.AssetTypeHost)
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}
	if got.Type() != types.AssetTypeHost {
		t.Errorf("Lookup type: got %q, want %q", got.Type(), types.AssetTypeHost)
	}
}

func TestProcessorRegistryDuplicateRegistration(t *testing.T) {
	reg := NewProcessorRegistry()
	p1 := &mockProcessor{typeVal: types.AssetTypeDevice}
	p2 := &mockProcessor{typeVal: types.AssetTypeDevice}

	if err := reg.Register(p1); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}
	if err := reg.Register(p2); err == nil {
		t.Error("duplicate Register should return error")
	}
}

func TestProcessorRegistryLookupNotFound(t *testing.T) {
	reg := NewProcessorRegistry()

	_, err := reg.Lookup(types.AssetTypeWebsite)
	if err == nil {
		t.Error("Lookup for unregistered processor should return error")
	}
}
