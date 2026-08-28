// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
)

// DefaultAggregationWindow is the default tumbling window duration.
const DefaultAggregationWindow = time.Minute

// DefaultMetricBufferCap is the default maximum number of data points a
// single metricBuffer retains before it starts dropping old points.
const DefaultMetricBufferCap = 10000

// metricBufferOverflowLogInterval controls how frequently overflow warnings
// are logged. Logging on every overflow would flood the logs under sustained
// pressure; instead we log once every N-th overflow per buffer.
const metricBufferOverflowLogInterval = 1000

// Metric is a single metric data point fed into the Aggregator.
type Metric struct {
	// Name is the metric identifier (e.g. "cpu_usage").
	Name string
	// Value is the numeric reading.
	Value float64
	// Timestamp is when the data point was observed.
	Timestamp time.Time
	// TenantID is the owning tenant, used when persisting aggregated results.
	TenantID int64
}

// aggregatedMetric holds the statistics computed for a single tumbling window.
type aggregatedMetric struct {
	assetID     int64
	tenantID    int64
	metricName  string
	avg         float64
	max         float64
	min         float64
	count       int64
	sum         float64
	windowStart time.Time
	windowEnd   time.Time
}

// metricBuffer accumulates data points for a single asset+metric+window.
type metricBuffer struct {
	mu            sync.Mutex
	assetID       int64
	tenantID      int64
	metricName    string
	points        []float64
	windowStart   time.Time
	windowEnd     time.Time
	maxPoints     int
	overflowCount int64
	logger        *zap.Logger
}

// add appends a data point to the buffer. When the buffer is full the
// oldest point is evicted (shift left) so that recent data is preserved.
// Overflow warnings are logged at a rate-limited interval to avoid
// flooding the logs under sustained back-pressure.
func (b *metricBuffer) add(value float64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	capacity := b.maxPoints
	if capacity <= 0 {
		capacity = DefaultMetricBufferCap
	}

	if len(b.points) >= capacity {
		// Option A: drop the oldest point to preserve recent data.
		copy(b.points, b.points[1:])
		b.points[len(b.points)-1] = value
		b.overflowCount++
		if b.logger != nil && b.overflowCount%metricBufferOverflowLogInterval == 1 {
			b.logger.Warn("metric buffer overflow, dropping oldest point",
				zap.Int64("asset_id", b.assetID),
				zap.String("metric_name", b.metricName),
				zap.Int("cap", capacity),
				zap.Int64("overflow_count", b.overflowCount),
			)
		}
		return
	}

	b.points = append(b.points, value)
}

// compute returns the aggregated metric and clears the buffered points.
// Returns nil if the buffer contains no points.
func (b *metricBuffer) compute() *aggregatedMetric {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.points) == 0 {
		return nil
	}

	sum := 0.0
	maxVal := b.points[0]
	minVal := b.points[0]
	for _, p := range b.points {
		sum += p
		maxVal = max(maxVal, p)
		minVal = min(minVal, p)
	}
	count := int64(len(b.points))
	am := &aggregatedMetric{
		assetID:     b.assetID,
		tenantID:    b.tenantID,
		metricName:  b.metricName,
		avg:         sum / float64(count),
		max:         maxVal,
		min:         minVal,
		count:       count,
		sum:         sum,
		windowStart: b.windowStart,
		windowEnd:   b.windowEnd,
	}
	b.points = b.points[:0]
	return am
}

// Aggregator aggregates metric data points over fixed tumbling time windows.
// Points sharing the same assetID + metricName + window are combined, and
// avg/max/min/count/sum statistics are emitted on FlushCh when the window
// expires.
//
// Aggregator is safe for concurrent use.
type Aggregator struct {
	window          time.Duration
	buffers         sync.Map // key: "assetID:metricName:windowStartNs" -> *metricBuffer
	logger          *zap.Logger
	flushCh         chan *aggregatedMetric
	maxBufferPoints int

	startMu sync.Mutex
	started bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewAggregator creates a new Aggregator with the given tumbling window.
// The window must be positive; callers that want to disable aggregation should
// pass a non-positive duration to the Engine instead of constructing an
// Aggregator directly.
func NewAggregator(window time.Duration, logger *zap.Logger) *Aggregator {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Aggregator{
		window:          window,
		logger:          logger,
		flushCh:         make(chan *aggregatedMetric, 256),
		maxBufferPoints: DefaultMetricBufferCap,
	}
}

// SetMaxBufferPoints configures the maximum number of data points each
// metricBuffer retains before it starts dropping old points. A non-positive
// value resets to the default. This must be called before Start; buffers
// created before the call retain their previous cap.
func (a *Aggregator) SetMaxBufferPoints(n int) {
	if n <= 0 {
		n = DefaultMetricBufferCap
	}
	a.maxBufferPoints = n
}

// FlushCh returns the read-only channel that receives aggregated metrics.
// Consumers must drain this channel to avoid drops.
func (a *Aggregator) FlushCh() <-chan *aggregatedMetric { //nolint:revive // returning the unexported concrete type is intentional; consumers use the exported interface
	return a.flushCh
}

// Aggregate adds metric data points to the aggregation buffers. Each point is
// assigned to the tumbling window that contains its timestamp.
func (a *Aggregator) Aggregate(_ context.Context, assetID int64, metrics []Metric) {
	for _, m := range metrics {
		ts := m.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		windowStart := ts.Truncate(a.window)
		key := bufferKey(assetID, m.Name, windowStart)
		bufAny, _ := a.buffers.LoadOrStore(key, &metricBuffer{
			assetID:     assetID,
			tenantID:    m.TenantID,
			metricName:  m.Name,
			windowStart: windowStart,
			windowEnd:   windowStart.Add(a.window),
			maxPoints:   a.maxBufferPoints,
			logger:      a.logger,
		})
		// Type guaranteed by construction: only *metricBuffer is stored in buffers map.
		bufAny.(*metricBuffer).add(m.Value) //nolint:errcheck // safe: buffers map only ever stores *metricBuffer
	}
}

// bufferKeyBufPool reuses []byte buffers across bufferKey calls to avoid
// per-call heap allocations. The returned string still allocates (strings
// are immutable in Go), but the intermediate byte work is pooled.
var bufferKeyBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 64)
		return &b
	},
}

// bufferKey builds the sync.Map key for a buffer.
//
// The key format is "assetID:metricName:windowStartUnixNano". It uses
// strconv.AppendInt on a pooled []byte buffer instead of fmt.Sprintf to
// avoid reflection and intermediate string allocations on the hot path.
func bufferKey(assetID int64, metricName string, windowStart time.Time) string {
	// Type guaranteed by construction: pool only stores *[]byte.
	bp := bufferKeyBufPool.Get().(*[]byte) //nolint:errcheck // safe: pool only ever stores *[]byte
	b := (*bp)[:0]

	b = strconv.AppendInt(b, assetID, 10)
	b = append(b, ':')
	b = append(b, metricName...)
	b = append(b, ':')
	b = strconv.AppendInt(b, windowStart.UnixNano(), 10)

	key := string(b)
	*bp = b
	bufferKeyBufPool.Put(bp)
	return key
}

// Start launches the background goroutine that periodically flushes expired
// windows. It is safe to call multiple times; subsequent calls are no-ops.
func (a *Aggregator) Start(ctx context.Context) {
	a.startMu.Lock()
	defer a.startMu.Unlock()
	if a.started {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.started = true
	a.wg.Add(1)
	// goroutine lifecycle: bound to ctx (cancelled by Aggregator.Stop);
	// tracked by a.wg so Stop can wait for run to drain and exit.
	go a.run(ctx)
}

// run supervises the background flush loop: superviseLoop restarts it after
// a panic so windowed aggregation cannot silently stop.
func (a *Aggregator) run(ctx context.Context) {
	defer a.wg.Done()
	superviseLoop(ctx, a.logger, "aggregator run", loopRestartBackoff, a.flushLoop)
}

// flushLoop periodically flushes expired windows and flushes all remaining
// buffers when the context is cancelled.
func (a *Aggregator) flushLoop(ctx context.Context) {
	interval := a.window / 2
	if interval <= 0 {
		interval = a.window
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			a.flushAll()
			return
		case <-ticker.C:
			a.flushExpired(time.Now())
		}
	}
}

// flushExpired flushes all buffers whose window end has passed.
func (a *Aggregator) flushExpired(now time.Time) {
	a.buffers.Range(func(key, val any) bool {
		// Type guaranteed by construction: only *metricBuffer is stored in buffers map.
		buf := val.(*metricBuffer) //nolint:errcheck // safe: buffers map only ever stores *metricBuffer
		if !now.Before(buf.windowEnd) {
			a.buffers.Delete(key)
			if am := buf.compute(); am != nil {
				a.send(am)
			}
		}
		return true
	})
}

// flushAll flushes every remaining buffer regardless of window expiry.
func (a *Aggregator) flushAll() {
	a.buffers.Range(func(key, val any) bool {
		a.buffers.Delete(key)
		// Type guaranteed by construction: only *metricBuffer is stored in buffers map.
		if am := val.(*metricBuffer).compute(); am != nil { //nolint:errcheck // safe: map only holds *metricBuffer
			a.send(am)
		}
		return true
	})
}

// send delivers an aggregated metric to the flush channel. It is non-blocking:
// if the channel is full the metric is dropped with a warning log.
func (a *Aggregator) send(am *aggregatedMetric) {
	select {
	case a.flushCh <- am:
	default:
		a.logger.Warn("aggregator flush channel full, dropping aggregated metric",
			zap.Int64("asset_id", am.assetID),
			zap.String("metric_name", am.metricName),
		)
	}
}

// Stop gracefully stops the aggregator. It signals the background goroutine to
// exit, which flushes all remaining buffers before returning. Stop blocks until
// the goroutine has finished or the context is cancelled.
//
// When the caller's context expires first, Stop returns the timeout error
// while the drain continues without it: the run goroutine observes the
// cancelled internal context and exits promptly, so its lifetime can exceed
// the Stop call only by that bounded drain, never indefinitely.
func (a *Aggregator) Stop(ctx context.Context) error {
	a.startMu.Lock()
	if !a.started {
		a.startMu.Unlock()
		return nil
	}
	a.started = false
	cancel := a.cancel
	a.startMu.Unlock()

	if cancel != nil {
		cancel()
	}

	done := make(chan struct{})
	// goroutine lifecycle: bounded — waits for a.wg to drain after the run
	// goroutine observes ctx cancellation; exits after close(done).
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("aggregator stop timed out: %w", ctx.Err())
	}
}
