// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package prism

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/pool"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// Engine: construction and lifecycle
// ---------------------------------------------------------------------------

// TestNewDefaults verifies that New returns a non-nil engine with default
// settings when no options are provided.
func TestNewDefaults(t *testing.T) {
	eng, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if eng == nil {
		t.Fatal("expected non-nil engine")
	}
	if eng.notifyPool == nil {
		t.Error("expected non-nil notification pool")
	}
	if !eng.poolOwned {
		t.Error("expected engine to own the pool when none injected")
	}
	if len(eng.Channels()) != 0 {
		t.Errorf("expected 0 channels, got %d", len(eng.Channels()))
	}
	if len(eng.Rules()) != 0 {
		t.Errorf("expected 0 rules, got %d", len(eng.Rules()))
	}
}

// TestNewWithPool verifies that an externally-injected pool is used and not
// shut down by the engine.
func TestNewWithPool(t *testing.T) {
	externalPool, err := pool.New(pool.WithWorkers(2))
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	defer func() { _ = externalPool.Shutdown(context.Background()) }()

	eng, err := New(WithPool(externalPool))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if eng.notifyPool != externalPool {
		t.Error("expected engine to use the injected pool")
	}
	if eng.poolOwned {
		t.Error("expected engine to not own the injected pool")
	}
}

// TestStartWithoutBusReturnsError verifies that Start fails when no event bus
// is configured.
func TestStartWithoutBusReturnsError(t *testing.T) {
	eng, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	err = eng.Start(context.Background())
	if !errors.Is(err, errdefs.ErrBusNotConfigured) {
		t.Errorf("expected ErrBusNotConfigured, got %v", err)
	}
}

// TestStartStopIdempotent verifies that Start and Stop are idempotent.
func TestStartStopIdempotent(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	eng, err := New(WithEventBus(bus), WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := eng.Start(ctx); err != nil {
		t.Fatalf("first start: %v", err)
	}
	// Second start is a no-op.
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("second start: %v", err)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := eng.Stop(stopCtx); err != nil {
		t.Fatalf("first stop: %v", err)
	}
	// Second stop is a no-op.
	if err := eng.Stop(stopCtx); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Engine: rule evaluation and dispatch
// ---------------------------------------------------------------------------

// recordingChannel is a test Channel that records received alerts.
type recordingChannel struct {
	mu     sync.Mutex
	alerts []alert.Event
	name   string
	err    error
}

func (r *recordingChannel) Send(_ context.Context, evt alert.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.alerts = append(r.alerts, evt)
	return nil
}

func (r *recordingChannel) Name() string { return r.name }

func (r *recordingChannel) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.alerts)
}

func (r *recordingChannel) snapshot() []alert.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]alert.Event, len(r.alerts))
	copy(out, r.alerts)
	return out
}

// TestMetricAlertDispatchedToChannel verifies that a metric alert event on
// the bus is dispatched to a registered channel.
func TestMetricAlertDispatchedToChannel(t *testing.T) {
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

	_ = event.Publish(context.Background(), bus, event.TypeTelemetryMetricExceeded, event.MetricExceededPayload{
		AssetID:     "42",
		TenantID:    "7",
		MetricName:  "cpu_usage",
		MetricValue: 95.5,
		Threshold:   90.0,
		Operator:    ">",
	})

	waitFor(t, func() bool { return ch.len() >= 1 }, 2*time.Second)

	alerts := ch.snapshot()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	a := alerts[0]
	if a.Type != alert.TypeMetric {
		t.Errorf("type: got %q, want %q", a.Type, alert.TypeMetric)
	}
	if a.AssetID != 42 {
		t.Errorf("asset_id: got %d, want 42", a.AssetID)
	}
	if len(a.Violations) != 1 {
		t.Fatalf("violations: got %d items, want 1", len(a.Violations))
	}
	if a.Violations[0].Metric == nil || a.Violations[0].Metric.Name != "cpu_usage" {
		t.Errorf("metric: got %+v, want %q", a.Violations[0].Metric, "cpu_usage")
	}
	if a.Violations[0].Metric.Value != 95.5 {
		t.Errorf("value: got %f, want 95.5", a.Violations[0].Metric.Value)
	}
}

// TestLogAlertDispatchedToChannel verifies that a log alert event on the
// bus is dispatched to a registered channel.
func TestLogAlertDispatchedToChannel(t *testing.T) {
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

	_ = event.Publish(context.Background(), bus, event.TypeTelemetryLogMatched, event.LogMatchedPayload{
		AssetID:  "10",
		TenantID: "3",
		Level:    "ERROR",
		Keyword:  "OOM",
		Content:  "Out of memory",
		SourceIP: "10.0.0.1",
	})

	waitFor(t, func() bool { return ch.len() >= 1 }, 2*time.Second)

	alerts := ch.snapshot()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	a := alerts[0]
	if a.Type != alert.TypeLog {
		t.Errorf("type: got %q, want %q", a.Type, alert.TypeLog)
	}
	if len(a.Violations) != 1 {
		t.Fatalf("violations: got %d items, want 1", len(a.Violations))
	}
	if a.Violations[0].Severity != "ERROR" {
		t.Errorf("level: got %q, want %q", a.Violations[0].Severity, "ERROR")
	}
	if a.Violations[0].Log == nil || a.Violations[0].Log.Keyword != "OOM" {
		t.Errorf("keyword: got %+v, want %q", a.Violations[0].Log, "OOM")
	}
	if a.Violations[0].Source != "10.0.0.1" {
		t.Errorf("source: got %q, want %q", a.Violations[0].Source, "10.0.0.1")
	}
}

// TestRuleSuppressesAlert verifies that a rule returning false suppresses
// alert dispatch.
func TestRuleSuppressesAlert(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	ch := &recordingChannel{name: "recorder"}
	eng, err := New(WithEventBus(bus), WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	eng.AddChannel(ch)
	// Matcher that suppresses all alerts.
	eng.AddRule(MatcherFunc(func(_ context.Context, _ alert.Event) bool {
		return false
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stopEngine(t, eng)

	_ = event.Publish(context.Background(), bus, event.TypeTelemetryMetricExceeded, event.MetricExceededPayload{
		AssetID:    "1",
		TenantID:   "1",
		MetricName: "cpu",
	})

	// Give the dispatch path time to run (it should not).
	time.Sleep(200 * time.Millisecond)
	if ch.len() != 0 {
		t.Errorf("expected 0 alerts (suppressed), got %d", ch.len())
	}
}

// TestRuleMatchesAlert verifies that a matching rule forwards the alert.
func TestRuleMatchesAlert(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	ch := &recordingChannel{name: "recorder"}
	eng, err := New(WithEventBus(bus), WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	eng.AddChannel(ch)
	// Matcher that matches only metric alerts for asset 42.
	eng.AddRule(MatcherFunc(func(_ context.Context, a alert.Event) bool {
		return a.Type == alert.TypeMetric && a.AssetID == 42
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stopEngine(t, eng)

	// Matching alert.
	_ = event.Publish(context.Background(), bus, event.TypeTelemetryMetricExceeded, event.MetricExceededPayload{
		AssetID:    "42",
		TenantID:   "1",
		MetricName: "cpu",
	})
	// Non-matching alert (wrong asset).
	_ = event.Publish(context.Background(), bus, event.TypeTelemetryMetricExceeded, event.MetricExceededPayload{
		AssetID:    "99",
		TenantID:   "1",
		MetricName: "cpu",
	})

	waitFor(t, func() bool { return ch.len() >= 1 }, 2*time.Second)
	time.Sleep(200 * time.Millisecond) // ensure second alert is processed

	alerts := ch.snapshot()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert (matched), got %d", len(alerts))
	}
	if alerts[0].AssetID != 42 {
		t.Errorf("asset_id: got %d, want 42", alerts[0].AssetID)
	}
}

// TestNoChannelsLogsAlert verifies that an alert with no channels registered
// does not panic and is silently observed via logs.
func TestNoChannelsLogsAlert(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	eng, err := New(WithEventBus(bus), WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stopEngine(t, eng)

	// Should not panic.
	_ = event.Publish(context.Background(), bus, event.TypeTelemetryMetricExceeded, event.MetricExceededPayload{
		AssetID:    "1",
		TenantID:   "1",
		MetricName: "cpu",
	})
	// Give the dispatch path time to run.
	time.Sleep(100 * time.Millisecond)
}

// TestMultipleChannelsAllNotified verifies that all registered channels
// receive the alert.
func TestMultipleChannelsAllNotified(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	ch1 := &recordingChannel{name: "ch1"}
	ch2 := &recordingChannel{name: "ch2"}
	eng, err := New(WithEventBus(bus), WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	eng.AddChannel(ch1)
	eng.AddChannel(ch2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stopEngine(t, eng)

	_ = event.Publish(context.Background(), bus, event.TypeTelemetryMetricExceeded, event.MetricExceededPayload{
		AssetID:    "1",
		TenantID:   "1",
		MetricName: "cpu",
	})

	waitFor(t, func() bool { return ch1.len() >= 1 && ch2.len() >= 1 }, 2*time.Second)
	if ch1.len() != 1 {
		t.Errorf("ch1: expected 1 alert, got %d", ch1.len())
	}
	if ch2.len() != 1 {
		t.Errorf("ch2: expected 1 alert, got %d", ch2.len())
	}
}

// TestChannelSendErrorLogged verifies that a channel Send error is logged
// and does not crash the engine.
func TestChannelSendErrorLogged(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	ch := &recordingChannel{name: "failing", err: errors.New("send failed")}
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

	_ = event.Publish(context.Background(), bus, event.TypeTelemetryMetricExceeded, event.MetricExceededPayload{
		AssetID:    "1",
		TenantID:   "1",
		MetricName: "cpu",
	})

	// The error is logged; give it time to process.
	time.Sleep(200 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// Event conversion
// ---------------------------------------------------------------------------

// TestMetricPayloadToAlert verifies the conversion from a metric alert
// payload to an Event.
func TestMetricPayloadToAlert(t *testing.T) {
	ts := time.Now()
	ev := event.Event[event.MetricExceededPayload]{
		Type: event.TypeTelemetryMetricExceeded,
		Payload: event.MetricExceededPayload{
			AssetID:     "5",
			TenantID:    "50",
			MetricName:  "cpu",
			MetricValue: 95.0,
			Threshold:   90.0,
			Operator:    ">",
			Resources:   map[string]float64{"cpu": 95.0},
		},
		Timestamp: ts,
	}
	a := metricPayloadToAlert(ev)
	if a.Type != alert.TypeMetric {
		t.Errorf("type: got %q, want %q", a.Type, alert.TypeMetric)
	}
	if a.AssetID != 5 {
		t.Errorf("asset_id: got %d, want 5", a.AssetID)
	}
	if a.Timestamp != ts {
		t.Errorf("timestamp: got %v, want %v", a.Timestamp, ts)
	}
	if a.Violations[0].Metric == nil || a.Violations[0].Metric.Metrics["cpu"] != 95.0 {
		t.Errorf("violations[0].metric.resources[cpu]: got %+v, want 95.0", a.Violations[0].Metric)
	}
	if len(a.Violations) != 1 {
		t.Fatalf("violations: got %d items, want 1", len(a.Violations))
	}
	v := a.Violations[0]
	if v.Kind != alert.ViolationKindMetric {
		t.Errorf("violations[0].kind: got %q, want %q", v.Kind, alert.ViolationKindMetric)
	}
	if v.Metric == nil || v.Metric.Name != "cpu" {
		t.Errorf("violations[0].metric: got %+v, want %q", v.Metric, "cpu")
	}
	if v.Metric.Value != 95.0 {
		t.Errorf("violations[0].value: got %f, want 95.0", v.Metric.Value)
	}
	if v.Metric.Threshold != 90.0 {
		t.Errorf("violations[0].threshold: got %f, want 90.0", v.Metric.Threshold)
	}
}

// TestLogPayloadToAlert verifies the conversion from a log alert payload
// to an Event.
func TestLogPayloadToAlert(t *testing.T) {
	ev := event.Event[event.LogMatchedPayload]{
		Type: event.TypeTelemetryLogMatched,
		Payload: event.LogMatchedPayload{
			AssetID:  "3",
			TenantID: "30",
			Level:    "ERROR",
			Keyword:  "OOM",
			Content:  "out of memory",
			SourceIP: "10.0.0.1",
		},
	}
	a := logPayloadToAlert(ev)
	if a.Type != alert.TypeLog {
		t.Errorf("type: got %q, want %q", a.Type, alert.TypeLog)
	}
	if a.Violations[0].Severity != "ERROR" {
		t.Errorf("level: got %q, want %q", a.Violations[0].Severity, "ERROR")
	}
	if a.Timestamp.IsZero() {
		t.Error("timestamp should be auto-filled when zero")
	}
	if len(a.Violations) != 1 {
		t.Fatalf("violations: got %d items, want 1", len(a.Violations))
	}
	v := a.Violations[0]
	if v.Kind != alert.ViolationKindLog {
		t.Errorf("violations[0].kind: got %q, want %q", v.Kind, alert.ViolationKindLog)
	}
	if v.Severity != "ERROR" {
		t.Errorf("violations[0].level: got %q, want %q", v.Severity, "ERROR")
	}
	if v.Log == nil || v.Log.Keyword != "OOM" {
		t.Errorf("violations[0].keyword: got %+v, want %q", v.Log, "OOM")
	}
	if v.Log == nil || v.Log.Content != "out of memory" {
		t.Errorf("violations[0].content: got %+v, want %q", v.Log, "out of memory")
	}
	if v.Source != "10.0.0.1" {
		t.Errorf("violations[0].source: got %q, want %q", v.Source, "10.0.0.1")
	}
}

// TestEventJSON verifies the JSON serialization of Event.
func TestEventJSON(t *testing.T) {
	a := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		TenantID:   2,
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu", Value: 90, Threshold: 80}}},
	}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got alert.Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != a.Type {
		t.Errorf("type: got %q, want %q", got.Type, a.Type)
	}
	if got.AssetID != a.AssetID {
		t.Errorf("asset_id: got %d, want %d", got.AssetID, a.AssetID)
	}
}

// ---------------------------------------------------------------------------
// MatcherFunc
// ---------------------------------------------------------------------------

// TestMatcherFunc verifies that MatcherFunc adapts a function into a Matcher.
func TestMatcherFunc(t *testing.T) {
	called := atomic.Bool{}
	r := MatcherFunc(func(_ context.Context, _ alert.Event) bool {
		called.Store(true)
		return true
	})
	if !r.Match(context.Background(), alert.Event{}) {
		t.Error("expected MatcherFunc to return true")
	}
	if !called.Load() {
		t.Error("MatcherFunc was not called")
	}
}

// TestAddRuleNilIsNoop verifies that AddRule(nil) is a no-op.
func TestAddRuleNilIsNoop(t *testing.T) {
	eng, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	eng.AddRule(nil)
	if len(eng.Rules()) != 0 {
		t.Errorf("expected 0 rules, got %d", len(eng.Rules()))
	}
}

// TestAddChannelNilIsNoop verifies that AddChannel(nil) is a no-op.
func TestAddChannelNilIsNoop(t *testing.T) {
	eng, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	eng.AddChannel(nil)
	if len(eng.Channels()) != 0 {
		t.Errorf("expected 0 channels, got %d", len(eng.Channels()))
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// stopEngine stops the engine with a timeout, failing the test on error.
func stopEngine(t *testing.T, eng *Engine) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := eng.Stop(ctx); err != nil {
		t.Errorf("stop engine: %v", err)
	}
}

// waitFor polls cond until it returns true or the timeout expires.
func waitFor(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not met within %v", timeout)
	}
}

// ---------------------------------------------------------------------------
// Dispatch: EventID propagation
// ---------------------------------------------------------------------------

// TestDispatchEventIDPropagatedToChannel verifies that the EventID assigned
// by Dispatch is stamped on the Event received by channels, so channels
// can correlate delivery records with the engine's tracking identifier.
func TestDispatchEventIDPropagatedToChannel(t *testing.T) {
	eng, err := New(WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ch := &recordingChannel{name: "recorder"}
	eng.AddChannel(ch)

	res := eng.Dispatch(context.Background(), alert.Event{Type: alert.TypeMetric, AssetID: 1})
	if !res.Accepted {
		t.Fatal("expected alert to be accepted")
	}
	if res.EventID == "" {
		t.Fatal("expected non-empty EventID in DispatchResult")
	}

	waitFor(t, func() bool { return ch.len() >= 1 }, 2*time.Second)
	alerts := ch.snapshot()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].EventID != res.EventID {
		t.Errorf("channel received EventID %q, want %q", alerts[0].EventID, res.EventID)
	}
}

// TestDispatchEventIDStableAcrossSuppression verifies that the EventID is
// assigned even when the alert is suppressed by rules, so callers can
// correlate suppressed alerts with their tracking records.
func TestDispatchEventIDStableAcrossSuppression(t *testing.T) {
	eng, err := New(WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	eng.AddRule(MatcherFunc(func(_ context.Context, _ alert.Event) bool {
		return false
	}))

	res := eng.Dispatch(context.Background(), alert.Event{Type: alert.TypeMetric})
	if res.Accepted {
		t.Fatal("expected alert to be suppressed")
	}
	if res.EventID == "" {
		t.Error("expected non-empty EventID for suppressed alert")
	}
}

// ---------------------------------------------------------------------------
// Dispatch: rule panic isolation
// ---------------------------------------------------------------------------

// panickingRule is a Matcher implementation whose Match method always panics.
type panickingRule struct{ name string }

func (p *panickingRule) Match(_ context.Context, _ alert.Event) bool {
	panic("boom from panickingRule")
}

func (p *panickingRule) Name() string { return p.name }

// TestRulePanicDoesNotCrashEngine verifies that a panicking custom Matcher is
// recovered by match, treated as not matching, and does not crash the
// engine. A second healthy rule should still be evaluated and match.
func TestRulePanicDoesNotCrashEngine(t *testing.T) {
	eng, err := New(WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ch := &recordingChannel{name: "recorder"}
	eng.AddChannel(ch)
	// First rule panics; second rule matches.
	eng.AddRule(&panickingRule{name: "panicker"})
	eng.AddRule(MatcherFunc(func(_ context.Context, _ alert.Event) bool {
		return true
	}))

	res := eng.Dispatch(context.Background(), alert.Event{Type: alert.TypeMetric, AssetID: 1})
	if !res.Accepted {
		t.Fatal("expected alert to be accepted via the healthy rule")
	}
	// The panicking rule is omitted from MatchedRules because match
	// treats it as not matching.
	for _, name := range res.MatchedRules {
		if name == "panicker" {
			t.Errorf("panicking rule should not appear in MatchedRules: %v", res.MatchedRules)
		}
	}

	waitFor(t, func() bool { return ch.len() >= 1 }, 2*time.Second)
	if ch.len() != 1 {
		t.Errorf("expected 1 alert delivered, got %d", ch.len())
	}
}

// violationMatcherRule is a test rule that implements both Matcher and
// ViolationMatcher. It always matches and returns a fixed set of
// violations, simulating a compound rule that matched multiple conditions.
type violationMatcherRule struct {
	name       string
	violations []alert.Violation
}

func (r *violationMatcherRule) Match(_ context.Context, _ alert.Event) bool { return true }
func (r *violationMatcherRule) Name() string                                { return r.name }
func (r *violationMatcherRule) MatchWithViolations(_ context.Context, _ alert.Event) []alert.Violation {
	return r.violations
}

// TestDispatchCollectsViolationsFromViolationMatcher verifies that Dispatch
// calls MatchWithViolations on rules implementing ViolationMatcher and
// replaces the payload-populated Event.Violations with the rule engine's
// violations. This enables compound rules (e.g. "cpu > 90 && mem > 85")
// to contribute one Violation per matched condition.
func TestDispatchCollectsViolationsFromViolationMatcher(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	ch := &recordingChannel{name: "recorder"}
	eng, err := New(WithEventBus(bus), WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	eng.AddChannel(ch)
	// Register a ViolationMatcher rule that returns two violations,
	// simulating a compound rule match.
	expected := []alert.Violation{
		{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu", Value: 95, Threshold: 90}},
		{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "mem", Value: 88, Threshold: 85}},
	}
	eng.AddRule(&violationMatcherRule{name: "compound", violations: expected})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stopEngine(t, eng)

	_ = event.Publish(context.Background(), bus, event.TypeTelemetryMetricExceeded, event.MetricExceededPayload{
		AssetID:    "1",
		TenantID:   "1",
		MetricName: "cpu",
	})

	waitFor(t, func() bool { return ch.len() >= 1 }, 2*time.Second)

	alerts := ch.snapshot()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	a := alerts[0]
	if len(a.Violations) != 2 {
		t.Fatalf("expected 2 violations from compound rule, got %d: %+v", len(a.Violations), a.Violations)
	}
	for i, v := range expected {
		got := a.Violations[i]
		if got.Metric == nil || got.Metric.Name != v.Metric.Name {
			t.Errorf("violation[%d].metric: got %v, want %q", i, got.Metric, v.Metric.Name)
		}
		if got.Metric == nil || got.Metric.Value != v.Metric.Value {
			t.Errorf("violation[%d].value: got %v, want %f", i, got.Metric, v.Metric.Value)
		}
	}
}

// TestDispatchPreservesPayloadViolationsWithoutViolationMatcher verifies
// that when no rule implements ViolationMatcher, the payload-populated
// Event.Violations are preserved unchanged.
func TestDispatchPreservesPayloadViolationsWithoutViolationMatcher(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	ch := &recordingChannel{name: "recorder"}
	eng, err := New(WithEventBus(bus), WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	eng.AddChannel(ch)
	// Register a plain Matcher (not ViolationMatcher) that matches all.
	eng.AddRule(MatcherFunc(func(_ context.Context, _ alert.Event) bool { return true }))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stopEngine(t, eng)

	_ = event.Publish(context.Background(), bus, event.TypeTelemetryMetricExceeded, event.MetricExceededPayload{
		AssetID:     "1",
		TenantID:    "1",
		MetricName:  "cpu",
		MetricValue: 95,
		Threshold:   90,
		Operator:    ">",
	})

	waitFor(t, func() bool { return ch.len() >= 1 }, 2*time.Second)

	alerts := ch.snapshot()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	a := alerts[0]
	// The payload-populated Violations should be preserved (single
	// violation from metricPayloadToAlert).
	if len(a.Violations) != 1 {
		t.Fatalf("expected 1 payload violation preserved, got %d: %+v", len(a.Violations), a.Violations)
	}
	if a.Violations[0].Kind != alert.ViolationKindMetric {
		t.Errorf("violation kind: got %q, want %q", a.Violations[0].Kind, alert.ViolationKindMetric)
	}
	if a.Violations[0].Metric == nil || a.Violations[0].Metric.Name != "cpu" {
		t.Errorf("violation metric: got %+v, want %q", a.Violations[0].Metric, "cpu")
	}
}
