// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * Naming convention utility for transparent snake_case ↔ camelCase transformation.
 *
 * Wraps the `humps` library (stable, MIT-licensed) for key conversion and adds
 * protection for non-serializable types (FormData, File, Blob, Date, ArrayBuffer).
 *
 * - **Response data** is automatically converted from snake_case → camelCase.
 * - **Request data** is automatically converted from camelCase → snake_case.
 */

import humps from 'humps'

/** Check if a value should not be recursively converted. */
function isRawValue(value: unknown): boolean {
  return (
    value instanceof FormData ||
    value instanceof File ||
    value instanceof Blob ||
    value instanceof Date ||
    value instanceof ArrayBuffer
  )
}

/** Deeply convert all object keys from snake_case to camelCase. */
export function camelizeKeys<T>(data: unknown): T {
  if (data === null || data === undefined) return data as T
  if (typeof data !== 'object') return data as T
  if (isRawValue(data)) return data as T
  return humps.camelizeKeys(data) as T
}

/** Deeply convert all object keys from camelCase to snake_case. */
export function snakeizeKeys<T>(data: unknown): T {
  if (data === null || data === undefined) return data as T
  if (typeof data !== 'object') return data as T
  if (isRawValue(data)) return data as T
  return humps.decamelizeKeys(data) as T
}
