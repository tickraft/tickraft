// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { Plugin, ViteDevServer } from 'vite'
import path from 'path'
import fs from 'fs'

/**
 * Mock 路由定义（与 vite-plugin-mock 的 MockMethod 兼容）
 */
export interface MockRoute {
  url: string
  method: string
  response: (options: {
    url: string
    body: Record<string, unknown>
    query: Record<string, string>
    headers: Record<string, string>
  }) => unknown
}

interface CompiledRoute {
  urlRegex: RegExp
  method: string
  response: MockRoute['response']
}

/**
 * 自定义 Mock 服务插件
 * 使用 Vite 的 ssrLoadModule 加载 mock 文件，支持 HMR 热更新
 */
export function mockServerPlugin(options: {
  mockPath: string
  enable?: boolean
  logger?: boolean
}): Plugin {
  const { mockPath, enable = false, logger = true } = options

  let server: ViteDevServer
  let compiledRoutes: CompiledRoute[] = []
  let reloadTimer: ReturnType<typeof setTimeout> | null = null

  /** 将 :param 风格的 URL 转换为正则表达式 */
  function urlToRegex(url: string): RegExp {
    const escaped = url.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const withParams = escaped.replace(/:([^/]+)/g, '([^/]+)')
    return new RegExp('^' + withParams + '$')
  }

  /** 加载所有 mock 路由定义 */
  async function loadMockRoutes() {
    compiledRoutes = []
    const mockDir = path.resolve(process.cwd(), mockPath)
    if (!fs.existsSync(mockDir)) {
      console.warn(`[mock] mock directory not found: ${mockDir}`)
      return
    }

    const files = fs.readdirSync(mockDir).filter((f) => f.endsWith('.ts') && !f.startsWith('_'))

    for (const file of files) {
      const filePath = path.join(mockDir, file)
      try {
        // 使用 Vite SSR 模块加载，自动处理 TypeScript 编译和缓存失效
        const module = (await server.ssrLoadModule(filePath)) as { default?: MockRoute[] }
        const routes: MockRoute[] = module.default || []

        for (const route of routes) {
          compiledRoutes.push({
            urlRegex: urlToRegex(route.url),
            method: route.method.toUpperCase(),
            response: route.response,
          })
        }
      } catch (err) {
        console.error(`[mock] Failed to load ${file}:`, err)
      }
    }

    if (logger) {
      console.log(`[mock] Loaded ${compiledRoutes.length} mock routes from ${files.length} files`)
    }
  }

  /** 解析请求体 */
  function parseBody(req: { on: (event: string, handler: (...args: unknown[]) => void) => void }): Promise<Record<string, unknown>> {
    return new Promise((resolve) => {
      let body = ''
      req.on('data', (chunk: unknown) => {
        body += chunk as string
      })
      req.on('end', () => {
        try {
          resolve(body ? JSON.parse(body) : {})
        } catch {
          resolve({})
        }
      })
    })
  }

  return {
    name: 'vite-plugin-tickraft-mock',
    enforce: 'pre',

    async configureServer(devServer) {
      if (!enable) return
      server = devServer

      await loadMockRoutes()

      devServer.middlewares.use(async (req, res, next) => {
        if (!req.url || !req.method) return next()

        const url = req.url.split('?')[0]
        const query: Record<string, string> = {}
        const searchParams = new URL(req.url, 'http://localhost').searchParams
        searchParams.forEach((v, k) => {
          query[k] = v
        })

        const matched = compiledRoutes.find((r) => r.method === req.method!.toUpperCase() && r.urlRegex.test(url))

        if (!matched) return next()

        let body: Record<string, unknown> = {}
        if (['POST', 'PUT', 'PATCH'].includes(req.method!.toUpperCase())) {
          body = await parseBody(req)
        }

        if (logger) {
          console.log(`[mock] ${req.method} ${req.url}`)
        }

        try {
          const result = matched.response({ url, body, query, headers: req.headers as Record<string, string> })
          res.setHeader('Content-Type', 'application/json')
          res.end(JSON.stringify(result))
        } catch (err) {
          console.error('[mock] Error:', err)
          res.statusCode = 500
          res.end(JSON.stringify({ code: 50000, message: 'Internal server error', data: null }))
        }
      })

      // 监听 mock 文件变化，自动重新加载
      const mockDir = path.resolve(process.cwd(), mockPath)
      devServer.watcher.add(mockDir)

      const scheduleReload = (file: string) => {
        if (!file.startsWith(mockDir)) return
        if (reloadTimer) clearTimeout(reloadTimer)
        reloadTimer = setTimeout(() => {
          if (logger) {
            console.log(`[mock] Reloading due to change: ${path.basename(file)}`)
          }
          loadMockRoutes()
        }, 100)
      }

      devServer.watcher.on('change', scheduleReload)
      devServer.watcher.on('add', scheduleReload)
      devServer.watcher.on('unlink', scheduleReload)
    },
  }
}
