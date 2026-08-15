// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import Icons from 'unplugin-icons/vite'
import { resolve } from 'node:path'

export default defineConfig({
  plugins: [
    vue(),
    // Feature api tests transitively import layout components whose
    // <i-ep-*> components resolve through unplugin-icons.
    Icons(),
  ],
  resolve: {
    alias: {
      '@tickraft/core': resolve(__dirname, 'packages/core/src'),
      '@tickraft/features': resolve(__dirname, 'packages/features/src'),
      // vue is declared as peerDependency of @tickraft/core; the actual install
      // lives under app/node_modules (workspace consumer). Point vitest at it
      // so packages/core/src tests can resolve the vue runtime.
      vue: resolve(__dirname, 'app/node_modules/vue'),
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    include: ['packages/**/*.test.ts', 'packages/**/*.spec.ts'],
  },
})
