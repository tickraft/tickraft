// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package governance

import (
	"context"

	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// Decision instructs the engine how to handle an alert event after a
// Guard inspects it.
type Decision int

const (
	// DecisionPass lets the alert continue through the remaining guard
	// chain and rule evaluation, ultimately reaching notification channels.
	DecisionPass Decision = iota
	// DecisionSuppress prevents the alert from reaching notification
	// channels. The event is still recorded (persisted) but no notification
	// is dispatched.
	DecisionSuppress
	// DecisionAggregate merges the alert into an existing aggregation group
	// so it is not dispatched individually. The aggregation group is
	// responsible for emitting a consolidated notification when it closes.
	DecisionAggregate
)

// Guard inspects an alert event before the engine evaluates rules and
// dispatches notifications. The engine calls each registered guard in
// order; the first non-Pass decision short-circuits the chain.
//
// This SPI is defined in the package because the
// Engine.Dispatch itself consumes it (define-at-consumption-site): in a
// single-process default deployment the guard chain is empty or
// carries only the built-in Dedup, so Dispatch proceeds directly to rule
// evaluation. The callers may inject the full governance chain
// (silence -> aggregator -> suppressor -> storm) at startup.
//
// Implementations must be safe for concurrent use and must return quickly to
// avoid blocking the event-bus consumer goroutine.
type Guard interface {
	// Process inspects the alert event and returns a governance decision.
	// The pointer is non-nil; implementations must not retain the reference
	// beyond the call.
	Process(ctx context.Context, evt *alert.Event) Decision
}
