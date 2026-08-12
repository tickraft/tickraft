// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package governance

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/prism/alert"
	"go.uber.org/zap"
)

func newDedupEvent() *alert.Event {
	return &alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		TenantID:   0,
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu_usage"}, Severity: "critical"}},
	}
}

func TestDedupFingerprintStableAndFirstPasses(t *testing.T) {
	t.Parallel()
	d := NewDedup(time.Second, zap.NewNop())

	evt := newDedupEvent()
	if got := d.Process(context.Background(), evt); got != DecisionPass {
		t.Fatalf("first alert: got %v, want %v", got, DecisionPass)
	}
	// A second identical alert within the window is suppressed.
	if got := d.Process(context.Background(), evt); got != DecisionSuppress {
		t.Fatalf("second alert: got %v, want %v", got, DecisionSuppress)
	}
}

func TestDedupDifferentFingerprintPasses(t *testing.T) {
	t.Parallel()
	d := NewDedup(time.Second, zap.NewNop())

	evtA := newDedupEvent()
	evtB := newDedupEvent()
	evtB.Violations[0].Metric = &alert.MetricContext{Name: "memory_usage"}

	if got := d.Process(context.Background(), evtA); got != DecisionPass {
		t.Fatalf("evtA: got %v, want %v", got, DecisionPass)
	}
	if got := d.Process(context.Background(), evtB); got != DecisionPass {
		t.Fatalf("evtB (different metric): got %v, want %v", got, DecisionPass)
	}
}

func TestDedupPassesAfterWindowExpires(t *testing.T) {
	t.Parallel()
	d := NewDedup(50*time.Millisecond, zap.NewNop())

	evt := newDedupEvent()
	if got := d.Process(context.Background(), evt); got != DecisionPass {
		t.Fatalf("first alert: got %v, want %v", got, DecisionPass)
	}
	if got := d.Process(context.Background(), evt); got != DecisionSuppress {
		t.Fatalf("within window: got %v, want %v", got, DecisionSuppress)
	}
	time.Sleep(80 * time.Millisecond)
	if got := d.Process(context.Background(), evt); got != DecisionPass {
		t.Fatalf("after window: got %v, want %v", got, DecisionPass)
	}
}

func TestDedupConcurrentSafe(t *testing.T) {
	t.Parallel()
	d := NewDedup(100*time.Millisecond, zap.NewNop())
	evt := newDedupEvent()

	var suppressed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if d.Process(context.Background(), evt) == DecisionSuppress {
				suppressed.Add(1)
			}
		}()
	}
	wg.Wait()

	// Exactly one Process call is the "first" (DecisionPass); all others are
	// suppressed. There is no data race (race detector enforces this).
	if got, want := suppressed.Load(), int64(49); got != want {
		t.Fatalf("suppressed count: got %d, want %d", got, want)
	}
}

func TestDedupNilEventPasses(t *testing.T) {
	t.Parallel()
	d := NewDedup(time.Second, zap.NewNop())
	if got := d.Process(context.Background(), nil); got != DecisionPass {
		t.Fatalf("nil event: got %v, want %v", got, DecisionPass)
	}
}

func TestDedupDefaultWindowWhenNonPositive(t *testing.T) {
	t.Parallel()
	d := NewDedup(0, nil)
	if d.window != defaultDedupWindow {
		t.Fatalf("window: got %v, want %v", d.window, defaultDedupWindow)
	}
	if d.logger == nil {
		t.Fatal("logger should default to nop, got nil")
	}
}

func TestDedupMultiViolationEventFingerprint(t *testing.T) {
	t.Parallel()
	d := NewDedup(time.Second, zap.NewNop())

	// Event with multiple violations
	evt := &alert.Event{
		Type:     alert.TypeMetric,
		AssetID:  1,
		TenantID: 0,
		Violations: []alert.Violation{
			{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu_usage"}, Severity: "critical"},
			{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "memory_usage"}, Severity: "warning"},
		},
	}

	// First event passes
	if got := d.Process(context.Background(), evt); got != DecisionPass {
		t.Fatalf("first alert: got %v, want %v", got, DecisionPass)
	}

	// Same multi-violation event should be suppressed
	if got := d.Process(context.Background(), evt); got != DecisionSuppress {
		t.Fatalf("second identical alert: got %v, want %v", got, DecisionSuppress)
	}

	// Event with different violation order but same content should also be suppressed
	evtReordered := &alert.Event{
		Type:     alert.TypeMetric,
		AssetID:  1,
		TenantID: 0,
		Violations: []alert.Violation{
			{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "memory_usage"}, Severity: "warning"},
			{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu_usage"}, Severity: "critical"},
		},
	}
	// Note: reordering also reorders the alert arrival, so this is a "new" alert
	// arriving with the same violations in different order.
	// The fingerprint should be stable (sorted), so this should be suppressed.
	if got := d.Process(context.Background(), evtReordered); got != DecisionSuppress {
		t.Fatalf("reordered same-violation alert: got %v, want %v (fingerprint should be stable)", got, DecisionSuppress)
	}
}

func TestDedupDifferentMultiViolationPasses(t *testing.T) {
	t.Parallel()
	d := NewDedup(time.Second, zap.NewNop())

	evtA := &alert.Event{
		Type:     alert.TypeMetric,
		AssetID:  1,
		TenantID: 0,
		Violations: []alert.Violation{
			{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu_usage"}, Severity: "critical"},
			{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "memory_usage"}, Severity: "warning"},
		},
	}
	evtB := &alert.Event{
		Type:     alert.TypeMetric,
		AssetID:  1,
		TenantID: 0,
		Violations: []alert.Violation{
			{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu_usage"}, Severity: "critical"},
			{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "disk_usage"}, Severity: "warning"},
		},
	}

	if got := d.Process(context.Background(), evtA); got != DecisionPass {
		t.Fatalf("evtA: got %v, want %v", got, DecisionPass)
	}
	if got := d.Process(context.Background(), evtB); got != DecisionPass {
		t.Fatalf("evtB (different violations): got %v, want %v", got, DecisionPass)
	}
}
