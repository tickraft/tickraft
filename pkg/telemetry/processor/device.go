// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package processor

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/types"
)

// deviceErrorKeywords are the log keywords that indicate a device abnormality.
var deviceErrorKeywords = []string{"error", "exception", "fail", "panic", "fatal"}

// Device handles state machine transitions for device assets.
// It implements the telemetry.Processor interface.
//
// The device state machine:
//
//	unknown -> normal:   device reported normal data
//	unknown -> abnormal: device reported abnormal data
//	normal -> abnormal:  device metrics indicate issues
//	abnormal -> normal: device recovered
//	normal -> offline:   timeout (no telemetry received)
//	abnormal -> offline: timeout (no telemetry received)
//	offline -> normal:   device came back online with normal data
//	offline -> abnormal: device came back online with abnormal data
type Device struct {
	store  asset.Store
	bus    event.Bus
	logger *zap.Logger
}

// Compile-time assertion that Device implements telemetry.Processor.
var _ telemetry.Processor = (*Device)(nil)

// NewDevice creates a new Device processor.
//
// The store persists status transitions, and the bus publishes status-change
// events when a device times out.
func NewDevice(store asset.Store, bus event.Bus, logger *zap.Logger) *Device {
	return &Device{
		store:  store,
		bus:    bus,
		logger: logger,
	}
}

// Type returns the asset type this processor handles.
func (p *Device) Type() types.AssetType {
	return types.AssetTypeDevice
}

// Process handles a device telemetry and determines the new status.
//
// The status is derived from the telemetry in priority order: an explicitly set
// t.Status wins; otherwise abnormal metrics or error keywords in the log
// content yield StatusAbnormal; a received telemetry without error indicators
// defaults to StatusNormal. The derived status is persisted through the store
// and compared against the previous status to determine whether a transition
// occurred.
func (p *Device) Process(ctx context.Context, t *telemetry.Telemetry) (*telemetry.ProcessResult, error) {
	if t == nil {
		return nil, fmt.Errorf("telemetry is nil")
	}

	currStatus := determineDeviceStatus(t)
	prevStatus := p.lookupStatus(ctx, t.AssetID)

	// Note: status persistence is handled by the stateManager via the manager
	// pipeline (m.state.UpdateStatus). The Processor only computes the new
	// status and returns it; writing to the store here would cause a double
	// write because the manager also writes after Process returns.

	reason := fmt.Sprintf("device telemetry processed: status=%s", currStatus)

	alerts := buildDeviceAlerts(t, currStatus)

	return &telemetry.ProcessResult{
		PrevStatus: prevStatus,
		CurrStatus: currStatus,
		Reason:     reason,
		Alerts:     alerts,
	}, nil
}

// OnTimeout handles the device timeout scenario by marking the asset offline
// and publishing a status-change event.
func (p *Device) OnTimeout(ctx context.Context, assetID int64) error {
	return telemetry.MarkOffline(ctx, p.store, p.bus, p.logger, telemetry.OfflineParams{
		AssetID:   assetID,
		AssetType: types.AssetTypeDevice,
		Reason:    "device timeout",
	})
}

// lookupStatus returns the currently persisted status for the given asset,
// or StatusUnknown when the asset cannot be loaded.
func (p *Device) lookupStatus(ctx context.Context, assetID int64) types.AssetStatus {
	a, err := p.store.GetByID(ctx, assetID)
	if err != nil || a == nil {
		p.logger.Debug("could not load previous device status",
			zap.Int64("asset_id", assetID),
			zap.Error(err),
		)
		return types.AssetStatusUnknown
	}
	return a.Status
}

// determineDeviceStatus determines the new device status from the telemetry.
// Priority: explicit t.Status > abnormal metrics > error log keywords >
// StatusNormal.
func determineDeviceStatus(t *telemetry.Telemetry) types.AssetStatus {
	switch t.Status {
	case types.AssetStatusNormal, types.AssetStatusAbnormal, types.AssetStatusOffline:
		return t.Status
	case types.AssetStatusUnknown:
		// Unknown: derive the status from the telemetry payload below.
	}
	if hasAbnormalMetrics(t.Metrics) {
		return types.AssetStatusAbnormal
	}
	if hasErrorKeywords(t.LogContent) {
		return types.AssetStatusAbnormal
	}
	return types.AssetStatusNormal
}

// hasAbnormalMetrics checks if any metrics indicate an abnormal condition.
func hasAbnormalMetrics(metrics map[string]float64) bool {
	if metrics == nil {
		return false
	}
	if packetLoss, ok := metrics["packet_loss"]; ok && packetLoss > 50 {
		return true
	}
	if rtt, ok := metrics["rtt_ms"]; ok && rtt > 1000 {
		return true
	}
	return false
}

// hasErrorKeywords checks whether the log content contains any error keyword.
func hasErrorKeywords(logContent string) bool {
	if logContent == "" {
		return false
	}
	lower := strings.ToLower(logContent)
	for _, kw := range deviceErrorKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// buildDeviceAlerts generates alert contexts for the telemetry and derived status.
// Returns nil when no alerts are triggered.
func buildDeviceAlerts(t *telemetry.Telemetry, currStatus types.AssetStatus) []telemetry.AlertContext {
	var alerts []telemetry.AlertContext

	if currStatus == types.AssetStatusAbnormal {
		alerts = append(alerts, telemetry.AlertContext{
			Level:   string(types.SeverityWarning),
			Title:   "Device Abnormal",
			Message: fmt.Sprintf("Device %d reported abnormal state", t.AssetID),
		})
	}
	if currStatus == types.AssetStatusOffline {
		alerts = append(alerts, telemetry.AlertContext{
			Level:   string(types.SeverityCritical),
			Title:   "Device Offline",
			Message: fmt.Sprintf("Device %d is offline", t.AssetID),
		})
	}

	if t.Metrics != nil {
		alerts = append(alerts, checkMetricAlerts(t)...)
	}

	return alerts
}

// checkMetricAlerts generates alerts for metric threshold violations.
func checkMetricAlerts(t *telemetry.Telemetry) []telemetry.AlertContext {
	var alerts []telemetry.AlertContext

	if t.Metrics == nil {
		return alerts
	}

	if rtt, ok := t.Metrics["rtt_ms"]; ok && rtt > 500 {
		alerts = append(alerts, telemetry.AlertContext{
			Level:   string(types.SeverityWarning),
			Title:   "High Latency",
			Message: fmt.Sprintf("Device %d RTT %.0fms exceeds 500ms threshold", t.AssetID, rtt),
			Metadata: map[string]string{
				"metric":    "rtt_ms",
				"value":     fmt.Sprintf("%.0f", rtt),
				"threshold": "500",
			},
		})
	}

	if packetLoss, ok := t.Metrics["packet_loss"]; ok && packetLoss > 10 {
		alerts = append(alerts, telemetry.AlertContext{
			Level:   "warning",
			Title:   "Packet Loss",
			Message: fmt.Sprintf("Device %d packet loss %.0f%% exceeds 10%% threshold", t.AssetID, packetLoss),
			Metadata: map[string]string{
				"metric":    "packet_loss",
				"value":     fmt.Sprintf("%.0f", packetLoss),
				"threshold": "10",
			},
		})
	}

	return alerts
}
