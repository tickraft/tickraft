// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

/**
 * Wizard → expression generation (rule-engine-design §9.2 mapping
 * table). Only structurally complete rows participate; the caller
 * decides what to do when the model is incomplete.
 */

import type { ExprEnv } from './catalog'
import { findVariable, isMapKind, operatorsForKind } from './catalog'
import type { ConditionRow, WizardModel } from './model'

/** Render the left-hand variable reference (`metrics["cpu"]`). */
function renderLhs(row: ConditionRow): string {
  if (row.key) {
    return `${row.variable}[${JSON.stringify(row.key)}]`
  }
  return row.variable
}

/** Parse a comma-separated list for the `in` operator into an array literal. */
function renderList(value: string): string | null {
  const items = value
    .split(',')
    .map((item) => item.trim())
    .filter((item) => item.length > 0)
  if (items.length === 0) return null
  return `[${items.map((item) => JSON.stringify(item)).join(', ')}]`
}

/** Render one complete row, or null when it is structurally invalid. */
function renderRow(env: ExprEnv, row: ConditionRow): string | null {
  if (!row.variable || !row.operator || !row.value.trim()) return null
  const def = findVariable(env, row.variable)
  if (!def) return null

  const needsKey = isMapKind(def.kind)
  if (needsKey !== Boolean(row.key)) return null

  const kind = needsKey
    ? def.kind === 'numberMap'
      ? 'number'
      : 'string'
    : def.kind
  if (!operatorsForKind(kind).includes(row.operator)) return null

  let rhs: string
  if (row.operator === 'in') {
    const list = renderList(row.value)
    if (!list) return null
    rhs = list
  } else if (kind === 'number') {
    if (Number.isNaN(Number(row.value))) return null
    rhs = String(Number(row.value))
  } else {
    rhs = JSON.stringify(row.value)
  }

  const comparison = `${renderLhs(row)} ${row.operator} ${rhs}`
  return row.negate ? `!(${comparison})` : comparison
}

/** Result of a generation attempt. */
export interface GenerateResult {
  /** Whether every row was structurally complete and valid. */
  ok: boolean
  /** The generated expression (undefined when ok is false). */
  expression?: string
}

/**
 * Compile the wizard model into an expression string. An empty row
 * list is "valid" and yields the empty expression (optional envs treat
 * it as default semantics; required envs reject it at the editor).
 */
export function generateExpression(env: ExprEnv, model: WizardModel): GenerateResult {
  const parts: string[] = []
  for (const row of model.rows) {
    const rendered = renderRow(env, row)
    if (rendered === null) {
      return { ok: false }
    }
    parts.push(rendered)
  }
  if (parts.length === 0) {
    return { ok: true, expression: '' }
  }
  const joiner = model.combinator === 'or' ? ' || ' : ' && '
  return { ok: true, expression: parts.join(joiner) }
}
