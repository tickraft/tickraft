// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package icmp

import (
	"context"
	"testing"

	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/types"
)

func TestName(t *testing.T) {
	p := New(0)
	if got := p.Name(); got != "icmp" {
		t.Errorf("Name: got %q, want %q", got, "icmp")
	}
}

func TestCapabilities(t *testing.T) {
	p := New(0)
	if got := p.Capabilities(); got != executor.CapProbe {
		t.Errorf("Capabilities: got %v, want %v", got, executor.CapProbe)
	}
}

// TestExecuteMissingAddress verifies that Execute returns an abnormal
// result when the executor config does not carry an address.
func TestExecuteMissingAddress(t *testing.T) {
	p := New(0)
	result, err := p.Execute(context.Background(), executor.ExecutionRequest{
		ExecutorName: "icmp",
		Config:       `{}`,
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
	p := New(0)
	_, err := p.Execute(context.Background(), executor.ExecutionRequest{
		ExecutorName: "icmp",
		Config:       `{invalid`,
	})
	if err == nil {
		t.Fatal("Execute with invalid config: expected error, got nil")
	}
}
