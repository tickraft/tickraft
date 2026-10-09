// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// Persistence writes collected metrics and logs to their respective stores.
// Both stores are optional: a nil store makes the corresponding Persist method
// a no-op.
type Persistence struct {
	metricStore MetricStore
	logStore    LogStore
	logger      *zap.Logger
}

// NewPersistence creates a new Persistence backed by the given stores.
// Either store may be nil to disable that persistence path.
func NewPersistence(ms MetricStore, ls LogStore, logger *zap.Logger) *Persistence {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Persistence{
		metricStore: ms,
		logStore:    ls,
		logger:      logger,
	}
}

// PersistMetrics writes a batch of metric records. When the metric store is nil
// the call is a no-op.
func (p *Persistence) PersistMetrics(ctx context.Context, metrics []CollectMetric) error {
	if p.metricStore == nil || len(metrics) == 0 {
		return nil
	}
	ptrs := make([]*CollectMetric, len(metrics))
	for i := range metrics {
		ptrs[i] = &metrics[i]
	}
	if err := p.metricStore.SaveMetricsBatch(ctx, ptrs); err != nil {
		return fmt.Errorf("persist metrics: %w", err)
	}
	return nil
}

// PersistLogs writes a batch of log records. When the log store is nil the call
// is a no-op.
func (p *Persistence) PersistLogs(ctx context.Context, logs []CollectLog) error {
	if p.logStore == nil || len(logs) == 0 {
		return nil
	}
	ptrs := make([]*CollectLog, len(logs))
	for i := range logs {
		ptrs[i] = &logs[i]
	}
	if err := p.logStore.SaveLogsBatch(ctx, ptrs); err != nil {
		return fmt.Errorf("persist logs: %w", err)
	}
	return nil
}

// Persist writes both metrics and logs in a single call. Metric persistence is
// attempted first; if it fails, log persistence is skipped and the error is
// returned.
func (p *Persistence) Persist(ctx context.Context, metrics []CollectMetric, logs []CollectLog) error {
	if err := p.PersistMetrics(ctx, metrics); err != nil {
		return err
	}
	return p.PersistLogs(ctx, logs)
}
