// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestAggregatorSingleWindow(t *testing.T) {
	agg := NewAggregator(time.Minute, zap.NewNop())
	agg.Start(context.Background())

	now := time.Now()
	agg.Aggregate(context.Background(), 1, []Metric{
		{Name: "cpu", Value: 10, Timestamp: now, TenantID: 1},
		{Name: "cpu", Value: 20, Timestamp: now, TenantID: 1},
		{Name: "cpu", Value: 30, Timestamp: now, TenantID: 1},
	})

	if err := agg.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	select {
	case am := <-agg.FlushCh():
		if am.assetID != 1 {
			t.Errorf("assetID: got %d, want 1", am.assetID)
		}
		if am.metricName != "cpu" {
			t.Errorf("metricName: got %q, want %q", am.metricName, "cpu")
		}
		if am.avg != 20 {
			t.Errorf("avg: got %v, want 20", am.avg)
		}
		if am.max != 30 {
			t.Errorf("max: got %v, want 30", am.max)
		}
		if am.min != 10 {
			t.Errorf("min: got %v, want 10", am.min)
		}
		if am.count != 3 {
			t.Errorf("count: got %d, want 3", am.count)
		}
		if am.sum != 60 {
			t.Errorf("sum: got %v, want 60", am.sum)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for aggregated metric")
	}
}

func TestAggregatorMultipleGroups(t *testing.T) {
	agg := NewAggregator(time.Minute, zap.NewNop())
	agg.Start(context.Background())

	now := time.Now()
	agg.Aggregate(context.Background(), 1, []Metric{
		{Name: "cpu", Value: 10, Timestamp: now, TenantID: 1},
		{Name: "mem", Value: 20, Timestamp: now, TenantID: 1},
	})
	agg.Aggregate(context.Background(), 2, []Metric{
		{Name: "cpu", Value: 30, Timestamp: now, TenantID: 2},
	})

	if err := agg.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	received := make(map[string]*aggregatedMetric)
	for i := range 3 {
		select {
		case am := <-agg.FlushCh():
			key := fmt.Sprintf("%d:%s", am.assetID, am.metricName)
			received[key] = am
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for aggregated metric %d", i)
		}
	}

	if len(received) != 3 {
		t.Fatalf("expected 3 aggregated metrics, got %d", len(received))
	}

	if am, ok := received["1:cpu"]; !ok {
		t.Error("missing 1:cpu")
	} else if am.avg != 10 || am.count != 1 {
		t.Errorf("1:cpu: avg=%v count=%d, want avg=10 count=1", am.avg, am.count)
	}
	if am, ok := received["1:mem"]; !ok {
		t.Error("missing 1:mem")
	} else if am.avg != 20 || am.count != 1 {
		t.Errorf("1:mem: avg=%v count=%d, want avg=20 count=1", am.avg, am.count)
	}
	if am, ok := received["2:cpu"]; !ok {
		t.Error("missing 2:cpu")
	} else if am.avg != 30 || am.count != 1 {
		t.Errorf("2:cpu: avg=%v count=%d, want avg=30 count=1", am.avg, am.count)
	}
}

func TestAggregatorWindowExpiry(t *testing.T) {
	window := 100 * time.Millisecond
	agg := NewAggregator(window, zap.NewNop())
	agg.Start(context.Background())
	defer agg.Stop(context.Background())

	now := time.Now()
	agg.Aggregate(context.Background(), 1, []Metric{
		{Name: "cpu", Value: 50, Timestamp: now, TenantID: 1},
	})

	// The flush ticker fires every window/2. After the window expires the
	// next tick should flush the buffered metric.
	select {
	case am := <-agg.FlushCh():
		if am.assetID != 1 || am.metricName != "cpu" {
			t.Errorf("got assetID=%d metricName=%q, want 1/cpu", am.assetID, am.metricName)
		}
		if am.avg != 50 || am.count != 1 {
			t.Errorf("got avg=%v count=%d, want avg=50 count=1", am.avg, am.count)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for window expiry flush")
	}
}

func TestAggregatorStopFlushesRemaining(t *testing.T) {
	agg := NewAggregator(time.Hour, zap.NewNop()) // long window so it won't auto-flush
	agg.Start(context.Background())

	now := time.Now()
	agg.Aggregate(context.Background(), 1, []Metric{
		{Name: "cpu", Value: 42, Timestamp: now, TenantID: 1},
	})

	if err := agg.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	select {
	case am := <-agg.FlushCh():
		if am.avg != 42 {
			t.Errorf("avg: got %v, want 42", am.avg)
		}
		if am.count != 1 {
			t.Errorf("count: got %d, want 1", am.count)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for remaining flush on Stop")
	}
}

func TestAggregationDisabledViaManager(t *testing.T) {
	mgr := mustNewManager(t,
		WithAssetStore(newMgrMockStore()),
		WithAggregationWindow(0),
		WithLogger(zap.NewNop()),
	).(*Manager)

	if mgr.aggregator != nil {
		t.Error("expected nil aggregator when window is 0")
	}
}

func TestAggregationEnabledViaManager(t *testing.T) {
	mgr := mustNewManager(t,
		WithAssetStore(newMgrMockStore()),
		WithAggregationWindow(30*time.Second),
		WithLogger(zap.NewNop()),
	).(*Manager)

	if mgr.aggregator == nil {
		t.Error("expected non-nil aggregator when window is positive")
	}
}

// bufferKeyOld is the fmt.Sprintf-based baseline implementation kept for
// benchmark comparison against the pooled-buffer bufferKey. It allocates
// intermediate strings and relies on reflection for argument formatting.
func bufferKeyOld(assetID int64, metricName string, windowStart time.Time) string {
	return fmt.Sprintf("%d:%s:%d", assetID, metricName, windowStart.UnixNano())
}

// BenchmarkBufferKeyOld measures the performance of the fmt.Sprintf-based
// baseline bufferKey implementation for comparison with the pooled-buffer
// variant benchmarked by BenchmarkBufferKey.
func BenchmarkBufferKeyOld(b *testing.B) {
	b.ReportAllocs()
	ts := time.Unix(1700000000, 123456789)
	for range b.N {
		_ = bufferKeyOld(12345, "cpu_usage", ts)
	}
}

// BenchmarkBufferKey measures the performance of the pooled-buffer bufferKey
// implementation using a sync.Pool-backed []byte buffer and strconv.AppendInt.
func BenchmarkBufferKey(b *testing.B) {
	b.ReportAllocs()
	ts := time.Unix(1700000000, 123456789)
	for range b.N {
		_ = bufferKey(12345, "cpu_usage", ts)
	}
}

// BenchmarkBufferKeyParallel measures the pooled-buffer implementation under
// concurrent access to verify the sync.Pool performs well under contention.
func BenchmarkBufferKeyParallel(b *testing.B) {
	b.ReportAllocs()
	ts := time.Unix(1700000000, 123456789)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = bufferKey(12345, "cpu_usage", ts)
		}
	})
}

// TestBufferKeyEquivalence verifies the pooled-buffer bufferKey produces the
// same output as the fmt.Sprintf-based baseline implementation.
func TestBufferKeyEquivalence(t *testing.T) {
	cases := []struct {
		assetID     int64
		metricName  string
		windowStart time.Time
	}{
		{1, "cpu", time.Unix(1700000000, 0)},
		{12345, "cpu_usage", time.Unix(1700000000, 123456789)},
		{0, "m", time.Unix(0, 1)},
		{-1, "negative", time.Unix(9999999999, 999999999)},
	}
	for _, tc := range cases {
		old := bufferKeyOld(tc.assetID, tc.metricName, tc.windowStart)
		got := bufferKey(tc.assetID, tc.metricName, tc.windowStart)
		if old != got {
			t.Errorf("bufferKey(%d, %q, %v) = %q, want %q",
				tc.assetID, tc.metricName, tc.windowStart, got, old)
		}
		// Sanity check the format is what we expect.
		want := strconv.FormatInt(tc.assetID, 10) + ":" + tc.metricName + ":" +
			strconv.FormatInt(tc.windowStart.UnixNano(), 10)
		if got != want {
			t.Errorf("bufferKey format mismatch: got %q, want %q", got, want)
		}
	}
}

// TestMetricBufferCap verifies that the buffer respects its capacity by
// dropping the oldest point and preserving the most recent data when full.
func TestMetricBufferCap(t *testing.T) {
	const capacity = 5
	b := &metricBuffer{
		assetID:    1,
		metricName: "cpu",
		maxPoints:  capacity,
		logger:     zap.NewNop(),
	}

	// Fill the buffer to capacity.
	for i := range capacity {
		b.add(float64(i))
	}
	if len(b.points) != capacity {
		t.Fatalf("expected %d points, got %d", capacity, len(b.points))
	}

	// Add one more — the oldest (0.0) should be evicted.
	b.add(99.0)
	if len(b.points) != capacity {
		t.Fatalf("expected %d points after overflow, got %d", capacity, len(b.points))
	}
	if b.points[0] != 1.0 {
		t.Errorf("expected oldest remaining point to be 1.0, got %v", b.points[0])
	}
	if b.points[capacity-1] != 99.0 {
		t.Errorf("expected newest point to be 99.0, got %v", b.points[capacity-1])
	}
	if b.overflowCount != 1 {
		t.Errorf("expected overflowCount=1, got %d", b.overflowCount)
	}

	// Add several more and verify the buffer never exceeds the cap.
	for i := range 20 {
		b.add(float64(100 + i))
	}
	if len(b.points) != capacity {
		t.Errorf("expected %d points after sustained overflow, got %d", capacity, len(b.points))
	}
	// The most recent 5 values should be 115, 116, 117, 118, 119.
	for i, want := range []float64{115, 116, 117, 118, 119} {
		if b.points[i] != want {
			t.Errorf("points[%d]: got %v, want %v", i, b.points[i], want)
		}
	}
}

// TestMetricBufferCapDefault verifies that a buffer with maxPoints<=0 falls
// back to the default cap and still functions correctly.
func TestMetricBufferCapDefault(t *testing.T) {
	b := &metricBuffer{
		assetID:    1,
		metricName: "cpu",
		logger:     zap.NewNop(),
		// maxPoints intentionally left zero to exercise the default path.
	}
	for i := range DefaultMetricBufferCap + 5 {
		b.add(float64(i))
	}
	if len(b.points) != DefaultMetricBufferCap {
		t.Errorf("expected default cap %d, got %d", DefaultMetricBufferCap, len(b.points))
	}
}

// TestAggregatorBufferCap verifies the Aggregator respects a custom
// maxBufferPoints setting via SetMaxBufferPoints.
func TestAggregatorBufferCap(t *testing.T) {
	agg := NewAggregator(time.Minute, zap.NewNop())
	agg.SetMaxBufferPoints(3)
	agg.Start(context.Background())

	now := time.Now()
	metrics := []Metric{
		{Name: "cpu", Value: 1, Timestamp: now, TenantID: 1},
		{Name: "cpu", Value: 2, Timestamp: now, TenantID: 1},
		{Name: "cpu", Value: 3, Timestamp: now, TenantID: 1},
		{Name: "cpu", Value: 4, Timestamp: now, TenantID: 1},
		{Name: "cpu", Value: 5, Timestamp: now, TenantID: 1},
	}
	agg.Aggregate(context.Background(), 1, metrics)

	if err := agg.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	am := <-agg.FlushCh()
	if am.count != 3 {
		t.Errorf("expected 3 points (capped), got %d", am.count)
	}
	if am.min != 3 {
		t.Errorf("expected min=3 (oldest evicted), got %v", am.min)
	}
	if am.max != 5 {
		t.Errorf("expected max=5, got %v", am.max)
	}
}
