// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { ref, reactive, computed, onBeforeUnmount } from 'vue'
import type { Ref, ComputedRef } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { PageParams } from '../types/api'

/** Sort order */
type SortOrder = 'asc' | 'desc'

/** Table row data constraint (relaxed to object so interface types do not need an explicit index signature) */
type TableRow = Record<string, unknown>

/** Set of query parameter keys managed by the table in the URL */
const TABLE_QUERY_KEYS = ['page', 'size', 'sort_by', 'sort_order', 'keyword'] as const

/** Default search debounce delay (ms) */
const DEFAULT_SEARCH_DEBOUNCE = 300

/** useTable options */
export interface UseTableOptions<T extends object> {
  /** Data fetch function */
  fetchFn: (
    params: PageParams & Record<string, unknown>,
  ) => Promise<{ items: T[]; total: number }>
  /** Default page size */
  defaultPageSize?: number
  /** Row unique key field name; defaults to 'id' */
  rowKey?: string
  /** Whether to sync pagination/search/sort state to the URL query (restorable on page refresh); defaults to false */
  syncUrl?: boolean
  /** Keyword search default debounce delay (ms); defaults to 300 */
  searchDebounceDelay?: number
}

/** Pagination state */
interface PaginationState {
  page: number
  size: number
}

/** useTable return type */
export interface UseTableReturn<T extends object> {
  // ── Canonical fields ──
  /** Current page data */
  data: Ref<T[]>
  /** Loading state */
  loading: Ref<boolean>
  /** Loading error */
  error: Ref<Error | null>
  /** Total record count */
  total: Ref<number>
  /** Current page number */
  page: ComputedRef<number>
  /** Page size */
  pageSize: ComputedRef<number>
  /** Debounced search (default 300ms) */
  search: (params: Record<string, unknown>) => void
  /** Immediate search (no debounce) */
  immediateSearch: (params?: Record<string, unknown>) => void
  /** Reset search conditions */
  resetSearch: () => void
  /** Cross-page selected row objects */
  selectedRows: ComputedRef<T[]>
  /** Toggle the selection state of a row (supports explicit select/deselect) */
  toggleRowSelection: (row: T, selected?: boolean) => void
  /** Clear all selections (cross-page) */
  clearSelection: () => void
  /** Get the selected row objects */
  getSelectedRows: () => T[]
  /** Refresh the current page */
  refresh: () => void
  /** Page number change */
  changePage: (page: number) => void
  /** Page size change */
  changePageSize: (size: number) => void
  /** Sort change */
  sortChange: (sortBy: string, sortOrder?: SortOrder) => void
  /** URL sync parameters object */
  urlParams: ComputedRef<Record<string, unknown>>
}

/**
 * Table data management composable.
 *
 * Provides pagination, search (with debounce), sort, cross-page selection
 * preservation and URL state sync.
 * @param options - options
 * @returns table state and operation methods
 */
export function useTable<T extends object>(
  options: UseTableOptions<T>,
): UseTableReturn<T> {
  const {
    fetchFn,
    defaultPageSize = 20,
    rowKey = 'id',
    syncUrl = false,
    searchDebounceDelay = DEFAULT_SEARCH_DEBOUNCE,
  } = options

  const data: Ref<T[]> = ref([])
  const loading = ref(false)
  const error: Ref<Error | null> = ref(null)
  const total = ref(0)

  const pagination = reactive<PaginationState>({
    page: 1,
    size: defaultPageSize,
  })

  const searchParams = reactive<Record<string, unknown>>({})
  const sortBy = ref('')
  const sortOrder = ref<SortOrder>('asc')

  /** Cross-page selected row map; key is the stringified row key, value is the row object */
  const selectedRowMap = new Map<string, T>()

  const route = useRoute()
  const router = useRouter()

  /** Debounce timer */
  let searchTimer: ReturnType<typeof setTimeout> | null = null

  /** Current page number (derived from pagination) */
  const page: ComputedRef<number> = computed(() => pagination.page)
  /** Page size (derived from pagination) */
  const pageSize: ComputedRef<number> = computed(() => pagination.size)

  /** Selected row objects */
  const selectedRows: ComputedRef<T[]> = computed(() => Array.from(selectedRowMap.values()))

  /** URL sync parameters object */
  const urlParams: ComputedRef<Record<string, unknown>> = computed(() => {
    const params: Record<string, unknown> = {
      page: pagination.page,
      size: pagination.size,
      sort_by: sortBy.value,
      sort_order: sortOrder.value,
    }
    if (searchParams.keyword !== undefined && searchParams.keyword !== '') {
      params.keyword = searchParams.keyword
    }
    return params
  })

  /**
   * Read the stringified value of a row's unique key.
   * @param row - row data
   * @returns row key string
   */
  function getRowKey(row: T): string {
    // Dynamic key access requires a cast to an index signature type (only converted locally here for dynamic access)
    const value = (row as TableRow)[rowKey]
    return value === undefined || value === null ? '' : String(value)
  }

  /**
   * Read initial state from the URL query (called once when syncUrl is enabled)
   */
  function readUrlState(): void {
    const query = route.query
    if (typeof query.page === 'string') {
      const pageValue = Number(query.page)
      if (!Number.isNaN(pageValue) && pageValue > 0) pagination.page = pageValue
    }
    if (typeof query.size === 'string') {
      const size = Number(query.size)
      if (!Number.isNaN(size) && size > 0) pagination.size = size
    }
    if (typeof query.sort_by === 'string') {
      sortBy.value = query.sort_by
    }
    if (typeof query.sort_order === 'string') {
      sortOrder.value = query.sort_order === 'desc' ? 'desc' : 'asc'
    }
    if (typeof query.keyword === 'string') {
      searchParams.keyword = query.keyword
    }
  }

  /**
   * Write pagination/search/sort state into the URL query (preserving non-table-managed parameters)
   */
  function writeUrlState(): void {
    if (!syncUrl) return
    const nextQuery: Record<string, string> = {}
    Object.entries(route.query).forEach(([key, value]) => {
      if (!TABLE_QUERY_KEYS.includes(key as (typeof TABLE_QUERY_KEYS)[number])) {
        if (typeof value === 'string') nextQuery[key] = value
      }
    })
    nextQuery.page = String(pagination.page)
    nextQuery.size = String(pagination.size)
    nextQuery.sort_by = sortBy.value
    nextQuery.sort_order = sortOrder.value
    if (searchParams.keyword) {
      nextQuery.keyword = String(searchParams.keyword)
    }
    void router.replace({ query: nextQuery }).catch(() => undefined)
  }

  /**
   * Clear the debounce timer
   */
  function clearSearchTimer(): void {
    if (searchTimer) {
      clearTimeout(searchTimer)
      searchTimer = null
    }
  }

  /**
   * Load data
   */
  async function fetchData(): Promise<void> {
    writeUrlState()
    loading.value = true
    error.value = null
    try {
      const result = await fetchFn({
        page: pagination.page,
        size: pagination.size,
        sort_by: sortBy.value,
        sort_order: sortOrder.value,
        ...searchParams,
      })
      data.value = result.items
      total.value = result.total
    } catch (err) {
      error.value = err instanceof Error ? err : new Error('Failed to fetch table data')
    } finally {
      loading.value = false
    }
  }

  /**
   * Page number change
   * @param newPage - new page number
   */
  function changePage(newPage: number): void {
    pagination.page = newPage
    void fetchData()
  }

  /**
   * Page size change
   * @param size - new page size
   */
  function changePageSize(size: number): void {
    pagination.size = size
    pagination.page = 1
    void fetchData()
  }

  /**
   * Debounced search (default 300ms)
   * @param params - search parameters
   */
  function search(params: Record<string, unknown>): void {
    clearSearchTimer()
    searchTimer = setTimeout(() => {
      Object.keys(searchParams).forEach((key) => delete searchParams[key])
      Object.assign(searchParams, params)
      pagination.page = 1
      void fetchData()
      searchTimer = null
    }, searchDebounceDelay)
  }

  /**
   * Immediate search (no debounce)
   * @param params - search parameters; when omitted, the current searchParams are reused
   */
  function immediateSearch(params?: Record<string, unknown>): void {
    clearSearchTimer()
    if (params) {
      Object.keys(searchParams).forEach((key) => delete searchParams[key])
      Object.assign(searchParams, params)
    }
    pagination.page = 1
    void fetchData()
  }

  /**
   * Reset search conditions
   */
  function resetSearch(): void {
    clearSearchTimer()
    Object.keys(searchParams).forEach((key) => delete searchParams[key])
    sortBy.value = ''
    sortOrder.value = 'asc'
    pagination.page = 1
    void fetchData()
  }

  /**
   * Refresh the current page
   */
  function refresh(): void {
    void fetchData()
  }

  /**
   * Sort change
   * @param field - sort field
   * @param order - sort order; when omitted, toggles
   */
  function sortChange(field: string, order?: SortOrder): void {
    sortBy.value = field
    sortOrder.value = order ?? (sortOrder.value === 'asc' ? 'desc' : 'asc')
    pagination.page = 1
    void fetchData()
  }

  /**
   * Toggle the selection state of a row
   * @param row - row data
   * @param selected - explicitly select/deselect; when omitted, toggles
   */
  function toggleRowSelection(row: T, selected?: boolean): void {
    const key = getRowKey(row)
    if (!key) return
    const shouldSelect = selected ?? !selectedRowMap.has(key)
    if (shouldSelect) {
      selectedRowMap.set(key, row)
    } else {
      selectedRowMap.delete(key)
    }
  }

  /**
   * Clear all selections (cross-page)
   */
  function clearSelection(): void {
    selectedRowMap.clear()
  }

  /**
   * Get the selected row objects
   * @returns selected row array
   */
  function getSelectedRows(): T[] {
    return Array.from(selectedRowMap.values())
  }

  // When URL sync is enabled, restore initial state from the query
  if (syncUrl) {
    readUrlState()
  }

  // Clean up the debounce timer on unmount to avoid memory leaks and stale callbacks
  onBeforeUnmount(() => {
    clearSearchTimer()
  })

  return {
    data,
    loading,
    error,
    total,
    page,
    pageSize,
    search,
    immediateSearch,
    resetSearch,
    selectedRows,
    toggleRowSelection,
    clearSelection,
    getSelectedRows,
    refresh,
    changePage,
    changePageSize,
    sortChange,
    urlParams,
  }
}
