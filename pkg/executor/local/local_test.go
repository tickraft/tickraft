// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package local

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/types"
)

// TestName verifies the name identifier.
func TestName(t *testing.T) {
	e := New()
	if got := e.Name(); got != "local" {
		t.Errorf("Name(): got %q, want %q", got, "local")
	}
}

// TestCapabilities verifies the capability bitmask.
func TestCapabilities(t *testing.T) {
	e := New()
	if got := e.Capabilities(); got != executor.CapExec {
		t.Errorf("Capabilities(): got %v, want %v", got, executor.CapExec)
	}
}

// TestExecuteSuccess verifies that Execute runs a simple command and
// returns the stdout in the result body.
func TestExecuteSuccess(t *testing.T) {
	e := New()
	cfg := config{
		Command: "echo",
		Args:    []string{"hello"},
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	req := executor.ExecutionRequest{
		Config:  string(cfgBytes),
		AssetID: 1,
	}
	result, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: got err %v, want nil", err)
	}
	if result.Status != types.AssetStatusNormal {
		t.Errorf("Status: got %q, want %q", result.Status, types.AssetStatusNormal)
	}
	if result.Body != "hello\n" {
		t.Errorf("Body: got %q, want %q", result.Body, "hello\n")
	}
}

// TestExecuteEmptyConfig verifies that Execute returns an error when the
// executor config is empty.
func TestExecuteEmptyConfig(t *testing.T) {
	e := New()
	req := executor.ExecutionRequest{}
	_, err := e.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("Execute: got nil err, want error for empty config")
	}
}

// TestExecuteMissingCommand verifies that Execute returns an error when
// the command field is missing.
func TestExecuteMissingCommand(t *testing.T) {
	e := New()
	req := executor.ExecutionRequest{
		Config: `{}`,
	}
	_, err := e.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("Execute: got nil err, want error for missing command")
	}
}

// TestExecuteNonZeroExit verifies that a non-zero exit code yields
// StatusAbnormal without returning a Go error.
func TestExecuteNonZeroExit(t *testing.T) {
	e := New()
	cfg := config{
		Command: "sh",
		Args:    []string{"-c", "echo to-stderr 1>&2; exit 1"},
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	req := executor.ExecutionRequest{
		Config: string(cfgBytes),
	}
	result, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: got err %v, want nil (non-zero exit is not an error)", err)
	}
	if result.Status != types.AssetStatusAbnormal {
		t.Errorf("Status: got %q, want %q", result.Status, types.AssetStatusAbnormal)
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode: got %d, want 1", result.ExitCode)
	}
}

// TestExecuteExitCodePropagation pins the unified result code contract:
// success records exit code 0, a specific non-zero exit is preserved, and
// a missing command yields -1 (no exit code available).
func TestExecuteExitCodePropagation(t *testing.T) {
	e := New()

	// Success: exit code 0.
	cfgBytes, _ := json.Marshal(config{Command: "true"})
	res, err := e.Execute(context.Background(), executor.ExecutionRequest{Config: string(cfgBytes)})
	if err != nil {
		t.Fatalf("execute true: %v", err)
	}
	if res.Status != types.AssetStatusNormal || res.ExitCode != 0 {
		t.Errorf("true: Status=%q ExitCode=%d, want normal/0", res.Status, res.ExitCode)
	}

	// Specific non-zero exit code is preserved.
	cfgBytes, _ = json.Marshal(config{Command: "sh", Args: []string{"-c", "exit 7"}})
	res, err = e.Execute(context.Background(), executor.ExecutionRequest{Config: string(cfgBytes)})
	if err != nil {
		t.Fatalf("execute exit 7: %v", err)
	}
	if res.Status != types.AssetStatusAbnormal || res.ExitCode != 7 {
		t.Errorf("exit 7: Status=%q ExitCode=%d, want abnormal/7", res.Status, res.ExitCode)
	}

	// Command not found: no exit code available, recorded as -1.
	cfgBytes, _ = json.Marshal(config{Command: "no-such-binary-tickraft-test"})
	res, err = e.Execute(context.Background(), executor.ExecutionRequest{Config: string(cfgBytes)})
	if err != nil {
		t.Fatalf("execute missing binary: %v", err)
	}
	if res.Status != types.AssetStatusAbnormal || res.ExitCode != -1 {
		t.Errorf("missing binary: Status=%q ExitCode=%d, want abnormal/-1", res.Status, res.ExitCode)
	}
}
