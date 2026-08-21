// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

/**
 * Variable catalog of the three expression environments
 * (rule-engine-design §9.3).
 *
 * The catalog is the single source of the wizard's variable dropdown,
 * the operator filter, and the expert-mode cheat sheet. It must stay
 * in sync with the backend env definitions:
 *
 *   alert       pkg/prism/alert/rule AlertEnv
 *   remediation pkg/prism/remediation RemediationEnv
 *   execution   pkg/executor ExecutionEnv
 */

/** Identifier of an evaluation environment (matches /expr/validate). */
export type ExprEnv = 'alert' | 'remediation' | 'execution'

/** Value kind of a variable, driving operator filtering and input shape. */
export type ValueKind = 'string' | 'number' | 'stringMap' | 'numberMap'

/** One catalog entry: the dotted path as written in expressions. */
export interface VariableDef {
  /** Variable path (`severity`, `asset.name`, `metrics`). */
  path: string
  /** Value kind exposed to comparisons. */
  kind: ValueKind
  /** i18n key of the one-line description shown in the UI. */
  descKey: string
}

/** Comparison operators offered for number-kind operands. */
export const NUMBER_OPERATORS = ['==', '!=', '>', '>=', '<', '<='] as const

/** Comparison operators offered for string-kind operands. */
export const STRING_OPERATORS = ['==', '!=', 'contains', 'startsWith', 'endsWith', 'matches', 'in'] as const

/** i18n key lookup for operator labels. */
export const OPERATOR_LABEL_KEYS: Record<string, string> = {
  '==': 'prism.expr.operators.eq',
  '!=': 'prism.expr.operators.neq',
  '>': 'prism.expr.operators.gt',
  '>=': 'prism.expr.operators.gte',
  '<': 'prism.expr.operators.lt',
  '<=': 'prism.expr.operators.lte',
  contains: 'prism.expr.operators.contains',
  startsWith: 'prism.expr.operators.startsWith',
  endsWith: 'prism.expr.operators.endsWith',
  matches: 'prism.expr.operators.matches',
  in: 'prism.expr.operators.in',
}

const desc = (key: string): string => `prism.expr.vars.${key}`

/** The variable catalog per environment. */
export const CATALOG: Record<ExprEnv, VariableDef[]> = {
  alert: [
    { path: 'type', kind: 'string', descKey: desc('type') },
    { path: 'severity', kind: 'string', descKey: desc('severity') },
    { path: 'source', kind: 'string', descKey: desc('source') },
    { path: 'keyword', kind: 'string', descKey: desc('keyword') },
    { path: 'content', kind: 'string', descKey: desc('content') },
    { path: 'metrics', kind: 'numberMap', descKey: desc('metrics') },
    { path: 'asset.id', kind: 'number', descKey: desc('assetId') },
    { path: 'asset.name', kind: 'string', descKey: desc('assetName') },
    { path: 'asset.type', kind: 'string', descKey: desc('assetType') },
    { path: 'asset.tags', kind: 'stringMap', descKey: desc('assetTags') },
  ],
  remediation: [
    { path: 'trigger', kind: 'string', descKey: desc('trigger') },
    { path: 'level', kind: 'string', descKey: desc('level') },
    { path: 'keyword', kind: 'string', descKey: desc('keyword') },
    { path: 'content', kind: 'string', descKey: desc('content') },
    { path: 'source', kind: 'string', descKey: desc('source') },
    { path: 'threshold', kind: 'number', descKey: desc('threshold') },
    { path: 'metric.name', kind: 'string', descKey: desc('metricName') },
    { path: 'metric.value', kind: 'number', descKey: desc('metricValue') },
    { path: 'status.previous', kind: 'string', descKey: desc('statusPrevious') },
    { path: 'status.current', kind: 'string', descKey: desc('statusCurrent') },
    { path: 'asset.id', kind: 'number', descKey: desc('assetId') },
    { path: 'asset.key', kind: 'string', descKey: desc('assetKey') },
    { path: 'asset.name', kind: 'string', descKey: desc('assetName') },
    { path: 'asset.type', kind: 'string', descKey: desc('assetType') },
    { path: 'asset.tags', kind: 'stringMap', descKey: desc('assetTags') },
  ],
  execution: [
    { path: 'code', kind: 'number', descKey: desc('code') },
    { path: 'body', kind: 'string', descKey: desc('body') },
    { path: 'error', kind: 'string', descKey: desc('error') },
    { path: 'duration', kind: 'number', descKey: desc('duration') },
    { path: 'metrics', kind: 'numberMap', descKey: desc('metricsExec') },
  ],
}

/** Look up a variable definition by path. */
export function findVariable(env: ExprEnv, path: string): VariableDef | undefined {
  return CATALOG[env].find((v) => v.path === path)
}

/** Operators offered for a variable's element kind. */
export function operatorsForKind(kind: ValueKind): readonly string[] {
  switch (kind) {
    case 'number':
    case 'numberMap':
      return NUMBER_OPERATORS
    default:
      return STRING_OPERATORS
  }
}

/** Whether the variable needs a key input (`metrics["cpu"]`). */
export function isMapKind(kind: ValueKind): boolean {
  return kind === 'numberMap' || kind === 'stringMap'
}
