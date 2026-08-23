// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package deadline

import (
	"context"
	"testing"
	"time"
)

// TestFallbackPreservesCallerDeadline pins the single-source contract: a
// context that already carries a deadline is returned unchanged, so an
// executor-level fallback can never shorten the runner's (task-configured)
// deadline.
func TestFallbackPreservesCallerDeadline(t *testing.T) {
	callerDeadline := time.Now().Add(30 * time.Second)
	parent, parentCancel := context.WithDeadline(context.Background(), callerDeadline)
	defer parentCancel()

	ctx, cancel := Fallback(parent, 5*time.Second)
	defer cancel()

	got, ok := ctx.Deadline()
	if !ok {
		t.Fatal("Fallback dropped the caller's deadline")
	}
	if !got.Equal(callerDeadline) {
		t.Errorf("Fallback deadline: got %v, want the caller's %v", got, callerDeadline)
	}
	if ctx != parent {
		t.Error("Fallback derived a new context despite an existing deadline")
	}
}

// TestFallbackAppliesToUnboundedContext verifies the fallback path: a context
// without a deadline receives the fallback deadline.
func TestFallbackAppliesToUnboundedContext(t *testing.T) {
	ctx, cancel := Fallback(context.Background(), 5*time.Second)
	defer cancel()

	dl, ok := ctx.Deadline()
	if !ok {
		t.Fatal("Fallback did not apply a deadline to an unbounded context")
	}
	if remaining := time.Until(dl); remaining <= 0 || remaining > 5*time.Second {
		t.Errorf("Fallback deadline remaining: got %v, want within (0, 5s]", remaining)
	}
}
