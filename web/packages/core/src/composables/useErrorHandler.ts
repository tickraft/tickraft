// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * useErrorHandler - standardized 3-layer error feedback for components.
 *
 * Implements Layer 2 (component-level) of the error feedback model:
 * - Layer 1 (interceptor): HTTP interceptor catches network/HTTP errors and
 *   shows generic toasts — already handled by each project's request module.
 * - Layer 2 (component): this composable catches errors in async operations,
 *   extracts a human-readable message, shows a contextual toast with i18n
 *   fallback, and tracks error state for optional inline display.
 * - Layer 3 (global): `app.config.errorHandler` catches uncaught Vue render
 *   errors and unhandled promise rejections — set up in each project's main.ts.
 *
 * Usage:
 * ```ts
 * const { handleError, guard, error, message } = useErrorHandler()
 *
 * // Imperative: catch and handle manually
 * try {
 *   await fetchData()
 * } catch (err) {
 *   handleError(err, t('data.loadFailed'))
 * }
 *
 * // Declarative: wrap an async function
 * const safeFetch = guard(fetchData)
 * await safeFetch()
 * ```
 *
 * Canonical implementation; `@tickraft/ui` re-exports it so consumers can
 * import from either package.
 */
import { computed, ref, type ComputedRef, type Ref } from 'vue'

/** Error severity levels mapped to toast types. */
export type ErrorSeverity = 'error' | 'warning' | 'info'

export interface UseErrorHandlerOptions {
  /**
   * Custom notifier. Defaults to a lazy import of ElMessage so the composable
   * works in any Element Plus project without hard wiring at module load.
   */
  notifier?: (message: string, severity: ErrorSeverity) => void
  /**
   * Custom message extractor. Defaults to {@link extractErrorMessage}.
   *
   * Frontends with their own error types (e.g. a project-specific `ApiError`)
   * or i18n fallback can pass an override so the canonical composable stays
   * reusable without each project keeping a local copy.
   */
  extractor?: (err: unknown, fallback?: string) => string
  /** Whether to show a toast notification on error (default `true`). */
  showToast?: boolean
  /** Whether to log the error to `console.error` (default `true`). */
  logToConsole?: boolean
}

export interface UseErrorHandlerReturn {
  /** The last captured error (`null` when cleared or never set). */
  error: Ref<unknown>
  /** Human-readable message extracted from the last error. */
  message: ComputedRef<string>
  /** Whether an error is currently active. */
  hasError: ComputedRef<boolean>
  /**
   * Handle an error: extract message, optionally toast + log, and store.
   *
   * @param err - the thrown value (typically an Error/ApiError/unknown)
   * @param fallbackMessage - i18n string shown when the error has no message
   */
  handleError: (err: unknown, fallbackMessage?: string) => void
  /**
   * Wrap an async function so any thrown error is auto-handled.
   *
   * Returns `undefined` on failure (the error is already toasted/logged).
   */
  guard: <TArgs extends unknown[], TResult>(
    fn: (...args: TArgs) => Promise<TResult>,
  ) => (...args: TArgs) => Promise<TResult | undefined>
  /** Clear the last error state. */
  clear: () => void
}

/**
 * Extract a human-readable message from a thrown value.
 *
 * Recognizes:
 * - Objects with a `.message` string property (Error, ApiError, etc.)
 * - Plain strings
 * - Everything else falls back to `String(value)` or the provided fallback
 */
export function extractErrorMessage(err: unknown, fallback?: string): string {
  if (err === null || err === undefined) {
    return fallback ?? 'Unknown error'
  }
  if (typeof err === 'string') return err
  if (err instanceof Error) return err.message || (fallback ?? 'Unknown error')
  if (typeof err === 'object' && 'message' in err) {
    const msg = (err as { message: unknown }).message
    if (typeof msg === 'string' && msg.length > 0) return msg
  }
  return fallback ?? String(err)
}

/** Default notifier using ElMessage (lazy-loaded to avoid side effects). */
async function defaultNotifier(message: string, severity: ErrorSeverity): Promise<void> {
  const { ElMessage } = await import('element-plus')
  if (severity === 'warning') {
    ElMessage.warning(message)
  } else if (severity === 'info') {
    ElMessage.info(message)
  } else {
    ElMessage.error(message)
  }
}

/**
 * Create a standardized error handler for component-level error feedback.
 *
 * @param options - configuration for notification and logging behavior
 */
export function useErrorHandler(
  options: UseErrorHandlerOptions = {},
): UseErrorHandlerReturn {
  const { notifier, extractor, showToast = true, logToConsole = true } = options
  const resolveMessage = extractor ?? extractErrorMessage

  const error = ref<unknown>(null)

  const message = computed(() =>
    resolveMessage(error.value),
  )

  const hasError = computed(() => error.value !== null)

  function handleError(err: unknown, fallbackMessage?: string): void {
    error.value = err
    const msg = resolveMessage(err, fallbackMessage)

    if (logToConsole) {
      console.error('[useErrorHandler]', err)
    }

    if (showToast) {
      if (notifier) {
        notifier(msg, 'error')
      } else {
        void defaultNotifier(msg, 'error')
      }
    }
  }

  function guard<TArgs extends unknown[], TResult>(
    fn: (...args: TArgs) => Promise<TResult>,
  ): (...args: TArgs) => Promise<TResult | undefined> {
    return async (...args: TArgs): Promise<TResult | undefined> => {
      try {
        return await fn(...args)
      } catch (err) {
        handleError(err)
        return undefined
      }
    }
  }

  function clear(): void {
    error.value = null
  }

  return { error, message, hasError, handleError, guard, clear }
}
