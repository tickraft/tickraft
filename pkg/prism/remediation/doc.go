// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package remediation implements the basic automated fault self-healing
// engine for the default deployment.
//
// The Engine subscribes to telemetry alert events on the event bus,
// evaluates registered remediation Rules against each event, and dispatches
// matching rules to a registered Operator for execution. The default
// edition ships only the LocalOperator (host script execution via
// pkg/executor/local); callers may inject additional operators
// (SSH, MySQL, Redis, ...) by registering them against the same Operator
// SPI defined here.
//
// Three safety mechanisms prevent runaway self-healing:
//   - Idempotency: a (rule, asset) pair currently executing is skipped until
//     the in-flight execution completes, so a flapping trigger cannot stack
//     up duplicate executions.
//   - Cooldown: a rule is not re-triggered within its configured Cooldown
//     window after the last execution.
//   - Circuit breaker: a rule whose consecutive failures reach
//     CircuitBreakerThreshold is paused (Status set to "paused") so a
//     persistently failing remediation does not hammer the target.
//
// The trigger condition (Rule.Expression) is evaluated against the
// RemediationEnv variable contract through the pkg/expr kernel with a
// bounded LRU program cache. An empty expression matches all events of
// the rule's trigger type. No custom functions are registered; entry
// points validate expressions via ValidateExpression (compile + domain
// check + sample evaluation) before persisting.
//
// This package is the base. Advanced capabilities — post-
// remediation verification, success-rate circuit breaking, global
// concurrency limits, remote operators — live in the callers
// and extend the types defined here via embedding.
package remediation
