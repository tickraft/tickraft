// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"context"
	"fmt"

	"github.com/tickraft/tickraft/pkg/prism/alert"
	"go.uber.org/zap"
)

// RuleTarget is the minimal interface rule.Register needs from the prism
// engine. It is defined here (at the consumption site) so the rule package
// does not need to import prism, avoiding a circular dependency.
type RuleTarget interface {
	AddRule(m alert.Matcher)
}

// Register is the startup entry point for the rule matching engine.
// It mirrors the Register pattern used by other subsystems
// (xchannel.Register, xcollector.Register): it builds the Engine,
// compiles static rules, optionally pulls dynamic rules from a Store,
// and injects the MetricMatcher into the prism Engine so metric
// alerts are filtered before dispatch.
//
// It returns the created *Engine so callers (e.g. the alert service
// layer) can invoke Reload after rule CRUD operations to refresh the
// in-memory rule set without a process restart. The caller should
// defer Engine.Stop to cancel the background reload loop goroutine
// when the engine is no longer needed.
//
// A zero-value Config is a no-op: IsEnabled returns false and the
// function returns (nil, nil) without touching the supplied prism
// Engine, so deployments that do not use rule-based matching are
// unaffected.
//
// When cfg.Store is non-nil and cfg.EvalInterval is positive, Register
// launches engine.runReloadLoop in a background goroutine. The goroutine
// is tied to a cancellable context derived from ctx, so it exits when
// either ctx is cancelled or Engine.Stop is called.
func Register(ctx context.Context, target RuleTarget, cfg Config) (*Engine, error) {
	if !cfg.IsEnabled() {
		return nil, nil
	}
	if target == nil {
		return nil, fmt.Errorf("rule: target is nil")
	}

	logger := cfg.logger()
	engine := NewEngineWithConfig(logger, cfg.CompilerConfig)

	// Compile and load static rules. Static rules receive negative
	// IDs so they never collide with database-assigned IDs (which are
	// always positive). Compilation failures are logged and skipped
	// so one bad expression does not block registration.
	staticRules := make([]Rule, 0, len(cfg.Rules))
	for i, spec := range cfg.Rules {
		// Pre-validate via the shared compiler so the Engine's Load
		// path never sees the rule. Load will re-compile it inside
		// compileAll; the duplicate compile is acceptable because
		// Register runs once at startup.
		if _, err := engine.compiler.Compile(spec.Scene, spec.Expression); err != nil {
			logger.Warn("skip invalid static rule",
				zap.String("name", spec.Name),
				zap.String("scene", string(spec.Scene)),
				zap.Error(err))
			continue
		}
		staticRules = append(staticRules, Rule{
			ID:         int64(-(i + 1)),
			Name:       spec.Name,
			Scene:      spec.Scene,
			Expression: spec.Expression,
			Priority:   spec.Priority,
			Enabled:    true,
			Metadata:   spec.Metadata,
		})
	}

	// Derive a cancellable context so the reload loop goroutine can be
	// stopped via Engine.Stop. The cancel func is stored on the engine.
	loopCtx, cancel := context.WithCancel(ctx)
	engine.reloadCancel = cancel

	if err := engine.Load(loopCtx, staticRules); err != nil {
		cancel()
		return nil, fmt.Errorf("load static rules: %w", err)
	}

	// When a Store is configured, perform an initial Reload so the
	// engine picks up database-managed rules. Reload replaces the
	// static rule set with the Store's enabled rules; a failure here
	// is non-fatal because the static rules (if any) remain loaded.
	if cfg.Store != nil {
		if err := engine.Reload(loopCtx, cfg.Store); err != nil {
			logger.Warn("initial reload from store failed", zap.Error(err))
		}
		// Wire the engine's reload closure to the process configuration
		// bus so rule changes are applied in real time. The polling loop
		// below remains as a fallback for lost notifications.
		if cfg.ReloadSubscriber != nil {
			store := cfg.Store
			cfg.ReloadSubscriber(func(reloadCtx context.Context) error {
				return engine.Reload(reloadCtx, store)
			})
		}
		if cfg.EvalInterval > 0 {
			go engine.runReloadLoop(loopCtx, cfg.Store, cfg.EvalInterval)
		}
	}

	// Inject the MetricMatcher as a pre-filter for alert dispatch.
	// When no metric-scene rules are loaded, MetricMatcher.Match
	// returns true so the default-allow prism semantics are
	// preserved.
	metricMatcher := NewMetricMatcher(engine, cfg.AssetStore)
	target.AddRule(metricMatcher)

	logger.Info("rule engine registered",
		zap.Int("static_rules", len(staticRules)),
		zap.Bool("store_enabled", cfg.Store != nil),
		zap.Bool("asset_store_enabled", cfg.AssetStore != nil),
		zap.Duration("eval_interval", cfg.EvalInterval))
	return engine, nil
}
