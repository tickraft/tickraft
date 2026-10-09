// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package types defines cross-domain shared value objects and constants
// used across the tickraft kernel.
//
// # Positioning
//
// pkg/types is the single home for value objects and constants that
// satisfy ALL of the following criteria:
//
//   - Cross-domain: a type or constant is referenced by two or more
//     business modules under pkg/ or internal/.
//   - Value-object semantics: the type carries no identity, no lifecycle,
//     and no persistence concerns. It is a pure value.
//   - Anti-duplication: introducing the type here replaces two or more
//     semantically equivalent definitions previously scattered across
//     packages (e.g. Severity was previously a bare string literal in
//     8+ packages; AssetType was duplicated between pkg/asset and
//     pkg/auth).
//
// Aggregates, entities, and persistence models MUST NOT live here; they
// belong in their owning business module's model.go. The shared Kernel
// holds only value objects and constants.
//
// # Hard constraints
//
//   - This package MUST NOT import any business module under pkg/ (it is a
//     kernel-level shared package). Only standard library imports are
//     allowed.
//   - Value-object types defined here are JSON-serializable: they MUST
//     marshal and unmarshal cleanly via encoding/json so they can be
//     embedded in event payloads and API responses.
//   - Domain packages MUST NOT re-export types defined here via type
//     aliases. Callers import pkg/types directly.
package types
