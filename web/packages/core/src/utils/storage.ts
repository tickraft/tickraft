// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/** Local storage key prefix */
const STORAGE_PREFIX = 'tk-'

/**
 * Get the prefixed storage key
 */
function getPrefixedKey(key: string): string {
  if (key.startsWith(STORAGE_PREFIX)) {
    return key
  }
  return `${STORAGE_PREFIX}${key}`
}

/**
 * Set a local storage value
 */
export function setStorage<T>(key: string, value: T): void {
  const prefixedKey = getPrefixedKey(key)
  const serialized = JSON.stringify(value)
  localStorage.setItem(prefixedKey, serialized)
}

/**
 * Set a session storage value. Session storage is scoped to the tab and
 * cleared when it closes, so it is used for sensitive short-lived values
 * (e.g. the refresh token) to reduce the blast radius of XSS.
 */
export function setSessionStorage<T>(key: string, value: T): void {
  const prefixedKey = getPrefixedKey(key)
  const serialized = JSON.stringify(value)
  sessionStorage.setItem(prefixedKey, serialized)
}

/**
 * Get a local storage value
 */
export function getStorage<T = unknown>(key: string): T | null {
  const prefixedKey = getPrefixedKey(key)
  const item = localStorage.getItem(prefixedKey)
  if (item === null) {
    return null
  }
  try {
    return JSON.parse(item) as T
  } catch {
    return null
  }
}

/**
 * Get a session storage value. Falls back to local storage so values
 * written before this split remain readable until replaced.
 */
export function getSessionStorage<T = unknown>(key: string): T | null {
  const prefixedKey = getPrefixedKey(key)
  const item = sessionStorage.getItem(prefixedKey)
  if (item !== null) {
    try {
      return JSON.parse(item) as T
    } catch {
      return null
    }
  }
  return getStorage<T>(key)
}

/**
 * Remove a value from both local and session storage.
 */
export function removeStorage(key: string): void {
  const prefixedKey = getPrefixedKey(key)
  localStorage.removeItem(prefixedKey)
  sessionStorage.removeItem(prefixedKey)
}

/**
 * Clear all local storage entries with the configured prefix
 */
export function clearStorage(): void {
  const keysToRemove: string[] = []
  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i)
    if (key && key.startsWith(STORAGE_PREFIX)) {
      keysToRemove.push(key)
    }
  }
  keysToRemove.forEach((key) => localStorage.removeItem(key))
}
