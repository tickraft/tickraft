// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { ref } from 'vue'
import type { Ref } from 'vue'
import type { Language } from 'element-plus/es/locale'

/**
 * Element Plus locale loader.
 *
 * The core ships with zh-Hans and en-US Element Plus locale packs; extension registers
 * custom locale packs via `registerElementPlusLocale` (zh-Hant, en-GB, de, fr, es, ja,
 * ru, ko, etc.).
 *
 * `elementPlusLocale` is a reactive ref; the `<el-config-provider>` in App.vue binds
 * to this value to keep Element Plus components (pagination, date picker, etc.) in sync.
 */

/** Element Plus locale module path map (core built-in) */
const builtinLoaders: Record<string, () => Promise<{ default: unknown }>> = {
  'zh-Hans': () => import('element-plus/es/locale/lang/zh-cn'),
  'en-US': () => import('element-plus/es/locale/lang/en'),
}

/** Custom Element Plus locale packs registered by extension */
const customLocales = new Map<string, unknown>()

/** Currently active Element Plus locale object */
export const elementPlusLocale: Ref<Language | null> = ref(null)

/**
 * Load an Element Plus locale pack.
 *
 * Priority: custom locale object registered via `registerElementPlusLocale` > exact match
 * with built-in loaders > language-level fallback (e.g. `en-GB` → `en-US`).
 * On load failure, keeps the current locale unchanged without affecting app operation.
 *
 * @param locale - BCP 47 locale code
 */
export async function loadElementPlusLocale(locale: string): Promise<void> {
  // 1. Check custom locales registered by extension
  if (customLocales.has(locale)) {
    elementPlusLocale.value = (customLocales.get(locale) as Language | undefined) ?? null
    return
  }

  // 2. Exact match with builtin loaders
  const loader = builtinLoaders[locale]
  if (loader) {
    try {
      const mod = await loader()
      elementPlusLocale.value = (mod.default as Language | undefined) ?? null
    } catch {
      // Load failed; keep current locale unchanged
    }
    return
  }

  // 3. Language-level fallback (e.g. en-GB → en-US, zh-Hant → zh-Hans)
  const lang = locale.split('-')[0]
  const fallbackLocale = lang === 'zh' ? 'zh-Hans' : lang === 'en' ? 'en-US' : null
  if (fallbackLocale && fallbackLocale !== locale) {
    await loadElementPlusLocale(fallbackLocale)
  }
}

/**
 * Register a custom Element Plus locale pack.
 *
 * Extension calls this function in `main.ts` to inject the corresponding Element Plus
 * locale object for extension locales (zh-Hant, en-GB, de, fr, es, ja, ru, ko, etc.).
 * After registration, `loadElementPlusLocale` uses the registered object first,
 * avoiding dynamic imports.
 *
 * @param locale - BCP 47 locale code
 * @param localeObj - Element Plus locale object (imported from `element-plus/es/locale/lang/...`)
 */
export function registerElementPlusLocale(locale: string, localeObj: unknown): void {
  customLocales.set(locale, localeObj)
}
