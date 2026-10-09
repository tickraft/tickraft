// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * useTheme - shared light/dark/auto theme controller.
 *
 * Applies the theme preference to documentElement via `data-theme` and
 * resolves `auto` against the system color-scheme preference. Persists the
 * preference to localStorage under `tk-theme`. This is the canonical theme
 * controller reused across frontends so behavior stays consistent.
 *
 * Singleton state: the preference, system-scheme and resolved theme live at
 * module scope so every component calling `useTheme()` reads and mutates the
 * same refs. The matchMedia listener is installed once for the whole app
 * lifetime by `initTheme`, which should be called at startup (main.ts) before
 * mount to apply the persisted preference and avoid a flash of the wrong theme.
 */
import { computed, ref, watch, type ComputedRef, type Ref } from 'vue'

/** Theme preference */
export type ThemeMode = 'light' | 'dark' | 'auto'

/** Resolved (effective) theme, never 'auto' */
export type EffectiveTheme = 'light' | 'dark'

export interface UseThemeOptions {
  /** Initial theme preference (default 'auto') */
  initial?: ThemeMode
  /** localStorage key (default 'tk-theme') */
  storageKey?: string
  /** Attribute name applied to documentElement (default 'data-theme') */
  attr?: string
}

export interface UseThemeReturn {
  /** User theme preference (may be 'auto') */
  theme: Ref<ThemeMode>
  /** Resolved effective theme (light/dark, follows system when preference is auto) */
  effectiveTheme: ComputedRef<EffectiveTheme>
  /** Whether the effective theme is dark */
  isDark: ComputedRef<boolean>
  /** Set the theme preference */
  setTheme: (mode: ThemeMode) => void
  /** Cycle light → dark → auto → light */
  toggleTheme: () => void
}

const STORAGE_KEY_DEFAULT = 'tk-theme'
const ATTR_DEFAULT = 'data-theme'

/* ============================================================
 * Module-scope shared singleton state.
 *
 * Declared at module scope so every component calling useTheme()
 * reads and mutates the same refs. The matchMedia listener is
 * installed once (by initTheme) for the whole app lifetime.
 * ============================================================ */
const preference = ref<ThemeMode>('auto')
const systemDark = ref<EffectiveTheme>('light')
let initialized = false
let mediaQuery: MediaQueryList | null = null
let activeStorageKey = STORAGE_KEY_DEFAULT
let activeAttr = ATTR_DEFAULT

/** Resolved theme: follows system when preference is auto. */
const effectiveTheme = computed<EffectiveTheme>(() => {
  if (preference.value === 'auto') return systemDark.value
  return preference.value
})

const isDark = computed(() => effectiveTheme.value === 'dark')

function readStored(storageKey: string, initial: ThemeMode): ThemeMode {
  if (typeof window === 'undefined') return initial
  try {
    const stored = window.localStorage.getItem(storageKey)
    if (stored === 'light' || stored === 'dark' || stored === 'auto') {
      return stored
    }
  } catch {
    // localStorage may be unavailable (private mode); fall back to initial
  }
  return initial
}

function resolveSystem(): EffectiveTheme {
  if (typeof window === 'undefined' || !window.matchMedia) return 'light'
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

/** Apply the resolved effective theme to documentElement. */
function applyAttr(): void {
  if (typeof document === 'undefined') return
  document.documentElement.setAttribute(activeAttr, effectiveTheme.value)
}

function onSystemChange(evt: MediaQueryListEvent): void {
  systemDark.value = evt.matches ? 'dark' : 'light'
  // applyAttr is driven by the effectiveTheme watcher, no need to call here.
}

/**
 * Initialize the shared theme controller synchronously.
 *
 * Call once at app startup (main.ts) before mount to apply the persisted
 * preference and avoid a flash of the wrong theme. Idempotent: subsequent
 * calls are no-ops. Must run on the client.
 *
 * @param options - theme options
 */
export function initTheme(options: UseThemeOptions = {}): void {
  if (initialized) return
  const { initial = 'auto', storageKey = STORAGE_KEY_DEFAULT, attr = ATTR_DEFAULT } = options
  activeStorageKey = storageKey
  activeAttr = attr
  preference.value = readStored(storageKey, initial)
  systemDark.value = resolveSystem()
  applyAttr()
  if (typeof window !== 'undefined' && window.matchMedia) {
    mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    mediaQuery.addEventListener('change', onSystemChange)
  }
  // Keep the documentElement attribute in sync whenever the effective
  // theme changes (preference change or system-scheme change).
  watch(effectiveTheme, applyAttr)
  initialized = true
}

/** Set the theme preference, persist it, and apply the attribute. */
function setTheme(mode: ThemeMode): void {
  preference.value = mode
  try {
    window.localStorage.setItem(activeStorageKey, mode)
  } catch {
    // ignore storage errors
  }
  applyAttr()
}

/** Cycle light → dark → auto → light. */
function toggleTheme(): void {
  const order: ThemeMode[] = ['light', 'dark', 'auto']
  const idx = order.indexOf(preference.value)
  const next = order[(idx + 1) % order.length] as ThemeMode
  setTheme(next)
}

/**
 * Access the shared theme controller. Returns the singleton state and
 * setters. The first call seeds initialization if `initTheme` was not
 * called explicitly (e.g. in tests or component-only usage).
 *
 * @param options - theme options (only honored on first initialization)
 */
export function useTheme(options: UseThemeOptions = {}): UseThemeReturn {
  if (!initialized) initTheme(options)
  return { theme: preference, effectiveTheme, isDark, setTheme, toggleTheme }
}
