// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

package telemetry

import (
	"testing"

	"github.com/tickraft/tickraft/pkg/executor"
)

// TestPointToProbeTaskJudgmentTransmission pins the execution-judgment
// transmission chain (rule-engine-design §6.3.3): the optional
// "expression" key of a monitoring point's config JSON is copied into the
// probe task's metadata, from where the trigger event carries it to the
// runner.
func TestPointToProbeTaskJudgmentTransmission(t *testing.T) {
	point := MonitorPoint{
		ID:       7,
		TenantID: 1,
		AssetID:  42,
		Mode:     ModeActive,
		Type:     "http",
		Interval: 60,
		Enabled:  true,
		Config:   `{"target":"192.0.2.1","expression":"code == 200 && duration < 500"}`,
	}

	task := pointToProbeTask(point)
	if got := task.Metadata["expression"]; got != "code == 200 && duration < 500" {
		t.Errorf("task metadata expression = %q, want the point config expression", got)
	}

	// Without the key, no expression metadata is set.
	point.Config = `{"target":"192.0.2.1"}`
	task = pointToProbeTask(point)
	if got := task.Metadata["expression"]; got != "" {
		t.Errorf("task metadata expression = %q, want empty for config without the key", got)
	}
}

// TestPointToProbeTaskBaseline pins the non-judgment task mapping: id
// offset, executor name from the point type, probe operation, and config
// passthrough.
func TestPointToProbeTaskBaseline(t *testing.T) {
	point := MonitorPoint{
		ID:     3,
		Mode:   ModeActive,
		Type:   "icmp",
		Config: `{"target":"192.0.2.1"}`,
	}
	task := pointToProbeTask(point)
	if task.ID != proberTaskID(3) {
		t.Errorf("task ID = %d, want %d", task.ID, proberTaskID(3))
	}
	if task.ExecutorName != "icmp" {
		t.Errorf("task ExecutorName = %q, want icmp", task.ExecutorName)
	}
	if task.Operation != executor.OpProbe {
		t.Errorf("task Operation = %v, want OpProbe", task.Operation)
	}
	if task.Config != point.Config {
		t.Errorf("task Config = %q, want passthrough %q", task.Config, point.Config)
	}
}
