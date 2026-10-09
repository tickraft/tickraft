// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/expr"
)

// Engine is the alert rule matching core. It owns the compiled rule
// snapshot and exposes a concurrent Evaluate method that runs the rules
// against an AlertEnv.
//
// Evaluate acquires a read lock only long enough to snapshot the
// compiled rule slice; the actual evaluations happen outside the lock so
// concurrent evaluators do not contend. Load and Reload acquire a write
// lock to swap the snapshot atomically.
//
// Tenant filtering is an engine internal: a rule applies to an event
// when its TenantID is 0 (global) or equals the event's tenant. It is
// never expressed as an expression variable.
type Engine struct {
	mu        sync.RWMutex
	rules     []compiledRule
	compiler  *Compiler
	extractor *ViolationExtractor
	logger    *zap.Logger

	// reloadCancel cancels the context that governs the background
	// reload loop launched by Register. It is nil when no reload loop
	// was started. Stop invokes it to ensure the goroutine exits.
	reloadCancel context.CancelFunc
}

// compiledRule is the engine's unit of evaluation: the rule model
// paired with its compiled program, produced once at Load time. The
// snapshot slice of compiledRule replaces the former separate rule
// slice and per-rule program map, so evaluation follows one pointer per
// rule with no map lookup on the hot path.
type compiledRule struct {
	rule    Rule
	program *expr.Program
}

// NewEngine creates an Engine with its own Compiler and
// ViolationExtractor. A nil logger is replaced with a no-op logger so
// callers never need to nil-check.
func NewEngine(logger *zap.Logger) *Engine {
	if logger == nil {
		logger = zap.NewNop()
	}
	compiler := NewCompiler()
	return &Engine{
		compiler:  compiler,
		extractor: NewViolationExtractor(compiler),
		logger:    logger,
	}
}

// Load compiles and installs the supplied rules, replacing any
// previously loaded rule set. Rules are compiled individually;
// compilation failures are logged and skipped so one bad rule does not
// poison the whole batch. The new snapshot is committed atomically
// under the write lock.
func (e *Engine) Load(ctx context.Context, rules []Rule) error {
	compiled, err := e.compileAll(ctx, rules)
	if err != nil {
		return err
	}

	e.mu.Lock()
	e.rules = compiled
	e.mu.Unlock()

	// Drop cached violation sub-programs compiled for the previous rule
	// set so retired expressions do not accumulate across reloads. The
	// extractor lazily recompiles sub-programs on the next Evaluate
	// call.
	if e.extractor != nil {
		e.extractor.Reset()
	}

	e.logger.Info("alert rule engine loaded",
		zap.Int("rules", len(compiled)))
	return nil
}

// Reload re-reads enabled rules from the Store and replaces the
// in-memory rule set. A listing failure aborts the reload without
// mutating state so the engine continues serving the previously loaded
// rules.
func (e *Engine) Reload(ctx context.Context, store Lister) error {
	rules, err := store.ListEnabled(ctx, 0)
	if err != nil {
		return err
	}
	return e.Load(ctx, rules)
}

// Evaluate runs the loaded rules against env in a single pass and
// returns both outcomes of that evaluation: the identity of all matching
// rules (for dispatch filtering and rule-hit attribution) and one
// Violation for every matched metric-fact comparison across all
// matching rules (for structured dispatch). A compound rule such as
// `metrics["cpu"] > 90 && metrics["mem"] > 85` contributes two
// Violations when both conditions hold; a predicate-only rule (for
// example `type == "status"`) matches without contributing
// Violations. Both results are nil when no rule matches.
//
// Each rule's program runs exactly once per evaluation. When a rule
// matches, the ViolationExtractor is invoked with matched=true; for
// pure-conjunction rules it builds Violations without re-evaluating the
// comparisons (a matched conjunction implies every sub-condition is
// true), and only rules containing || fall back to per-comparison
// evaluation.
//
// Rules scoped to another tenant are skipped. Disabled rules, and rules
// whose program fails to evaluate, are skipped without affecting
// sibling rules (fail-closed per rule, never per batch).
func (e *Engine) Evaluate(
	ctx context.Context, tenantID int64, env AlertEnv,
) (matched []MatchedRule, violations []Violation) {
	e.mu.RLock()
	rulesSnapshot := e.rules
	extractor := e.extractor
	e.mu.RUnlock()

	for i := range rulesSnapshot {
		cr := &rulesSnapshot[i]
		if !cr.rule.Enabled {
			continue
		}
		if cr.rule.TenantID != 0 && cr.rule.TenantID != tenantID {
			continue
		}
		ruleMatched, err := expr.RunBool(cr.program, env)
		if err != nil {
			e.logger.Warn("rule eval failed",
				zap.Int64("rule_id", cr.rule.ID),
				zap.Error(err))
			continue
		}
		if !ruleMatched {
			continue
		}
		matched = append(matched, MatchedRule{ID: cr.rule.ID, Name: cr.rule.Name})
		if extractor != nil {
			// Pass matched=true so the extractor can skip
			// per-comparison re-evaluation for pure-conjunction rules
			// (the common case).
			violations = append(violations, extractor.Extract(ctx, cr.rule, env, true)...)
		}
	}
	return matched, violations
}

// HasRules reports whether any rule is currently loaded. It is used by
// AlertMatcher to implement default-allow semantics: when no rules are
// configured, the matcher forwards every alert so the rule engine never
// silently drops alerts simply because no rules exist.
func (e *Engine) HasRules() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.rules) > 0
}

// compileAll compiles each rule expression through the Compiler and
// returns the compiled snapshot. Compilation failures are logged and
// the offending rule is skipped. A nil input slice yields an empty
// (non-nil) slice so subsequent Evaluate calls do not allocate.
func (e *Engine) compileAll(_ context.Context, rules []Rule) ([]compiledRule, error) {
	compiled := make([]compiledRule, 0, len(rules))
	for i := range rules {
		r := &rules[i]
		program, err := e.compiler.Compile(r.Expression)
		if err != nil {
			e.logger.Warn("skip rule with invalid expression",
				zap.Int64("rule_id", r.ID),
				zap.String("name", r.Name),
				zap.Error(err))
			continue
		}
		compiled = append(compiled, compiledRule{rule: *r, program: program})
	}
	return compiled, nil
}

// runReloadLoop periodically calls Reload from the Store until ctx is
// cancelled. It is intended to be invoked in a dedicated goroutine by
// Register; the caller is responsible for lifecycle management.
func (e *Engine) runReloadLoop(ctx context.Context, store Lister, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.Reload(ctx, store); err != nil {
				e.logger.Warn("reload rules failed", zap.Error(err))
			}
		}
	}
}

// Stop cancels the background reload loop goroutine launched by
// Register. It is safe to call on an Engine whose reload loop was never
// started (reloadCancel is nil) and is idempotent. The provided ctx is
// reserved for future graceful-drain semantics; the current
// implementation cancels the loop immediately so the goroutine exits on
// the next tick.
func (e *Engine) Stop(_ context.Context) error {
	e.mu.Lock()
	cancel := e.reloadCancel
	e.reloadCancel = nil
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}
