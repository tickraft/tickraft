// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package types

// EventKind is the cross-domain event category shared by the alert,
// remediation, rule, and telemetry domains. Each domain keeps its own
// typed enum (alert.Type, remediation.TriggerType, rule.Scene,
// telemetry.Kind) but derives the constant values from these canonical
// definitions, so the wire values ("metric", "log", "status_change",
// "heartbeat", "status") can never drift apart across packages.
type EventKind string

const (
	// EventKindMetric identifies events emitted by metric threshold
	// violations.
	EventKindMetric EventKind = "metric"
	// EventKindLog identifies events emitted by log keyword matches.
	EventKindLog EventKind = "log"
	// EventKindStatusChange identifies events emitted by asset status
	// transitions.
	EventKindStatusChange EventKind = "status_change"
	// EventKindHeartbeat identifies events emitted when an asset stops
	// reporting heartbeats.
	EventKindHeartbeat EventKind = "heartbeat"
	// EventKindStatus identifies events emitted when an asset transitions
	// to an abnormal state.
	EventKindStatus EventKind = "status"
)
