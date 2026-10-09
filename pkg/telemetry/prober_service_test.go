// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

package telemetry

import (
	"testing"

	"github.com/tickraft/tickraft/pkg/executor"
)

// TestPointToProbeTaskJudgmentTransmission pins the execution-judgment
// transmission chain: the optional
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
		Config:   map[string]any{"target": "192.0.2.1", "expression": "code == 200 && duration < 500"},
	}

	task := pointToProbeTask(point)
	if got := task.Metadata["expression"]; got != "code == 200 && duration < 500" {
		t.Errorf("task metadata expression = %q, want the point config expression", got)
	}

	// Without the key, no expression metadata is set.
	point.Config = map[string]any{"target": "192.0.2.1"}
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
		Config: map[string]any{"target": "192.0.2.1"},
	}
	task := pointToProbeTask(point)
	if task.ID != proberTaskID(3) {
		t.Errorf("task ID = %d, want %d", task.ID, proberTaskID(3))
	}
	// Probe task IDs live in the negative synthetic range so persisting them
	// can never push sys_schedule_task's auto-increment counter into the
	// probe range (the legacy positive-scheme poisoning).
	if task.ID >= 0 {
		t.Errorf("task ID = %d, want negative synthetic ID", task.ID)
	}
	if task.ExecutorType != "icmp" {
		t.Errorf("task ExecutorType = %q, want icmp", task.ExecutorType)
	}
	if task.Operation != executor.OpProbe {
		t.Errorf("task Operation = %v, want OpProbe", task.Operation)
	}
	if got, _ := task.Config["target"].(string); got != "192.0.2.1" {
		t.Errorf("task Config[target] = %q, want the point config target", got)
	}
	if task.Name != "prober-3" || task.Group != "prober" || !task.Enabled {
		t.Errorf("task identity fields = %q/%q/%v, want prober-3/prober/true", task.Name, task.Group, task.Enabled)
	}
	// An empty Schedule falls back to the interval field, defaulted to 60s.
	if task.Schedule != "1m0s" {
		t.Errorf("task Schedule = %q, want 1m0s", task.Schedule)
	}
	if task.TimeoutSeconds != 10 {
		t.Errorf("task TimeoutSeconds = %d, want default 10", task.TimeoutSeconds)
	}
}
