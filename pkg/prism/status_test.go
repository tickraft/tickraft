// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package prism

import (
	"context"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// statusPayloadToAlert: unit tests
// ---------------------------------------------------------------------------

// TestStatusPayloadToAlertHeartbeatLoss verifies that a status-change event
// whose Source is "timeout" (published by telemetry.MarkOffline on heartbeat
// loss) is mapped to a TypeHeartbeat alert carrying a ViolationKindHeartbeat
// violation with critical severity and a StatusContext recording the
// transition.
func TestStatusPayloadToAlertHeartbeatLoss(t *testing.T) {
	ev := event.Event[event.StatusChangePayload]{
		Timestamp: time.Unix(1700000000, 0).UTC(),
		Payload: event.StatusChangePayload{
			AssetID:    "42",
			TenantID:   "7",
			AssetKey:   "host-42",
			PrevStatus: "healthy",
			CurrStatus: "offline",
			Reason:     "no telemetry for 3m",
			Source:     "timeout",
		},
	}

	got, ok := statusPayloadToAlert(ev)
	if !ok {
		t.Fatal("expected ok=true for heartbeat-loss transition")
	}
	if got.Type != alert.TypeHeartbeat {
		t.Errorf("Type: got %q, want %q", got.Type, alert.TypeHeartbeat)
	}
	if got.AssetID != 42 {
		t.Errorf("AssetID: got %d, want 42", got.AssetID)
	}
	if got.TenantID != 7 {
		t.Errorf("TenantID: got %d, want 7", got.TenantID)
	}
	if len(got.Violations) != 1 {
		t.Fatalf("Violations: got %d items, want 1", len(got.Violations))
	}
	v := got.Violations[0]
	if v.Kind != alert.ViolationKindHeartbeat {
		t.Errorf("Kind: got %q, want %q", v.Kind, alert.ViolationKindHeartbeat)
	}
	if v.Severity != "critical" {
		t.Errorf("Severity: got %q, want %q", v.Severity, "critical")
	}
	if v.Source != "host-42" {
		t.Errorf("Source: got %q, want %q", v.Source, "host-42")
	}
	if v.Status == nil {
		t.Fatal("Status context is nil")
	}
	if v.Status.PrevStatus != "healthy" {
		t.Errorf("PrevStatus: got %q, want %q", v.Status.PrevStatus, "healthy")
	}
	if v.Status.CurrStatus != "offline" {
		t.Errorf("CurrStatus: got %q, want %q", v.Status.CurrStatus, "offline")
	}
	if v.Message == "" {
		t.Error("Message should be non-empty")
	}
}

// TestStatusPayloadToAlertStatusDegradation verifies that a non-heartbeat
// abnormal transition (Source != "timeout") is mapped to a TypeStatus alert
// carrying a ViolationKindStatus violation.
func TestStatusPayloadToAlertStatusDegradation(t *testing.T) {
	ev := event.Event[event.StatusChangePayload]{
		Timestamp: time.Unix(1700000001, 0).UTC(),
		Payload: event.StatusChangePayload{
			AssetID:    "10",
			TenantID:   "3",
			AssetKey:   "device-10",
			PrevStatus: "healthy",
			CurrStatus: "critical",
			Reason:     "probe failure",
			Source:     "prober",
		},
	}

	got, ok := statusPayloadToAlert(ev)
	if !ok {
		t.Fatal("expected ok=true for status degradation")
	}
	if got.Type != alert.TypeStatus {
		t.Errorf("Type: got %q, want %q", got.Type, alert.TypeStatus)
	}
	if got.AssetID != 10 {
		t.Errorf("AssetID: got %d, want 10", got.AssetID)
	}
	if len(got.Violations) != 1 {
		t.Fatalf("Violations: got %d items, want 1", len(got.Violations))
	}
	v := got.Violations[0]
	if v.Kind != alert.ViolationKindStatus {
		t.Errorf("Kind: got %q, want %q", v.Kind, alert.ViolationKindStatus)
	}
	if v.Severity != "error" {
		t.Errorf("Severity: got %q, want %q", v.Severity, "error")
	}
	if v.Status == nil {
		t.Fatal("Status context is nil")
	}
	if v.Status.PrevStatus != "healthy" {
		t.Errorf("PrevStatus: got %q, want %q", v.Status.PrevStatus, "healthy")
	}
	if v.Status.CurrStatus != "critical" {
		t.Errorf("CurrStatus: got %q, want %q", v.Status.CurrStatus, "critical")
	}
}

// TestStatusPayloadToAlertRecoverySkipped verifies that transitions to a
// non-abnormal state (healthy, unknown) return ok=false so the engine does
// not emit alert noise for recoveries.
func TestStatusPayloadToAlertRecoverySkipped(t *testing.T) {
	for _, curr := range []string{"healthy", "unknown", ""} {
		ev := event.Event[event.StatusChangePayload]{
			Payload: event.StatusChangePayload{
				AssetID:    "42",
				CurrStatus: curr,
				Source:     "prober",
			},
		}
		if _, ok := statusPayloadToAlert(ev); ok {
			t.Errorf("expected ok=false for recovery to %q", curr)
		}
	}
}

// TestStatusPayloadToAlertWarningStatus verifies that a transition to
// "warning" (an abnormal but non-offline state) still produces a TypeStatus
// alert, covering the middle tier of the severity spectrum.
func TestStatusPayloadToAlertWarningStatus(t *testing.T) {
	ev := event.Event[event.StatusChangePayload]{
		Payload: event.StatusChangePayload{
			AssetID:    "55",
			TenantID:   "1",
			PrevStatus: "healthy",
			CurrStatus: "warning",
			Source:     "listener",
		},
	}
	got, ok := statusPayloadToAlert(ev)
	if !ok {
		t.Fatal("expected ok=true for warning transition")
	}
	if got.Type != alert.TypeStatus {
		t.Errorf("Type: got %q, want %q", got.Type, alert.TypeStatus)
	}
	if got.Violations[0].Kind != alert.ViolationKindStatus {
		t.Errorf("Kind: got %q, want %q", got.Violations[0].Kind, alert.ViolationKindStatus)
	}
}

// TestStatusPayloadToAlertZeroTimestamp verifies that a zero event timestamp
// is replaced with time.Now() so the resulting alert always carries a valid
// timestamp for downstream consumers.
func TestStatusPayloadToAlertZeroTimestamp(t *testing.T) {
	ev := event.Event[event.StatusChangePayload]{
		Payload: event.StatusChangePayload{
			AssetID:    "1",
			CurrStatus: "offline",
			Source:     "timeout",
		},
	}
	got, ok := statusPayloadToAlert(ev)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp when event timestamp is zero")
	}
}

// TestStatusPayloadToAlertMessageContainsReason verifies that the Reason
// field is appended to the violation message when present, giving operators
// context for the transition.
func TestStatusPayloadToAlertMessageContainsReason(t *testing.T) {
	ev := event.Event[event.StatusChangePayload]{
		Payload: event.StatusChangePayload{
			AssetID:    "1",
			CurrStatus: "offline",
			Reason:     "probe timeout after 30s",
			Source:     "timeout",
		},
	}
	got, _ := statusPayloadToAlert(ev)
	if got.Violations[0].Message == "" {
		t.Fatal("expected non-empty message")
	}
}

// ---------------------------------------------------------------------------
// Engine: status-change event dispatch integration tests
// ---------------------------------------------------------------------------

// TestHeartbeatAlertDispatchedToChannel verifies the end-to-end flow: a
// StatusChangePayload event with Source="timeout" (heartbeat loss) published
// on the event bus is picked up by the alert engine, normalized into a
// TypeHeartbeat alert, and dispatched to a registered channel.
func TestHeartbeatAlertDispatchedToChannel(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	ch := &recordingChannel{name: "recorder"}
	eng, err := New(WithEventBus(bus), WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	eng.AddChannel(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stopEngine(t, eng)

	_ = event.Publish(context.Background(), bus, event.TypeAssetStatusChanged, event.StatusChangePayload{
		AssetID:    "42",
		TenantID:   "7",
		AssetKey:   "host-42",
		PrevStatus: "healthy",
		CurrStatus: "offline",
		Reason:     "no heartbeat for 3m",
		Source:     "timeout",
	})

	waitFor(t, func() bool { return ch.len() >= 1 }, 2*time.Second)

	alerts := ch.snapshot()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	a := alerts[0]
	if a.Type != alert.TypeHeartbeat {
		t.Errorf("Type: got %q, want %q", a.Type, alert.TypeHeartbeat)
	}
	if a.AssetID != 42 {
		t.Errorf("AssetID: got %d, want 42", a.AssetID)
	}
	if len(a.Violations) != 1 {
		t.Fatalf("violations: got %d items, want 1", len(a.Violations))
	}
	v := a.Violations[0]
	if v.Kind != alert.ViolationKindHeartbeat {
		t.Errorf("Kind: got %q, want %q", v.Kind, alert.ViolationKindHeartbeat)
	}
	if v.Severity != "critical" {
		t.Errorf("Severity: got %q, want %q", v.Severity, "critical")
	}
	if v.Status == nil {
		t.Fatal("Status context is nil")
	}
	if v.Status.PrevStatus != "healthy" {
		t.Errorf("PrevStatus: got %q, want %q", v.Status.PrevStatus, "healthy")
	}
	if v.Status.CurrStatus != "offline" {
		t.Errorf("CurrStatus: got %q, want %q", v.Status.CurrStatus, "offline")
	}
}

// TestStatusAlertDispatchedToChannel verifies the end-to-end flow: a
// StatusChangePayload event with Source="prober" (status degradation) is
// normalized into a TypeStatus alert and dispatched to a registered channel.
func TestStatusAlertDispatchedToChannel(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	ch := &recordingChannel{name: "recorder"}
	eng, err := New(WithEventBus(bus), WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	eng.AddChannel(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stopEngine(t, eng)

	_ = event.Publish(context.Background(), bus, event.TypeAssetStatusChanged, event.StatusChangePayload{
		AssetID:    "10",
		TenantID:   "3",
		AssetKey:   "device-10",
		PrevStatus: "warning",
		CurrStatus: "critical",
		Reason:     "probe failure",
		Source:     "prober",
	})

	waitFor(t, func() bool { return ch.len() >= 1 }, 2*time.Second)

	alerts := ch.snapshot()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	a := alerts[0]
	if a.Type != alert.TypeStatus {
		t.Errorf("Type: got %q, want %q", a.Type, alert.TypeStatus)
	}
	if a.AssetID != 10 {
		t.Errorf("AssetID: got %d, want 10", a.AssetID)
	}
	if len(a.Violations) != 1 {
		t.Fatalf("violations: got %d items, want 1", len(a.Violations))
	}
	v := a.Violations[0]
	if v.Kind != alert.ViolationKindStatus {
		t.Errorf("Kind: got %q, want %q", v.Kind, alert.ViolationKindStatus)
	}
	if v.Severity != "error" {
		t.Errorf("Severity: got %q, want %q", v.Severity, "error")
	}
}

// TestRecoveryNotDispatched verifies that a transition to a healthy state
// (recovery) does NOT produce an alert, so channels are not spammed with
// noise when assets return to normal.
func TestRecoveryNotDispatched(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	ch := &recordingChannel{name: "recorder"}
	eng, err := New(WithEventBus(bus), WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	eng.AddChannel(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stopEngine(t, eng)

	_ = event.Publish(context.Background(), bus, event.TypeAssetStatusChanged, event.StatusChangePayload{
		AssetID:    "42",
		TenantID:   "7",
		PrevStatus: "offline",
		CurrStatus: "healthy",
		Reason:     "recovered",
		Source:     "prober",
	})

	// Wait briefly to confirm no alert is dispatched.
	time.Sleep(200 * time.Millisecond)
	if got := ch.len(); got != 0 {
		alerts := ch.snapshot()
		t.Errorf("expected 0 alerts for recovery, got %d (first type=%q)", got, alerts[0].Type)
	}
}

// TestHeartbeatAndStatusBothDispatched verifies that both heartbeat-loss and
// status-degradation events dispatched in sequence each produce their
// respective alert types on the channel, confirming the engine handles
// mixed violation kinds correctly.
func TestHeartbeatAndStatusBothDispatched(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	ch := &recordingChannel{name: "recorder"}
	eng, err := New(WithEventBus(bus), WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	eng.AddChannel(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stopEngine(t, eng)

	// Publish heartbeat loss.
	_ = event.Publish(context.Background(), bus, event.TypeAssetStatusChanged, event.StatusChangePayload{
		AssetID:    "1",
		TenantID:   "1",
		PrevStatus: "healthy",
		CurrStatus: "offline",
		Source:     "timeout",
	})
	// Publish status degradation.
	_ = event.Publish(context.Background(), bus, event.TypeAssetStatusChanged, event.StatusChangePayload{
		AssetID:    "2",
		TenantID:   "1",
		PrevStatus: "healthy",
		CurrStatus: "critical",
		Source:     "prober",
	})

	waitFor(t, func() bool { return ch.len() >= 2 }, 2*time.Second)

	alerts := ch.snapshot()
	if len(alerts) != 2 {
		t.Fatalf("expected 2 alerts, got %d", len(alerts))
	}

	kinds := make(map[string]int)
	types := make(map[alert.Type]int)
	for _, a := range alerts {
		types[a.Type]++
		for _, v := range a.Violations {
			kinds[v.Kind]++
		}
	}
	if types[alert.TypeHeartbeat] != 1 {
		t.Errorf("expected 1 TypeHeartbeat alert, got %d", types[alert.TypeHeartbeat])
	}
	if types[alert.TypeStatus] != 1 {
		t.Errorf("expected 1 TypeStatus alert, got %d", types[alert.TypeStatus])
	}
	if kinds[alert.ViolationKindHeartbeat] != 1 {
		t.Errorf("expected 1 ViolationKindHeartbeat, got %d", kinds[alert.ViolationKindHeartbeat])
	}
	if kinds[alert.ViolationKindStatus] != 1 {
		t.Errorf("expected 1 ViolationKindStatus, got %d", kinds[alert.ViolationKindStatus])
	}
}
