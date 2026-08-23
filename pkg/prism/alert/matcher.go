// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"

	"github.com/tickraft/tickraft/pkg/asset"
)

// MatchResult is the outcome of evaluating an alert event against the
// configured rules. Forward is the dispatch decision; Violations is the
// structured detail of the same evaluation.
type MatchResult struct {
	// Forward reports whether the event should be dispatched to
	// channels.
	Forward bool
	// Violations carries one Violation per matched comparison
	// sub-condition across all matching rules, so a compound rule such
	// as `metrics["cpu"] > 90 && metrics["mem"] > 85` contributes two.
	// It is nil when no rule matched or no matched rule contains
	// comparison sub-conditions; callers merge it into Event.Violations.
	Violations []Violation
}

// Matcher evaluates whether an alert event should be dispatched to
// channels. Match performs a single evaluation that yields both the
// forward decision and the structured violations — there is no separate
// violations pass, so implementations must not evaluate the rule set
// twice.
type Matcher interface {
	// Match evaluates the alert event and returns the dispatch decision
	// together with the violations of every matched rule.
	Match(ctx context.Context, evt Event) MatchResult
}

// Channel sends an alert notification to an external system.
type Channel interface {
	// Send delivers the alert payload. It must be safe for concurrent use.
	Send(ctx context.Context, evt Event) error
	// Name identifies the channel in logs and metrics.
	Name() string
}

// Compile-time assertion that AlertMatcher satisfies Matcher. Failures
// surface at build time rather than at registration time.
var _ Matcher = (*AlertMatcher)(nil)

// AlertMatcher adapts the rule Engine to the Matcher interface, acting
// as a pre-filter for alert dispatch. Injected into the prism engine
// via AddRule, it evaluates the event once against the loaded rules and
// reports Forward=true when at least one rule matches. An empty rule
// set forwards every alert (default-allow) so the rule engine never
// silently drops alerts simply because no rules are configured.
//
//nolint:revive // AlertMatcher is the design-doc contract name, kept for cross-repo symmetry with tickraft-x
type AlertMatcher struct {
	engine *Engine
	store  asset.Getter
}

// NewAlertMatcher creates an AlertMatcher backed by the supplied engine
// and optional asset.Getter (satisfied by any asset.Store). The getter
// enriches the evaluation environment with the asset associated with
// the alert; a nil getter leaves the asset domain limited to the
// event's asset id.
func NewAlertMatcher(engine *Engine, store asset.Getter) *AlertMatcher {
	return &AlertMatcher{engine: engine, store: store}
}

// buildEnv projects the alert into an AlertEnv, enriching the asset
// domain from the asset store when the lookup succeeds. A failed
// lookup is non-fatal: the env keeps the event's asset id and empty
// name/type/tags.
func (m *AlertMatcher) buildEnv(ctx context.Context, evt Event) AlertEnv {
	var res *asset.Asset
	if m.store != nil {
		if found, err := m.store.GetByID(ctx, evt.AssetID); err == nil && found != nil {
			res = found
		}
	}
	return buildAlertEnv(evt, res)
}

// Match implements Matcher. It projects the alert into an AlertEnv,
// evaluates the rules once, and packages both outcomes: Forward is true
// when at least one rule matched (or when no rules are loaded, the
// default-allow contract), and Violations carries the structured
// violations of the matched rules.
func (m *AlertMatcher) Match(ctx context.Context, evt Event) MatchResult {
	// Default-allow semantics: when no rules are loaded the matcher
	// forwards every alert without evaluation.
	if !m.engine.HasRules() {
		return MatchResult{Forward: true}
	}
	env := m.buildEnv(ctx, evt)
	matched, violations := m.engine.Evaluate(ctx, evt.TenantID, env)
	return MatchResult{Forward: len(matched) > 0, Violations: violations}
}
