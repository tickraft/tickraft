// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package prism

import (
	"context"

	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// Channel sends an alert notification to an external system.
//
// This is a type alias for alert.Channel so that the canonical interface
// definition lives in the alert domain package, allowing downstream packages
// (channel, rule) to reference it without importing prism.
type Channel = alert.Channel

// OnAlertFunc is invoked when an alert event matches the registered rules
// (or when no rules are registered, in which case all alerts match). It is
// called synchronously within dispatch before channel notification, so
// implementations must return quickly to avoid blocking the event bus
// consumer. Persistent side effects (e.g. writing to a store) should be
// offloaded when they may block.
type OnAlertFunc func(ctx context.Context, evt alert.Event)

// PostGuardHook is invoked after an alert passes the governance guard chain
// (every guard returned DecisionPass) and before rule evaluation. The
// callers may use this SPI to notify the Suppressor about active
// source alerts so that future dependent target alerts are suppressed.
//
// The hook receives a pointer to the Event so mutations (e.g. stamping
// enrichment fields) are visible to subsequent rule evaluation and channel
// dispatch. Implementations must be safe for concurrent use and must return
// quickly to avoid blocking the dispatch path; long-running side effects
// should be offloaded. A nil hook is a no-op, preserving the
// behaviour where no post-guard notification is required.
type PostGuardHook func(ctx context.Context, evt *alert.Event)

// DeadLetterHandler receives notification events that could not be dispatched
// because the worker pool rejected them (e.g. pool at capacity). The handler
// may persist the event to a dead-letter queue for later retry, or it may be
// nil (default) in which case rejected notifications are logged and dropped.
//
// Implementations must be safe for concurrent use and must return quickly to
// avoid blocking the dispatch path.
type DeadLetterHandler interface {
	// HandleDeadLetter is invoked when a notification dispatch fails to be
	// submitted to the worker pool. The event and channel name identify
	// what was supposed to be sent.
	HandleDeadLetter(ctx context.Context, evt alert.Event, channelName string)
}
