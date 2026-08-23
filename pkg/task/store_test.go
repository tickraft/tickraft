// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/types"
)

// openTaskStoreDB opens an in-memory SQLite database with the task tables
// migrated, backed by the same db.Open path production uses.
func openTaskStoreDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbc, err := db.Open(context.Background(), db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := dbc.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := Migrate(context.Background(), dbc); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return dbc
}

// TestStoreSavePersistsAllColumns is a regression test for the converter-era
// bug where taskToModel never assigned max_retries/retry_interval, so every
// Save wrote 0 and the runner's retry config had no data source. The direct
// model must persist every wire-visible and internal column verbatim.
func TestStoreSavePersistsAllColumns(t *testing.T) {
	dbc := openTaskStoreDB(t)
	store := NewStore(dbc)

	created := time.Now().Add(-time.Minute)
	tk := &Task{
		ID:                   7,
		TenantID:             1,
		AssetID:              2,
		Name:                 "full-row",
		Description:          "desc",
		ExecutorType:         "http",
		Schedule:             "*/5 * * * *",
		Enabled:              true,
		Config:               map[string]any{"url": "http://example.com"},
		TimeoutSeconds:       30,
		MaxRetries:           3,
		RetryIntervalSeconds: 60,
		Priority:             5,
		DependsOn:            9,
		Metadata:             map[string]string{"monitor_point_id": "11"},
		Group:                "prober",
		Tags:                 []string{"critical", "nightly"},
		RunID:                "run-1",
		Concurrency:          1,
		CreatedAt:            created,
	}
	if err := store.Save(context.Background(), tk); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := store.Get(context.Background(), 7)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.MaxRetries != 3 || got.RetryIntervalSeconds != 60 {
		t.Errorf("retry columns = %d/%d, want 3/60 (converter wrote 0)",
			got.MaxRetries, got.RetryIntervalSeconds)
	}
	if got.Description != "desc" || got.Group != "prober" {
		t.Errorf("description/group = %q/%q, want desc/prober", got.Description, got.Group)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "critical" || got.Tags[1] != "nightly" {
		t.Errorf("tags = %v, want [critical nightly]", got.Tags)
	}
	if got.Priority != 5 || got.DependsOn != 9 {
		t.Errorf("priority/depends_on = %d/%d, want 5/9", got.Priority, got.DependsOn)
	}
	if got.Metadata["monitor_point_id"] != "11" {
		t.Errorf("metadata = %v, want monitor_point_id=11", got.Metadata)
	}
	if got.TimeoutSeconds != 30 {
		t.Errorf("timeout = %d, want 30", got.TimeoutSeconds)
	}
}

// TestStoreUpsertPreservesUnlistedColumns verifies the upsert column
// whitelist semantics: saving an updated row keeps created_at intact (the
// column is not in the update list) while enabled/schedule updates land.
func TestStoreUpsertPreservesUnlistedColumns(t *testing.T) {
	dbc := openTaskStoreDB(t)
	store := NewStore(dbc)

	created := time.Now().Add(-time.Hour).Truncate(time.Second)
	tk := &Task{
		ID:                   1,
		ExecutorType:         "http",
		Schedule:             "*/5 * * * *",
		Enabled:              true,
		MaxRetries:           3,
		RetryIntervalSeconds: 60,
		CreatedAt:            created,
	}
	if err := store.Save(context.Background(), tk); err != nil {
		t.Fatalf("save (insert): %v", err)
	}

	// Simulate a Pause: same row with Enabled=false and a later CreatedAt
	// that must be ignored by the upsert.
	paused := *tk
	paused.Enabled = false
	paused.CreatedAt = created.Add(time.Second)
	if err := store.Save(context.Background(), &paused); err != nil {
		t.Fatalf("save (update): %v", err)
	}

	got, err := store.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Enabled {
		t.Error("enabled = true after disabled upsert, want false")
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("created_at = %v, want preserved %v", got.CreatedAt, created)
	}
	if got.MaxRetries != 3 || got.RetryIntervalSeconds != 60 {
		t.Errorf("retry columns = %d/%d, want 3/60 after upsert", got.MaxRetries, got.RetryIntervalSeconds)
	}
}

// TestExecutionRecordStoreMapsAssetStatus verifies the vocabulary bridge: the
// adapter is the single place asset status values (normal/abnormal/...) are
// translated to the stored success/failed/unknown vocabulary.
func TestExecutionRecordStoreMapsAssetStatus(t *testing.T) {
	dbc := openTaskStoreDB(t)
	execStore := NewExecutionStore(dbc)
	recordStore := NewExecutionRecordStore(execStore)

	cases := map[string]string{
		"normal":   StatusSuccess,
		"abnormal": StatusFailed,
		"offline":  StatusUnknown,
	}
	taskID := int64(0)
	for assetStatus, wantStored := range cases {
		taskID++
		now := time.Now()
		record := executor.ExecutionRecord{
			TaskID:       taskID,
			TenantID:     1,
			ExecutorName: "http",
			Status:       types.AssetStatus(assetStatus),
			Duration:     1500 * time.Millisecond,
			StartedAt:    now.Add(-time.Second),
			FinishedAt:   now,
		}
		if err := recordStore.Save(context.Background(), record); err != nil {
			t.Fatalf("save %s: %v", assetStatus, err)
		}
		items, total, err := execStore.Query(context.Background(), ExecutionQuery{TaskID: taskID}, 1, 10)
		if err != nil {
			t.Fatalf("query %s: %v", assetStatus, err)
		}
		if total != 1 || len(items) != 1 {
			t.Fatalf("asset status %q: query returned total=%d len=%d, want 1/1", assetStatus, total, len(items))
		}
		got := items[0]
		if got.Status != wantStored {
			t.Errorf("asset status %q stored as %q, want %q", assetStatus, got.Status, wantStored)
		}
		if got.Duration != 1500 {
			t.Errorf("duration = %d ms, want 1500 (ns→ms conversion)", got.Duration)
		}
		if got.FinishedAt == nil {
			t.Errorf("asset status %q: finished_at is nil, want set", assetStatus)
		}
	}
}

// TestExecutionRecordStoreTimedOutOverridesStatus verifies that a deadline
// expiry is persisted as the distinct "timeout" state even though the
// executor reported the failure in the asset vocabulary (abnormal).
func TestExecutionRecordStoreTimedOutOverridesStatus(t *testing.T) {
	dbc := openTaskStoreDB(t)
	execStore := NewExecutionStore(dbc)
	recordStore := NewExecutionRecordStore(execStore)

	now := time.Now()
	record := executor.ExecutionRecord{
		TaskID:       1,
		TenantID:     1,
		ExecutorName: "tcp",
		Status:       types.AssetStatusAbnormal,
		ErrorMsg:     "context deadline exceeded",
		Duration:     5 * time.Second,
		StartedAt:    now.Add(-5 * time.Second),
		FinishedAt:   now,
		TimedOut:     true,
	}
	if err := recordStore.Save(context.Background(), record); err != nil {
		t.Fatalf("save: %v", err)
	}
	items, total, err := execStore.Query(context.Background(), ExecutionQuery{TaskID: 1}, 1, 10)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("query returned total=%d len=%d, want 1/1", total, len(items))
	}
	if items[0].Status != StatusTimeout {
		t.Errorf("status = %q, want %q", items[0].Status, StatusTimeout)
	}
}
