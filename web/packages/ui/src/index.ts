// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * @tickraft/ui - cross-frontend shared UI primitives and composables.
 *
 * Provides self-contained, Element-Plus-backed primitives (Button, Input,
 * Dialog, Drawer) plus shared composables (useEventListener, useInterval,
 * useLoading, useFormGuard, useFocusRestore, useTheme) for consistent UI
 * across all Tickraft frontends.
 *
 * Higher-level data components (DataTable, SearchForm, StatusTag) and the
 * useForm composable are re-exported from @tickraft/core so consumers have a
 * single import surface without duplicating implementations.
 */
// Local primitives
export { default as Button } from './components/Button.vue'
export { default as Input } from './components/Input.vue'
export { default as Dialog } from './components/Dialog.vue'
export { default as Drawer } from './components/Drawer.vue'

// Composables: canonical implementations live in @tickraft/core (single
// source of truth); useFocusRestore and useTheme are ui-local.
export { useEventListener } from './composables/useEventListener'
export type { UseEventListenerOptions } from './composables/useEventListener'
export { useInterval } from './composables/useInterval'
export type { UseIntervalControls, UseIntervalOptions } from './composables/useInterval'
export { useLoading } from './composables/useLoading'
export type { UseLoadingReturn, UseLoadingOptions } from './composables/useLoading'
export { useErrorHandler, extractErrorMessage } from './composables/useErrorHandler'
export type {
  UseErrorHandlerOptions,
  UseErrorHandlerReturn,
  ErrorSeverity,
} from './composables/useErrorHandler'
export { useFormGuard } from './composables/useFormGuard'
export type { UseFormGuardOptions, UseFormGuardReturn } from './composables/useFormGuard'
export { useFocusRestore } from './composables/useFocusRestore'
export type { UseFocusRestoreReturn } from './composables/useFocusRestore'
export { useTheme, initTheme } from './composables/useTheme'
export type { ThemeMode, EffectiveTheme, UseThemeOptions, UseThemeReturn } from './composables/useTheme'

// Re-exported from @tickraft/core (single import surface)
export { DataTable, SearchForm, StatusTag, useForm } from '@tickraft/core'
export type { UseFormOptions, UseFormReturn } from '@tickraft/core'
