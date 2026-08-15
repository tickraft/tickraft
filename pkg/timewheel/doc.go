// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package timewheel provides a hierarchical time wheel for scheduling
// callbacks with second-level precision.
//
// The implementation uses a two-layer wheel (seconds + minutes) driven
// by a single goroutine. Expired callbacks are dispatched to a worker
// pool for asynchronous execution without blocking the wheel tick.
package timewheel
