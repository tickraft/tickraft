// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import "context"

// Matcher evaluates whether an alert event should be dispatched to channels.
// A matcher returns true to forward the alert, false to suppress it.
type Matcher interface {
	// Match returns true if the alert event should be dispatched.
	Match(ctx context.Context, evt Event) bool
}

// NamedMatcher is an optional interface that rules may implement to expose their
// name for observability and logging. Rules that do not implement NamedMatcher
// are counted as matched but omitted from the returned name list.
type NamedMatcher interface {
	Matcher
	// Name returns the human-readable rule identifier.
	Name() string
}

// ViolationMatcher is an optional interface that rules may implement to
// return structured Violations for a matched alert event. When a rule
// implements ViolationMatcher, Dispatch calls MatchWithViolations after
// Match returns true and collects the returned violations into
// Event.Violations, replacing the single violation populated by the
// payload converter. This enables compound rules (e.g.
// "cpu > 90 && mem > 85") to contribute one Violation per matched
// condition.
//
// Rules that do not implement ViolationMatcher are unaffected: Dispatch
// continues to use the payload-populated Event.Violations as-is.
type ViolationMatcher interface {
	// MatchWithViolations evaluates the rule and returns all violations
	// for the matched comparison sub-conditions. Returns nil or an empty
	// slice when the rule does not match or produces no structured
	// violations; in that case Dispatch preserves the existing
	// Event.Violations.
	MatchWithViolations(ctx context.Context, evt Event) []Violation
}

// MatcherFunc adapts a function into a Matcher.
type MatcherFunc func(ctx context.Context, evt Event) bool

// Match implements Matcher.
func (f MatcherFunc) Match(ctx context.Context, evt Event) bool {
	return f(ctx, evt)
}

// Channel sends an alert notification to an external system.
type Channel interface {
	// Send delivers the alert payload. It must be safe for concurrent use.
	Send(ctx context.Context, evt Event) error
	// Name identifies the channel in logs and metrics.
	Name() string
}
