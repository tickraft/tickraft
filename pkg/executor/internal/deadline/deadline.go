// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package deadline converges executor timeout control on the caller's
// context as the single source: the runner's lifecycle derives the deadline
// from the task's TimeoutSeconds, and executor-level timeouts exist only so
// a direct Execute call made with an unbounded context still terminates.
// The package is internal to pkg/executor on purpose — this is an
// implementation detail of the executor family, not kernel SPI.
package deadline

import (
	"context"
	"time"
)

// Fallback applies d as the deadline only when ctx carries none. A context
// that already has a deadline is returned unchanged alongside a no-op
// cancel, so the caller's (typically the runner's) deadline is never
// shortened by an executor-level default.
func Fallback(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}
