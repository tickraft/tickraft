// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/asset"
)

// Config is the configuration input to Register. The zero-value Config
// disables the rule engine and produces a no-op Register call, so
// deployments that do not use rule-based matching are unaffected.
type Config struct {
	// Logger is the structured logger. When nil, Register falls back
	// to a no-op logger.
	Logger *zap.Logger
	// Store is the rule persistence layer consumed via the Lister
	// port. When nil, only the static Rules below are loaded and no
	// dynamic Reload loop is started.
	Store Lister
	// AssetStore is used by the AlertMatcher to enrich its evaluation
	// environment with asset metadata. When nil, the asset domain is
	// limited to the event's asset id.
	AssetStore asset.Store
	// Rules is the static rule set compiled at startup. Useful for
	// deployments that prefer configuration-file rules over database
	// management.
	Rules []Spec
	// EvalInterval is the dynamic Reload period. When zero and Store
	// is non-nil, the engine still performs an initial Reload but
	// does not start a periodic loop. The loop is the polling fallback
	// for lost notifications; real-time reload is driven by the
	// ReloadSubscriber wired to the process config bus.
	EvalInterval time.Duration
	// ReloadSubscriber, when non-nil, is invoked once during Register with
	// a reload closure that re-reads enabled rules from Store. The caller
	// wires the closure to its configuration refresh bus so rule changes
	// are applied without waiting for the polling fallback. The closure
	// captures the engine and Store built by Register, so it must be
	// invoked as-is. This indirection keeps this package free of any
	// dependency on a higher-level bus package.
	ReloadSubscriber func(reload func(ctx context.Context) error)
}

// IsEnabled reports whether the configuration enables the rule engine.
// The engine is considered enabled when at least one static rule is
// supplied or a Store is configured for dynamic loading.
func (c Config) IsEnabled() bool {
	return len(c.Rules) > 0 || c.Store != nil
}

// logger returns the configured logger or a no-op fallback. It is a
// helper used by Register so the nil-handling lives in one place.
func (c Config) logger() *zap.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return zap.NewNop()
}
