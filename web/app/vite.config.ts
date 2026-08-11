// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import legacy from '@vitejs/plugin-legacy'
import UnoCSS from 'unocss/vite'
import Icons from 'unplugin-icons/vite'
import IconsResolver from 'unplugin-icons/resolver'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import VueI18nPlugin from '@intlify/unplugin-vue-i18n/vite'
import { mockServerPlugin } from './vite-plugins/mock-server.js'
import path from 'path'

export default defineConfig({
  plugins: [
    vue(),
    UnoCSS(),
    Icons({ compiler: 'vue3', autoInstall: true }),
    Components({
      resolvers: [
        // Tree-shake element-plus: only components used in templates are
        // imported. The full CSS is kept (importStyle: false) to avoid
        // duplicated per-component styles.
        ElementPlusResolver({ importStyle: false }),
        IconsResolver({ enabledCollections: ['ep'] }),
      ],
    }),
    VueI18nPlugin({
      // 同时纳入内核（common 命名空间）与开源业务（auth/scheduler/collector/alert/system）的语言包
      // Note: paths intentionally use runtime JSON import fallback; pre-compilation is
      // disabled because some locale messages contain intentional HTML (e.g. <code>).
      include: [
        path.resolve(import.meta.dirname, '../../packages/core/src/i18n/locales/**'),
        path.resolve(import.meta.dirname, '../../packages/features/src/i18n/locales/**'),
      ],
    }),
    mockServerPlugin({
      // mock 定义已迁移至 @tickraft/features 包内
      // Vite 以 web/app 为 cwd，packages 在 web/ 下，故用 ../packages（一个 .. ）
      // Mock disabled by default — frontend now connects to the real backend
      // via the vite proxy below. Set enable: true to use mock data for
      // standalone frontend development when the backend is unavailable.
      mockPath: '../packages/features/src/mock',
      enable: true,
      logger: true,
    }),
    legacy({
      targets: ['defaults and not dead', '> 0.5%', 'Firefox ESR', 'last 2 versions'],
      renderLegacyChunks: true,
      additionalLegacyPolyfills: ['core-js/stable'],
    }),
  ],
  resolve: {
    // @tickraft/core 与 @tickraft/features 通过 pnpm workspace 符号链接自动解析，无需额外 alias
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
    // Force framework libs with module-level singletons (pinia's
    // activePinia/piniaSymbol, vue-router, vue-i18n) to resolve to a single
    // file. pnpm can install multiple physical instances of the same version
    // (different peer combos, e.g. a stale typescript@7 peer), which otherwise
    // splits the singleton across copies and crashes production builds.
    dedupe: ['pinia', 'vue', 'vue-router', 'vue-i18n', '@vue/devtools-api'],
  },
  // css.preprocessorOptions.scss no longer needs the `api: 'modern'` override:
  // Vite 8 (Rolldown) uses the modern Sass API by default and removed the
  // `api` option from SassPreprocessorOptions.
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:6153',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://localhost:6153',
        ws: true,
        changeOrigin: true,
      },
    },
  },
  build: {
    // Element Plus is tree-shaken via ElementPlusResolver, keeping the modern
    // chunk at ~700 kB and the legacy chunk at ~1.2 MB. The limit is set to
    // 1300 kB as a regression guard: exceeding it warns about new bloat
    // instead of silently growing.
    chunkSizeWarningLimit: 1300,
    rolldownOptions: {
      output: {
        // Vite 8 / Rolldown chunk splitting API. A manualChunks function that
        // only splits element-plus pulls framework runtimes into other chunks
        // (Vue's runtime, pinia's createPinia/useStore), duplicating module
        // singletons and breaking render/injection at runtime (blank page in
        // production builds: "_s" / activePinia errors). codeSplitting groups
        // keep every module-singleton framework lib in one shared chunk so the
        // app, element-plus and lazy route chunks all import the same instance.
        codeSplitting: {
          groups: [
            {
              name: 'vue',
              test: /node_modules[\\/](?:vue|@vue|pinia|vue-router|vue-i18n|vue-demi|@intlify)[\\/]/,
            },
            {
              name: 'element-plus',
              test: /node_modules[\\/]element-plus[\\/]/,
            },
          ],
        },
      },
    },
  },
})
