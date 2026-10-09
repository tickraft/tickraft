// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

/**
 * Wizard condition-row model. This is
 * frontend-internal state only — the generated expression string is
 * the single source of truth persisted to the backend.
 */

/** One condition row: variable × operator × value, optionally negated. */
export interface ConditionRow {
  /** Catalog path of the variable (`severity`, `asset.tags`). */
  variable: string
  /** Key for map variables (`metrics["cpu"]` → `cpu`). */
  key: string
  /** Comparison operator. */
  operator: string
  /** Raw value text; numbers and string lists are parsed on generation. */
  value: string
  /** Row-level NOT (rendered as `!(...)`). */
  negate: boolean
}

/** The whole wizard state: rows joined by a single combinator. */
export interface WizardModel {
  /** How rows combine: `&&` (and) or `||` (or). */
  combinator: 'and' | 'or'
  rows: ConditionRow[]
}

/** An empty wizard model. */
export function emptyModel(): WizardModel {
  return { combinator: 'and', rows: [] }
}
