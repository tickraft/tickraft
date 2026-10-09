// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package tcp

import (
	"context"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/types"
)

func TestName(t *testing.T) {
	p := New(5 * time.Second)
	if got := p.Name(); got != "tcp" {
		t.Errorf("Name: got %q, want %q", got, "tcp")
	}
}

func TestCapabilities(t *testing.T) {
	p := New(5 * time.Second)
	if got := p.Capabilities(); got != executor.CapProbe {
		t.Errorf("Capabilities: got %v, want %v", got, executor.CapProbe)
	}
}

// TestExecuteMissingPort verifies that Execute returns an abnormal result
// when the executor config does not carry a port.
func TestExecuteMissingPort(t *testing.T) {
	p := New(5 * time.Second)
	result, err := p.Execute(context.Background(), executor.ExecutionRequest{
		ExecutorName: "tcp",
		Config:       `{"address":"127.0.0.1"}`,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != types.AssetStatusAbnormal {
		t.Errorf("Status: got %q, want %q", result.Status, types.AssetStatusAbnormal)
	}
}

// TestExecuteInvalidConfig verifies that Execute returns a parse error
// when the executor config is not valid JSON.
func TestExecuteInvalidConfig(t *testing.T) {
	p := New(5 * time.Second)
	_, err := p.Execute(context.Background(), executor.ExecutionRequest{
		ExecutorName: "tcp",
		Config:       `{invalid`,
	})
	if err == nil {
		t.Fatal("Execute with invalid config: expected error, got nil")
	}
}

// TestCEAvailability_RegistrationInRegistry verifies that the TCP prober is
// available by registering it with the executor Registry and confirming it
// can be looked up. This guards against the prober being moved behind a
// build-tag-restricted file in the future: if that happens, this test file
// (which is itself tag-free) would no longer compile against the registration
// path and the test would fail.
func TestCEAvailability_RegistrationInRegistry(t *testing.T) {
	p := New(5 * time.Second)
	reg := executor.NewRegistry()
	if err := reg.Register(p); err != nil {
		t.Fatalf("Register tcp prober: %v", err)
	}

	got, err := reg.Lookup("tcp")
	if err != nil {
		t.Fatalf("Lookup tcp: %v", err)
	}
	if got.Name() != "tcp" {
		t.Errorf("registered Name = %q, want %q", got.Name(), "tcp")
	}
	if got.Capabilities() != executor.CapProbe {
		t.Errorf("registered Capabilities = %v, want %v", got.Capabilities(), executor.CapProbe)
	}

	// The default prober list must include tcp so workers running without
	// any extended build tags can still perform TCP port reachability checks.
	names := reg.NamesByCapability(executor.CapProbe)
	found := false
	for _, n := range names {
		if n == "tcp" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("tcp not in prober names %v; expected default prober set to include tcp", names)
	}
}
