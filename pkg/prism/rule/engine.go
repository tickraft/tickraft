// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"go.uber.org/zap"
)

// Engine is the rule matching core. It owns the per-scene rule
// slices and the shared *vm.Program cache, and exposes concurrent
// Match methods that evaluate the cache against the supplied Env.
//
// All Match methods acquire a read lock only long enough to snapshot
// the rule slice and program map references; the actual expr.Run
// invocations happen outside the lock so concurrent evaluators do not
// contend. Load and Reload acquire a write lock to swap the snapshots
// atomically.
type Engine struct {
	mu               sync.RWMutex
	taskRules        []Rule
	probeRules       []Rule
	metricRules      []Rule
	remediationRules []Rule
	programs         map[int64]*vm.Program
	compiler         *Compiler
	extractor        *ViolationExtractor
	logger           *zap.Logger

	// reloadCancel cancels the context that governs the background
	// reload loop launched by Register. It is nil when no reload loop
	// was started. Stop invokes it to ensure the goroutine exits.
	reloadCancel context.CancelFunc
}

// NewEngine creates an Engine with its own Compiler (default
// config: MaxNodes=1000, MaxComparisons=3) and ViolationExtractor.
// A nil logger is replaced with a no-op logger so callers never need to
// nil-check.
func NewEngine(logger *zap.Logger) *Engine {
	return NewEngineWithConfig(logger, CompilerConfig{})
}

// NewEngineWithConfig creates an Engine with a Compiler built from
// the supplied CompilerConfig. Zero-valued fields fall back to default
// defaults (MaxNodes=1000, MaxComparisons=3); a negative
// MaxComparisons disables the comparison-count check. A nil logger is
// replaced with a no-op logger.
func NewEngineWithConfig(logger *zap.Logger, cfg CompilerConfig) *Engine {
	if logger == nil {
		logger = zap.NewNop()
	}
	compiler := NewCompilerWithConfig(cfg)
	return &Engine{
		programs:  make(map[int64]*vm.Program),
		compiler:  compiler,
		extractor: NewViolationExtractor(compiler),
		logger:    logger,
	}
}

// Load compiles and installs the supplied rules, replacing any
// previously loaded rule set. Rules are grouped by Scene and compiled
// individually; compilation failures are logged and skipped so one
// bad rule does not poison the whole batch. The new rule set and
// program cache are committed atomically under the write lock.
func (e *Engine) Load(ctx context.Context, rules []Rule) error {
	taskRules, probeRules, metricRules, remediationRules, programs, err := e.compileAll(ctx, rules)
	if err != nil {
		return err
	}

	e.mu.Lock()
	e.taskRules = taskRules
	e.probeRules = probeRules
	e.metricRules = metricRules
	e.remediationRules = remediationRules
	e.programs = programs
	e.mu.Unlock()

	// Drop cached violation sub-programs compiled for the previous rule
	// set so retired expressions do not accumulate across reloads. The
	// extractor lazily recompiles sub-programs on the next
	// MatchMetricWithViolations call.
	if e.extractor != nil {
		e.extractor.Reset()
	}

	e.logger.Info("rule engine loaded",
		zap.Int("task_rules", len(taskRules)),
		zap.Int("probe_rules", len(probeRules)),
		zap.Int("metric_rules", len(metricRules)),
		zap.Int("remediation_rules", len(remediationRules)))
	return nil
}

// Reload re-reads enabled rules from the Store across all four
// scenes and replaces the in-memory rule set. A failure listing one
// scene aborts the reload without mutating state so the engine
// continues serving the previously loaded rules.
func (e *Engine) Reload(ctx context.Context, store Lister) error {
	var allRules []Rule
	for _, scene := range []Scene{SceneTask, SceneProbe, SceneMetric, SceneRemediation} {
		models, err := store.ListEnabled(ctx, 0, scene)
		if err != nil {
			return fmt.Errorf("list enabled rules for scene %s: %w", scene, err)
		}
		for i := range models {
			allRules = append(allRules, models[i].toRule())
		}
	}
	return e.Load(ctx, allRules)
}

// MatchTask evaluates the task-scene rules against env and returns
// the IDs of all matching rules. A rule whose program is missing from
// the cache, fails to evaluate, or yields a non-bool result is logged
// and skipped without affecting sibling rules.
func (e *Engine) MatchTask(ctx context.Context, env TaskMatchEnv) []int64 {
	return e.match(ctx, SceneTask, env)
}

// MatchProbe evaluates the probe-scene rules against env and returns
// the IDs of all matching rules. See MatchTask for error-handling
// semantics.
func (e *Engine) MatchProbe(ctx context.Context, env ProbeMatchEnv) []int64 {
	return e.match(ctx, SceneProbe, env)
}

// MatchMetric evaluates the metric-scene rules against env and
// returns the IDs of all matching rules. See MatchTask for
// error-handling semantics.
func (e *Engine) MatchMetric(ctx context.Context, env MetricMatchEnv) []int64 {
	return e.match(ctx, SceneMetric, env)
}

// MatchRemediation evaluates the remediation-scene rules against env
// and returns the IDs of all matching rules. See MatchTask for
// error-handling semantics.
func (e *Engine) MatchRemediation(ctx context.Context, env RemediationMatchEnv) []int64 {
	return e.match(ctx, SceneRemediation, env)
}

// HasMetricRules reports whether any metric-scene rule is currently
// loaded. It is used by MetricMatcher to implement default-allow
// semantics: when no metric rules are configured, the matcher
// forwards every alert so the rule engine never silently drops
// alerts simply because no rules exist.
func (e *Engine) HasMetricRules() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.metricRules) > 0
}

// HasRemediationRules reports whether any remediation-scene rule is
// currently loaded. Callers use it to short-circuit the
// remediation-match path when no rules are configured, mirroring the
// default-allow semantics of HasMetricRules for the metric scene.
func (e *Engine) HasRemediationRules() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.remediationRules) > 0
}

// MatchMetricWithViolations evaluates the metric-scene rules against env
// and returns one alert.Violation for every matched comparison
// sub-condition across all matching rules. A compound rule such as
// `alert.metrics["cpu"] > 90 && alert.metrics["mem"] > 85` contributes
// two Violations when both conditions hold; a single-condition rule
// contributes one. Returns nil when no rule matches or no comparison
// sub-condition holds.
//
// Evaluation strategy: each rule's compiled program is run exactly once
// to determine whether it matches. When it matches, the ViolationExtractor
// is invoked with matched=true. For pure-conjunction rules (only &&  joining
// comparisons) the extractor builds Violations for all comparisons without
// re-evaluating them, since a matched conjunction implies every comparison
// sub-condition is true. For rules containing || the extractor evaluates
// each comparison individually to determine which branches matched. This
// eliminates the previous double-evaluation cost of running the full
// program and then re-running every comparison for the common
// single-comparison and &&-compound cases.
//
// This method complements MatchMetric: MatchMetric returns the matched
// rule IDs (used by the bool MetricMatcher.Match pre-filter for the
// alert engine), while MatchMetricWithViolations returns
// structured violations for dispatch paths that enrich
// alert.Event.Violations. Both methods evaluate the rule set
// independently so callers pick the one matching their needs without
// paying double evaluation cost.
func (e *Engine) MatchMetricWithViolations(ctx context.Context, env MetricMatchEnv) []alert.Violation {
	e.mu.RLock()
	programs := e.programs
	rulesSnapshot := e.metricRules
	extractor := e.extractor
	e.mu.RUnlock()

	if extractor == nil {
		return nil
	}

	var violations []alert.Violation
	for _, r := range rulesSnapshot {
		if !r.Enabled {
			continue
		}
		program, ok := programs[r.ID]
		if !ok {
			e.logger.Warn("rule program not found in cache during violation extraction",
				zap.Int64("rule_id", r.ID))
			continue
		}

		output, err := expr.Run(program, env)
		if err != nil {
			e.logger.Warn("rule eval failed during violation extraction",
				zap.Int64("rule_id", r.ID),
				zap.Error(err))
			continue
		}

		result, ok := output.(bool)
		if !ok || !result {
			continue
		}

		// Pass matched=true so the extractor can skip per-comparison
		// re-evaluation for pure-conjunction rules (the common case),
		// eliminating the double-evaluation cost.
		violations = append(violations, extractor.Extract(ctx, r, env, true)...)
	}
	return violations
}

// match is the shared evaluation loop. It snapshots the scene's rule
// slice and the program map under the read lock, then evaluates each
// rule outside the lock. ctx is currently reserved for future
// deadline-aware evaluation; expr.Run itself is synchronous and does
// not consult ctx.
func (e *Engine) match(ctx context.Context, scene Scene, env any) []int64 {
	_ = ctx

	e.mu.RLock()
	var rulesSnapshot []Rule
	switch scene {
	case SceneTask:
		rulesSnapshot = e.taskRules
	case SceneProbe:
		rulesSnapshot = e.probeRules
	case SceneMetric:
		rulesSnapshot = e.metricRules
	case SceneRemediation:
		rulesSnapshot = e.remediationRules
	}
	programs := e.programs
	e.mu.RUnlock()

	var matched []int64
	for _, r := range rulesSnapshot {
		if !r.Enabled {
			continue
		}
		program, ok := programs[r.ID]
		if !ok {
			e.logger.Warn("rule program not found in cache",
				zap.Int64("rule_id", r.ID),
				zap.String("scene", string(scene)))
			continue
		}

		output, err := expr.Run(program, env)
		if err != nil {
			e.logger.Warn("rule eval failed",
				zap.Int64("rule_id", r.ID),
				zap.String("scene", string(scene)),
				zap.Error(err))
			continue
		}

		result, ok := output.(bool)
		if !ok {
			e.logger.Warn("rule eval returned non-bool",
				zap.Int64("rule_id", r.ID),
				zap.String("scene", string(scene)),
				zap.Any("output", output))
			continue
		}
		if result {
			matched = append(matched, r.ID)
		}
	}
	return matched
}

// compileAll groups rules by scene, compiles each expression through
// the Compiler, and returns the per-scene slices plus the shared
// program cache. Compilation failures are logged and the offending
// rule is skipped. A nil input slice yields empty (non-nil) per-scene
// slices so subsequent Match calls do not allocate.
func (e *Engine) compileAll(ctx context.Context, rules []Rule) ([]Rule, []Rule, []Rule, []Rule, map[int64]*vm.Program, error) {
	_ = ctx

	taskRules := make([]Rule, 0)
	probeRules := make([]Rule, 0)
	metricRules := make([]Rule, 0)
	remediationRules := make([]Rule, 0)
	programs := make(map[int64]*vm.Program, len(rules))

	for _, r := range rules {
		program, err := e.compiler.Compile(r.Scene, r.Expression)
		if err != nil {
			e.logger.Warn("skip rule with invalid expression",
				zap.Int64("rule_id", r.ID),
				zap.String("name", r.Name),
				zap.String("scene", string(r.Scene)),
				zap.Error(err))
			continue
		}

		switch r.Scene {
		case SceneTask:
			taskRules = append(taskRules, r)
		case SceneProbe:
			probeRules = append(probeRules, r)
		case SceneMetric:
			metricRules = append(metricRules, r)
		case SceneRemediation:
			remediationRules = append(remediationRules, r)
		default:
			e.logger.Warn("skip rule with unknown scene",
				zap.Int64("rule_id", r.ID),
				zap.String("scene", string(r.Scene)))
			continue
		}
		programs[r.ID] = program
	}

	return taskRules, probeRules, metricRules, remediationRules, programs, nil
}

// runReloadLoop periodically calls Reload from the Store until ctx
// is cancelled. It is intended to be invoked in a dedicated goroutine
// by Register; the caller is responsible for lifecycle management.
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

// Stop cancels the background reload loop goroutine launched by Register.
// It is safe to call on an Engine whose reload loop was never started
// (reloadCancel is nil) and is idempotent. The provided ctx is reserved
// for future graceful-drain semantics; the current implementation cancels
// the loop immediately so the goroutine exits on the next tick.
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
