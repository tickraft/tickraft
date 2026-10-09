// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * useLoading - track async operation loading state.
 *
 * Wraps an async function so `isLoading` reflects whether the operation is
 * in-flight. Supports an optional minimum duration to avoid flicker on very
 * fast responses, and an error ref capturing the most recent failure.
 *
 * Canonical implementation; `@tickraft/ui` re-exports it so consumers can
 * import from either package.
 */
import { ref, type Ref } from 'vue'

export interface UseLoadingReturn<TArgs extends unknown[], TResult> {
  /** Whether the wrapped operation is currently in-flight */
  isLoading: Ref<boolean>
  /** Most recent error (null when the last call succeeded) */
  error: Ref<unknown>
  /** Run the wrapped async function */
  run: (...args: TArgs) => Promise<TResult | undefined>
  /** Reset loading + error state */
  reset: () => void
}

export interface UseLoadingOptions {
  /** Minimum visible duration in ms to avoid flicker (default 0 = none) */
  minDuration?: number
}

/**
 * Wrap an async function with reactive loading/error tracking.
 *
 * @param fn - the async function to wrap
 * @param options - loading options
 */
export function useLoading<TArgs extends unknown[], TResult>(
  fn: (...args: TArgs) => Promise<TResult>,
  options: UseLoadingOptions = {},
): UseLoadingReturn<TArgs, TResult> {
  const { minDuration = 0 } = options
  const isLoading = ref(false)
  const error = ref<unknown>(null)

  async function run(...args: TArgs): Promise<TResult | undefined> {
    isLoading.value = true
    error.value = null
    const startedAt = Date.now()
    try {
      const result = await fn(...args)
      return result
    } catch (err) {
      error.value = err
      return undefined
    } finally {
      const elapsed = Date.now() - startedAt
      if (minDuration > 0 && elapsed < minDuration) {
        setTimeout(() => {
          isLoading.value = false
        }, minDuration - elapsed)
      } else {
        isLoading.value = false
      }
    }
  }

  function reset(): void {
    isLoading.value = false
    error.value = null
  }

  return { isLoading, error, run, reset }
}
