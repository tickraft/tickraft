// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * Column width persistence composable.
 *
 * Provides column width persistence for DataTable:
 * - localStorage as primary storage, key is `tk-table-widths`
 * - value shape: `{ [tableId]: { [prop]: width } }`
 * - Privacy mode fallback: when localStorage is unavailable, automatically degrades to an in-memory Map
 *
 * Naming alignment: the local storage key carries the `tk-` prefix, complying with
 */

const STORAGE_KEY = 'tk-table-widths'

/** In-memory fallback store (used when localStorage is unavailable or writes fail) */
const memoryStore = new Map<string, Record<string, number>>()

/** Whether the composable has degraded to in-memory mode */
let useMemory = false

// Detect localStorage availability at startup; on failure, permanently degrade to in-memory mode
try {
  const testKey = '__tk_test__'
  localStorage.setItem(testKey, testKey)
  localStorage.removeItem(testKey)
} catch {
  useMemory = true
}

/** Full data shape of the column width store */
type AllWidths = Record<string, Record<string, number>>

/**
 * Load column widths for all tables.
 * Any error silently degrades to an empty object so business logic is not interrupted.
 */
function loadAll(): AllWidths {
  if (useMemory) {
    return Object.fromEntries(memoryStore.entries())
  }
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw ? (JSON.parse(raw) as AllWidths) : {}
  } catch {
    return {}
  }
}

/**
 * Persist column widths for all tables.
 * Write failures silently degrade so the table continues to work.
 */
function saveAll(all: AllWidths): void {
  if (useMemory) {
    for (const [k, v] of Object.entries(all)) {
      memoryStore.set(k, v)
    }
    return
  }
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(all))
  } catch {
    // Silent degradation: storage is full or disabled; no longer throws
  }
}

/**
 * Load the column width map for a given table.
 *
 * @param tableId - table persistence identifier
 * @returns column width map `{ [prop]: width }`; empty object when no record exists
 */
export function loadWidths(tableId: string): Record<string, number> {
  if (!tableId) return {}
  return loadAll()[tableId] || {}
}

/**
 * Batch-save the column width map for a given table.
 *
 * @param tableId - table persistence identifier
 * @param widths - column width map `{ [prop]: width }`
 */
export function saveWidths(tableId: string, widths: Record<string, number>): void {
  if (!tableId) return
  const all = loadAll()
  all[tableId] = { ...widths }
  saveAll(all)
}

/**
 * Save the width of a single column.
 *
 * @param tableId - table persistence identifier
 * @param prop - column field name
 * @param width - column width (px)
 */
export function saveWidth(tableId: string, prop: string, width: number): void {
  if (!tableId || !prop) return
  const all = loadAll()
  if (!all[tableId]) {
    all[tableId] = {}
  }
  all[tableId][prop] = width
  saveAll(all)
}

/**
 * Clear all column width records for a given table.
 *
 * @param tableId - table persistence identifier
 */
export function clearWidths(tableId: string): void {
  if (!tableId) return
  const all = loadAll()
  delete all[tableId]
  saveAll(all)
}
