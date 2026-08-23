// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package telemetry implements the unified observation engine for tickraft.
// It combines active probing (ProberService) and passive listening
// (HTTP webhook listener) into a single pipeline: Validator -> Processor ->
// StateManager -> Emitter -> Aggregator -> Persistence.
//
// The package ships a concrete Engine implementation, an HTTP
// webhook Listener, device/task Processors, a tumbling-window Aggregator,
// GORM-backed MetricStore/LogStore, and a template store seeded with
// built-in probe templates. callers may plug in additional
// Processors and probe executors via the exported registries and WithXxx
// Options without modifying the core.
package telemetry
