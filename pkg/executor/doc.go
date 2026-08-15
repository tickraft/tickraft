// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package executor implements the task execution engine for tickraft.
//
// It is a sibling module to pkg/scheduler, containing the Executor SPI
// (Service Provider Interface), the Registry for executor lookup, and the
// Runner that subscribes to execution trigger events and executes them through
// a bounded worker pool with retry support.
//
// The execution flow is event-driven: the Runner subscribes to
// event.TypeExecutionTriggered on the event bus, constructs an ExecutionRequest
// from the trigger payload, looks up the appropriate Executor via the
// Registry, and executes it. On completion it publishes an
// event.TypeExecutionCompleted event carrying the result and inferred asset
// status.
//
// # Dependencies
//
// This package imports pkg/event, pkg/asset, pkg/pool, pkg/retry, and
// pkg/timewheel. It does NOT import pkg/scheduler, avoiding circular
// dependencies.
//
// The TargetConfig and Result types are defined in this package and serve as
// the shared data contracts between the Runner and individual Executor
// implementations.
package executor
