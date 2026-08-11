// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { createApp, type Component } from 'vue'
import { createPinia } from 'pinia'
import piniaPluginPersistedstate from 'pinia-plugin-persistedstate'
import { vLoading } from 'element-plus'
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
import 'element-plus/dist/index.css'
import 'virtual:uno.css'

// Global styles must load before component rendering to ensure CSS variables
// (Design Tokens) are available. Loaded via @tickraft/core/styles subpath:
// tokens → themes → element → common
import '@tickraft/core/styles'

import { App, createRouter, createI18n, vFeature, BASE_MENUS_KEY } from '@tickraft/core'
import { baseRoutes, baseMessages, baseMenus } from '@tickraft/features'

// Legacy browsers (IE11 / Trident compatibility mode) do not support CSS
// custom properties. css-vars-ponyfill is loaded on demand and resolves
// var() references BEFORE the app mounts, so the first paint never shows
// unstyled markup (FOUC). Modern browsers short-circuit via the feature
// check below and pay zero download or execution cost.
function supportsCssVars(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.CSS?.supports === 'function' &&
    window.CSS.supports('--a', '0')
  )
}

// Kick off the lazy load early so its download overlaps with app setup.
// onFinally resolves once the stylesheet analysis finishes; watch keeps
// var() values in sync on theme switches (dark/light). Failures resolve
// anyway so the app still mounts (unstyled) instead of blank-screening.
const cssVarsReady: Promise<void> = supportsCssVars()
  ? Promise.resolve()
  : new Promise((resolve) => {
      import('css-vars-ponyfill')
        .then(({ default: cssVars }) => {
          cssVars({
            onlyLegacy: true,
            watch: true,
            silent: true,
            onFinally: () => resolve(),
          })
        })
        .catch(() => resolve())
    })

// Open-source standalone entry: builds router and i18n from kernel base data.
// Extension app (tickraft-x/web/src/main.ts) merges extension data before
// calling the same kernel factories.
const router = createRouter(baseRoutes)
const i18n = createI18n(baseMessages)

const app = createApp(App)

// Pinia state management
const pinia = createPinia()
pinia.use(piniaPluginPersistedstate)
app.use(pinia)

// i18n
app.use(i18n)

// Router
app.use(router)

// Element Plus: components are tree-shaken per-template via
// unplugin-vue-components (ElementPlusResolver in vite.config.ts), so there is
// no full app.use(ElementPlus). The v-loading directive is registered here.
app.directive('loading', vLoading)

// Globally register Element Plus icon components.
// Menu icons resolve via <component :is="iconName"> using these registrations.
// Login.vue etc. also reference icons by string name (prefix-icon="User").
for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
  app.component(key, component as Component)
}

// Custom directive
app.directive('feature', vFeature)

// Provide base menus to DefaultLayout via injection key.
// This breaks the circular dependency: core DefaultLayout needs baseMenus
// from features, but features depends on core. App root provides it here.
app.provide(BASE_MENUS_KEY, baseMenus)

// Dev error handler for diagnosing render issues
app.config.errorHandler = (err, _instance, info) => {
  console.error('[Web Error]', err, info)
}

// Mount only after legacy CSS variables are resolved (a no-op on modern
// browsers), preventing an unstyled first paint in compatibility mode.
async function mountApp(): Promise<void> {
  await cssVarsReady
  app.mount('#app')
}

void mountApp()
