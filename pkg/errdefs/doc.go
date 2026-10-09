// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package errdefs defines cross-domain shared error sentinels, error codes,
// and the ErrorCoder interface used to map errors to HTTP status codes.
//
// # Positioning
//
// pkg/errdefs is the single home for error vocabulary that satisfies ALL of
// the following criteria:
//
//   - Cross-domain: an error sentinel or code is referenced by two or more
//     business modules under pkg/ or internal/.
//   - Transport-agnostic: the sentinel itself carries no HTTP-specific
//     behavior; the ErrorCoder interface bridges to transport layers but
//     remains decoupled from net/http.
//   - Anti-duplication: a sentinel introduced here replaces two or more
//     semantically equivalent sentinels previously scattered across
//     packages (e.g. ErrBusNotConfigured previously existed in both
//     pkg/prism/alert and pkg/executor).
//
// Errors that fail the criteria above MUST live in their owning business
// module. Domain-specific errors (e.g. task.ErrTaskAlreadyPaused,
// telemetry.ErrMetricLimitExceeded) are intentionally NOT defined here.
//
// # Wrapping convention
//
// Domain packages that wish to expose a domain-specific error variant of a
// shared sentinel SHOULD wrap the sentinel with fmt.Errorf("domain: %w",
// errdefs.ErrNotFound) so that errors.Is(err, errdefs.ErrNotFound) returns
// true uniformly across the codebase.
//
// # Hard constraints
//
//   - This package MUST NOT import any business module under pkg/ (it is a
//     kernel-level shared package). Only standard library imports are allowed.
//   - Code constants are stable API contracts: do not renumber existing
//     codes. Add new codes at the end of their segment.
package errdefs
