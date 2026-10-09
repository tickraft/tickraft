// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

/** Pure-frontend expr builder: catalog, wizard model, generation, and
 * reverse parsing. */
export type { ExprEnv, ValueKind, VariableDef } from './catalog'
export {
  CATALOG,
  NUMBER_OPERATORS,
  STRING_OPERATORS,
  OPERATOR_LABEL_KEYS,
  findVariable,
  operatorsForKind,
  isMapKind,
} from './catalog'
export type { ConditionRow, WizardModel } from './model'
export { emptyModel } from './model'
export type { GenerateResult } from './generate'
export { generateExpression } from './generate'
export { parseExpression } from './parse'
