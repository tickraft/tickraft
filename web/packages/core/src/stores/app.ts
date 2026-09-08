// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref } from 'vue'
import { getStorage, setStorage } from '../utils/storage'
import { getLocale, setI18nLocale } from '../i18n'
import type { ThemeMode, LocaleType, SidebarState } from '../types/global'

/** Theme storage key */
const THEME_KEY = 'tk-theme'
/** Locale storage key */
const LOCALE_KEY = 'tk-locale'
/** Sidebar state storage key */
const SIDEBAR_KEY = 'tk-sidebar'
/** System theme detection media query */
const THEME_MEDIA_QUERY = '(prefers-color-scheme: dark)'

/** Module-level guard that prevents binding the theme listener repeatedly when the store is recreated */
let themeListenerBound = false

/**
 * Read the system theme preference.
 *
 * Falls back to `light` in SSR or environments without matchMedia support.
 */
function resolveSystemTheme(): 'light' | 'dark' {
  if (typeof window === 'undefined' || !window.matchMedia) {
    return 'light'
  }
  return window.matchMedia(THEME_MEDIA_QUERY).matches ? 'dark' : 'light'
}

/**
 * Global application state store.
 *
 * The theme supports three states: `light` / `dark` / `auto`. In `auto` mode
 * the theme follows the system `prefers-color-scheme` and reactively responds
 * to system theme changes.
 */
export const useAppStore = defineStore('app', () => {
  /** Current theme preference (user-selected, persisted) */
  const theme = ref<ThemeMode>(getStorage<ThemeMode>(THEME_KEY) || 'auto')

  /** Current locale */
  const locale = ref<LocaleType>(getLocale() as LocaleType)

  /** Sidebar state */
  const sidebar = ref<SidebarState>(getStorage<SidebarState>(SIDEBAR_KEY) || { collapsed: false })

  /** System theme (reactive, updated by the matchMedia listener) */
  const systemTheme = ref<'light' | 'dark'>(resolveSystemTheme())

  /**
   * Effective theme.
   *
   * When `theme === 'auto'`, this resolves to `light` or `dark` based on the
   * system preference; otherwise it returns the `theme` value directly. The
   * UI layer should prefer this value to determine the current theme state
   * rather than reading `theme` directly (`auto` is not a real theme).
   */
  const effectiveTheme = computed<'light' | 'dark'>(() =>
    theme.value === 'auto' ? systemTheme.value : theme.value,
  )

  /** Apply the theme to documentElement (silently ignored when DOM is unavailable) */
  function applyThemeToDom(actual: 'light' | 'dark'): void {
    try {
      document.documentElement.setAttribute('data-theme', actual)
    } catch {
      // Silently ignored when DOM is unavailable (SSR or abnormal environments)
    }
  }

  /** Sync document.documentElement.lang so assistive technologies and search engines can identify the current language */
  function applyLocaleToDom(code: string): void {
    try {
      document.documentElement.lang = code
    } catch {
      // Silently ignored when DOM is unavailable (SSR or abnormal environments)
    }
  }

  /**
   * Switch the theme.
   *
   * - `auto`: does not set `data-theme` directly; derives indirectly from the
   *   system preference. When the system theme changes, the matchMedia listener
   *   syncs the DOM in real time.
   * - `light` / `dark`: sets `data-theme` directly.
   *
   * The persisted value is the user preference (including `auto`); the
   * effective theme is exposed via `effectiveTheme`.
   */
  function setTheme(newTheme: ThemeMode): void {
    theme.value = newTheme
    setStorage(THEME_KEY, newTheme)
    if (newTheme === 'auto') {
      applyThemeToDom(systemTheme.value)
    } else {
      applyThemeToDom(newTheme)
    }
  }

  /**
   * Switch the locale.
   *
   * Syncs four places — the store state, localStorage, the i18n instance and
   * `document.documentElement.lang` — so page text switches immediately,
   * assistive technologies and search engines can identify the current
   * language, and no refresh is required.
   */
  function setLocale(newLocale: LocaleType): void {
    locale.value = newLocale
    setStorage(LOCALE_KEY, newLocale)
    setI18nLocale(newLocale)
    applyLocaleToDom(newLocale)
  }

  /** Toggle the sidebar collapsed state */
  function toggleSidebar(): void {
    sidebar.value.collapsed = !sidebar.value.collapsed
    setStorage(SIDEBAR_KEY, sidebar.value)
  }

  // Initialize: apply the current theme and locale to the DOM
  applyThemeToDom(effectiveTheme.value)
  applyLocaleToDom(locale.value)

  // Register a system theme change listener (the module-level guard prevents
  // duplicate binding; onScopeDispose auto-cleans up when the store is
  // disposed, primarily serving test scenarios — in production the store is a
  // singleton that lives for the entire app lifetime)
  if (!themeListenerBound && typeof window !== 'undefined' && window.matchMedia) {
    const mediaQuery = window.matchMedia(THEME_MEDIA_QUERY)
    const handler = (event: MediaQueryListEvent): void => {
      systemTheme.value = event.matches ? 'dark' : 'light'
      // In auto mode, sync the DOM in real time; in light/dark mode, ignore system changes
      if (theme.value === 'auto') {
        applyThemeToDom(systemTheme.value)
      }
    }
    mediaQuery.addEventListener('change', handler)
    themeListenerBound = true
    onScopeDispose(() => {
      mediaQuery.removeEventListener('change', handler)
      themeListenerBound = false
    })
  }

  return {
    theme,
    locale,
    sidebar,
    effectiveTheme,
    setTheme,
    setLocale,
    toggleSidebar,
  }
})
