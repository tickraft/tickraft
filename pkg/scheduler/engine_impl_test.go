// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestEngineStartStop verifies the basic lifecycle: Start is idempotent,
// Stop is idempotent, and Stop waits for the goroutine to exit.
func TestEngineStartStop(t *testing.T) {
	eng, err := NewEngine(WithEngineLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	ctx := context.Background()

	// Start should succeed.
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	// Start again is idempotent.
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	// Stop should succeed.
	if err := eng.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	// Stop again is idempotent.
	if err := eng.Stop(ctx); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
}

// TestEngineAddRemove verifies that Add/Remove work without error and
// that Remove is a no-op for unknown IDs.
func TestEngineAddRemove(t *testing.T) {
	eng, _ := NewEngine(WithEngineLogger(zap.NewNop()))
	ctx := context.Background()

	if err := eng.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer eng.Stop(ctx)

	// Add a never-firing entry.
	if err := eng.Add(1, NewNeverSchedule(), func(int64) {}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	// Remove it.
	if err := eng.Remove(1); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	// Remove unknown ID is a no-op.
	if err := eng.Remove(999); err != nil {
		t.Fatalf("Remove(unknown) error = %v", err)
	}
}

// TestEngineAddNilSchedule verifies that Add with a nil schedule returns
// an error.
func TestEngineAddNilSchedule(t *testing.T) {
	eng, _ := NewEngine(WithEngineLogger(zap.NewNop()))
	if err := eng.Add(1, nil, func(int64) {}); err == nil {
		t.Error("Add(nil schedule) expected error, got nil")
	}
}

// TestEnginePanicRecovery verifies that a panicking callback does not
// crash the engine and that recurring schedules continue to fire after
// a panic.
func TestEnginePanicRecovery(t *testing.T) {
	eng, _ := NewEngine(WithEngineLogger(zap.NewNop()))
	ctx := context.Background()

	if err := eng.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer eng.Stop(ctx)

	var fireCount atomic.Int64
	panicCb := func(id int64) {
		fireCount.Add(1)
		panic("intentional test panic")
	}

	// Use a short-interval schedule so the entry fires quickly.
	if err := eng.Add(1, NewConstantIntervalSchedule(time.Second), panicCb); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	// Wait for at least 2 firings to verify the engine survived the first
	// panic and continued rescheduling.
	deadline := time.After(5 * time.Second)
	for fireCount.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("fireCount = %d, want >= 2 (panic should not stop rescheduling)", fireCount.Load())
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Clean up.
	eng.Remove(1)
}
