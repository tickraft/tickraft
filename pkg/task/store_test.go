// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
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

// TestExecutionStatsFilterAcrossTimezones is a regression test for the
// offset-mixing trap: created_at rows are stored as offset-carrying TEXT and
// SQLite compares TEXT lexicographically, so naive bound comparisons only
// work when the query bound carries the same offset as the stored rows.
// Stats and StatsByDay must resolve both sides to instants via datetime().
func TestExecutionStatsFilterAcrossTimezones(t *testing.T) {
	dbc := openTaskStoreDB(t)
	store := NewExecutionStore(dbc)
	ctx := context.Background()

	cst := time.FixedZone("CST", 8*3600)
	rowTime := time.Date(2026, 9, 8, 20, 19, 15, 0, cst) // +08:00 evening row
	rows := []Execution{
		{
			TaskID: 1, AssetID: 1, ExecutorType: "http", Status: StatusSuccess,
			Duration: 100, StartedAt: rowTime, CreatedAt: rowTime,
		},
		{
			TaskID: 1, AssetID: 1, ExecutorType: "http", Status: StatusFailed,
			Duration: 200, StartedAt: rowTime.Add(time.Minute), CreatedAt: rowTime.Add(time.Minute),
		},
	}
	for i := range rows {
		if err := dbc.WithContext(ctx).Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed execution %d: %v", i, err)
		}
	}

	// Same day expressed with UTC bounds. The end bound's hour ("13")
	// sorts before the rows' hour ("20"), so a naive lexicographic
	// comparison excludes the rows even though 13:00Z is 21:00+08 —
	// instant-wise after them. This is the exact shape that zeroed the
	// dashboard's today-executions card.
	from := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 8, 13, 0, 0, 0, time.UTC)

	stats, err := store.Stats(ctx, from, to, 0)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.TotalExecutions != 2 || stats.SuccessCount != 1 || stats.FailureCount != 1 {
		t.Errorf("stats = total %d / success %d / failure %d, want 2/1/1 (offset-mixing regression)",
			stats.TotalExecutions, stats.SuccessCount, stats.FailureCount)
	}

	byDay, err := store.StatsByDay(ctx, from, to, 0)
	if err != nil {
		t.Fatalf("stats by day: %v", err)
	}
	if len(byDay) != 1 || byDay[0].Total != 2 || byDay[0].Success != 1 || byDay[0].Failed != 1 {
		t.Errorf("stats by day = %+v, want one day with total 2 / success 1 / failed 1", byDay)
	}

	// A UTC window that ends before the rows' instant must exclude them.
	earlyTo := time.Date(2026, 9, 8, 11, 0, 0, 0, time.UTC)
	stats, err = store.Stats(ctx, from, earlyTo, 0)
	if err != nil {
		t.Fatalf("stats early window: %v", err)
	}
	if stats.TotalExecutions != 0 {
		t.Errorf("early-window stats total = %d, want 0", stats.TotalExecutions)
	}
}

// TestStoreListExcludeSynthetic is a regression test for the user-facing task
// list leaking telemetry probe rows: synthetic rows persisted with negative
// IDs by other domains must be dropped when ExcludeSynthetic is set and must
// stay invisible to Count (quota), while internal callers still see them.
func TestStoreListExcludeSynthetic(t *testing.T) {
	dbc := openTaskStoreDB(t)
	store := NewStore(dbc)
	ctx := context.Background()

	rows := []*Task{
		{ID: 3, Name: "user-task", ExecutorType: "local", Schedule: "@every 1h", Enabled: true},
		{ID: -(1 << 40), Name: "prober-synthetic", ExecutorType: "http", Schedule: "@every 1m", Enabled: true},
	}
	for _, r := range rows {
		if err := store.Save(ctx, r); err != nil {
			t.Fatalf("save %+v: %v", r, err)
		}
	}

	all, err := store.List(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("internal list returned %d rows, want 2", len(all))
	}

	userRows, err := store.List(ctx, ListOptions{ExcludeSynthetic: true})
	if err != nil {
		t.Fatalf("list exclude synthetic: %v", err)
	}
	if len(userRows) != 1 || userRows[0].ID != 3 {
		t.Errorf("ExcludeSynthetic list = %+v, want only user task id 3", userRows)
	}

	count, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (synthetic rows must not consume quota)", count)
	}
}
