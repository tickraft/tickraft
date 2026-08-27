// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/types"
)

func TestTelemetryFields(t *testing.T) {
	now := time.Now()
	tel := &Telemetry{
		AssetID:     100,
		AssetType:   types.AssetTypeHost,
		SourceType:  "webhook",
		RemoteAddr:  "10.0.0.1",
		CollectedAt: now,
		RawData:     []byte("ping reply"),
		Metrics:     map[string]float64{"rtt": 12.5},
		LogContent:  "host reachable",
		Status:      types.AssetStatusNormal,
	}

	if tel.AssetID != 100 {
		t.Errorf("AssetID: got %d, want 100", tel.AssetID)
	}
	if tel.AssetType != types.AssetTypeHost {
		t.Errorf("AssetType: got %q, want %q", tel.AssetType, types.AssetTypeHost)
	}
	if tel.SourceType != "webhook" {
		t.Errorf("SourceType: got %q, want %q", tel.SourceType, "webhook")
	}
	if tel.RemoteAddr != "10.0.0.1" {
		t.Errorf("RemoteAddr: got %q, want %q", tel.RemoteAddr, "10.0.0.1")
	}
	if !tel.CollectedAt.Equal(now) {
		t.Errorf("CollectedAt: got %v, want %v", tel.CollectedAt, now)
	}
	if string(tel.RawData) != "ping reply" {
		t.Errorf("RawData: got %q, want %q", string(tel.RawData), "ping reply")
	}
	if tel.Metrics["rtt"] != 12.5 {
		t.Errorf("Metrics[rtt]: got %f, want 12.5", tel.Metrics["rtt"])
	}
	if tel.LogContent != "host reachable" {
		t.Errorf("LogContent: got %q, want %q", tel.LogContent, "host reachable")
	}
	if tel.Status != types.AssetStatusNormal {
		t.Errorf("Status: got %q, want %q", tel.Status, types.AssetStatusNormal)
	}
}

func TestProcessResultFields(t *testing.T) {
	alerts := []AlertContext{
		{Level: "critical", Title: "Host Down", Message: "no response", Metadata: map[string]string{"ip": "10.0.0.1"}},
	}
	result := &ProcessResult{
		PrevStatus: types.AssetStatusNormal,
		CurrStatus: types.AssetStatusOffline,
		Reason:     "timeout exceeded",
		Alerts:     alerts,
	}

	if result.PrevStatus != types.AssetStatusNormal {
		t.Errorf("PrevStatus: got %q, want %q", result.PrevStatus, types.AssetStatusNormal)
	}
	if result.CurrStatus != types.AssetStatusOffline {
		t.Errorf("CurrStatus: got %q, want %q", result.CurrStatus, types.AssetStatusOffline)
	}
	if result.Reason != "timeout exceeded" {
		t.Errorf("Reason: got %q, want %q", result.Reason, "timeout exceeded")
	}
	if len(result.Alerts) != 1 {
		t.Fatalf("Alerts length: got %d, want 1", len(result.Alerts))
	}
	if result.Alerts[0].Level != "critical" {
		t.Errorf("Alert Level: got %q, want %q", result.Alerts[0].Level, "critical")
	}
	if result.Alerts[0].Title != "Host Down" {
		t.Errorf("Alert Title: got %q, want %q", result.Alerts[0].Title, "Host Down")
	}
	if result.Alerts[0].Message != "no response" {
		t.Errorf("Alert Message: got %q, want %q", result.Alerts[0].Message, "no response")
	}
	if result.Alerts[0].Metadata["ip"] != "10.0.0.1" {
		t.Errorf("Alert Metadata[ip]: got %q, want %q", result.Alerts[0].Metadata["ip"], "10.0.0.1")
	}
}

func TestAlertContextFields(t *testing.T) {
	alert := AlertContext{
		Level:    "warning",
		Title:    "High Latency",
		Message:  "RTT exceeded threshold",
		Metadata: map[string]string{"rtt_ms": "500"},
	}

	if alert.Level != "warning" {
		t.Errorf("Level: got %q, want %q", alert.Level, "warning")
	}
	if alert.Title != "High Latency" {
		t.Errorf("Title: got %q, want %q", alert.Title, "High Latency")
	}
	if alert.Message != "RTT exceeded threshold" {
		t.Errorf("Message: got %q, want %q", alert.Message, "RTT exceeded threshold")
	}
	if alert.Metadata["rtt_ms"] != "500" {
		t.Errorf("Metadata[rtt_ms]: got %q, want %q", alert.Metadata["rtt_ms"], "500")
	}
}
