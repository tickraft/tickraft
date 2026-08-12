// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package quota implements the default quota [Provider]. It contains the
// fixed ceiling table and a [Register] function that injects the
// [DefaultProvider] into pkg/quota at startup.
//
// # Default Ceilings
//
// The runtime enforces the following fixed quotas:
//
//	monitoring assets (devices): 20
//	active probers:              20
//	scheduled tasks:             20
//	remediation rules:            5
//	minimum probe interval:     60s
//	minimum task interval:      60s
//	daily event ingestion:  100000
//	host monitoring:              0
//
// Source compilation remains the extension point for lifting these ceilings.
//
// # Hard constraints
//
//   - This package is internal: it is NOT importable by downstream
//     repositories.
//   - The constants here are the stable public contract of the runtime.
package quota
