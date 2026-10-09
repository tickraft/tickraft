// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/pool"
)

// processLoop is the main loop that drains the telemetry channel and
// dispatches each telemetry to the worker pool for concurrent processing.
//
// The loop itself is a single goroutine (per the architecture decision
// "keep single goroutine + internal pool for telemetry processing"): it
// only reads from telemetryCh and submits to reportPool. The pool provides
// the concurrency for the actual Validate -> Processor -> StateManager
// -> Emitter -> Aggregate -> Persist pipeline. If the pool rejects the
// submission (closed or caller context cancelled) the telemetry is
// processed synchronously as a fallback so no telemetry is silently
// dropped due to pool unavailability.
func (m *Engine) processLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-m.telemetryCh:
			m.dispatchReport(ctx, t)
		}
	}
}

// dispatchReport submits a single telemetry to the worker pool for
// processing. When the pool is unavailable or rejects the job the
// telemetry is processed inline to avoid silent data loss. The inline
// fallback is wrapped with a panic recover so a buggy processor cannot
// crash the processLoop goroutine.
func (m *Engine) dispatchReport(ctx context.Context, t *Telemetry) {
	if m.reportPool == nil {
		m.processReportSafe(ctx, t)
		return
	}
	err := m.reportPool.Submit(ctx, pool.Lambda(func(ctx context.Context) error {
		m.processReport(ctx, t)
		return nil
	}))
	if err != nil {
		m.logger.Warn("pool submit failed, processing telemetry synchronously",
			zap.Int64("asset_id", t.AssetID),
			zap.Error(err),
		)
		m.processReportSafe(ctx, t)
	}
}

// processReportSafe wraps processReport with panic recovery. It is used on
// the inline fallback path where the worker pool is unavailable; the pool
// itself already recovers panics for submitted jobs.
func (m *Engine) processReportSafe(ctx context.Context, t *Telemetry) {
	defer m.recoverPanic("processReport inline")
	m.processReport(ctx, t)
}

// processReport handles a single telemetry through the
// Validate -> Processor -> StateManager -> Emitter -> Aggregate -> Persist pipeline.
func (m *Engine) processReport(ctx context.Context, t *Telemetry) {
	// Validate the telemetry before any processing. Invalid telemetry is discarded.
	if m.validator != nil {
		if err := m.validator.Validate(ctx, t); err != nil {
			m.logger.Warn("telemetry validation failed, discarding",
				zap.Int64("asset_id", t.AssetID),
				zap.Error(err),
			)
			return
		}
	}

	// Look up the processor for this asset type.
	if m.processorRegistry == nil {
		m.logger.Warn("no processor registry configured")
		return
	}

	proc, err := m.processorRegistry.Lookup(t.AssetType)
	if err != nil {
		m.logger.Warn("no processor found for asset type",
			zap.String("asset_type", string(t.AssetType)),
			zap.Int64("asset_id", t.AssetID),
		)
		return
	}

	// Process the telemetry to determine the new status.
	result, err := proc.Process(ctx, t)
	if err != nil {
		m.logger.Error("processor failed",
			zap.String("asset_type", string(t.AssetType)),
			zap.Int64("asset_id", t.AssetID),
			zap.Error(err),
		)
		return
	}

	// Renew the timeout entry (heartbeat).
	m.state.UpdateActive(t.AssetID)

	// Always attempt to update status; StateManager determines if there was an actual change.
	prevStatus := m.state.GetStatus(t.AssetID)
	changed, err := m.state.UpdateStatus(ctx, t.AssetID, result.CurrStatus, result.Reason)
	if err != nil {
		m.logger.Error("failed to update status",
			zap.Int64("asset_id", t.AssetID),
			zap.Error(err),
		)
		return
	}

	// Fetch asset once if needed by either the status-change or alerts
	// branch. This avoids a duplicate store query when both branches need
	// the asset.
	var a *asset.Asset
	var fetchErr error
	if changed || len(result.Alerts) > 0 {
		a, fetchErr = m.store.GetByID(ctx, t.AssetID)
	}

	if changed {
		if fetchErr != nil {
			m.logger.Error("failed to fetch asset for event",
				zap.Int64("asset_id", t.AssetID),
				zap.Error(fetchErr),
			)
			a = &asset.Asset{ID: t.AssetID, AssetType: t.AssetType}
		}

		m.emitter.EmitStatusChange(ctx, event.StatusChangePayload{
			AssetID:    strconv.FormatInt(t.AssetID, 10),
			TenantID:   strconv.FormatInt(a.TenantID, 10),
			AssetType:  string(t.AssetType),
			AssetKey:   a.AssetKey,
			PrevStatus: string(prevStatus),
			CurrStatus: string(result.CurrStatus),
			Reason:     result.Reason,
			DetectedAt: time.Now().UnixNano(),
		})
	}

	// Emit any alerts from the processor.
	if len(result.Alerts) > 0 {
		if fetchErr != nil {
			m.logger.Error("failed to fetch asset for alerts",
				zap.Int64("asset_id", t.AssetID),
				zap.Error(fetchErr),
			)
		} else {
			m.emitter.EmitAlerts(ctx, result.Alerts, t.AssetID, a.TenantID)
		}
	}

	m.aggregateAndPersist(ctx, t)
}

// aggregateAndPersist aggregates the telemetry's metrics and persists its
// log content. Metrics are aggregated when an aggregator is configured;
// log content is persisted directly (logs are not aggregated).
func (m *Engine) aggregateAndPersist(ctx context.Context, t *Telemetry) {
	// Aggregate metrics when an aggregator is configured.
	if m.aggregator != nil && len(t.Metrics) > 0 {
		metrics := make([]Metric, 0, len(t.Metrics))
		for name, val := range t.Metrics {
			metrics = append(metrics, Metric{
				Name:      name,
				Value:     val,
				Timestamp: t.CollectedAt,
				TenantID:  t.TenantID,
			})
		}
		m.aggregator.Aggregate(ctx, t.AssetID, metrics)
	}

	// Persist log content directly (logs are not aggregated).
	if m.persistence != nil && t.LogContent != "" {
		level := t.LogLevel
		if level == "" {
			level = "INFO"
		}
		logModel := CollectLog{
			TenantID:  t.TenantID,
			AssetID:   t.AssetID,
			Level:     level,
			Content:   t.LogContent,
			SourceIP:  t.RemoteAddr,
			Timestamp: t.CollectedAt,
		}
		if err := m.persistence.PersistLogs(ctx, []CollectLog{logModel}); err != nil {
			m.logger.Error("failed to persist logs",
				zap.Int64("asset_id", t.AssetID),
				zap.Error(err),
			)
		}
	}
}

// consumeAggregated drains the aggregator flush channel and persists each
// aggregated metric batch. It runs until the context is cancelled.
func (m *Engine) consumeAggregated(ctx context.Context) {
	if m.aggregator == nil {
		return
	}
	flushCh := m.aggregator.FlushCh()
	for {
		select {
		case <-ctx.Done():
			return
		case am := <-flushCh:
			m.persistAggregated(ctx, am)
		}
	}
}

// persistAggregated converts an aggregated metric into persisted model records
// and writes them through the persistence layer. One aggregated metric produces
// five records suffixed with _avg, _max, _min, _count, and _sum.
func (m *Engine) persistAggregated(ctx context.Context, am *aggregatedMetric) {
	if m.persistence == nil {
		return
	}
	models := []CollectMetric{
		{TenantID: am.tenantID, AssetID: am.assetID,
			MetricName: am.metricName + "_avg", MetricValue: am.avg, Timestamp: am.windowEnd},
		{TenantID: am.tenantID, AssetID: am.assetID,
			MetricName: am.metricName + "_max", MetricValue: am.max, Timestamp: am.windowEnd},
		{TenantID: am.tenantID, AssetID: am.assetID,
			MetricName: am.metricName + "_min", MetricValue: am.min, Timestamp: am.windowEnd},
		{TenantID: am.tenantID, AssetID: am.assetID,
			MetricName: am.metricName + "_count", MetricValue: float64(am.count), Timestamp: am.windowEnd},
		{TenantID: am.tenantID, AssetID: am.assetID,
			MetricName: am.metricName + "_sum", MetricValue: am.sum, Timestamp: am.windowEnd},
	}
	if err := m.persistence.PersistMetrics(ctx, models); err != nil {
		m.logger.Error("failed to persist aggregated metrics",
			zap.Int64("asset_id", am.assetID),
			zap.String("metric_name", am.metricName),
			zap.Error(err),
		)
	}
}

// handleTimeout is the callback invoked when an asset times out. It runs on
// the time wheel goroutine; a panic in the processor chain is recovered so
// the time wheel keeps ticking for other assets.
func (m *Engine) handleTimeout(ctx context.Context, assetID int64) {
	defer m.recoverPanic("handleTimeout")

	// Look up the asset to determine its type.
	a, err := m.store.GetByID(ctx, assetID)
	if err != nil {
		m.logger.Error("failed to fetch asset for timeout handling",
			zap.Int64("asset_id", assetID),
			zap.Error(err),
		)
		return
	}

	// Look up the processor for this asset type.
	if m.processorRegistry == nil {
		m.logger.Warn("no processor registry configured for timeout")
		return
	}

	proc, err := m.processorRegistry.Lookup(a.AssetType)
	if err != nil {
		m.logger.Warn("no processor found for timeout",
			zap.String("asset_type", string(a.AssetType)),
			zap.Int64("asset_id", assetID),
		)
		return
	}

	// Delegate timeout handling to the processor.
	if err := proc.OnTimeout(ctx, assetID); err != nil {
		m.logger.Error("processor OnTimeout failed",
			zap.Int64("asset_id", assetID),
			zap.Error(err),
		)
	}

	// The processor may have transitioned the stored status out-of-band
	// (e.g. to offline). Refresh the cached status from the store so the
	// next report computes its transition against the persisted value
	// instead of a stale one.
	m.state.SyncStatus(ctx, assetID)
}
