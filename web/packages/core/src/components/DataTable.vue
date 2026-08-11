// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script lang="ts">
/**
 * DataTable type exports (re-exported from a standalone .ts file for cross-project type visibility)
 */
export type { DataTableColumn, DataTableProps } from './data-table-types'
</script>

<script setup lang="ts" generic="T extends object">
/**
 * DataTable - generic data table component.
 * Built on el-table, supports pagination/sorting/multi-select/skeleton/empty state/
 * error state/density toggle/fixed header/column width drag.
 * Aligned with the .tk-table visual spec.
 */
import { computed, nextTick, onMounted, ref } from 'vue'
import { CircleCloseFilled } from '@element-plus/icons-vue'
import type { TableInstance } from 'element-plus'
import { loadWidths, saveWidth } from '../composables/useColumnWidths'
import type { DataTableProps } from './data-table-types'

/**
 * Extended table column context from el-table's internal store.
 * Element Plus manages column width via `realWidth` (ref) and `width` (number).
 * During native drag, el-table sets `column.width = column.realWidth = columnWidth`
 * and calls `store.scheduleLayout()`, which reads `realWidth || width` to set
 * DOM colgroup widths.
 */
interface InternalColumnContext {
  property?: string
  prop?: string
  width: number | null
  realWidth: number | null
  minWidth?: number | string
  id: string
}

type SortOrder = 'ascending' | 'descending' | null

/** el-table column context (only declares fields needed by this component, avoids any) */
interface TableColumnContext {
  property?: string
  label?: string
  width?: number | string
  realWidth?: number
}

/** Double-click auto-width estimation constants */
const AUTO_WIDTH = {
  /** Estimated width of CJK/full-width characters (px, based on 14px font) */
  CJK_CHAR: 14,
  /** Estimated width of ASCII/half-width characters (px) */
  ASCII_CHAR: 7,
  /** Total left/right padding of a cell (px) */
  PADDING: 32,
  /** Column width lower bound (px) */
  MIN: 80,
  /** Column width upper bound (px) */
  MAX: 500,
  /** Double-click detection time window (ms) */
  DBLCLICK_THRESHOLD: 350,
  /** Header horizontal padding (td 24px + inner .cell 24px) */
  HEADER_PADDING: 48,
  /** Sort caret + spacing reserve (px) */
  SORT_ICON: 24,
  /** Minimum width for fixed-right (action) columns to prevent collapse (px) */
  FIXED_MIN: 120,
} as const

interface DataTableEmits<T> {
  (e: 'selection-change', payload: { keys: Array<T[keyof T]>; rows: Array<T> }): void
  (e: 'sort-change', payload: { prop: string; order: SortOrder }): void
  (e: 'page-change', payload: { current: number; pageSize: number }): void
  (e: 'retry'): void
  (e: 'row-click', row: T): void
  (e: 'column-width-change', payload: { prop: string; width: number }): void
}

const props = withDefaults(defineProps<DataTableProps<T>>(), {
  loading: false,
  error: null,
  selectable: false,
  density: 'default',
  fixedHeader: true,
  resizable: true,
  pagination: true,
  total: 0,
  current: 1,
  pageSize: 20,
  pageSizes: () => [10, 20, 50, 100],
  tableId: '',
  columnWidths: () => ({}),
  rowClassName: undefined,
})

const emit = defineEmits<DataTableEmits<T>>()

const tableRef = ref<TableInstance>()

const tableRowKey = computed<string | ((row: T) => string) | undefined>(() => {
  const key = props.rowKey
  if (typeof key === 'function') {
    return (row: T) => String(key(row))
  }
  return key !== undefined ? String(key) : undefined
})

const tableSize = computed<'large' | 'default' | 'small'>(() => {
  if (props.density === 'compact') return 'small'
  if (props.density === 'loose') return 'large'
  return 'default'
})

const wrapperClass = computed(() => [
  'tk-data-table',
  `tk-data-table--${props.density}`,
  {
    'tk-data-table--loading': props.loading,
    'tk-data-table--error': !!props.error,
  },
])

const tableMaxHeight = computed(() => (props.fixedHeader ? props.maxHeight : undefined))

/** Adapter: wraps rowClassName prop to match el-table's row-class-name signature */
const tableRowClassName = computed<((row: T, rowIndex: number) => string) | undefined>(() => {
  if (!props.rowClassName) return undefined
  return (row: T, rowIndex: number) => props.rowClassName!({ row, rowIndex })
})

// ── Column width persistence and auto-fit ──
/**
 * Authoritative column width state.
 *
 * Stores persisted column widths (from localStorage or user drag).
 * Used ONLY during Phase 1 (initialization) to seed el-table's internal state.
 * After initialization, dragged widths are managed directly in el-table's store.
 *
 * Two-phase width management (root cause fix for multi-column-jump bug):
 *
 * Phase 1 — Initial render (initialized = false):
 *   mergedColumns provides `:width` from columnWidthState (persisted) AND
 *   col.width (explicit) AND columnWidths (controlled). This seeds
 *   el-table-column's realWidth ref so columns render at correct widths.
 *
 * Phase 2 — Runtime (initialized = true):
 *   mergedColumns provides `:width` ONLY from col.width (explicit) AND
 *   columnWidths (controlled). Dragged widths are NOT provided via `:width`.
 *   Instead, handleHeaderDragEnd writes directly to el-table's internal store
 *   column (width + realWidth). This prevents realWidth snap-back because:
 *
 *   - el-table-column's realWidth ref is set ONCE from the initial `:width` prop
 *   - When setColumnWidth runs during re-registration, it uses realWidth.value
 *     (which holds the initial persisted width) — NOT the dragged width
 *   - BUT we also set column.realWidth in the store, so if realWidth.value
 *     is falsy, setColumnWidth falls back to column.realWidth → correct!
 *
 *   This eliminates the chaotic multi-column jump because:
 *   1. Dragged columns don't trigger :width prop changes → no re-registration
 *   2. Fixed columns (col.width or columnWidths) keep their width stable
 *   3. Only flex columns (minWidth only) redistribute proportionally
 */
const columnWidthState = ref<Record<string, number>>(
  props.tableId ? loadWidths(props.tableId) : {},
)

/** Whether initial seeding is complete; when true, mergedColumns stops using columnWidthState */
const initialized = ref(false)

/**
 * Merged column definitions.
 *
 * Width resolution priority:
 *   1. props.columnWidths[prop]   (controlled mode, always respected regardless of init)
 *   2. col.width                  (explicit definition width, always respected)
 *   3. columnWidthState[prop]     (persisted width, ONLY during Phase 1)
 *   4. undefined                  (el-table distributes via minWidth)
 *
 * Critical: after initialized = true, columnWidthState is NOT used.
 * Dragged widths are written directly to el-table's internal store column
 * (width + realWidth), so providing :width from columnWidthState would
 * cause realWidth snap-back during re-registration.
 */
const mergedColumns = computed(() => {
  return props.columns.map((col) => {
    let width: number | undefined

    if (props.columnWidths[col.prop] != null) {
      width = props.columnWidths[col.prop]
    } else if (col.width != null) {
      width = typeof col.width === 'number' ? col.width : parseInt(String(col.width), 10)
    } else if (!initialized.value && columnWidthState.value[col.prop] != null) {
      width = columnWidthState.value[col.prop]
    }

    const minWidth = col.minWidth
    const align = col.align ?? (col.slot ? 'center' : 'left')
    return { ...col, width, minWidth, align }
  })
})

/** Double-click detection: records the last click timestamp for each column */
const clickTimestamps = new Map<string, number>()

/**
 * Estimate text rendering width (based on 14px font).
 * CJK/full-width characters use 14px, ASCII/half-width characters use 7px.
 */
function estimateTextWidth(text: string): number {
  let width = 0
  for (const char of text) {
    // CJK unified ideographs and full-width symbols
    width += /[\u4e00-\u9fa5\uff00-\uffef]/.test(char) ? AUTO_WIDTH.CJK_CHAR : AUTO_WIDTH.ASCII_CHAR
  }
  return width
}

/** Normalize minWidth config to a numeric lower bound */
function normalizeMinWidth(minWidth: string | number | undefined): number {
  if (minWidth === undefined) return AUTO_WIDTH.MIN
  const numeric = typeof minWidth === 'number' ? minWidth : Number(minWidth)
  return Number.isFinite(numeric) && numeric > 0 ? numeric : AUTO_WIDTH.MIN
}

/**
 * Compute the minimum column width based on the header label text.
 * Ensures the header text never wraps or gets clipped when the user drags
 * the column narrower. Accounts for cell padding and optional sort icon.
 */
function calculateHeaderMinWidth(label: string, sortable?: boolean | 'custom'): number {
  const textWidth = estimateTextWidth(label)
  const extra = AUTO_WIDTH.HEADER_PADDING + (sortable ? AUTO_WIDTH.SORT_ICON : 0)
  return Math.max(textWidth + extra, AUTO_WIDTH.MIN)
}

/**
 * Compute column auto-fit width:
 * Iterates all cells of the column for the max text width, compares with the header
 * label width and takes the larger, adds padding, then clamps to min-width lower bound
 * and 500px upper bound.
 */
function calculateAutoWidth(prop: string): number {
  const col = props.columns.find((c) => c.prop === prop)
  if (!col) return AUTO_WIDTH.MIN
  const headerMin = calculateHeaderMinWidth(col.label, col.sortable)
  const configMin = normalizeMinWidth(col.minWidth)
  const minWidth = Math.max(headerMin, configMin)
  const labelWidth = estimateTextWidth(col.label)
  let maxWidth = labelWidth
  for (const row of props.data) {
    const raw = (row as Record<string, unknown>)[prop]
    const value = raw === null || raw === undefined ? '' : String(raw)
    const w = estimateTextWidth(value)
    if (w > maxWidth) maxWidth = w
  }
  return Math.min(Math.max(maxWidth + AUTO_WIDTH.PADDING, minWidth), AUTO_WIDTH.MAX)
}

/**
 * Find an internal store column by its property name.
 * el-table's store.states.columns.value holds the authoritative column list
 * with width/realWidth properties that scheduleLayout uses to size DOM cols.
 */
function findInternalColumn(prop: string): InternalColumnContext | undefined {
  const cols = tableRef.value?.store?.states?.columns?.value
  if (!Array.isArray(cols)) return undefined
  return cols.find(
    (c) => (c.property || c.prop) === prop,
  ) as InternalColumnContext | undefined
}

/**
 * Set width directly on el-table's internal store column and refresh layout.
 *
 * Mirrors what el-table's native drag handler does (see table-header
 * handleMouseUp): sets `column.width = column.realWidth = width` then
 * calls `store.scheduleLayout()`, which applies widths to DOM colgroup
 * elements via `onColumnsChange`.
 *
 * Writing BOTH width and realWidth is critical:
 * - realWidth is used by setColumnWidth during column re-registration
 *   when el-table-column's realWidth ref is falsy (no :width prop)
 * - width is used by scheduleLayout for DOM colgroup sizing
 * - scheduleLayout() does NOT trigger addColumn / setColumnWidth, so it's safe
 */
function setInternalColumnWidth(prop: string, width: number): void {
  const col = findInternalColumn(prop)
  if (!col) return
  col.width = width
  col.realWidth = width
  tableRef.value?.store?.scheduleLayout()
}

/**
 * Column width drag end handler (el-table native resize).
 *
 * Flow after el-table's native drag (mousedown → mousemove → mouseup → header-dragend):
 *   1. El-table already set column.width/realWidth in its store during drag
 *      and updated DOM via onColumnResize
 *   2. We clamp the final width to minWidth (el-table doesn't enforce this)
 *   3. We write the clamped width to el-table's internal store (width + realWidth)
 *      and call scheduleLayout() to update DOM — this prevents snap-back
 *      because both the DOM and internal store agree
 *   4. We persist to localStorage (for next session restoration)
 *   5. We emit column-width-change for parent consumption
 *
 * We do NOT update columnWidthState (mergedColumns won't use it after initialized)
 * to avoid triggering :width prop change that would cause re-registration.
 */
function handleHeaderDragEnd(
  newWidth: number,
  _oldWidth: number,
  column: TableColumnContext,
): void {
  const prop = column.property
  if (!prop) return

  const col = props.columns.find((c) => c.prop === prop)
  const minWidth = col ? normalizeMinWidth(col.minWidth) : AUTO_WIDTH.MIN
  const clampedWidth = Math.max(Math.round(newWidth), minWidth)

  // Write clamped width to el-table's internal store and refresh DOM
  setInternalColumnWidth(prop, clampedWidth)

  // Persist for next session
  if (props.tableId) {
    saveWidth(props.tableId, prop, clampedWidth)
  }
  emit('column-width-change', { prop, width: clampedWidth })
}

/**
 * Header single click: detect two consecutive clicks on the same column (within 350ms)
 * to trigger auto-fit. Does not block sort-change; sorting is handled independently by el-table.
 */
function handleHeaderClick(column: TableColumnContext): void {
  const prop = column.property
  if (!prop) return
  const now = Date.now()
  const last = clickTimestamps.get(prop) || 0
  if (now - last < AUTO_WIDTH.DBLCLICK_THRESHOLD) {
    clickTimestamps.delete(prop)
    handleDoubleClickColumn(prop)
  } else {
    clickTimestamps.set(prop, now)
  }
}

/**
 * Double-click column triggers auto-fit width and persists.
 * Uses the same internal-store write pattern as handleHeaderDragEnd.
 */
function handleDoubleClickColumn(prop: string): void {
  const col = props.columns.find((c) => c.prop === prop)
  if (!col) return
  const autoWidth = calculateAutoWidth(prop)

  setInternalColumnWidth(prop, autoWidth)

  if (props.tableId) {
    saveWidth(props.tableId, prop, autoWidth)
  }
  emit('column-width-change', { prop, width: autoWidth })
}

const skeletonRows = computed(() => {
  const count = Math.min(props.pageSize, 10)
  return Array.from({ length: count }, (_, i) => i)
})

const skeletonBlockCount = computed(() => {
  const colCount = props.columns.length + (props.selectable ? 1 : 0)
  return Math.min(Math.max(colCount, 5), 8)
})

function getRowKeyValue(row: T): unknown {
  const key = props.rowKey
  if (key === undefined) return undefined
  if (typeof key === 'function') return key(row)
  return row[key]
}

function handleSelectionChange(rows: readonly T[]): void {
  const keys = rows.map(getRowKeyValue) as Array<T[keyof T]>
  emit('selection-change', { keys, rows: [...rows] })
}

function handleSortChange({ prop, order }: { prop: string; order: SortOrder }): void {
  emit('sort-change', { prop, order })
}

function handleCurrentChange(page: number): void {
  emit('page-change', { current: page, pageSize: props.pageSize })
}

function handleSizeChange(size: number): void {
  emit('page-change', { current: 1, pageSize: size })
}

function handleRowClick(row: T): void {
  emit('row-click', row)
}

function handleRetry(): void {
  emit('retry')
}

function clearSelection(): void {
  tableRef.value?.clearSelection()
}

function doLayout(): void {
  tableRef.value?.doLayout()
}

function clearSort(): void {
  tableRef.value?.clearSort()
}

/**
 * On mount: seed el-table's internal store with persisted widths from
 * localStorage and mark initialization complete.
 *
 * Flow:
 *   1. First render uses :width props (from mergedColumns, Phase 1) to seed
 *      el-table-column's realWidth refs — columns render at persisted widths.
 *   2. After mount + nextTick, we write persisted widths DIRECTLY to el-table's
 *      internal store (width + realWidth) and call scheduleLayout(). This
 *      ensures el-table's internal state (store columns) matches the rendered
 *      widths (DOM).
 *   3. Set initialized = true → mergedColumns stops providing columnWidthState
 *      widths as :width props (Phase 2). Subsequent drags won't trigger
 *      re-registration because :width won't change.
 */
onMounted(() => {
  const persisted = columnWidthState.value
  const hasPersisted = Object.keys(persisted).length > 0

  nextTick(() => {
    if (hasPersisted && tableRef.value?.store) {
      for (const [prop, width] of Object.entries(persisted)) {
        const col = findInternalColumn(prop)
        if (col) {
          col.width = width
          col.realWidth = width
        }
      }
      tableRef.value.store.scheduleLayout()
    }

    initialized.value = true
  })
})

defineExpose({ clearSelection, doLayout, clearSort })
</script>

<template>
  <div :class="wrapperClass">
    <!-- Error state -->
    <div
      v-if="error"
      class="tk-data-table__error"
    >
      <el-icon class="tk-data-table__error-icon">
        <CircleCloseFilled />
      </el-icon>
      <p class="tk-data-table__error-text">
        {{ error }}
      </p>
      <el-button
        type="primary"
        @click="handleRetry"
      >
        {{ $t('common.app.retry') }}
      </el-button>
    </div>

    <!-- Table area -->
    <template v-else>
      <div class="tk-data-table__body">
        <el-table
          ref="tableRef"
          :data="data"
          :row-key="tableRowKey"
          :row-class-name="tableRowClassName"
          :size="tableSize"
          :max-height="tableMaxHeight"
          :border="true"
          :default-sort="defaultSort"
          stripe
          style="width: 100%"
          @sort-change="handleSortChange"
          @selection-change="handleSelectionChange"
          @row-click="handleRowClick"
          @header-click="handleHeaderClick"
          @header-dragend="handleHeaderDragEnd"
        >
          <el-table-column
            v-if="selectable"
            type="selection"
            width="55"
            :reserve-selection="true"
          />
          <!-- @vue-ignore — vue-tsc cannot infer el-table-column scope type in v-for -->
          <el-table-column
            v-for="col in mergedColumns"
            :key="col.prop"
            :prop="col.prop"
            :label="col.label"
            :width="col.width"
            :min-width="col.minWidth"
            :fixed="col.fixed"
            :sortable="col.sortable"
            :resizable="resizable && col.resizable !== false"
            :align="col.align"
            :show-overflow-tooltip="col.showOverflowTooltip"
            :formatter="col.formatter"
            :class-name="col.slot ? 'tk-table-col--slot' : ''"
          >
            <template
              v-if="col.slot"
              #default="scope"
            >
              <slot
                :name="col.slot"
                :row="scope.row"
                :column="scope.column"
                :index="scope.$index"
              />
            </template>
          </el-table-column>
          <slot name="action-column" />

          <!-- Empty state -->
          <template #empty>
            <slot name="empty">
              <div class="tk-data-table__empty">
                <el-empty :description="$t('common.app.noData')" />
              </div>
            </slot>
          </template>
        </el-table>

        <!-- Skeleton overlay (replaces spinner) -->
        <div
          v-if="loading"
          class="tk-data-table__loading"
          aria-busy="true"
          aria-live="polite"
        >
          <div
            v-for="row in skeletonRows"
            :key="row"
            class="tk-data-table__skeleton-row"
          >
            <div
              v-for="n in skeletonBlockCount"
              :key="n"
              class="tk-data-table__skeleton-block"
            />
          </div>
        </div>
      </div>

      <!-- Pagination -->
      <div
        v-if="pagination && total > 0"
        class="tk-data-table__pagination"
      >
        <el-pagination
          :current-page="current"
          :page-size="pageSize"
          :total="total"
          :page-sizes="pageSizes"
          background
          layout="total, sizes, prev, pager, next, jumper"
          @current-change="handleCurrentChange"
          @size-change="handleSizeChange"
        />
      </div>
    </template>
  </div>
</template>

<style scoped lang="scss">
.tk-data-table {
  position: relative;
  width: 100%;

  // Clip the table corners (rounded border-radius). This must be on the
  // OUTER wrapper, NOT on .el-table itself — putting overflow:hidden on
  // .el-table interferes with its internal scroll-container hierarchy and
  // breaks header/body scroll synchronization.
  overflow: hidden;
  background: var(--tk-bg-surface);
  border-radius: var(--tk-border-radius-base);

  --tk-data-table-row-height: 48px;

  &--compact {
    --tk-data-table-row-height: 32px;
  }

  &--default {
    --tk-data-table-row-height: 48px;
  }

  &--loose {
    --tk-data-table-row-height: 64px;
  }

  &__body {
    position: relative;
  }

  &--compact {
    :deep(.el-table .el-table__cell) {
      padding: 4px 8px;
    }

    :deep(.el-table__header-wrapper th.el-table__cell) {
      height: 32px;
      padding: 4px 8px;
    }
  }

  &--default {
    :deep(.el-table .el-table__cell) {
      padding: 8px 12px;
    }

    :deep(.el-table__header-wrapper th.el-table__cell) {
      height: 48px;
    }
  }

  &--loose {
    :deep(.el-table .el-table__cell) {
      padding: 12px 16px;
    }

    :deep(.el-table__header-wrapper th.el-table__cell) {
      height: 64px;
    }
  }

  :deep(.el-table__header-wrapper th.el-table__cell) {
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    white-space: nowrap;
    background-color: var(--tk-gray-2);

    // All headers center-aligned for visual consistency (common pattern
    // in Ant Design, Element Plus admin panels). Body cells follow their
    // own :align prop for content alignment.
    .cell {
      overflow: hidden;
      text-overflow: ellipsis;
      text-align: center;
      white-space: nowrap;
    }
  }

  // Header alignment follows el-table-column's :align prop (no global override).

  :deep(.el-table__body-wrapper td.el-table__cell) {
    line-height: var(--tk-text-sm--line-height);
    vertical-align: middle;
    color: var(--tk-text-secondary);
    border-bottom: 1px solid var(--tk-border-subtle);
  }

  // Slot columns (tags, switches, buttons): use flexbox for proper vertical
  // centering and tag layout. Suppress ellipsis (text-overflow: clip) so tags
  // and badges are never truncated with "…"; when the column is too narrow the
  // table's horizontal scrollbar handles overflow instead.
  // flex-wrap: nowrap ensures action buttons never wrap to a second line.
  :deep(.el-table__body-wrapper td.tk-table-col--slot .cell) {
    display: flex;
    flex-wrap: nowrap;
    gap: var(--tk-spacing-2);
    align-items: center;
    overflow: hidden;
    text-overflow: clip;
    white-space: nowrap;
  }

  // Map Element Plus align classes to flexbox justify-content for slot columns
  :deep(.el-table__body-wrapper td.tk-table-col--slot.is-center .cell) {
    justify-content: center;
  }

  :deep(.el-table__body-wrapper td.tk-table-col--slot.is-right .cell) {
    justify-content: flex-end;
  }

  :deep(.el-table__body tr:hover > td.el-table__cell) {
    background-color: var(--tk-bg-hover) !important;
    transition: background-color 0.2s ease;
  }

  :deep(.el-table__body tr.el-table__row--striped > td.el-table__cell) {
    background-color: var(--tk-bg-stripe);
  }

  :deep(.el-table) {
    --el-table-border-color: var(--tk-border-color-base);
    --el-table-header-bg-color: var(--tk-gray-2);
    --el-table-row-hover-bg-color: var(--tk-bg-hover);
    --el-table-text-color: var(--tk-text-secondary);
    --el-table-header-text-color: var(--tk-text-tertiary);

    // Element Plus fixed columns inherit --el-bg-color for their background;
    // expose the surface token so the fixed action column and its bottom
    // patch area stay fully opaque (no see-through over body rows).
    --el-bg-color: var(--tk-bg-surface);

    font-size: var(--tk-font-size-sm);
    background-color: var(--tk-bg-surface);

    // Do NOT set overflow:hidden here — it breaks el-table's internal
    // scroll-container hierarchy and causes header/body scroll desync.
    // Rounded corners are clipped by the outer .tk-data-table wrapper.
  }

  // Fixed columns in Element Plus 2.x use position: sticky on td/th elements
  // (class .el-table-fixed-column--right / --left), not the old cloned-container
  // approach (.el-table__fixed-right). The sticky cells inherit an opaque
  // background from <tr> via `background: inherit`, but the stripe/hover rules
  // above override ALL body cells — including sticky ones — with semi-transparent
  // colors (rgb 2-5% alpha). When the table body scrolls horizontally, non-fixed
  // content bleeds through the translucent fixed column. Restore opacity by
  // giving fixed cells an opaque surface base and compositing the stripe/hover
  // tint via background-image (which layers on top of background-color without
  // replacing it), so the visual matches non-fixed cells exactly while staying
  // fully opaque to scrolled content underneath.
  :deep(td.el-table__cell.el-table-fixed-column--right),
  :deep(td.el-table__cell.el-table-fixed-column--left),
  :deep(.el-table__fixed-right-patch) {
    background-color: var(--tk-bg-surface) !important;
  }

  // Header fixed cells: keep the header background opaque
  :deep(th.el-table__cell.el-table-fixed-column--right),
  :deep(th.el-table__cell.el-table-fixed-column--left) {
    background-color: var(--tk-gray-2) !important;
  }

  // Striped fixed cells: opaque base + semi-transparent stripe overlay
  :deep(.el-table__body tr.el-table__row--striped > td.el-table__cell.el-table-fixed-column--right),
  :deep(.el-table__body tr.el-table__row--striped > td.el-table__cell.el-table-fixed-column--left) {
    background-color: var(--tk-bg-surface) !important;
    background-image: linear-gradient(var(--tk-bg-stripe), var(--tk-bg-stripe)) !important;
  }

  // Hovered fixed cells: opaque base + semi-transparent hover overlay.
  // Element Plus applies hover via both .hover-row class and tr:hover.
  :deep(.el-table__body tr:hover > td.el-table__cell.el-table-fixed-column--right),
  :deep(.el-table__body tr:hover > td.el-table__cell.el-table-fixed-column--left),
  :deep(.el-table__body tr.hover-row > td.el-table__cell.el-table-fixed-column--right),
  :deep(.el-table__body tr.hover-row > td.el-table__cell.el-table-fixed-column--left) {
    background-color: var(--tk-bg-surface) !important;
    background-image: linear-gradient(var(--tk-bg-hover), var(--tk-bg-hover)) !important;
  }

  // Fixed-column floating shadow: Element Plus manages this natively via
  // is-scrolling-* classes on .el-table. When the table has horizontal
  // overflow, is-scrolling-left/right/middle applies a box-shadow to the
  // fixed column's ::before pseudo-element; when there is no overflow,
  // is-scrolling-none sets box-shadow: none. No custom detection needed.

  // ── Column resize ──
  // el-table's native resize is used with live-resize enhancement.
  // Border is always enabled (:border="true") for consistent grid lines.
  // The `resizable` prop controls per-column resizability via el-table-column
  // :resizable and the live-resize mousedown handler.
  // el-table provides: col-resize cursor, drag proxy line, scroll sync.

  &__loading {
    position: absolute;
    inset: 0;
    z-index: 10;
    padding: var(--tk-spacing-md);
    overflow: hidden;
    background: var(--tk-glass-bg);
    border-radius: var(--tk-border-radius-base);
    backdrop-filter: var(--tk-glass-blur);
  }

  &__skeleton-row {
    display: flex;
    gap: var(--tk-spacing-md);
    align-items: center;
    height: var(--tk-data-table-row-height);
    padding: 0 var(--tk-spacing-sm);
  }

  &__skeleton-block {
    flex: 1;
    height: 14px;
    background: linear-gradient(
      90deg,
      var(--tk-bg-hover) 25%,
      var(--tk-bg-active) 50%,
      var(--tk-bg-hover) 75%
    );
    background-size: 200% 100%;
    border-radius: var(--tk-border-radius-sm);
    animation: tk-data-table-skeleton 1.4s ease infinite;
  }

  &__empty {
    padding: var(--tk-spacing-xl) var(--tk-spacing-md);
  }

  &__error {
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-md);
    align-items: center;
    justify-content: center;
    padding: var(--tk-spacing-2xl) var(--tk-spacing-md);
    text-align: center;
  }

  &__error-icon {
    font-size: 56px;
    color: var(--tk-danger-color-text);
  }

  &__error-text {
    max-width: 360px;
    margin: 0;
    font-size: var(--tk-font-size-base);
    line-height: var(--tk-line-height-relaxed);
    color: var(--tk-text-secondary);
    word-break: break-all;
  }

  &__pagination {
    display: flex;
    justify-content: flex-end;
    padding-bottom: var(--tk-spacing-md);
    margin-top: var(--tk-spacing-xl);
  }
}

@keyframes tk-data-table-skeleton {
  0% {
    background-position: 100% 50%;
  }

  100% {
    background-position: 0 50%;
  }
}

/* Accessibility: respect reduced-motion preference */
@media (prefers-reduced-motion: reduce) {
  .tk-data-table__skeleton-block {
    animation: none;
  }
}
</style>
