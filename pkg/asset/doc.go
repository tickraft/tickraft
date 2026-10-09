// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package asset provides the unified asset abstraction model shared by the
// Scheduler and Collector modules.
//
// Asset represents an entity that can be scheduled (by Scheduler) and/or
// observed (by Collector). The package exposes a 4-state base Status
// enumeration (normal, abnormal, offline, unknown) used by kernel-side
// state display. callers may add a 6-state business enum (adding
// warning, error, maintenance) and map those back to the base states for
// kernel consumption.
package asset
