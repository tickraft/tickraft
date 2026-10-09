// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/types"
)

// emitter publishes typed events through the event bus.
// It is the output layer of the four-layer telemetry architecture.
type emitter struct {
	bus    event.Bus
	logger *zap.Logger
}

// newEmitter creates a new emitter with the given event bus.
func newEmitter(bus event.Bus, logger *zap.Logger) *emitter {
	return &emitter{
		bus:    bus,
		logger: logger,
	}
}

// EmitStatusChange publishes a StatusChange event. The provided ctx controls
// cancellation of the publish call and is propagated to subscribers.
func (e *emitter) EmitStatusChange(ctx context.Context, payload event.StatusChangePayload) {
	if err := event.Publish(ctx, e.bus, event.TypeAssetStatusChanged, payload); err != nil {
		e.logger.Warn("failed to publish status change event",
			zap.String("asset_id", payload.AssetID),
			zap.String("prev_status", payload.PrevStatus),
			zap.String("curr_status", payload.CurrStatus),
			zap.Error(err),
		)
		return
	}
	e.logger.Info("emitted status change event",
		zap.String("asset_id", payload.AssetID),
		zap.String("prev_status", payload.PrevStatus),
		zap.String("curr_status", payload.CurrStatus),
	)
}

// EmitMetricAlert publishes a MetricAlert event. The provided ctx controls
// cancellation of the publish call and is propagated to subscribers.
func (e *emitter) EmitMetricAlert(ctx context.Context, payload event.MetricExceededPayload) {
	if err := event.Publish(ctx, e.bus, event.TypeTelemetryMetricExceeded, payload); err != nil {
		e.logger.Warn("failed to publish metric alert event",
			zap.String("asset_id", payload.AssetID),
			zap.String("metric_name", payload.MetricName),
			zap.Error(err),
		)
		return
	}
	e.logger.Info("emitted metric alert event",
		zap.String("asset_id", payload.AssetID),
		zap.String("metric_name", payload.MetricName),
	)
}

// EmitLogAlert publishes a LogAlert event. The provided ctx controls
// cancellation of the publish call and is propagated to subscribers.
func (e *emitter) EmitLogAlert(ctx context.Context, payload event.LogMatchedPayload) {
	if err := event.Publish(ctx, e.bus, event.TypeTelemetryLogMatched, payload); err != nil {
		e.logger.Warn("failed to publish log alert event",
			zap.String("asset_id", payload.AssetID),
			zap.String("level", payload.Level),
			zap.Error(err),
		)
		return
	}
	e.logger.Info("emitted log alert event",
		zap.String("asset_id", payload.AssetID),
		zap.String("level", payload.Level),
	)
}

// EmitAlerts converts alert contexts to typed events and publishes them. The
// provided ctx controls cancellation of each publish call.
func (e *emitter) EmitAlerts(ctx context.Context, alerts []AlertContext, assetID, tenantID int64) {
	assetIDStr := strconv.FormatInt(assetID, 10)
	tenantIDStr := strconv.FormatInt(tenantID, 10)
	detectedAt := time.Now().UnixNano()
	for _, alert := range alerts {
		switch alert.Level {
		case string(types.SeverityCritical), string(types.SeverityWarning):
			e.EmitMetricAlert(ctx, event.MetricExceededPayload{
				AssetID:     assetIDStr,
				TenantID:    tenantIDStr,
				MetricName:  alert.Title,
				MetricValue: metricMetadataValue(alert, "value"),
				Threshold:   metricMetadataValue(alert, "threshold"),
				Operator:    metricOperator(alert),
				Resources:   alertToMetrics(alert),
				Severity:    alert.Level,
				DetectedAt:  detectedAt,
			})
		default:
			e.EmitLogAlert(ctx, event.LogMatchedPayload{
				AssetID:    assetIDStr,
				TenantID:   tenantIDStr,
				Level:      alert.Level,
				Keyword:    alert.Title,
				Content:    alert.Message,
				SourceIP:   alert.SourceIP,
				Severity:   alert.Level,
				DetectedAt: detectedAt,
			})
		}
	}
}

// metricMetadataValue parses a float64 from alert.Metadata[key].
// Returns 0 when the key is absent or the value cannot be parsed.
func metricMetadataValue(alert AlertContext, key string) float64 {
	if alert.Metadata == nil {
		return 0
	}
	raw, ok := alert.Metadata[key]
	if !ok {
		return 0
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return f
}

// metricOperator resolves the comparison operator from alert.Metadata.
// Defaults to ">" (exceeds) when the "operator" key is absent, matching
// the "value exceeds threshold" semantics of the built-in processors.
func metricOperator(alert AlertContext) string {
	if alert.Metadata != nil {
		if op, ok := alert.Metadata["operator"]; ok && op != "" {
			return op
		}
	}
	return ">"
}

// alertToMetrics converts alert metadata to a float64 map for metric events.
func alertToMetrics(alert AlertContext) map[string]float64 {
	metrics := make(map[string]float64, len(alert.Metadata))
	for k, v := range alert.Metadata {
		// Store metadata values as-is; callers should interpret accordingly.
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			continue
		}
		if f != 0 {
			metrics[k] = f
		}
	}
	return metrics
}

// AlertContext describes an alert triggered during processing.
type AlertContext struct {
	// Level is the alert severity (info/warning/critical).
	Level string
	// Title is the alert title.
	Title string
	// Message is the alert detail.
	Message string
	// SourceIP is the origin address of the telemetry that triggered the alert,
	// used to populate LogMatchedPayload.SourceIP. Empty when unavailable.
	SourceIP string
	// Metadata holds additional alert information.
	// For metric alerts, conventionally populated keys are:
	//   - "metric":    the metric name (e.g. "rtt_ms")
	//   - "value":     the observed metric value (stringified float64)
	//   - "threshold": the threshold value (stringified float64)
	//   - "operator":  the comparison operator (e.g. ">"); defaults to ">" when absent
	Metadata map[string]string
}
