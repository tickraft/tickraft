// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package integration_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/event"
	prismengine "github.com/tickraft/tickraft/pkg/prism"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"go.uber.org/zap"
)

// mockAlertRecordStore implements alert.RecordStore for the alerting
// integration test. It records every Create call so the test can assert that
// the OnAlert callback persisted the expected alert.Record.
type mockAlertRecordStore struct {
	mu      sync.Mutex
	records []*alert.Record
}

func newMockAlertRecordStore() *mockAlertRecordStore {
	return &mockAlertRecordStore{}
}

func (s *mockAlertRecordStore) Create(_ context.Context, m *alert.Record) error {
	if m == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Copy to avoid aliasing the caller's pointer.
	cp := *m
	s.records = append(s.records, &cp)
	return nil
}

func (s *mockAlertRecordStore) CreateBatch(_ context.Context, models []*alert.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range models {
		if m == nil {
			continue
		}
		cp := *m
		s.records = append(s.records, &cp)
	}
	return nil
}

func (s *mockAlertRecordStore) GetByID(_ context.Context, _ int64) (*alert.Record, error) {
	return nil, nil
}

func (s *mockAlertRecordStore) List(_ context.Context, _, _ int, _ alert.RecordFilter) ([]*alert.Record, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*alert.Record, len(s.records))
	copy(out, s.records)
	return out, int64(len(out)), nil
}

func (s *mockAlertRecordStore) Acknowledge(_ context.Context, id int64) (*alert.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.records {
		if r.ID == id {
			r.Status = "acknowledged"
			now := time.Now()
			r.AcknowledgedAt = &now
			cp := *r
			return &cp, nil
		}
	}
	return nil, errdefs.ErrNotFound
}

func (s *mockAlertRecordStore) Resolve(_ context.Context, id int64) (*alert.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.records {
		if r.ID == id {
			r.Status = "resolved"
			now := time.Now()
			r.ResolvedAt = &now
			cp := *r
			return &cp, nil
		}
	}
	return nil, errdefs.ErrNotFound
}

// Records returns a snapshot of the persisted alert records.
func (s *mockAlertRecordStore) Records() []*alert.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*alert.Record, len(s.records))
	copy(out, s.records)
	return out
}

// TestAlertFlow verifies the alerting pipeline end-to-end:
//
//  1. A collector MetricAlert event is published on the event bus.
//  2. The alert.Engine (subscribed to TypeTelemetryMetricExceeded) receives it and
//     evaluates it against a registered metric rule.
//  3. When the rule matches, the OnAlert callback (prism.RecordAlert) is
//     invoked synchronously inside dispatch.
//  4. The OnAlert callback writes an alert.Record into the
//     AlertRecordStore.
//
// The test asserts that exactly one record is persisted with the metric
// name as RuleName (the expr-lang rule engine does not surface matched
// rule IDs through the OnAlert callback) and the metric value propagated
// from the alert event, mirroring the wiring in
// internal/service/prism.go.
func TestAlertFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bus := event.NewBus()
	t.Cleanup(func() { _ = bus.Close() })

	// Set up mock record store used by the production OnAlert callback
	// (prism.RecordAlert).
	recordStore := newMockAlertRecordStore()

	// Wire the production OnAlert callback exactly as internal/service
	// does so the test exercises the same record-persistence path.
	onAlert := func(ctx context.Context, evt alert.Event) {
		if err := alert.RecordAlert(ctx, recordStore, evt); err != nil {
			t.Logf("RecordAlert returned error: %v", err)
		}
	}

	eng, err := prismengine.New(
		prismengine.WithEventBus(bus),
		prismengine.WithLogger(zap.NewNop()),
		prismengine.WithOnAlert(onAlert),
	)
	if err != nil {
		t.Fatalf("create prism engine: %v", err)
	}
	// Register a metric rule that matches the upcoming alert: the engine
	// matches on metric name + operator comparison (value > threshold).
	eng.AddRule(prismengine.MatcherFunc(func(_ context.Context, evt alert.Event) bool {
		return evt.Type == alert.TypeMetric &&
			len(evt.Violations) > 0 &&
			evt.Violations[0].Metric != nil &&
			evt.Violations[0].Metric.Name == "cpu_usage" &&
			evt.Violations[0].Metric.Value > 90.0
	}))

	if err := eng.Start(ctx); err != nil {
		t.Fatalf("start prism engine: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = eng.Stop(stopCtx)
	})

	// Publish a MetricAlert event that the rule matches.
	wantResourceID := int64(7)
	wantTenantID := int64(1)
	wantMetricValue := 95.5
	wantThreshold := 90.0

	_ = event.Publish(context.Background(), bus, event.TypeTelemetryMetricExceeded, event.MetricExceededPayload{
		AssetID:     strconv.FormatInt(wantResourceID, 10),
		TenantID:    strconv.FormatInt(wantTenantID, 10),
		MetricName:  "cpu_usage",
		MetricValue: wantMetricValue,
		Threshold:   wantThreshold,
		Operator:    "gt",
	})

	// Wait for the OnAlert callback to persist the record. The dispatch path
	// is asynchronous on the event bus consumer goroutine, so poll the store
	// until the record appears or the timeout elapses.
	deadline := time.Now().Add(5 * time.Second)
	var got *alert.Record
	for time.Now().Before(deadline) {
		records := recordStore.Records()
		if len(records) > 0 {
			got = records[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got == nil {
		t.Fatal("timed out waiting for alert record to be persisted")
	}

	// Verify the persisted record carries the metric name as RuleName and
	// the alert value. RuleID is zero because the expr-lang rule engine
	// does not surface matched rule IDs through the OnAlert callback.
	if got.RuleID != 0 {
		t.Errorf("RuleID = %d, want 0", got.RuleID)
	}
	if got.RuleName != "cpu_usage" {
		t.Errorf("RuleName = %q, want %q", got.RuleName, "cpu_usage")
	}
	if got.Value != wantMetricValue {
		t.Errorf("Value = %v, want %v", got.Value, wantMetricValue)
	}
	if got.Status != "firing" {
		t.Errorf("Status = %q, want %q", got.Status, "firing")
	}
	if got.Message == "" {
		t.Error("Message should not be empty")
	}
	if got.TriggeredAt.IsZero() {
		t.Error("TriggeredAt should be set")
	}

	// Verify no extra records were persisted.
	if records := recordStore.Records(); len(records) != 1 {
		t.Errorf("expected 1 alert record, got %d", len(records))
	}
}

// TestAlertFlowRuleSuppressed verifies that when no registered rule matches an
// incoming metric alert, the OnAlert callback is not invoked and no record is
// persisted. This guards the default-deny behavior of the rule engine: only
// matching alerts flow through to the persistence layer.
func TestAlertFlowRuleSuppressed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bus := event.NewBus()
	t.Cleanup(func() { _ = bus.Close() })

	recordStore := newMockAlertRecordStore()

	onAlert := func(ctx context.Context, evt alert.Event) {
		_ = alert.RecordAlert(ctx, recordStore, evt)
	}

	eng, err := prismengine.New(
		prismengine.WithEventBus(bus),
		prismengine.WithLogger(zap.NewNop()),
		prismengine.WithOnAlert(onAlert),
	)
	if err != nil {
		t.Fatalf("create prism engine: %v", err)
	}
	// Register a rule that only matches cpu_usage; the test will emit a
	// memory_usage alert which must be suppressed.
	eng.AddRule(prismengine.MatcherFunc(func(_ context.Context, evt alert.Event) bool {
		return len(evt.Violations) > 0 &&
			evt.Violations[0].Metric != nil &&
			evt.Violations[0].Metric.Name == "cpu_usage"
	}))

	if err := eng.Start(ctx); err != nil {
		t.Fatalf("start prism engine: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = eng.Stop(stopCtx)
	})

	_ = event.Publish(context.Background(), bus, event.TypeTelemetryMetricExceeded, event.MetricExceededPayload{
		AssetID:     "1",
		TenantID:    "1",
		MetricName:  "memory_usage",
		MetricValue: 80.0,
		Threshold:   70.0,
		Operator:    "gt",
	})

	// Give the consumer goroutine a chance to process the event. Since the
	// rule does not match, no record should be persisted.
	time.Sleep(300 * time.Millisecond)

	if records := recordStore.Records(); len(records) != 0 {
		t.Errorf("expected 0 alert records for suppressed alert, got %d", len(records))
	}
}
