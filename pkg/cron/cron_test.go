// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cron

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWithWorkerPool(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	crontab := New(WithContext(ctx), WithWorkerSize(2))

	var count int64
	sched := Every(1 * time.Second)
	_ = crontab.Add(1, sched, Lambda(func(_ context.Context) {
		atomic.AddInt64(&count, 1)
	}))

	time.Sleep(2500 * time.Millisecond)
	crontab.Remove(1)

	c := atomic.LoadInt64(&count)
	if c < 1 {
		t.Fatalf("expected at least 1 execution, got %d", c)
	}
}

func TestMultipleJobs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	crontab := New(WithContext(ctx), WithWorkerSize(4))

	var mu sync.Mutex
	results := make(map[int64]int)

	for i := int64(1); i <= 5; i++ {
		id := i
		sched := Every(1 * time.Second)
		_ = crontab.Add(id, sched, Lambda(func(_ context.Context) {
			mu.Lock()
			results[id]++
			mu.Unlock()
		}))
	}

	time.Sleep(2500 * time.Millisecond)

	mu.Lock()
	for id, count := range results {
		if count < 1 {
			t.Fatalf("job %d: expected at least 1 execution, got %d", id, count)
		}
	}
	mu.Unlock()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := crontab.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestStop(t *testing.T) {
	crontab := New(WithContext(context.Background()))

	sched := Every(1 * time.Second)
	_ = crontab.Add(1, sched, Lambda(func(_ context.Context) {}))

	time.Sleep(100 * time.Millisecond)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := crontab.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestAddReplace(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	crontab := New(WithContext(ctx))

	var count1, count2 int64

	sched1 := Every(1 * time.Second)
	_ = crontab.Add(1, sched1, Lambda(func(_ context.Context) {
		atomic.AddInt64(&count1, 1)
	}))

	// Replace job 1 with a different job
	sched2 := Every(2 * time.Second)
	_ = crontab.Add(1, sched2, Lambda(func(_ context.Context) {
		atomic.AddInt64(&count2, 1)
	}))

	time.Sleep(1500 * time.Millisecond)

	// Only the replacement job should have fired
	if atomic.LoadInt64(&count1) != 0 {
		t.Fatal("original job should not have fired after replacement")
	}
}

func TestConcurrentAddRemove(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	crontab := New(WithContext(ctx), WithWorkerSize(4))

	var wg sync.WaitGroup
	sched := Every(1 * time.Second)

	// Concurrently add and remove entries
	for i := range 10 {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			_ = crontab.Add(id, sched, Lambda(func(_ context.Context) {}))
			time.Sleep(10 * time.Millisecond)
			crontab.Remove(id)
		}(int64(i + 1))
	}
	wg.Wait()
}

// TestPanicIsolation verifies that a panicking job is recovered and does not
// crash the scheduler or prevent sibling jobs from running.
func TestPanicIsolation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	crontab := New(WithContext(ctx), WithWorkerSize(2))

	// Job 1 panics; job 2 must still execute afterwards.
	sched := Every(1 * time.Second)
	_ = crontab.Add(1, sched, Lambda(func(_ context.Context) {
		panic("boom")
	}))

	var ran int64
	_ = crontab.Add(2, sched, Lambda(func(_ context.Context) {
		atomic.AddInt64(&ran, 1)
	}))

	time.Sleep(2500 * time.Millisecond)

	if atomic.LoadInt64(&ran) < 1 {
		t.Fatal("job 2 should have run despite job 1 panicking")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := crontab.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}
