// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/types"
)

// openProbeRecordDB opens an in-memory SQLite database with the monitor
// point and probe record tables migrated.
func openProbeRecordDB(t *testing.T) *probeFixture {
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
		t.Fatalf("migrate monitor points: %v", err)
	}
	store := NewProbeRecordStore(dbc)
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate probe records: %v", err)
	}
	return &probeFixture{t: t, dbc: dbc, store: store}
}

type probeFixture struct {
	t     *testing.T
	dbc   *gorm.DB
	store *ProbeRecordStore
}

// seedPoint inserts a monitor point row and returns its ID.
func (f *probeFixture) seedPoint(id int64, status string) {
	f.t.Helper()
	point := &MonitorPoint{
		ID:       id,
		Name:     "point-" + status,
		Mode:     ModeActive,
		Type:     "icmp",
		Schedule: "60s",
		Enabled:  true,
		Status:   status,
		Interval: 60,
		Timeout:  30,
	}
	if err := f.dbc.Create(point).Error; err != nil {
		f.t.Fatalf("seed monitor point: %v", err)
	}
}

// saveRecord persists an execution record through the store under test.
func (f *probeFixture) saveRecord(rec executor.ExecutionRecord) {
	f.t.Helper()
	if err := f.store.Save(context.Background(), rec); err != nil {
		f.t.Fatalf("save probe record: %v", err)
	}
}

func probeRecordFor(pointID int64, status types.AssetStatus, startedAt time.Time) executor.ExecutionRecord {
	return executor.ExecutionRecord{
		TaskID:       proberTaskID(pointID),
		AssetID:      42,
		ExecutorName: "icmp",
		Operation:    executor.OpProbe,
		Status:       status,
		StatusCode:   200,
		Output:       "pong",
		Duration:     1500 * time.Millisecond,
		RetryCount:   1,
		RunID:        "run-1",
		TriggerType:  "schedule",
		StartedAt:    startedAt,
	}
}

// TestProbeRecordSaveDecodesPointID verifies the core mapping contract: the
// synthetic prober task ID is decoded back to the monitor point ID, the
// full result envelope lands in the row, and the nanosecond duration is
// converted to milliseconds.
func TestProbeRecordSaveDecodesPointID(t *testing.T) {
	f := openProbeRecordDB(t)
	f.seedPoint(7, MonitorStatusPending)

	started := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	f.saveRecord(probeRecordFor(7, types.AssetStatusNormal, started))

	recs, total, err := f.store.QueryByPoint(context.Background(), ProbeQuery{PointID: 7})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 1 || len(recs) != 1 {
		t.Fatalf("records = %d/%d, want 1/1", len(recs), total)
	}
	got := recs[0]
	if got.PointID != 7 {
		t.Errorf("point ID = %d, want 7", got.PointID)
	}
	if got.ExecutorType != "icmp" || got.Status != types.AssetStatusNormal {
		t.Errorf("executor/status = %q/%q, want icmp/normal", got.ExecutorType, got.Status)
	}
	if got.Output != "pong" || got.StatusCode != http.StatusOK || got.Error != "" {
		t.Errorf("output/code/error = %q/%d/%q, want pong/200/empty", got.Output, got.StatusCode, got.Error)
	}
	if got.Duration != 1500 {
		t.Errorf("duration = %d ms, want 1500", got.Duration)
	}
	if got.RetryCount != 1 || got.RunID != "run-1" {
		t.Errorf("retry/run = %d/%q, want 1/run-1", got.RetryCount, got.RunID)
	}
}

// TestProbeRecordSaveSyncsPointStatus pins the runtime status contract: a
// normal probe leaves the point active, a failed one marks it error, and a
// missing point row (deleted mid-flight) does not fail the save.
func TestProbeRecordSaveSyncsPointStatus(t *testing.T) {
	f := openProbeRecordDB(t)
	f.seedPoint(7, MonitorStatusPending)
	f.seedPoint(8, MonitorStatusActive)

	f.saveRecord(probeRecordFor(7, types.AssetStatusNormal, time.Now()))
	f.saveRecord(probeRecordFor(8, types.AssetStatusAbnormal, time.Now()))

	var p7, p8 MonitorPoint
	if err := f.dbc.First(&p7, "id = ?", 7).Error; err != nil {
		t.Fatalf("load point 7: %v", err)
	}
	if p7.Status != MonitorStatusActive {
		t.Errorf("point 7 status = %q, want active after normal probe", p7.Status)
	}
	if err := f.dbc.First(&p8, "id = ?", 8).Error; err != nil {
		t.Fatalf("load point 8: %v", err)
	}
	if p8.Status != MonitorStatusError {
		t.Errorf("point 8 status = %q, want error after abnormal probe", p8.Status)
	}

	// A record for a deleted point is kept; the status update is a no-op.
	f.saveRecord(probeRecordFor(999, types.AssetStatusNormal, time.Now()))
	if _, _, err := f.store.QueryByPoint(context.Background(), ProbeQuery{PointID: 999}); err != nil {
		t.Fatalf("query orphan records: %v", err)
	}
}

// TestProbeRecordSaveRejectsForeignRecords verifies the store refuses
// records that do not belong to the telemetry domain: execute operations
// and task IDs outside the negative probe range.
func TestProbeRecordSaveRejectsForeignRecords(t *testing.T) {
	f := openProbeRecordDB(t)

	executeRec := probeRecordFor(7, types.AssetStatusNormal, time.Now())
	executeRec.Operation = executor.OpExecute
	if err := f.store.Save(context.Background(), executeRec); err == nil {
		t.Error("expected error for execute operation")
	}

	for _, taskID := range []int64{
		12,                    // regular task ID
		legacyProberTaskID(7), // legacy positive-scheme probe ID
		-ProbeTaskIDOffset,    // decodes to point 0, which cannot exist
	} {
		rec := probeRecordFor(7, types.AssetStatusNormal, time.Now())
		rec.TaskID = taskID
		if err := f.store.Save(context.Background(), rec); err == nil {
			t.Errorf("expected error for task ID %d", taskID)
		}
	}

	recs, total, err := f.store.QueryByPoint(context.Background(), ProbeQuery{PointID: 7})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 0 || len(recs) != 0 {
		t.Errorf("rejected records were persisted: %d rows", len(recs))
	}
}

// TestProbeRecordQueryOrderPagingAndWindow covers the query contract:
// newest-first ordering, time-window filters, and page/size pagination.
func TestProbeRecordQueryOrderPagingAndWindow(t *testing.T) {
	f := openProbeRecordDB(t)
	base := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	for i := range 5 {
		f.saveRecord(probeRecordFor(7, types.AssetStatusNormal, base.Add(time.Duration(i)*time.Minute)))
	}

	// Newest first.
	recs, total, err := f.store.QueryByPoint(context.Background(), ProbeQuery{PointID: 7})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 5 || len(recs) != 5 {
		t.Fatalf("records = %d/%d, want 5/5", len(recs), total)
	}
	newest := base.Add(4 * time.Minute)
	if !recs[0].StartedAt.Equal(newest) {
		t.Errorf("first record started at %v, want %v (newest first)", recs[0].StartedAt, newest)
	}

	// Pagination: page 2 with size 2 returns the 3rd/4th newest.
	recs, _, err = f.store.QueryByPoint(context.Background(), ProbeQuery{PointID: 7, Page: 2, Size: 2})
	if err != nil {
		t.Fatalf("query page 2: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("page 2 records = %d, want 2", len(recs))
	}
	want3rd := base.Add(2 * time.Minute)
	want4th := base.Add(1 * time.Minute)
	if !recs[0].StartedAt.Equal(want3rd) || !recs[1].StartedAt.Equal(want4th) {
		t.Errorf("page 2 = [%v, %v], want [%v, %v]",
			recs[0].StartedAt, recs[1].StartedAt, want3rd, want4th)
	}

	// Time window narrows the result set.
	mid := base.Add(2 * time.Minute)
	recs, total, err = f.store.QueryByPoint(context.Background(), ProbeQuery{PointID: 7, Start: mid})
	if err != nil {
		t.Fatalf("query window: %v", err)
	}
	if total != 3 || len(recs) != 3 {
		t.Errorf("windowed records = %d/%d, want 3/3", len(recs), total)
	}

	// Other points are isolated.
	if _, total, err = f.store.QueryByPoint(context.Background(), ProbeQuery{PointID: 8}); err != nil || total != 0 {
		t.Errorf("other point records = %d (err %v), want 0", total, err)
	}
}

// TestProbeRecordLatestByPoint covers the latest-record accessor used by
// the status endpoint: newest row on hit, errdefs.ErrNotFound when the
// point has never been probed.
func TestProbeRecordLatestByPoint(t *testing.T) {
	f := openProbeRecordDB(t)
	base := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	f.saveRecord(probeRecordFor(7, types.AssetStatusAbnormal, base))
	f.saveRecord(probeRecordFor(7, types.AssetStatusNormal, base.Add(time.Minute)))

	got, err := f.store.LatestByPoint(context.Background(), 7)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got.Status != types.AssetStatusNormal {
		t.Errorf("latest status = %q, want normal (newest row)", got.Status)
	}

	if _, err = f.store.LatestByPoint(context.Background(), 8); !errors.Is(err, errdefs.ErrNotFound) {
		t.Errorf("latest for unprobed point err = %v, want ErrNotFound", err)
	}
}
