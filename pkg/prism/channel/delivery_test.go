// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/channel/tracking"
)

// ---------------------------------------------------------------------------
// DeliveryStore: Record / Get / UpdateAttempt
// ---------------------------------------------------------------------------

func TestDeliveryStore_Record(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	rec := tracking.DeliveryRecord{
		Identity:  tracking.Identity{ChannelID: 5, ChannelName: "feishu-default", ChannelType: "feishu"},
		AlertType: "task_failed",
		Status:    tracking.StatusSuccess,
		SentAt:    time.Now().UTC(),
	}
	if err := s.Record(ctx, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	records, total, err := s.List(ctx, DeliveryListParams{Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total 1, got %d", total)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].ChannelName != "feishu-default" {
		t.Errorf("channel name: got %q, want %q", records[0].ChannelName, "feishu-default")
	}
	if records[0].Status != string(tracking.StatusSuccess) {
		t.Errorf("status: got %q, want %q", records[0].Status, tracking.StatusSuccess)
	}
	if len(records[0].Attempts) != 1 {
		t.Errorf("expected 1 initial attempt, got %d", len(records[0].Attempts))
	}
}

func TestDeliveryStore_Record_FailedWithError(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	rec := tracking.DeliveryRecord{
		Identity:  tracking.Identity{ChannelID: 6, ChannelName: "slack", ChannelType: "slack"},
		AlertType: "device_offline",
		Status:    tracking.StatusFailed,
		Error:     "connection refused",
		SentAt:    time.Now().UTC(),
	}
	if err := s.Record(ctx, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	records, _, err := s.List(ctx, DeliveryListParams{Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Error != "connection refused" {
		t.Errorf("error: got %q, want %q", records[0].Error, "connection refused")
	}
	if records[0].Status != string(tracking.StatusFailed) {
		t.Errorf("status: got %q, want %q", records[0].Status, tracking.StatusFailed)
	}
}

// TestDeliveryStore_Get returns the record by id, with the serialized event
// payload and the initial attempt entry materialised by Record.
func TestDeliveryStore_Get(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	if err := s.Record(ctx, tracking.DeliveryRecord{
		Identity: tracking.Identity{ChannelID: 9, ChannelName: "get-chan", ChannelType: "slack"},
		Status:   tracking.StatusFailed,
		Error:    "boom",
		Event:    trackingEvent(),
		SentAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	var seeded DeliveryRecord
	if err := db.First(&seeded).Error; err != nil {
		t.Fatalf("load seeded: %v", err)
	}

	got, err := s.Get(ctx, seeded.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != seeded.ID || got.Error != "boom" {
		t.Errorf("Get mismatch: %+v", got)
	}
	if got.RequestPayload == "" {
		t.Error("RequestPayload: got empty, want serialized event")
	}
	if len(got.Attempts) != 1 || got.Attempts[0].N != 0 {
		t.Errorf("Attempts: got %+v, want single entry 0", got.Attempts)
	}

	// A missing record is reported as not-found.
	if _, err := s.Get(ctx, 99999); !errors.Is(err, errdefs.ErrNotFound) {
		t.Errorf("Get on missing row = %v, want errdefs.ErrNotFound", err)
	}
}

// TestDeliveryStore_UpdateAttempt verifies the retry persistence path: the
// appended attempt and refreshed top-level fields are written.
func TestDeliveryStore_UpdateAttempt(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	if err := s.Record(ctx, tracking.DeliveryRecord{
		Identity: tracking.Identity{ChannelID: 12, ChannelName: "upd-chan", ChannelType: "feishu"},
		Status:   tracking.StatusFailed,
		Error:    "first failure",
		Event:    trackingEvent(),
		SentAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	var rec DeliveryRecord
	if err := db.First(&rec).Error; err != nil {
		t.Fatalf("load seeded: %v", err)
	}

	rec.Status = string(tracking.StatusSuccess)
	rec.Error = ""
	rec.ResponseCode = 0
	rec.DurationMs = 55
	rec.Attempts = append(rec.Attempts, Attempt{
		Time:       time.Now().UTC(),
		N:          1,
		Result:     string(tracking.StatusSuccess),
		DurationMs: 55,
	})
	if err := s.UpdateAttempt(ctx, &rec); err != nil {
		t.Fatalf("UpdateAttempt: %v", err)
	}

	var reloaded DeliveryRecord
	if err := db.First(&reloaded, rec.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Status != string(tracking.StatusSuccess) || reloaded.Error != "" {
		t.Errorf("top-level mismatch: status=%q error=%q", reloaded.Status, reloaded.Error)
	}
	if len(reloaded.Attempts) != 2 {
		t.Fatalf("Attempts: got %d, want 2", len(reloaded.Attempts))
	}
	if reloaded.Attempts[1].N != 1 || reloaded.Attempts[1].Result != string(tracking.StatusSuccess) {
		t.Errorf("appended attempt mismatch: %+v", reloaded.Attempts[1])
	}
}

// ---------------------------------------------------------------------------
// DeliveryStore: List filters
// ---------------------------------------------------------------------------

func TestDeliveryStore_ListFilterByChannelID(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	now := time.Now().UTC()
	for _, id := range []int64{11, 11, 22} {
		if err := s.Record(ctx, tracking.DeliveryRecord{
			Identity: tracking.Identity{ChannelID: id, ChannelName: "ch", ChannelType: "feishu"},
			Status:   tracking.StatusSuccess,
			SentAt:   now,
		}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	records, total, err := s.List(ctx, DeliveryListParams{
		ChannelID: 11,
		Page:      1,
		Size:      10,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total 2 for channel 11, got %d", total)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 records for channel 11, got %d", len(records))
	}
}

func TestDeliveryStore_ListFilterByStatus(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	now := time.Now().UTC()
	statuses := []tracking.DeliveryStatus{
		tracking.StatusSuccess,
		tracking.StatusFailed,
		tracking.StatusSuccess,
	}
	for _, st := range statuses {
		if err := s.Record(ctx, tracking.DeliveryRecord{
			Identity: tracking.Identity{ChannelName: "ch"},
			Status:   st,
			SentAt:   now,
		}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	records, total, err := s.List(ctx, DeliveryListParams{
		Status: string(tracking.StatusFailed),
		Page:   1,
		Size:   10,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total 1 for failed, got %d", total)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record for failed, got %d", len(records))
	}
}

// TestDeliveryStore_ListFilterByAlertTitle verifies the case-insensitive
// substring filter on the rendered alert title.
func TestDeliveryStore_ListFilterByAlertTitle(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	now := time.Now().UTC()
	for _, title := range []string{"CPU usage high", "Disk usage high", "cpu idle"} {
		if err := s.Record(ctx, tracking.DeliveryRecord{
			Identity:   tracking.Identity{ChannelName: "ch"},
			AlertTitle: title,
			Status:     tracking.StatusSuccess,
			SentAt:     now,
		}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	records, total, err := s.List(ctx, DeliveryListParams{
		AlertTitle: "cpu",
		Page:       1,
		Size:       10,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(records) != 2 {
		t.Errorf("records = %d, want 2", len(records))
	}
}

func TestDeliveryStore_ListFilterByTimeRange(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	times := []time.Time{base, base.Add(1 * time.Hour), base.Add(2 * time.Hour)}
	for _, at := range times {
		if err := s.Record(ctx, tracking.DeliveryRecord{
			Identity: tracking.Identity{ChannelName: "ch"},
			Status:   tracking.StatusSuccess,
			SentAt:   at,
		}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	start := base.Add(30 * time.Minute)
	end := base.Add(90 * time.Minute)
	records, total, err := s.List(ctx, DeliveryListParams{
		StartTime: &start,
		EndTime:   &end,
		Page:      1,
		Size:      10,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// Only the record at base+1h falls within [start, end].
	if total != 1 {
		t.Errorf("expected total 1 in time range, got %d", total)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record in time range, got %d", len(records))
	}
}

func TestDeliveryStore_ListFilterByStartTimeOnly(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{base, base.Add(1 * time.Hour), base.Add(2 * time.Hour)} {
		if err := s.Record(ctx, tracking.DeliveryRecord{
			Identity: tracking.Identity{ChannelName: "ch"},
			Status:   tracking.StatusSuccess,
			SentAt:   at,
		}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	start := base.Add(30 * time.Minute)
	_, total, err := s.List(ctx, DeliveryListParams{
		StartTime: &start,
		Page:      1,
		Size:      10,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total 2 after start, got %d", total)
	}
}

// ---------------------------------------------------------------------------
// DeliveryStore: List pagination and ordering
// ---------------------------------------------------------------------------

func TestDeliveryStore_ListPagination(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 5 {
		if err := s.Record(ctx, tracking.DeliveryRecord{
			Identity: tracking.Identity{ChannelName: "ch"},
			Status:   tracking.StatusSuccess,
			SentAt:   base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}

	records, total, err := s.List(ctx, DeliveryListParams{Page: 1, Size: 2})
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 records on page 1, got %d", len(records))
	}

	records2, _, err := s.List(ctx, DeliveryListParams{Page: 2, Size: 2})
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	if len(records2) != 2 {
		t.Errorf("expected 2 records on page 2, got %d", len(records2))
	}

	records3, _, err := s.List(ctx, DeliveryListParams{Page: 3, Size: 2})
	if err != nil {
		t.Fatalf("List page 3: %v", err)
	}
	if len(records3) != 1 {
		t.Errorf("expected 1 record on page 3, got %d", len(records3))
	}
}

func TestDeliveryStore_ListOrdersBySentAtDesc(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Insert out of order to verify the descending sort.
	for _, i := range []int{2, 0, 1} {
		if err := s.Record(ctx, tracking.DeliveryRecord{
			Identity: tracking.Identity{ChannelName: "ch"},
			Status:   tracking.StatusSuccess,
			SentAt:   base.Add(time.Duration(i) * time.Hour),
		}); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}

	records, _, err := s.List(ctx, DeliveryListParams{Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	// Newest first.
	if !records[0].SentAt.After(records[1].SentAt) {
		t.Errorf("expected descending order; got %v then %v", records[0].SentAt, records[1].SentAt)
	}
	if !records[1].SentAt.After(records[2].SentAt) {
		t.Errorf("expected descending order; got %v then %v", records[1].SentAt, records[2].SentAt)
	}
}

// TestDeliveryStore_ListClampsPage verifies out-of-range values are clamped
// instead of erroring; strict 400 validation happens in the HTTP layer.
func TestDeliveryStore_ListClampsPage(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	if _, _, err := s.List(ctx, DeliveryListParams{Page: 0, Size: 10}); err != nil {
		t.Errorf("page 0: unexpected error: %v", err)
	}
	if _, _, err := s.List(ctx, DeliveryListParams{Page: 1, Size: 0}); err != nil {
		t.Errorf("size 0: unexpected error: %v", err)
	}
	if _, _, err := s.List(ctx, DeliveryListParams{Page: 1, Size: 101}); err != nil {
		t.Errorf("size 101: unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DeliveryStore: keyset pagination
// ---------------------------------------------------------------------------

// deliverySeed describes the rows seedDeliveryRecords inserts: n records
// with sent_at timestamps monotonically increasing by step.
type deliverySeed struct {
	n    int
	base time.Time
	step time.Duration
}

// seedDeliveryRecords inserts seed.n delivery records with monotonically
// increasing sent_at timestamps (seed.base + i*seed.step). It returns the
// persisted rows ordered newest first so callers can reason about the
// expected descending page order.
func seedDeliveryRecords(
	ctx context.Context, t *testing.T, s *DeliveryStore, seed deliverySeed,
) []DeliveryRecord {
	t.Helper()
	for i := range seed.n {
		rec := tracking.DeliveryRecord{
			Identity: tracking.Identity{ChannelID: 1, ChannelName: "ch", ChannelType: "feishu"},
			Status:   tracking.StatusSuccess,
			SentAt:   seed.base.Add(time.Duration(i) * seed.step),
		}
		if err := s.Record(ctx, rec); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}
	persisted, _, err := s.List(ctx, DeliveryListParams{Page: 1, Size: 100})
	if err != nil {
		t.Fatalf("reload seeded records: %v", err)
	}
	return persisted
}

func TestDeliveryStore_ListKeyset_Empty(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	result, err := s.ListKeyset(ctx, DeliveryListParams{Size: 10})
	if err != nil {
		t.Fatalf("ListKeyset empty: %v", err)
	}
	if len(result.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(result.Items))
	}
	if result.Total != 0 {
		t.Errorf("expected total 0, got %d", result.Total)
	}
	if result.NextCursor != "" {
		t.Errorf("expected empty next cursor, got %q", result.NextCursor)
	}
}

func TestDeliveryStore_ListKeyset_FirstPage(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seeded := seedDeliveryRecords(ctx, t, s, deliverySeed{n: 5, base: base, step: time.Minute})

	result, err := s.ListKeyset(ctx, DeliveryListParams{Size: 2})
	if err != nil {
		t.Fatalf("ListKeyset first page: %v", err)
	}
	if result.Total != 5 {
		t.Errorf("expected total 5, got %d", result.Total)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result.Items))
	}
	// The first page should contain the two newest records.
	if result.Items[0].SentAt != seeded[0].SentAt {
		t.Errorf("first item sent_at = %v, want %v", result.Items[0].SentAt, seeded[0].SentAt)
	}
	if result.Items[1].SentAt != seeded[1].SentAt {
		t.Errorf("second item sent_at = %v, want %v", result.Items[1].SentAt, seeded[1].SentAt)
	}
	if result.NextCursor == "" {
		t.Fatal("expected non-empty next cursor when more rows remain")
	}
}

func TestDeliveryStore_ListKeyset_MiddlePage(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seeded := seedDeliveryRecords(ctx, t, s, deliverySeed{n: 5, base: base, step: time.Minute})

	page1, err := s.ListKeyset(ctx, DeliveryListParams{Size: 2})
	if err != nil {
		t.Fatalf("ListKeyset page 1: %v", err)
	}
	if page1.NextCursor == "" {
		t.Fatal("expected non-empty next cursor after page 1")
	}

	page2, err := s.ListKeyset(ctx, DeliveryListParams{Size: 2, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("ListKeyset page 2: %v", err)
	}
	if len(page2.Items) != 2 {
		t.Fatalf("expected 2 items on page 2, got %d", len(page2.Items))
	}
	// Page 2 should continue after page 1's last record, in descending order.
	if page2.Items[0].SentAt != seeded[2].SentAt {
		t.Errorf("page2 first item sent_at = %v, want %v", page2.Items[0].SentAt, seeded[2].SentAt)
	}
	if page2.Items[1].SentAt != seeded[3].SentAt {
		t.Errorf("page2 second item sent_at = %v, want %v", page2.Items[1].SentAt, seeded[3].SentAt)
	}
	if page2.NextCursor == "" {
		t.Fatal("expected non-empty next cursor after page 2 (1 row remains)")
	}
}

func TestDeliveryStore_ListKeyset_LastPageWalk(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seeded := seedDeliveryRecords(ctx, t, s, deliverySeed{n: 5, base: base, step: time.Minute})

	// Walk through all pages until the next cursor is empty.
	cursor := ""
	var all []DeliveryRecord
	for {
		result, err := s.ListKeyset(ctx, DeliveryListParams{Size: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("ListKeyset walk: %v", err)
		}
		all = append(all, result.Items...)
		cursor = result.NextCursor
		if cursor == "" {
			break
		}
	}

	if len(all) != 5 {
		t.Fatalf("expected 5 total items across pages, got %d", len(all))
	}
	// The last item should be the oldest record.
	wantLast := seeded[len(seeded)-1]
	if all[len(all)-1].SentAt != wantLast.SentAt {
		t.Errorf("last item sent_at = %v, want oldest %v", all[len(all)-1].SentAt, wantLast.SentAt)
	}
}

// TestDeliveryStore_ListKeyset_TiesBrokenByID verifies that records sharing
// a sent_at value are ordered by id DESC and the cursor walks past them
// without skipping or duplicating rows.
func TestDeliveryStore_ListKeyset_TiesBrokenByID(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedDeliveryRecords(ctx, t, s, deliverySeed{n: 4, base: at}) // all four share sent_at

	seeded, _, err := s.List(ctx, DeliveryListParams{Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	cursor := ""
	var all []DeliveryRecord
	for {
		result, err := s.ListKeyset(ctx, DeliveryListParams{Size: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("ListKeyset walk: %v", err)
		}
		all = append(all, result.Items...)
		cursor = result.NextCursor
		if cursor == "" {
			break
		}
	}

	if len(all) != 4 {
		t.Fatalf("expected 4 items across tie pages, got %d", len(all))
	}
	for i := range seeded {
		if all[i].ID != seeded[i].ID {
			t.Errorf("tie order mismatch at %d: got id %d, want %d", i, all[i].ID, seeded[i].ID)
		}
	}
}

func TestDeliveryStore_ListKeyset_InvalidCursor(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedDeliveryRecords(ctx, t, s, deliverySeed{n: 1, base: base, step: time.Minute})

	_, err := s.ListKeyset(ctx, DeliveryListParams{Size: 10, Cursor: "not-a-valid-cursor!!!"})
	if err == nil {
		t.Fatal("expected error for invalid cursor, got nil")
	}
	if !strings.Contains(err.Error(), "keyset") && !strings.Contains(err.Error(), "cursor") {
		t.Errorf("expected cursor/keyset related error, got %v", err)
	}
}

// TestDeliveryStore_SatisfiesTrackingInterface exercises construction with a
// fresh DB in addition to the compile-time assertion in delivery.go.
func TestDeliveryStore_SatisfiesTrackingInterface(t *testing.T) {
	var _ tracking.DeliveryRecordStore = NewDeliveryStore(newStoreTestDB(t))
}

// TestDeliveryStore_DeleteOlderThan verifies the retention sweep: rows with
// sent_at before the cutoff are removed across tenants, newer ones stay,
// and the removed count is reported.
func TestDeliveryStore_DeleteOlderThan(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewDeliveryStore(db)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedDeliveryRecords(ctx, t, s, deliverySeed{n: 5, base: base, step: 24 * time.Hour})

	removed, err := s.DeleteOlderThan(ctx, base.Add(3*24*time.Hour))
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if removed != 3 {
		t.Fatalf("expected 3 removed, got %d", removed)
	}
	_, total, err := s.List(ctx, DeliveryListParams{Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("List after sweep: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 remaining records, got %d", total)
	}

	// An empty sweep is a no-op.
	if removed, err := s.DeleteOlderThan(ctx, base); err != nil || removed != 0 {
		t.Fatalf("second sweep: removed=%d err=%v", removed, err)
	}
}

// ---------------------------------------------------------------------------
// Attempts Valuer/Scanner
// ---------------------------------------------------------------------------

// TestAttemptsRoundTrip exercises the Attempts Valuer/Scanner pair: the
// empty list serializes as "[]" and a marshaled list scans back equal.
func TestAttemptsRoundTrip(t *testing.T) {
	empty := Attempts{}
	v, err := empty.Value()
	if err != nil {
		t.Fatalf("empty Value: %v", err)
	}
	if v != "[]" {
		t.Errorf("empty Value = %v, want []", v)
	}
	var back Attempts
	if err := back.Scan(v); err != nil {
		t.Fatalf("empty Scan: %v", err)
	}
	if len(back) != 0 {
		t.Errorf("empty Scan = %+v, want empty", back)
	}

	list := Attempts{
		{Time: time.Unix(1700000000, 0).UTC(), N: 0, Result: "failed", Error: "x", DurationMs: 10},
		{Time: time.Unix(1700000100, 0).UTC(), N: 1, Result: "success", DurationMs: 20},
	}
	v, err = list.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	var scanned Attempts
	if err := scanned.Scan(v); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(scanned) != 2 || scanned[1].N != 1 || scanned[1].Result != "success" || scanned[0].Error != "x" {
		t.Errorf("round trip mismatch: %+v", scanned)
	}

	// nil and unsupported types.
	var nilAttempts Attempts
	if err := nilAttempts.Scan(nil); err != nil || nilAttempts != nil {
		t.Errorf("nil Scan: %+v err=%v", nilAttempts, err)
	}
	if err := nilAttempts.Scan(42); err == nil {
		t.Error("expected error for unsupported Scan type")
	}
}

// trackingEvent is a minimal alert event used by the delivery store
// persistence tests.
func trackingEvent() alert.Event {
	return alert.Event{
		Type:      alert.TypeLog,
		TenantID:  1,
		Timestamp: time.Unix(1700000000, 0).UTC(),
		Violations: []alert.Violation{{
			Kind: alert.ViolationKindLog,
			Log:  &alert.LogContext{Keyword: "store-test", Content: "payload"},
		}},
	}
}
