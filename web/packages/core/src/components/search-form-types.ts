// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

export type FieldType = 'input' | 'select' | 'date' | 'daterange' | 'cascader' | 'number'

export interface SelectOption {
  label: string
  value: string | number
}

/** Field configuration for SearchForm */
export interface SearchFormField {
  /** Field name */
  prop: string
  /** Label */
  label: string
  /** Control type */
  type: FieldType
  /** Options for select / cascader */
  options?: SelectOption[]
  /** Placeholder hint */
  placeholder?: string
  /** Default value */
  defaultValue?: unknown
  /** Whether clearable; defaults to true */
  clearable?: boolean
  /** Grid span (24-grid system, applied only at lg and above); defaults to 6 */
  span?: number
  /** Whether visible; defaults to true */
  visible?: boolean
  /** Names of fields this field depends on, used for cascade clearing */
  dependencies?: string[]
}

/** Props for SearchForm component */
export interface SearchFormProps {
  /** Field config list */
  fields?: SearchFormField[]
  /** Form initial values */
  modelValue?: Record<string, unknown>
  /** Search loading state */
  loading?: boolean
  /** Whether to show expand/collapse capability; defaults to true */
  showCollapse?: boolean
  /** Collapse threshold; fields beyond this count are collapsed under "Show more"; defaults to 3 */
  collapseThreshold?: number
  /** Whether to require confirmation on reset; defaults to false */
  resetConfirm?: boolean
}
