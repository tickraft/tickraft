// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// mockMetricStore is an in-memory MetricStore for testing.
type mockMetricStore struct {
	mu      sync.Mutex
	saved   []*CollectMetric
	failErr error
}

func (s *mockMetricStore) SaveMetric(_ context.Context, m *CollectMetric) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failErr != nil {
		return s.failErr
	}
	s.saved = append(s.saved, m)
	return nil
}

func (s *mockMetricStore) SaveMetricsBatch(_ context.Context, metrics []*CollectMetric) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failErr != nil {
		return s.failErr
	}
	s.saved = append(s.saved, metrics...)
	return nil
}

func (s *mockMetricStore) QueryMetrics(_ context.Context, _ MetricQuery) ([]CollectMetric, error) {
	return nil, nil
}

func (s *mockMetricStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.saved)
}

// mockLogStore is an in-memory LogStore for testing.
type mockLogStore struct {
	mu      sync.Mutex
	saved   []*CollectLog
	failErr error
}

func (s *mockLogStore) SaveLog(_ context.Context, l *CollectLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failErr != nil {
		return s.failErr
	}
	s.saved = append(s.saved, l)
	return nil
}

func (s *mockLogStore) SaveLogsBatch(_ context.Context, logs []*CollectLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failErr != nil {
		return s.failErr
	}
	s.saved = append(s.saved, logs...)
	return nil
}

func (s *mockLogStore) QueryLogs(_ context.Context, _ LogQuery) ([]CollectLog, error) {
	return nil, nil
}

func (s *mockLogStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.saved)
}

func TestPersistencePersistMetrics(t *testing.T) {
	ms := &mockMetricStore{}
	p := NewPersistence(ms, nil, zap.NewNop())

	metrics := []CollectMetric{
		{TenantID: 1, AssetID: 1, MetricName: "cpu_avg", MetricValue: 50, Timestamp: time.Now()},
		{TenantID: 1, AssetID: 1, MetricName: "cpu_max", MetricValue: 80, Timestamp: time.Now()},
	}
	if err := p.PersistMetrics(context.Background(), metrics); err != nil {
		t.Fatalf("PersistMetrics failed: %v", err)
	}
	if got := ms.count(); got != 2 {
		t.Errorf("saved metric count: got %d, want 2", got)
	}
}

func TestPersistencePersistLogs(t *testing.T) {
	ls := &mockLogStore{}
	p := NewPersistence(nil, ls, zap.NewNop())

	logs := []CollectLog{
		{TenantID: 1, AssetID: 1, Level: "ERROR", Content: "boom", Timestamp: time.Now()},
	}
	if err := p.PersistLogs(context.Background(), logs); err != nil {
		t.Fatalf("PersistLogs failed: %v", err)
	}
	if got := ls.count(); got != 1 {
		t.Errorf("saved log count: got %d, want 1", got)
	}
}

func TestPersistenceNilStoreNoOp(t *testing.T) {
	p := NewPersistence(nil, nil, zap.NewNop())

	if err := p.PersistMetrics(context.Background(), []CollectMetric{{MetricName: "x"}}); err != nil {
		t.Errorf("PersistMetrics with nil store should be no-op, got %v", err)
	}
	if err := p.PersistLogs(context.Background(), []CollectLog{{Content: "x"}}); err != nil {
		t.Errorf("PersistLogs with nil store should be no-op, got %v", err)
	}
	if err := p.Persist(context.Background(), []CollectMetric{{}}, []CollectLog{{}}); err != nil {
		t.Errorf("Persist with nil stores should be no-op, got %v", err)
	}
}

func TestPersistencePersistCombined(t *testing.T) {
	ms := &mockMetricStore{}
	ls := &mockLogStore{}
	p := NewPersistence(ms, ls, zap.NewNop())

	metrics := []CollectMetric{
		{TenantID: 1, AssetID: 1, MetricName: "cpu", MetricValue: 50, Timestamp: time.Now()},
	}
	logs := []CollectLog{
		{TenantID: 1, AssetID: 1, Level: "INFO", Content: "ok", Timestamp: time.Now()},
	}
	if err := p.Persist(context.Background(), metrics, logs); err != nil {
		t.Fatalf("Persist failed: %v", err)
	}
	if got := ms.count(); got != 1 {
		t.Errorf("saved metric count: got %d, want 1", got)
	}
	if got := ls.count(); got != 1 {
		t.Errorf("saved log count: got %d, want 1", got)
	}
}

func TestPersistenceEmptyBatchNoOp(t *testing.T) {
	ms := &mockMetricStore{}
	ls := &mockLogStore{}
	p := NewPersistence(ms, ls, zap.NewNop())

	if err := p.PersistMetrics(context.Background(), nil); err != nil {
		t.Errorf("PersistMetrics with empty batch should be no-op, got %v", err)
	}
	if err := p.PersistLogs(context.Background(), nil); err != nil {
		t.Errorf("PersistLogs with empty batch should be no-op, got %v", err)
	}
	if got := ms.count(); got != 0 {
		t.Errorf("metric count: got %d, want 0", got)
	}
	if got := ls.count(); got != 0 {
		t.Errorf("log count: got %d, want 0", got)
	}
}

func TestPersistenceMetricErrorPropagates(t *testing.T) {
	errSentinel := errors.New("db down")
	ls := &mockLogStore{failErr: errSentinel}
	p := NewPersistence(nil, ls, zap.NewNop())

	err := p.PersistLogs(context.Background(), []CollectLog{{Content: "x"}})
	if err == nil {
		t.Fatal("expected error from failing store")
	}
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected wrapped sentinel error, got %v", err)
	}
}
