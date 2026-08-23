// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// Target is the minimal interface Register needs from the prism
// engine. It is defined here (at the consumption site) so the alert
// package does not need to import prism, avoiding a circular
// dependency.
type Target interface {
	AddRule(m Matcher)
}

// Spec is the static configuration specification used to load rules
// from configuration files at startup. Unlike Rule, it omits runtime
// fields such as ID and TenantID which are assigned by the calling
// context (Register assigns negative IDs so static rules never collide
// with database-assigned ones).
type Spec struct {
	// Name is the human-readable rule name.
	Name string
	// Expression is the expr-lang source text compiled by the Compiler.
	Expression string
	// Priority orders rules; higher values fire first.
	Priority int
	// Metadata holds optional extension key-value pairs.
	Metadata map[string]string
}

// Register is the startup entry point for the alert rule matching
// engine. It mirrors the Register pattern used by other subsystems
// (xchannel.Register, xcollector.Register): it builds the Engine,
// compiles static rules, optionally pulls dynamic rules from a Store,
// and injects the AlertMatcher into the prism Engine so alerts are
// filtered before dispatch.
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
// Invalid static rules are not pre-validated here; Engine.Load compiles
// every rule, logs and skips the ones that fail, so one bad expression
// never blocks registration of the remaining rules.
//
// When cfg.Store is non-nil and cfg.EvalInterval is positive, Register
// launches engine.runReloadLoop in a background goroutine. The goroutine
// is tied to a cancellable context derived from ctx, so it exits when
// either ctx is cancelled or Engine.Stop is called.
func Register(ctx context.Context, target Target, cfg Config) (*Engine, error) {
	if !cfg.IsEnabled() {
		return nil, nil
	}
	if target == nil {
		return nil, fmt.Errorf("rule: target is nil")
	}

	logger := cfg.logger()
	engine := NewEngine(logger)

	// Build the static rule set. Static rules receive negative IDs so
	// they never collide with database-assigned IDs (which are always
	// positive). Invalid expressions are skipped by Load's compile pass,
	// which logs the rule name alongside the failure.
	staticRules := make([]Rule, 0, len(cfg.Rules))
	for i, spec := range cfg.Rules {
		staticRules = append(staticRules, Rule{
			ID:         int64(-(i + 1)),
			Name:       spec.Name,
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

	// Inject the AlertMatcher as a pre-filter for alert dispatch.
	// When no rules are loaded, AlertMatcher.Match forwards every alert
	// so the default-allow prism semantics are preserved.
	target.AddRule(NewAlertMatcher(engine, cfg.AssetStore))

	logger.Info("alert rule engine registered",
		zap.Int("static_rules", len(staticRules)),
		zap.Bool("store_enabled", cfg.Store != nil),
		zap.Bool("asset_store_enabled", cfg.AssetStore != nil),
		zap.Duration("eval_interval", cfg.EvalInterval))
	return engine, nil
}
