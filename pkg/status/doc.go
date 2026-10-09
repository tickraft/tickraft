// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package status provides the public service status page: a singleton
// configuration row (title, description, enable flag, component map), an
// aggregation service that derives per-component and overall health from
// monitor points and their latest probe records, and optional
// infrastructure probes injected by the assembly layer.
//
// The package owns the kernel baseline: one status page driven by the
// monitor topology. The extension edition builds its richer surface
// (multiple pages, themes, custom domains, uptime history, manual
// incidents) on the aggregation seams exported here.
package status
