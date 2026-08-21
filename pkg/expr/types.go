// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package expr

// DomainMap is a map-backed domain object (asset, metric, status) whose
// field set is closed by the env contract. Env struct definitions use it
// for their domain objects; both `asset.id` and `asset["id"]` reference
// the same value.
//
// The type exists to make typo detection possible: because the element
// type is any, the expr-lang compiler accepts any field name on a
// DomainMap, and a missing key silently evaluates to nil at runtime.
// Validate therefore verifies that constant member accesses on
// DomainMap values (asset.naem) reference keys present in the sample
// env, where a plain sample run alone cannot tell a typo from a nil.
//
// Open data maps whose keys are user data (metrics, asset.tags) stay
// plain map types (map[string]float64, map[string]string) and are
// exempt from the key check — their keys cannot be enumerated by a
// sample.
type DomainMap map[string]any
