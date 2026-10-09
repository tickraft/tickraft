// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { createI18n as createVueI18n } from 'vue-i18n'
import type { DefineLocaleMessage } from 'vue-i18n'
import { ref } from 'vue'
import { getStorage } from '../utils/storage'

/**
 * i18n message bundle type.
 *
 * Compatible with vue-i18n `LocaleMessage<VueMessageType>`, acting as the shared
 * message bundle type boundary between the core and extension. JSON locale packs
 * are asserted via `as Messages` on import.
 */
export type Messages = Record<string, DefineLocaleMessage>

/**
 * Locale metadata.
 *
 * Used to render the language switcher option list (DefaultLayout top bar, Settings page).
 */
export interface LocaleMeta {
  /** BCP 47 locale code, e.g. `zh-Hans`, `en-US`, `ja` */
  code: string
  /** Display label for the switcher, e.g. "中文", "English" */
  label: string
  /** Optional English name, e.g. "Chinese (Simplified)" */
  englishName?: string
  /** Text direction, defaults to `ltr`; extension must set `rtl` for RTL languages */
  direction?: 'ltr' | 'rtl'
}

/**
 * i18n instance controller.
 *
 * Internal interface used to access the instance created by the `createI18n` factory,
 * avoiding complex vue-i18n generic parameters at module-level variables. The store
 * and `registerLocale` use this controller to operate on the i18n instance indirectly,
 * enabling locale switching and message merging.
 */
interface I18nController {
  /** Synchronously set the current i18n locale (`global.locale.value`). */
  setLocale(locale: string): void
  /** Merge additional messages for a given locale into the i18n instance. */
  mergeLocaleMessage(code: string, messages: Record<string, unknown>): void
  /** Translate a key with the global composer. */
  t(key: string): string
}

/**
 * Module-level i18n controller singleton.
 *
 * Populated by the `createI18n` factory; read indirectly by `setI18nLocale` and
 * `registerLocale`. Stays `null` until `createI18n` is called, in which case the
 * related APIs silently return.
 */
let i18nController: I18nController | null = null

/**
 * Reactive registry of available locales.
 *
 * The core ships with zh-Hans and en-US; extension adds custom locales via
 * `registerLocale`. Locale switchers should render options based on this registry
 * so newly registered locales become visible automatically.
 *
 * Wrapped in `ref` so runtime `registerLocale` calls trigger reactive updates of
 * the language dropdown (Vue templates auto-unwrap `.value`; scripts must use `.value`).
 */
export const availableLocales = ref<LocaleMeta[]>([
  { code: 'zh-Hans', label: '中文' },
  { code: 'en-US', label: 'English' },
])

/**
 * Merge core base messages with extension messages.
 *
 * Same-name locales are shallow-merged between core and extension;
 * Extension namespaces do not conflict with core namespaces.
 *
 * @param base - core base messages (baseMessages)
 * @param extension - extension messages (extensionMessages)
 * @returns merged full message bundle
 */
export function mergeMessages(base: Messages, extension: Messages): Messages {
  const result: Messages = { ...base }
  Object.entries(extension).forEach(([locale, messages]) => {
    result[locale] = { ...(result[locale] ?? {}), ...messages }
  })
  return result
}

/** Default locale code. */
const DEFAULT_LOCALE = 'zh-Hans'

/** Core-supported locale set (extension extends via registerLocale). */
const KERNEL_LOCALES = new Set(['zh-Hans', 'en-US'])

/**
 * Browser locale detection.
 *
 * Iterates `navigator.languages` in priority order and returns the first locale
 * supported by the core. Tries exact match first (e.g. `zh-Hans`), then language-level
 * match (e.g. `en-GB` → `en-US`). Returns `DEFAULT_LOCALE` in SSR or no-`navigator` envs.
 */
function detectBrowserLocale(): string {
  if (typeof navigator === 'undefined' || !navigator.languages) {
    return DEFAULT_LOCALE
  }
  // Pass 1: exact match (e.g. navigator has "zh-Hans", kernel supports "zh-Hans")
  for (const navLocale of navigator.languages) {
    if (KERNEL_LOCALES.has(navLocale)) {
      return navLocale
    }
  }
  // Pass 2: language-level match (e.g. navigator has "en-GB", kernel supports "en-US")
  // map language prefix to kernel locale
  for (const navLocale of navigator.languages) {
    const lang = navLocale.split('-')[0]
    if (lang === 'zh') return 'zh-Hans'
    if (lang === 'en') return 'en-US'
  }
  return DEFAULT_LOCALE
}

/**
 * Return the stored locale, browser-detected locale, or default locale.
 *
 * Single source of truth for locale resolution: the i18n instance, the
 * `X-Tickraft-Locale` request header, and backend-error localization all
 * resolve through this so the UI language and error language can never
 * diverge (e.g. browser-detected en-US UI receiving zh-Hans error text).
 */
export function getLocale(): string {
  return getStorage<string>('tk-locale') || detectBrowserLocale()
}

/**
 * i18n instance factory.
 *
 * Callers (open-source `main.ts` or extension `main.ts`) pass the fully merged message
 * bundle; the core initializes `legacy: false`, `fallbackLocale: 'zh-Hans'`, and the
 * persisted current locale uniformly.
 *
 * The factory also wraps the instance into an `I18nController` cached at module scope,
 * so `setI18nLocale` and `registerLocale` can access the i18n instance indirectly,
 * avoiding explicit injection in the Pinia store.
 *
 * Returns the vue-i18n `I18n` instance, ready for `app.use(i18n)`.
 *
 * @param messages - full message bundle (typically `mergeMessages(baseMessages, extensionMessages)`)
 * @returns vue-i18n `I18n` instance
 */
export function createI18n(messages: Messages) {
  const instance = createVueI18n({
    legacy: false,
    locale: getLocale(),
    fallbackLocale: 'zh-Hans',
    messages,
  })
  i18nController = {
    setLocale: (locale: string) => {
      instance.global.locale.value = locale
    },
    mergeLocaleMessage: (code: string, msgs: Record<string, unknown>) => {
      instance.global.mergeLocaleMessage(code, msgs)
    },
    t: (key: string) => instance.global.t(key),
  }
  return instance
}

/**
 * Translate a message key with the global composer of the running i18n
 * instance. Used outside component setup context (e.g. the router's
 * document.title binding). Returns the key unchanged if `createI18n` has
 * not been called yet.
 *
 * @param key - message key (e.g. `task.list.title`)
 */
export function translate(key: string): string {
  return i18nController ? i18nController.t(key) : key
}

/**
 * Synchronously switch the current i18n locale.
 *
 * Called by `useAppStore.setLocale` to keep the store state, localStorage, and i18n
 * instance in sync, so UI text switches immediately without a page refresh.
 *
 * Silently returns if `createI18n` has not been called yet (e.g. during SSR).
 *
 * @param locale - target locale code
 */
export function setI18nLocale(locale: string): void {
  i18nController?.setLocale(locale)
}

/**
 * Register a new locale.
 *
 * Extension uses this API to extend languages beyond the built-in set
 * (e.g. zh-Hant, en-GB, de, fr, es, ja, ru, ko):
 * 1. Merge the message bundle into the i18n instance (`mergeLocaleMessage`)
 * 2. Append the `LocaleMeta` to the `availableLocales` registry (skip duplicate codes)
 *
 * After registration, locale switchers (DefaultLayout top bar, Settings page) include
 * the new locale automatically.
 *
 * Silently returns if `createI18n` has not been called yet.
 *
 * @param code - BCP 47 locale code (e.g. `ja`)
 * @param label - display label (e.g. "日本語")
 * @param messages - full message bundle for this locale (multi-namespace object)
 * @param options - optional metadata (English name, text direction)
 */
export function registerLocale(
  code: string,
  label: string,
  messages: Record<string, unknown>,
  options?: { englishName?: string; direction?: 'ltr' | 'rtl' },
): void {
  if (!i18nController) {
    return
  }
  i18nController.mergeLocaleMessage(code, messages)
  if (!availableLocales.value.some((item) => item.code === code)) {
    availableLocales.value.push({
      code,
      label,
      englishName: options?.englishName,
      direction: options?.direction,
    })
  }
}
