// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { AxiosRequestConfig, AxiosResponse, InternalAxiosRequestConfig } from 'axios'
import axios from 'axios'

/**
 * Full-stack tests for the request layer: the naming interceptors
 * (camelCase in, snake_case out), the unified envelope handling, and the
 * 40101 single-flight refresh + replay flow the backend token contract
 * relies on.
 *
 * The service instance is re-imported per test (vi.resetModules) because
 * the refresh single-flight state (isRefreshing / pendingRequests) is
 * module-level and must start clean.
 */

type RequestModule = typeof import('./request')

let mod: RequestModule
let service: RequestModule['default']

/** Captured API requests hitting the service adapter. */
let apiCalls: InternalAxiosRequestConfig[]
/** Captured refresh calls hitting the global axios adapter. */
let refreshCalls: AxiosRequestConfig[]
/** Scripted API responses, consumed in order. */
let apiScript: Array<{ status: number; body: unknown }>
/** Scripted refresh responses, consumed in order. */
let refreshScript: Array<{ status: number; body: unknown }>

function makeResponse(config: InternalAxiosRequestConfig, status: number, body: unknown): AxiosResponse {
  return {
    data: body,
    status,
    statusText: String(status),
    headers: {},
    config,
  }
}

function installAdapters(): void {
  apiCalls = []
  refreshCalls = []
  apiScript = []
  refreshScript = []

  service.defaults.adapter = async (config: InternalAxiosRequestConfig) => {
    apiCalls.push(config)
    const next = apiScript.shift() ?? { status: 200, body: { code: 0, message: 'ok', data: null } }
    if (next.status >= 400) {
      const err = new Error(`Request failed with status code ${next.status}`) as Error & {
        response?: AxiosResponse
        config?: InternalAxiosRequestConfig
        isAxiosError?: boolean
      }
      err.response = makeResponse(config, next.status, next.body)
      err.config = config
      err.isAxiosError = true
      throw err
    }
    return makeResponse(config, next.status, next.body)
  }

  axios.defaults.adapter = async (config: InternalAxiosRequestConfig) => {
    refreshCalls.push(config)
    const next = refreshScript.shift() ?? { status: 200, body: { code: 0, message: 'ok', data: null } }
    return makeResponse(config, next.status, next.body)
  }
}

/** Replace window.location with a plain object so redirects are observable. */
function stubLocation(): void {
  Object.defineProperty(window, 'location', {
    configurable: true,
    writable: true,
    value: { href: 'http://localhost/' },
  })
}

beforeEach(async () => {
  vi.resetModules()
  stubLocation()
  mod = await import('./request')
  service = mod.default
  installAdapters()
  mod.setToken('')
  window.localStorage.clear()
  window.sessionStorage.clear()
})

afterEach(() => {
  window.localStorage.clear()
  window.sessionStorage.clear()
})

describe('request naming interceptors', () => {
  it('snake_cases request body and query params, and injects auth/trace headers', async () => {
    mod.setToken('token-abc')
    apiScript.push({ status: 200, body: { code: 0, message: 'ok', data: { ok: true } } })

    await mod.request({ url: '/assets', method: 'post', params: { pageSize: 20, assetType: 'host' }, data: { assetKey: 'k', createdAt: 'now' } })

    expect(apiCalls).toHaveLength(1)
    const call = apiCalls[0]
    expect(call.params).toEqual({ page_size: 20, asset_type: 'host' })
    // axios serializes the body before the adapter runs
    expect(JSON.parse(call.data as string)).toEqual({ asset_key: 'k', created_at: 'now' })
    expect(call.headers.Authorization).toBe('Bearer token-abc')
    expect(call.headers['X-Tickraft-Request-Id']).toBeTruthy()
    expect(call.headers['X-Tickraft-Locale']).toBe('zh-Hans')
  })

  it('camelizes the payload of a successful envelope', async () => {
    apiScript.push({
      status: 200,
      body: {
        code: 0,
        message: 'ok',
        data: { total: 1, page_size: 20, items: [{ asset_key: 'k', created_at: 't' }] },
      },
    })

    const result = await mod.request<{ total: number; pageSize: number; items: Array<{ assetKey: string }> }>({
      url: '/assets',
      method: 'get',
    })

    expect(result).toEqual({ total: 1, pageSize: 20, items: [{ assetKey: 'k', createdAt: 't' }] })
  })

  it('rejects with the envelope message for business error codes', async () => {
    apiScript.push({ status: 200, body: { code: 40000, message: 'invalid request', data: null } })

    await expect(mod.request({ url: '/assets', method: 'get' })).rejects.toThrow('invalid request')
  })
})

describe('40101 token expiry handling', () => {
  it('refreshes once, replays the original request with the new token, and rotates the stored tokens', async () => {
    mod.setToken('expired-token')
    mod.setRefreshToken('refresh-1')

    apiScript.push(
      { status: 200, body: { code: 40101, message: 'token expired', data: null } },
      { status: 200, body: { code: 0, message: 'ok', data: 7 } },
    )
    refreshScript.push({
      status: 200,
      body: { code: 0, message: 'ok', data: { access_token: 'new-token', refresh_token: 'refresh-2' } },
    })

    const result = await mod.request<number>({ url: '/system/stats', method: 'get' })

    expect(result).toBe(7)
    expect(refreshCalls).toHaveLength(1)
    expect(JSON.parse(refreshCalls[0].data as string)).toEqual({ refresh_token: 'refresh-1' })
    // The replayed call carries the refreshed token.
    expect(apiCalls).toHaveLength(2)
    expect(apiCalls[1].headers.Authorization).toBe('Bearer new-token')
    // Tokens were rotated in storage.
    expect(mod.getToken()).toBe('new-token')
    expect(mod.getRefreshToken()).toBe('refresh-2')
  })

  it('coalesces concurrent 40101 responses into a single refresh', async () => {
    mod.setToken('expired-token')
    mod.setRefreshToken('refresh-1')

    apiScript.push(
      { status: 200, body: { code: 40101, message: 'token expired', data: null } },
      { status: 200, body: { code: 40101, message: 'token expired', data: null } },
      { status: 200, body: { code: 0, message: 'ok', data: 'a' } },
      { status: 200, body: { code: 0, message: 'ok', data: 'b' } },
    )
    refreshScript.push({
      status: 200,
      body: { code: 0, message: 'ok', data: { access_token: 'new-token', refresh_token: 'refresh-2' } },
    })

    const [a, b] = await Promise.all([
      mod.request<string>({ url: '/a', method: 'get' }),
      mod.request<string>({ url: '/b', method: 'get' }),
    ])

    expect(a).toBe('a')
    expect(b).toBe('b')
    expect(refreshCalls).toHaveLength(1)
    expect(apiCalls.map((c) => c.url)).toEqual(['/a', '/b', '/a', '/b'])
  })

  it('handles HTTP-401 responses carrying code 40101 (error-path refresh)', async () => {
    mod.setToken('expired-token')
    mod.setRefreshToken('refresh-1')

    apiScript.push(
      { status: 401, body: { code: 40101, message: 'token expired', data: null } },
      { status: 200, body: { code: 0, message: 'ok', data: 'done' } },
    )
    refreshScript.push({
      status: 200,
      body: { code: 0, message: 'ok', data: { access_token: 'new-token', refresh_token: 'refresh-2' } },
    })

    const result = await mod.request<string>({ url: '/c', method: 'get' })

    expect(result).toBe('done')
    expect(refreshCalls).toHaveLength(1)
    expect(apiCalls[1].headers.Authorization).toBe('Bearer new-token')
  })

  it('redirects to login and rejects when the refresh fails', async () => {
    mod.setToken('expired-token')
    mod.setRefreshToken('refresh-bad')
    window.localStorage.setItem('tk-user-info', '{}')

    apiScript.push({ status: 200, body: { code: 40101, message: 'token expired', data: null } })
    refreshScript.push({ status: 200, body: { code: 40100, message: 'invalid refresh token', data: null } })

    await expect(mod.request({ url: '/d', method: 'get' })).rejects.toThrow()
    await Promise.resolve()

    expect(window.location.href).toBe('/login')
    expect(mod.getToken()).toBe('')
    expect(window.localStorage.getItem('tk-user-info')).toBeNull()
  })
})

describe('40100 unauthorized handling', () => {
  it('redirects to login on envelope code 40100', async () => {
    mod.setToken('bad-token')
    apiScript.push({ status: 200, body: { code: 40100, message: 'unauthorized', data: null } })

    await expect(mod.request({ url: '/e', method: 'get' })).rejects.toThrow('unauthorized')
    expect(window.location.href).toBe('/login')
  })

  it('redirects to login on a plain HTTP 401 without 40101', async () => {
    mod.setToken('bad-token')
    apiScript.push({ status: 401, body: { code: 40100, message: 'no auth', data: null } })

    await expect(mod.request({ url: '/f', method: 'get' })).rejects.toThrow('no auth')
    expect(window.location.href).toBe('/login')
  })
})
