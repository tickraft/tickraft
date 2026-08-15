// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useTable } from './useTable'
import type { PageParams } from '../types/api'

/**
 * Pagination contract tests for useTable: the fetchFn receives
 * { page, size, sort_by, sort_order, ...filters }, and page resets to 1
 * on search / page-size changes.
 */

interface Row {
  id: number
}

describe('useTable pagination contract', () => {
  let fetches: Array<PageParams & Record<string, unknown>>

  beforeEach(() => {
    fetches = []
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  function makeTable(defaultPageSize = 20) {
    return useTable<Row>({
      defaultPageSize,
      fetchFn: async (params) => {
        fetches.push(params)
        return { items: [{ id: 1 }], total: 42 }
      },
    })
  }

  it('initial load passes page=1 and the default page size', () => {
    const table = makeTable(15)
    table.immediateSearch()
    expect(fetches.at(-1)?.page).toBe(1)
    expect(fetches.at(-1)?.size).toBe(15)
    expect(table.page.value).toBe(1)
    expect(table.pageSize.value).toBe(15)
  })

  it('changePage updates the page passed to fetchFn', () => {
    const table = makeTable()
    table.changePage(3)
    expect(fetches.at(-1)?.page).toBe(3)
    expect(table.page.value).toBe(3)
  })

  it('changePageSize resets the page to 1', () => {
    const table = makeTable()
    table.changePage(4)
    table.changePageSize(50)
    expect(fetches.at(-1)?.page).toBe(1)
    expect(fetches.at(-1)?.size).toBe(50)
  })

  it('immediateSearch merges filters and resets the page', () => {
    const table = makeTable()
    table.changePage(5)
    table.immediateSearch({ status: 'failed', keyword: 'web' })
    expect(fetches.at(-1)?.page).toBe(1)
    expect(fetches.at(-1)?.status).toBe('failed')
    expect(fetches.at(-1)?.keyword).toBe('web')
  })

  it('resetSearch clears filters and sorting', () => {
    const table = makeTable()
    table.immediateSearch({ severity: 'critical' })
    table.sortChange('created_at', 'desc')
    table.resetSearch()
    const last = fetches.at(-1)
    expect(last?.severity).toBeUndefined()
    expect(last?.sort_by).toBe('')
    expect(last?.sort_order).toBe('asc')
    expect(last?.page).toBe(1)
  })

  it('search debounces and replaces previous filters', () => {
    vi.useFakeTimers()
    const table = makeTable()
    table.immediateSearch({ severity: 'warning' })
    table.search({ severity: 'critical' })
    // Not fired until the debounce elapses.
    expect(fetches.at(-1)?.severity).toBe('warning')
    vi.advanceTimersByTime(300)
    expect(fetches.at(-1)?.severity).toBe('critical')
  })

  it('captures fetch errors in the error ref without throwing', async () => {
    const table = useTable<Row>({
      fetchFn: async () => {
        throw new Error('backend down')
      },
    })
    table.immediateSearch()
    await Promise.resolve()
    await Promise.resolve()
    expect(table.error.value).toBeInstanceOf(Error)
    expect(table.error.value?.message).toBe('backend down')
    expect(table.loading.value).toBe(false)
  })
})
