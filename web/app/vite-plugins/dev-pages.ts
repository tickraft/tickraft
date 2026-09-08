// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { Plugin } from 'vite'
import path from 'path'
import fs from 'fs'

/**
 * Dev-only helper pages (browser smoke-test token seeding and screenshot
 * redirects) served from app/dev-pages.
 *
 * These pages set mock auth tokens in localStorage, so they must never ship
 * in the production build. Files in public/ are copied verbatim into dist/,
 * which is why they live outside it and are served by this dev-server
 * middleware instead — the pages resolve at the same URLs as before
 * (/set-token.html, /screenshot-redirect.html) during `vite dev`.
 */
export function devPagesPlugin(): Plugin {
  const pagesDir = path.resolve(import.meta.dirname, '../dev-pages')

  return {
    name: 'tickraft-dev-pages',
    apply: 'serve',
    configureServer(server) {
      server.middlewares.use((req, _res, next) => {
        const name = (req.url ?? '').split('?')[0].replace(/^\//, '')
        if (name !== 'set-token.html' && name !== 'screenshot-redirect.html') {
          next()
          return
        }
        const file = path.join(pagesDir, name)
        if (!fs.existsSync(file)) {
          next()
          return
        }
        _res.setHeader('Content-Type', 'text/html; charset=utf-8')
        _res.end(fs.readFileSync(file, 'utf-8'))
      })
    },
  }
}
