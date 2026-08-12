// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package governance provides the alert governance SPI and a basic in-process
// implementation for the default deployment.
//
// Guard is the SPI consumed by the prism alert Engine
// (pkg/prism/alert): the engine invokes each registered Guard before
// rule evaluation, and the first non-Pass decision short-circuits the chain.
// In a single-process default deployment the chain is either empty
// (direct dispatch) or carries the built-in Dedup guard; the extended
// edition injects the full governance chain (silence -> aggregator ->
// suppressor -> storm) at startup.
//
// This package is defined in the the repository because the
// Engine itself consumes it (define-at-consumption-site). The
// shared alert.Event type referenced by Guard.Process lives in
// pkg/prism/alert.
package governance
