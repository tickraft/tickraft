// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package prism

import (
	"context"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// AddRule registers an alert rule. Rules are evaluated in registration
// order; if any rule matches, the alert is dispatched. When no rules are
// registered, all alerts are dispatched (default-allow). It must be
// called before Start.
func (e *Engine) AddRule(rule alert.Matcher) {
	if rule == nil {
		return
	}
	e.rulesMu.Lock()
	e.rules = append(e.rules, rule)
	e.rulesMu.Unlock()
}

// Rules returns the registered alert rules. The returned slice is a copy
// and safe to read concurrently with AddRule.
func (e *Engine) Rules() []alert.Matcher {
	e.rulesMu.RLock()
	defer e.rulesMu.RUnlock()
	out := make([]alert.Matcher, len(e.rules))
	copy(out, e.rules)
	return out
}

// match invokes rule.Match with panic recovery so that a buggy custom
// Matcher implementation cannot crash the prism engine. A panic is recovered,
// logged at error level, and treated as not matching so the alert evaluation
// continues with the remaining rules.
func match(ctx context.Context, rule alert.Matcher, evt alert.Event, logger *zap.Logger) (result alert.MatchResult) {
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
