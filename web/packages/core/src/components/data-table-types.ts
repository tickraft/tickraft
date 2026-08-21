// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/** Column configuration for DataTable */
export interface DataTableColumn<T> {
  prop: string
  label: string
  width?: string | number
  minWidth?: string | number
  fixed?: 'left' | 'right' | boolean
  sortable?: boolean | 'custom'
  resizable?: boolean
  align?: 'left' | 'center' | 'right'
  showOverflowTooltip?: boolean
  formatter?: (row: T, column: unknown, cellValue: unknown) => string
  slot?: string
}

/** Props for DataTable component */
export interface DataTableProps<T> {
  data: Array<T>
  columns: Array<DataTableColumn<T>>
  loading?: boolean
  error?: string | null
  selectable?: boolean
  rowKey?: keyof T | ((row: T) => string | number)
  density?: 'compact' | 'default' | 'loose'
  fixedHeader?: boolean
  maxHeight?: string | number
  resizable?: boolean
  pagination?: boolean
  total?: number
  page?: number
  size?: number
  sizeOptions?: Array<number>
  defaultSort?: { prop: string; order: 'ascending' | 'descending' }
  /** Table persistence identifier; when non-empty, enables column width localStorage persistence */
  tableId?: string
  /** Controlled-mode column width map, takes priority over localStorage and columns.width */
  columnWidths?: Record<string, number>
  /** Row class name resolver, forwarded to el-table row-class-name */
  rowClassName?: (payload: { row: T; rowIndex: number }) => string
}
