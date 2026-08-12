// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package prism

import (
	"context"

	"github.com/tickraft/tickraft/pkg/prism/alert"
	"go.uber.org/zap"
)

// Matcher evaluates whether an alert event should be dispatched to channels.
//
// These are type aliases for alert.Matcher so that the canonical interface
// definitions live in the alert domain package, allowing downstream packages
// (rule) to reference them without importing prism.
type Matcher = alert.Matcher

// NamedMatcher is an optional interface that rules may implement to expose
// their name for observability and logging.
type NamedMatcher = alert.NamedMatcher

// ViolationMatcher is an optional interface that rules may implement to
// return structured Violations for a matched alert event.
type ViolationMatcher = alert.ViolationMatcher

// MatcherFunc adapts a function into a Matcher.
type MatcherFunc = alert.MatcherFunc

// AddRule registers an alert rule. Rules are evaluated in registration
// order; if any rule matches, the alert is dispatched. When no rules are
// registered, all alerts are dispatched (default-allow). It must be
// called before Start.
func (e *Engine) AddRule(rule Matcher) {
	if rule == nil {
		return
	}
	e.rulesMu.Lock()
	e.rules = append(e.rules, rule)
	e.rulesMu.Unlock()
}

// Rules returns the registered alert rules. The returned slice is a copy
// and safe to read concurrently with AddRule.
func (e *Engine) Rules() []Matcher {
	e.rulesMu.RLock()
	defer e.rulesMu.RUnlock()
	out := make([]Matcher, len(e.rules))
	copy(out, e.rules)
	return out
}

// UpdateRule replaces the rule identified by oldName with newMatcher. If
// no rule with oldName is registered, newMatcher is appended so that an
// updated rule not previously tracked by the engine (for example, one that
// was disabled at load time) is still registered. If newMatcher is nil the
// rule is removed, making this call equivalent to RemoveRule. Rules that do
// not implement NamedMatcher are skipped during the lookup. It is safe to
// call concurrently with AddRule, RemoveRule and Dispatch.
func (e *Engine) UpdateRule(oldName string, newMatcher Matcher) {
	e.rulesMu.Lock()
	defer e.rulesMu.Unlock()
	for i, r := range e.rules {
		nr, ok := r.(NamedMatcher)
		if !ok {
			continue
		}
		if nr.Name() == oldName {
			if newMatcher == nil {
				e.rules = append(e.rules[:i], e.rules[i+1:]...)
			} else {
				e.rules[i] = newMatcher
			}
			return
		}
	}
	// Rule not found: append newMatcher so an updated rule that was not
	// previously registered is tracked.
	if newMatcher != nil {
		e.rules = append(e.rules, newMatcher)
	}
}

// RemoveRule removes the rule identified by name from the engine. If no
// rule with the given name is registered, the call is a no-op. Rules that
// do not implement NamedMatcher are skipped during the lookup. It is safe
// to call concurrently with AddRule, UpdateRule and Dispatch.
func (e *Engine) RemoveRule(name string) {
	e.rulesMu.Lock()
	defer e.rulesMu.Unlock()
	for i, r := range e.rules {
		nr, ok := r.(NamedMatcher)
		if !ok {
			continue
		}
		if nr.Name() == name {
			e.rules = append(e.rules[:i], e.rules[i+1:]...)
			return
		}
	}
}

// match invokes rule.Match with panic recovery so that a buggy custom
// Matcher implementation cannot crash the prism engine. A panic is recovered,
// logged at error level, and treated as not matching so the alert evaluation
// continues with the remaining rules.
func match(ctx context.Context, rule Matcher, evt alert.Event, logger *zap.Logger) (matched bool) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("rule Match panicked",
				zap.String("event_id", evt.EventID),
				zap.String("type", string(evt.Type)),
				zap.Int64("asset_id", evt.AssetID),
				zap.Any("panic", r),
			)
		}
	}()
	return rule.Match(ctx, evt)
}

// matchWithViolations invokes vm.MatchWithViolations with panic recovery
// so that a buggy custom ViolationMatcher implementation cannot crash the
// prism engine. A panic is recovered, logged at error level, and
// treated as returning no violations so the alert evaluation continues with
// the remaining rules. The returned slice may be nil.
func matchWithViolations(ctx context.Context, vm ViolationMatcher, evt alert.Event, logger *zap.Logger) (violations []alert.Violation) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("rule MatchWithViolations panicked",
				zap.String("event_id", evt.EventID),
				zap.String("type", string(evt.Type)),
				zap.Int64("asset_id", evt.AssetID),
				zap.Any("panic", r),
			)
			violations = nil
		}
	}()
	return vm.MatchWithViolations(ctx, evt)
}
