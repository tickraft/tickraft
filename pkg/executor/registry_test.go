// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/tickraft/tickraft/pkg/types"
)

// mockExecutor is a test Executor that implements the NEW interface
// (Name, Capabilities, Execute) for registry testing.
type mockExecutor struct {
	name string
	caps Capability
}

func (m *mockExecutor) Name() string             { return m.name }
func (m *mockExecutor) Capabilities() Capability { return m.caps }
func (m *mockExecutor) Execute(_ context.Context, _ ExecutionRequest) (*Result, error) {
	return &Result{Status: types.AssetStatusNormal}, nil
}

// --- Register ---

// TestRegisterSuccess verifies that Register stores an executor under its
// Name() and that it can be looked up afterwards.
func TestRegisterSuccess(t *testing.T) {
	reg := NewRegistry()
	e := &mockExecutor{name: "tcp", caps: CapProbe}

	if err := reg.Register(e); err != nil {
		t.Fatalf("Register: unexpected error: %v", err)
	}

	got, err := reg.Lookup("tcp")
	if err != nil {
		t.Fatalf("Lookup: unexpected error: %v", err)
	}
	if got.Name() != "tcp" {
		t.Errorf("Lookup returned name %q, want %q", got.Name(), "tcp")
	}
}

// TestRegisterDuplicate verifies that registering two executors with the same
// name returns ErrExecutorAlreadyRegistered.
func TestRegisterDuplicate(t *testing.T) {
	reg := NewRegistry()
	e := &mockExecutor{name: "tcp", caps: CapProbe}

	if err := reg.Register(e); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	err := reg.Register(e)
	if !errors.Is(err, ErrExecutorAlreadyRegistered) {
		t.Fatalf("second Register: expected ErrExecutorAlreadyRegistered, got %v", err)
	}
}

// --- Lookup ---

// TestLookupNotFound verifies that Lookup returns ErrExecutorNotFound for an
// unknown name.
func TestLookupNotFound(t *testing.T) {
	reg := NewRegistry()

	_, err := reg.Lookup("nonexistent")
	if !errors.Is(err, ErrExecutorNotFound) {
		t.Fatalf("Lookup: expected ErrExecutorNotFound, got %v", err)
	}
}

// --- Names ---

// TestNames verifies that Names returns all registered executor names.
func TestNames(t *testing.T) {
	reg := NewRegistry()
	executors := []*mockExecutor{
		{name: "tcp", caps: CapProbe},
		{name: "ssh", caps: CapExec},
		{name: "webhook", caps: CapNotify},
	}
	for _, e := range executors {
		if err := reg.Register(e); err != nil {
			t.Fatalf("Register %q: %v", e.name, err)
		}
	}

	names := reg.Names()
	if len(names) != len(executors) {
		t.Fatalf("Names: got %d names, want %d", len(names), len(executors))
	}

	seen := make(map[string]bool, len(names))
	for _, n := range names {
		seen[n] = true
	}
	for _, e := range executors {
		if !seen[e.name] {
			t.Errorf("Names: missing %q in %v", e.name, names)
		}
	}
}

// --- Executors ---

// TestExecutors verifies that Executors returns all registered executors.
func TestExecutors(t *testing.T) {
	reg := NewRegistry()
	executors := []*mockExecutor{
		{name: "tcp", caps: CapProbe},
		{name: "ssh", caps: CapExec},
	}
	for _, e := range executors {
		if err := reg.Register(e); err != nil {
			t.Fatalf("Register %q: %v", e.name, err)
		}
	}

	got := reg.Executors()
	if len(got) != len(executors) {
		t.Fatalf("Executors: got %d, want %d", len(got), len(executors))
	}
}

// --- LookupWithOp ---

// TestLookupWithOpSuccess verifies that LookupWithOp returns the executor when
// its capabilities satisfy the operation's required capability.
func TestLookupWithOpSuccess(t *testing.T) {
	reg := NewRegistry()
	e := &mockExecutor{name: "tcp", caps: CapProbe}
	if err := reg.Register(e); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := reg.LookupWithOp("tcp", OpProbe)
	if err != nil {
		t.Fatalf("LookupWithOp(OpProbe): unexpected error: %v", err)
	}
	if got.Name() != "tcp" {
		t.Errorf("LookupWithOp returned name %q, want %q", got.Name(), "tcp")
	}
}

// TestLookupWithOpDualMode verifies that a dual-mode executor satisfies both
// OpProbe and OpExecute.
func TestLookupWithOpDualMode(t *testing.T) {
	reg := NewRegistry()
	e := &mockExecutor{name: "ssh", caps: CapProbe | CapExec}
	if err := reg.Register(e); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, err := reg.LookupWithOp("ssh", OpProbe); err != nil {
		t.Errorf("LookupWithOp(OpProbe): unexpected error: %v", err)
	}
	if _, err := reg.LookupWithOp("ssh", OpExecute); err != nil {
		t.Errorf("LookupWithOp(OpExecute): unexpected error: %v", err)
	}
}

// TestLookupWithOpNotFound verifies that LookupWithOp returns
// ErrExecutorNotFound when the executor is not registered.
func TestLookupWithOpNotFound(t *testing.T) {
	reg := NewRegistry()

	_, err := reg.LookupWithOp("nonexistent", OpProbe)
	if !errors.Is(err, ErrExecutorNotFound) {
		t.Fatalf("LookupWithOp: expected ErrExecutorNotFound, got %v", err)
	}
}

// TestLookupWithOpCapabilityNotSupported verifies that LookupWithOp returns
// ErrCapabilityNotSupported when the executor lacks the required capability.
func TestLookupWithOpCapabilityNotSupported(t *testing.T) {
	reg := NewRegistry()
	// CapProbe-only executor cannot serve OpExecute (requires CapWrite).
	e := &mockExecutor{name: "icmp", caps: CapProbe}
	if err := reg.Register(e); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err := reg.LookupWithOp("icmp", OpExecute)
	if !errors.Is(err, ErrCapabilityNotSupported) {
		t.Fatalf("LookupWithOp(OpExecute) on probe-only executor: expected ErrCapabilityNotSupported, got %v", err)
	}
}

// TestLookupWithOpExecuteOnWriteExecutor verifies that an executor with
// CapMutate satisfies OpExecute (which requires CapWrite).
func TestLookupWithOpExecuteOnWriteExecutor(t *testing.T) {
	reg := NewRegistry()
	e := &mockExecutor{name: "mysql", caps: CapMutate}
	if err := reg.Register(e); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, err := reg.LookupWithOp("mysql", OpExecute); err != nil {
		t.Errorf("LookupWithOp(OpExecute) on CapMutate executor: unexpected error: %v", err)
	}
}

// --- ByCapability ---

// TestByCapability verifies that ByCapability returns only executors that
// declare the given capability.
func TestByCapability(t *testing.T) {
	reg := NewRegistry()
	executors := []*mockExecutor{
		{name: "icmp", caps: CapProbe},
		{name: "tcp", caps: CapProbe},
		{name: "ssh", caps: CapExec},
		{name: "webhook", caps: CapNotify},
	}
	for _, e := range executors {
		if err := reg.Register(e); err != nil {
			t.Fatalf("Register %q: %v", e.name, err)
		}
	}

	probers := reg.ByCapability(CapProbe)
	if len(probers) != 2 {
		t.Errorf("ByCapability(CapProbe): got %d executors, want 2", len(probers))
	}

	names := make(map[string]bool, len(probers))
	for _, e := range probers {
		names[e.Name()] = true
	}
	if !names["icmp"] || !names["tcp"] {
		t.Errorf("ByCapability(CapProbe): expected icmp and tcp, got %v", names)
	}
}

// TestByCapabilityDualMode verifies that a dual-mode executor is returned for
// both probe and write capability queries.
func TestByCapabilityDualMode(t *testing.T) {
	reg := NewRegistry()
	e := &mockExecutor{name: "ssh", caps: CapProbe | CapExec}
	if err := reg.Register(e); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if got := reg.ByCapability(CapProbe); len(got) != 1 {
		t.Errorf("ByCapability(CapProbe): got %d, want 1", len(got))
	}
	if got := reg.ByCapability(CapExec); len(got) != 1 {
		t.Errorf("ByCapability(CapExec): got %d, want 1", len(got))
	}
}

// TestByCapabilityEmpty verifies that ByCapability returns an empty slice when
// no executor matches.
func TestByCapabilityEmpty(t *testing.T) {
	reg := NewRegistry()
	e := &mockExecutor{name: "icmp", caps: CapProbe}
	if err := reg.Register(e); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got := reg.ByCapability(CapExec)
	if len(got) != 0 {
		t.Errorf("ByCapability(CapExec) on probe-only registry: got %d, want 0", len(got))
	}
}

// --- NamesByCapability ---

// TestNamesByCapability verifies that NamesByCapability returns the names of
// executors that declare the given capability.
func TestNamesByCapability(t *testing.T) {
	reg := NewRegistry()
	executors := []*mockExecutor{
		{name: "icmp", caps: CapProbe},
		{name: "tcp", caps: CapProbe},
		{name: "ssh", caps: CapExec},
	}
	for _, e := range executors {
		if err := reg.Register(e); err != nil {
			t.Fatalf("Register %q: %v", e.name, err)
		}
	}

	names := reg.NamesByCapability(CapProbe)
	if len(names) != 2 {
		t.Fatalf("NamesByCapability(CapProbe): got %d, want 2", len(names))
	}

	seen := make(map[string]bool, len(names))
	for _, n := range names {
		seen[n] = true
	}
	if !seen["icmp"] || !seen["tcp"] {
		t.Errorf("NamesByCapability(CapProbe): expected icmp and tcp, got %v", names)
	}
}

// TestNamesByCapabilityEmpty verifies that NamesByCapability returns an empty
// slice when no executor matches.
func TestNamesByCapabilityEmpty(t *testing.T) {
	reg := NewRegistry()

	got := reg.NamesByCapability(CapProbe)
	if len(got) != 0 {
		t.Errorf("NamesByCapability on empty registry: got %d, want 0", len(got))
	}
}

// --- Unregister ---

// TestUnregisterSuccess verifies that Unregister removes an executor.
func TestUnregisterSuccess(t *testing.T) {
	reg := NewRegistry()
	e := &mockExecutor{name: "tcp", caps: CapProbe}
	if err := reg.Register(e); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := reg.Unregister("tcp"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}

	if _, err := reg.Lookup("tcp"); !errors.Is(err, ErrExecutorNotFound) {
		t.Fatalf("Lookup after Unregister: expected ErrExecutorNotFound, got %v", err)
	}
}

// TestUnregisterNotFound verifies that Unregister returns ErrExecutorNotFound
// for an unknown name.
func TestUnregisterNotFound(t *testing.T) {
	reg := NewRegistry()

	err := reg.Unregister("nonexistent")
	if !errors.Is(err, ErrExecutorNotFound) {
		t.Fatalf("Unregister: expected ErrExecutorNotFound, got %v", err)
	}
}
