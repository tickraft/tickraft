// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package pool

import (
	"context"
	"runtime"
	"sync"
	"testing"
)

// BenchmarkSubmitThroughput measures end-to-end submit+execute
// throughput under different worker/queue configurations. Each
// iteration submits a no-op [Lambda] and the benchmark waits for all
// jobs to complete before stopping the timer, so the reported ns/op
// reflects the full pipeline cost.
func BenchmarkSubmitThroughput(b *testing.B) {
	cases := []struct {
		name      string
		workers   int
		queueSize int
	}{
		{"1x1", 1, 1},
		{"NumCPUx1024", runtime.NumCPU(), 1024},
		{"4x128", 4, 128},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			p, err := New(WithWorkers(c.workers), WithQueueSize(c.queueSize))
			if err != nil {
				b.Fatalf("New: %v", err)
			}

			var wg sync.WaitGroup
			wg.Add(b.N)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
					wg.Done()
					return nil
				})); err != nil {
					b.Fatalf("submit: %v", err)
				}
			}
			wg.Wait()
			b.StopTimer()

			if err := p.Shutdown(context.Background()); err != nil {
				b.Fatalf("shutdown: %v", err)
			}
		})
	}
}

// BenchmarkCallerRuns measures submit throughput under the
// [RejectionCallerRuns] policy. The single worker and queue slot are
// permanently occupied by a blocking job, so every measured Submit
// executes synchronously in the benchmark goroutine. The reported
// ns/op therefore reflects the cost of the CallerRuns fast path
// (rejection decision + execute) plus the no-op job body.
func BenchmarkCallerRuns(b *testing.B) {
	p, err := New(
		WithWorkers(1), WithQueueSize(1),
		WithRejectionPolicy(RejectionCallerRuns),
	)
	if err != nil {
		b.Fatalf("New: %v", err)
	}

	release := make(chan struct{})
	// Block the single worker so every subsequent Submit hits
	// CallerRuns.
	if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
		<-release
		return nil
	})); err != nil {
		b.Fatalf("submit blocker: %v", err)
	}
	// Fill the queue so the next Submit cannot enqueue.
	if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
		return nil
	})); err != nil {
		b.Fatalf("submit filler: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := p.Submit(context.Background(), Lambda(func(ctx context.Context) error {
			return nil
		})); err != nil {
			b.Fatalf("submit: %v", err)
		}
	}
	b.StopTimer()

	close(release)
	if err := p.Shutdown(context.Background()); err != nil {
		b.Fatalf("shutdown: %v", err)
	}
}
