// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package timewheel

import (
	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/pool"
)

// defaultWorkerSize is the worker count used for the internally created
// pool when no explicit pool is injected via [WithPool] and no
// [WithWorkerSize] option is provided.
const defaultWorkerSize = 100

// defaultQueueSize is the bounded task queue capacity used for the
// internally created pool. It is sized at 4x the worker count so the
// pool can absorb bursts of expirations without rejecting callbacks.
const defaultQueueMultiplier = 4

// Option configures a [Wheel] at construction time.
//
// Options are applied in the order they are passed to [New]. An invalid
// option value (for example a non-positive worker count) is not
// reported by the option itself; it is normalized by the constructor.
type Option interface {
	apply(*config)
}

// config holds the resolved configuration of a wheel after applying
// all [Option] values.
type config struct {
	workerSize int
	pool       pool.Pool
	logger     *zap.Logger
}

// workerSizeOption sets the worker count for the internally created
// pool. It has no effect when [WithPool] is also provided.
type workerSizeOption int

func (o workerSizeOption) apply(c *config) { c.workerSize = int(o) }

// WithWorkerSize sets the number of worker goroutines used by the
// default pool. It is ignored when [WithPool] is used to inject an
// explicit pool. A non-positive value is normalized to
// [defaultWorkerSize] by the constructor.
func WithWorkerSize(n int) Option { return workerSizeOption(n) }

// poolOption injects an externally owned [pool.Pool] for callback
// dispatch. The wheel does not shut down an injected pool on [Stop];
// the caller is responsible for its lifecycle.
type poolOption struct{ p pool.Pool }

func (o poolOption) apply(c *config) { c.pool = o.p }

// WithPool injects a [pool.Pool] used to dispatch expired callbacks.
// When a pool is injected the wheel does not create a default one and
// does not shut it down on [Stop]; the caller owns the pool's
// lifecycle. When no pool is injected the wheel creates a default pool
// sized by [WithWorkerSize] (or [defaultWorkerSize]) and closes it on
// [Stop].
func WithPool(p pool.Pool) Option { return poolOption{p: p} }

// loggerOption sets the logger used to report dispatch rejections.
type loggerOption struct{ l *zap.Logger }

func (o loggerOption) apply(c *config) { c.logger = o.l }

// WithLogger sets the [zap.Logger] used to warn about rejected
// callback dispatches (for example when the pool is saturated or shut
// down). When no logger is configured the wheel uses a no-op logger.
func WithLogger(l *zap.Logger) Option { return loggerOption{l: l} }
