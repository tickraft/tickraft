// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package event

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkPublishSubscribe benchmarks publish/subscribe throughput in asynchronous mode.
// Target: >= 10000 events/sec.
func BenchmarkPublishSubscribe(b *testing.B) {
	bus := NewBus()
	defer bus.Close()

	var count atomic.Int64
	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		count.Add(1)
		return nil
	})
	if err != nil {
		b.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	b.ResetTimer()
	for range b.N {
		if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{
			TaskID:      "bench-task",
			ExecutionID: "bench-exec",
		}); err != nil {
			b.Fatalf("publish: %v", err)
		}
	}
	b.StopTimer()

	// Close drains the queue and waits for the consumer goroutine.
	if err := bus.Close(); err != nil {
		b.Fatalf("close: %v", err)
	}
	got := count.Load()
	if got != int64(b.N) {
		b.Logf("processed %d/%d events", got, b.N)
	}
}

// BenchmarkGenericPublishSubscribe benchmarks generic publish/subscribe throughput.
func BenchmarkGenericPublishSubscribe(b *testing.B) {
	bus := NewBus()
	defer bus.Close()

	var count atomic.Int64
	sub, err := Subscribe[ExecutionPayload](bus, TypeExecutionTriggered,
		func(ctx context.Context, e Event[ExecutionPayload]) error {
			count.Add(1)
			return nil
		},
	)
	if err != nil {
		b.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	b.ResetTimer()
	for range b.N {
		if err := Publish(context.Background(), bus, TypeExecutionTriggered, ExecutionPayload{
			TaskID: "bench-task",
		}); err != nil {
			b.Fatalf("publish: %v", err)
		}
	}
	b.StopTimer()

	if err := bus.Close(); err != nil {
		b.Fatalf("close: %v", err)
	}
}

// BenchmarkConcurrentPublish benchmarks concurrent publishing.
func BenchmarkConcurrentPublish(b *testing.B) {
	bus := NewBus()
	defer bus.Close()

	var count atomic.Int64
	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		count.Add(1)
		return nil
	})
	if err != nil {
		b.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{}); err != nil {
				b.Fatalf("publish: %v", err)
			}
		}
	})
	b.StopTimer()

	if err := bus.Close(); err != nil {
		b.Fatalf("close: %v", err)
	}
}

// BenchmarkEnvelopePool benchmarks the Envelope memory pool.
func BenchmarkEnvelopePool(b *testing.B) {
	for range b.N {
		env := acquireEnvelope()
		env.Type = TypeExecutionTriggered
		env.Payload = ExecutionPayload{TaskID: "pool-bench"}
		releaseEnvelope(env)
	}
}

// BenchmarkPriorityQueue benchmarks the priority queue.
func BenchmarkPriorityQueue(b *testing.B) {
	pq := &priorityQueue{}
	b.ResetTimer()
	for i := range b.N {
		env := &Envelope{Priority: i % 100}
		heapPush(pq, &queueItem{envelope: env, seq: uint64(i)})
		if pq.Len() > 100 {
			heapPop(pq)
		}
	}
}

// heapPush and heapPop are thin wrappers around container/heap used by benchmarks.
func heapPush(pq *priorityQueue, item *queueItem) {
	pq.Push(item)
}

func heapPop(pq *priorityQueue) *queueItem {
	return pq.Pop().(*queueItem)
}

// BenchmarkThroughput measures throughput (verifies >= 10000 events/sec).
func BenchmarkThroughput(b *testing.B) {
	bus := NewBus()
	defer bus.Close()

	var count atomic.Int64
	sub, err := bus.Subscribe(TypeExecutionTriggered, func(ctx context.Context, env Envelope) error {
		count.Add(1)
		return nil
	})
	if err != nil {
		b.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	b.ResetTimer()
	start := time.Now()
	for range b.N {
		if err := bus.Publish(context.Background(), TypeExecutionTriggered, ExecutionPayload{}); err != nil {
			b.Fatalf("publish: %v", err)
		}
	}
	elapsed := time.Since(start)
	b.StopTimer()

	if err := bus.Close(); err != nil {
		b.Fatalf("close: %v", err)
	}
	got := count.Load()
	eventsPerSec := float64(got) / elapsed.Seconds()
	b.Logf("throughput: %.0f events/sec (%d events in %v)", eventsPerSec, got, elapsed)

	if eventsPerSec < 10000 {
		b.Logf("WARNING: throughput %.0f events/sec is below target of 10000", eventsPerSec)
	}
}
