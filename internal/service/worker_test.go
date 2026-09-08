// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package service

import (
	"context"
	"testing"

	"github.com/tickraft/tickraft/pkg/executor"
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

	probeRec := executor.ExecutionRecord{TaskID: -(telemetry.ProbeTaskIDOffset + 7), Operation: executor.OpProbe}
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
