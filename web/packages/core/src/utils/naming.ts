// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * Naming convention utility for transparent snake_case ↔ camelCase transformation.
 *
 * Wraps the `humps` library (stable, MIT-licensed) for key conversion and adds
 * protection for non-serializable types (FormData, File, Blob, Date, ArrayBuffer).
 *
 * - **Response data** is converted from snake_case → camelCase, except the
 *   user-authored subtrees under `headers`/`metadata` keys, which stay verbatim.
 * - **Request data** has its top-level keys converted from camelCase →
 *   snake_case; nested objects travel verbatim (callers build wire-format
 *   configs, and user maps like HTTP headers must not be rewritten).
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

/** Keys whose subtree holds user-authored key-value data (HTTP headers,
 * rule metadata). The wire format must round-trip those keys verbatim, so
 * conversion never descends into them. */
const VERBATIM_SUBTREE_KEYS = new Set(['headers', 'metadata'])

function camelizeDeep(value: unknown): unknown {
  if (value === null || typeof value !== 'object' || isRawValue(value)) return value
  if (Array.isArray(value)) return value.map(camelizeDeep)
  const out: Record<string, unknown> = {}
  for (const [key, val] of Object.entries(value)) {
    if (VERBATIM_SUBTREE_KEYS.has(key)) {
      out[key] = val
    }
    else {
      out[humps.camelize(key)] = camelizeDeep(val)
    }
  }
  return out
}

/** Deeply convert all object keys from snake_case to camelCase. */
export function camelizeKeys<T>(data: unknown): T {
  if (data === null || data === undefined) return data as T
  if (typeof data !== 'object') return data as T
  if (isRawValue(data)) return data as T
  return camelizeDeep(data) as T
}

/** Deeply convert all object keys from camelCase to snake_case. */
export function snakeizeKeys<T>(data: unknown): T {
  if (data === null || data === undefined) return data as T
  if (typeof data !== 'object') return data as T
  if (isRawValue(data)) return data as T
  return humps.decamelizeKeys(data) as T
}

/** Convert only the first level of keys from camelCase to snake_case.
 *
 * Request bodies use this instead of the deep variant: nested objects in
 * bodies are payloads whose keys are already wire-format (channel/monitor
 * configs) or user-authored maps (HTTP headers, rule metadata) that must
 * reach the backend verbatim instead of being rewritten. */
export function snakeizeTopLevel<T>(data: unknown): T {
  if (data === null || data === undefined) return data as T
  if (typeof data !== 'object' || isRawValue(data)) return data as T
  if (Array.isArray(data)) return data.map((item) => snakeizeTopLevel(item)) as T
  const out: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(data)) {
    out[humps.decamelize(key)] = value
  }
  return out as T
}
