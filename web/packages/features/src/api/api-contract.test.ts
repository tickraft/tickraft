// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { InternalAxiosRequestConfig } from 'axios'
import { setToken } from '@tickraft/core'
import service from '@tickraft/core/utils/request'

/**
 * API-layer contract tests: each feature api function must hit the right
 * URL with the right method, and pagination/filter parameters must leave
 * the browser as the backend's snake_case contract (page/page_size,
 * asset_type, task_name, ...) after the naming interceptor runs.
 */

let calls: InternalAxiosRequestConfig[]

beforeEach(() => {
  calls = []
  service.defaults.adapter = async (config: InternalAxiosRequestConfig) => {
    calls.push(config)
    return {
      data: { code: 0, message: 'ok', data: null },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  setToken('test-token')
  window.localStorage.clear()
})

describe('asset api', () => {
  it('getAssets sends page/page_size/keyword/asset_type/status', async () => {
    const { getAssets } = await import('./asset')
    await getAssets({
      page: 2,
      pageSize: 50,
      keyword: 'web',
      assetType: 'service',
      status: 'normal',
    } as Parameters<typeof getAssets>[0])

    expect(calls).toHaveLength(1)
    const call = calls[0]
    expect(call.url).toBe('/assets')
    expect(call.method).toBe('get')
    expect(call.params).toEqual({
      page: 2,
      page_size: 50,
      keyword: 'web',
      asset_type: 'service',
      status: 'normal',
    })
  })

  it('updateAssetStatus PUTs to /assets/:id/status with a snake_case body', async () => {
    const { updateAssetStatus } = await import('./asset')
    await updateAssetStatus(7, 'abnormal')

    const call = calls[0]
    expect(call.url).toBe('/assets/7/status')
    expect(call.method).toBe('put')
    expect(JSON.parse(call.data as string)).toEqual({ status: 'abnormal' })
  })
})

describe('task api', () => {
  it('getTaskExecutions sends task_name/executor/status filters with pagination', async () => {
    const task = await import('./task')
    await task.getLogs(0, {
      page: 1,
      pageSize: 20,
      taskName: 'backup',
      executor: 'local',
      status: 'success',
    } as never)

    const call = calls[0]
    expect(call.url).toContain('/tasks/0/executions')
    expect(call.method).toBe('get')
    expect(call.params).toEqual({
      page: 1,
      page_size: 20,
      task_name: 'backup',
      executor: 'local',
      status: 'success',
    })
  })

  it('task lifecycle actions hit pause/resume/copy endpoints', async () => {
    const task = await import('./task')
    const fns = task as unknown as Record<string, (id: number, ...rest: unknown[]) => Promise<unknown>>
    const actions: Array<[string, string, string]> = [
      ['pauseTask', 'post', '/tasks/3/pause'],
      ['resumeTask', 'post', '/tasks/3/resume'],
      ['copyTask', 'post', '/tasks/3/copy'],
      ['triggerTask', 'post', '/tasks/3/trigger'],
    ]
    for (const [name, method, url] of actions) {
      if (typeof fns[name] !== 'function') {
        throw new Error(`task api: missing ${name}`)
      }
      await fns[name](3)
      const call = calls.at(-1)
      expect(call?.method).toBe(method)
      expect(call?.url).toBe(url)
    }
  })
})

describe('prism api', () => {
  it('acknowledge and resolve hit the alert record action endpoints', async () => {
    const prism = await import('./prism')
    const fns = prism as unknown as Record<string, (id: number) => Promise<unknown>>
    if (typeof fns.acknowledgeAlertRecord !== 'function' || typeof fns.resolveAlertRecord !== 'function') {
      throw new Error('prism api: missing alert record action functions')
    }
    await fns.acknowledgeAlertRecord(11)
    expect(calls.at(-1)?.method).toBe('put')
    expect(calls.at(-1)?.url).toBe('/prism/alert/records/11/acknowledge')

    await fns.resolveAlertRecord(11)
    expect(calls.at(-1)?.method).toBe('put')
    expect(calls.at(-1)?.url).toBe('/prism/alert/records/11/resolve')
  })

  it('getRemediationRecords sends page/page_size and the status filter', async () => {
    const prism = await import('./prism')
    const fns = prism as unknown as Record<string, (params?: unknown) => Promise<unknown>>
    if (typeof fns.getRemediationRecords !== 'function') {
      throw new Error('prism api: missing getRemediationRecords')
    }
    await fns.getRemediationRecords({ page: 1, pageSize: 15, status: 'failed' })

    const call = calls[0]
    expect(call.url).toBe('/prism/remediation/records')
    expect(call.params).toEqual({ page: 1, page_size: 15, status: 'failed' })
  })
})

describe('auth api', () => {
  it('getApiKeys sends page/page_size', async () => {
    const auth = await import('./auth')
    const fns = auth as unknown as Record<string, (params?: unknown) => Promise<unknown>>
    if (typeof fns.getApiKeys !== 'function') {
      throw new Error('auth api: missing getApiKeys')
    }
    await fns.getApiKeys({ page: 1, pageSize: 20 })

    const call = calls[0]
    expect(call.url).toBe('/auth/apikeys')
    expect(call.params).toEqual({ page: 1, page_size: 20 })
  })
})

describe('telemetry api', () => {
  it('builtin templates endpoint precedes the :id route', async () => {
    const telemetry = await import('./telemetry')
    const fns = telemetry as unknown as Record<string, (...rest: unknown[]) => Promise<unknown>>
    if (typeof fns.getBuiltinTemplates !== 'function') {
      throw new Error('telemetry api: missing getBuiltinTemplates')
    }
    await fns.getBuiltinTemplates()
    expect(calls.at(-1)?.url).toBe('/telemetry/templates/builtin')
  })
})

describe('system api', () => {
  it('getGlobalStats and profile endpoints', async () => {
    const system = await import('./system')
    const fns = system as unknown as Record<string, (...rest: unknown[]) => Promise<unknown>>
    if (typeof fns.getGlobalStats !== 'function' || typeof fns.getProfile !== 'function') {
      throw new Error('system api: missing stats/profile functions')
    }
    await fns.getGlobalStats()
    expect(calls.at(-1)?.url).toBe('/system/stats')
    await fns.getProfile()
    expect(calls.at(-1)?.url).toBe('/system/profile')
  })
})
