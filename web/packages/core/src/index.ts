// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// ── Components ──
export { default as DataTable } from './components/DataTable.vue'
export { default as SearchForm } from './components/SearchForm.vue'
export { default as StatusTag } from './components/StatusTag.vue'
export { default as ConfirmDialog } from './components/ConfirmDialog.vue'
export { default as PageEmpty } from './components/PageEmpty.vue'
export { default as FeatureGuard } from './components/FeatureGuard.vue'
export { default as TrendChart } from './components/TrendChart.vue'
export { default as LogoMark } from './components/LogoMark.vue'

// ── Component types ──
export type { DataTableColumn, DataTableProps } from './components/data-table-types'
export type { SearchFormField, SearchFormProps } from './components/search-form-types'

// ── Layouts ──
export { default as DefaultLayout } from './layouts/DefaultLayout.vue'
export { default as BlankLayout } from './layouts/BlankLayout.vue'

// ── Root component ──
export { default as App } from './App.vue'

// ── Composables ──
export { useTable } from './composables/useTable'
export { useForm } from './composables/useForm'
export { useChart } from './composables/useChart'
export { usePermission, registerFeatureProvider, FeatureConstants } from './composables/usePermission'
export { loadWidths, saveWidth, saveWidths, clearWidths } from './composables/useColumnWidths'
export { filterMenusByFeature } from './composables/useMenuFilter'
export { useResponsive } from './composables/useResponsive'
export { useFormGuard } from './composables/useFormGuard'
export { useEventListener } from './composables/useEventListener'
export { useInterval } from './composables/useInterval'
export { useLoading } from './composables/useLoading'
export {
  useErrorHandler,
  extractErrorMessage,
} from './composables/useErrorHandler'

// ── Composable types ──
export type { UseTableOptions, UseTableReturn } from './composables/useTable'
export type { UseFormOptions, UseFormReturn } from './composables/useForm'
export type { UseChartReturn } from './composables/useChart'
export type { UsePermissionReturn, FeatureKey, FeatureProvider } from './composables/usePermission'
export type { Breakpoint, UseResponsiveReturn } from './composables/useResponsive'
export type { UseFormGuardOptions, UseFormGuardReturn } from './composables/useFormGuard'
export type { UseEventListenerOptions } from './composables/useEventListener'
export type { UseIntervalControls, UseIntervalOptions } from './composables/useInterval'
export type { UseLoadingOptions, UseLoadingReturn } from './composables/useLoading'
export type {
  ErrorSeverity,
  UseErrorHandlerOptions,
  UseErrorHandlerReturn,
} from './composables/useErrorHandler'

// ── Utilities ──
export { request, getToken, setToken, getRefreshToken, setRefreshToken, clearAuth } from './utils/request'
export { getStorage, setStorage, removeStorage, clearStorage } from './utils/storage'
export { formatDate, formatRelativeTime, formatDuration } from './utils/date'
export { isValidCron, isValidUrl, isValidIp, isValidPort, isNonEmpty, isValidJson } from './utils/validate'

// ── Stores ──
export { useAppStore } from './stores/app'
export { useUserStore } from './stores/user'

// ── Router factory ──
export { createRouter } from './router'

// ── i18n factory ──
export { createI18n, mergeMessages, setI18nLocale, registerLocale, availableLocales, translate } from './i18n'
export { common } from './i18n/common'
export type { Messages, LocaleMeta } from './i18n'

// ── Element Plus locale loaders (for extension to register custom locale packs) ──
export {
  elementPlusLocale,
  loadElementPlusLocale,
  registerElementPlusLocale,
} from './i18n/element-plus'

// ── Custom directives ──
export { vFeature } from './directives/feature'

// ── Injection keys ──
export { BASE_MENUS_KEY } from './symbols'

// ── Types ──
export type { ApiResponse, PageData, PageParams, LoginParams, LoginData } from './types/api'
export type {
  ThemeMode,
  LocaleType,
  BuiltinLocale,
  SidebarState,
  FeatureFlag,
  FeatureFlags,
  UserInfo,
  AssetStatus,
  AlertStatus,
  TaskStatus,
  LogStatus,
  StatusCategory,
  StatusSize,
} from './types/global'
export type { MenuItem, MenuBadge } from './types/menu'
