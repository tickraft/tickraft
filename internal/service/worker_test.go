// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package service

import (
	"context"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/task"
	"github.com/tickraft/tickraft/pkg/telemetry"
)

// captureRecordStore records every Save call so routing tests can assert
// which domain store received a record.
type captureRecordStore struct {
	saved []executor.ExecutionRecord
}

func (s *captureRecordStore) Save(_ context.Context, record executor.ExecutionRecord) error {
	s.saved = append(s.saved, record)
	return nil
}

// TestRoutingRecordStoreDispatchesByOperation pins the assembly-layer
// domain routing: OpProbe records land in the telemetry probe store,
// everything else in the task execution log.
func TestRoutingRecordStoreDispatchesByOperation(t *testing.T) {
	tasks := &captureRecordStore{}
	probes := &captureRecordStore{}
	router := routingRecordStore{tasks: tasks, probes: probes}

	probeRec := executor.ExecutionRecord{TaskID: telemetry.ProbeTaskIDOffset + 7, Operation: executor.OpProbe}
	if err := router.Save(context.Background(), probeRec); err != nil {
		t.Fatalf("route probe record: %v", err)
	}
	executeRec := executor.ExecutionRecord{TaskID: 12, Operation: executor.OpExecute}
	if err := router.Save(context.Background(), executeRec); err != nil {
		t.Fatalf("route execute record: %v", err)
	}

	if len(probes.saved) != 1 || probes.saved[0].TaskID != probeRec.TaskID {
		t.Errorf("probe store received %v, want only task %d", probes.saved, probeRec.TaskID)
	}
	if len(tasks.saved) != 1 || tasks.saved[0].TaskID != executeRec.TaskID {
		t.Errorf("task store received %v, want only task %d", tasks.saved, executeRec.TaskID)
	}
}

// TestCleanupLegacyProbeRows verifies the one-time migration sweep: probe
// rows (task_id at or above the probe offset) are removed from the task
// execution log while task-domain rows survive, and a second run is a no-op.
func TestCleanupLegacyProbeRows(t *testing.T) {
	dbc, err := db.Open(context.Background(), db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := dbc.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := task.Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now()
	rows := []task.Execution{
		{TaskID: 42, ExecutorType: "http", Status: "success", StartedAt: now},
		{TaskID: telemetry.ProbeTaskIDOffset + 7, ExecutorType: "icmp", Status: "success", StartedAt: now},
		{TaskID: telemetry.ProbeTaskIDOffset, ExecutorType: "tcp", Status: "success", StartedAt: now},
	}
	for i := range rows {
		if err := dbc.Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed execution row: %v", err)
		}
	}

	if err := cleanupLegacyProbeRows(context.Background(), dbc); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	var remaining []task.Execution
	if err := dbc.Order("task_id").Find(&remaining).Error; err != nil {
		t.Fatalf("load remaining: %v", err)
	}
	if len(remaining) != 1 || remaining[0].TaskID != 42 {
		t.Fatalf("remaining rows = %v, want only task 42", remaining)
	}

	// Idempotent: a second sweep keeps the same state.
	if err := cleanupLegacyProbeRows(context.Background(), dbc); err != nil {
		t.Fatalf("second cleanup: %v", err)
	}
	if err := dbc.Find(&remaining).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("rows after second cleanup = %d, want 1", len(remaining))
	}
}
