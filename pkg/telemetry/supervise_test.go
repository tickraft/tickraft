// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestSuperviseLoopRestartsAfterPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		superviseLoop(ctx, zap.NewNop(), "test loop", time.Millisecond, func(context.Context) {
			if calls.Add(1) == 1 {
				panic("boom")
			}
			cancel()
		})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("superviseLoop did not return after panic-restart cycle")
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("expected fn to run twice (panic then restart), ran %d times", n)
	}
}

func TestSuperviseLoopExitsOnContextDoneWithoutRestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		superviseLoop(ctx, zap.NewNop(), "test loop", time.Hour, func(context.Context) {
			calls.Add(1)
		})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("superviseLoop hung on cancelled context")
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("expected a single invocation with no restart, ran %d times", n)
	}
}
