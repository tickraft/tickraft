// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { MockMethod } from './types'

/** One day in milliseconds */
const DAY_MS = 86_400_000

/**
 * API Key list (6 items, aligned with prototype mock-data.js)
 * Fields: id, name, prefix, created_at, last_used_at, expires_at, status
 */
const mockApiKeys = [
  {
    id: 1,
    name: 'Default API Key',
    prefix: 'tk_abc1',
    created_at: '2026-06-01 10:00:00',
    last_used_at: '2026-06-30 13:20:00',
    expires_at: '2027-06-30 00:00:00',
    status: 'active',
  },
  {
    id: 2,
    name: 'Telemetry Report Key',
    prefix: 'tk_def2',
    created_at: '2026-06-02 10:00:00',
    last_used_at: '2026-06-30 12:45:00',
    expires_at: '2027-06-30 00:00:00',
    status: 'active',
  },
  {
    id: 3,
    name: 'Admin Key',
    prefix: 'tk_ghi3',
    created_at: '2026-06-03 10:00:00',
    last_used_at: '2026-06-28 09:10:00',
    expires_at: '',
    status: 'active',
  },
  {
    id: 4,
    name: 'Third-Party Integration',
    prefix: 'tk_jkl4',
    created_at: '2026-05-15 10:00:00',
    last_used_at: '2026-06-15 16:30:00',
    expires_at: '2026-06-15 00:00:00',
    status: 'revoked',
  },
  {
    id: 5,
    name: 'Temporary Debug Key',
    prefix: 'tk_mno5',
    created_at: '2026-06-15 10:00:00',
    last_used_at: '2026-06-20 14:00:00',
    expires_at: '2026-07-15 00:00:00',
    status: 'active',
  },
  {
    id: 6,
    name: 'CI/CD Pipeline',
    prefix: 'tk_pqr6',
    created_at: '2026-06-20 10:00:00',
    last_used_at: '2026-06-30 08:00:00',
    expires_at: '',
    status: 'active',
  },
]

/** Auto-increment ID for new API keys */
let apiKeySeq = mockApiKeys.length

/** Generate a random string of specified length (lowercase letters + digits) */
function generateRandomString(length: number): string {
  const chars = 'abcdefghijklmnopqrstuvwxyz0123456789'
  let result = ''
  for (let i = 0; i < length; i++) {
    result += chars.charAt(Math.floor(Math.random() * chars.length))
  }
  return result
}

/** Format a Date to `YYYY-MM-DD HH:mm:ss` */
function formatDateTime(date: Date): string {
  const pad = (n: number): string => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

/** Generate a raw key (only returned once on creation) */
function generateRawKey(): string {
  return 'tk_sk_' + generateRandomString(32)
}

/** Extract numeric ID from URL path segment */
function extractId(url: string): number {
  const match = url.match(/\/(\d+)(?:\/|$)/)
  return match ? Number(match[1]) : 0
}

export default [
  {
    url: '/api/v1/auth/login',
    method: 'post',
    response: () => ({
      code: 0,
      message: 'success',
      data: {
        access_token: 'mock-jwt-token-' + Date.now(),
        refresh_token: 'mock-refresh-token-' + Date.now(),
        must_change_password: false,
      },
    }),
  },
  {
    url: '/api/v1/auth/refresh',
    method: 'post',
    response: () => ({
      code: 0,
      message: 'success',
      data: {
        access_token: 'mock-jwt-token-refreshed-' + Date.now(),
        refresh_token: 'mock-refresh-token-refreshed-' + Date.now(),
        must_change_password: false,
      },
    }),
  },
  {
    url: '/api/v1/auth/logout',
    method: 'post',
    response: () => ({
      code: 0,
      message: 'success',
      data: null,
    }),
  },
  {
    url: '/api/v1/auth/password',
    method: 'put',
    response: () => ({
      code: 0,
      message: 'success',
      data: null,
    }),
  },
  {
    url: '/api/v1/auth/apikeys',
    method: 'get',
    response: ({ query }: { query: Record<string, string> }) => {
      const page = Number(query?.page) || 1
      const size = Number(query?.page_size) || 20
      const start = (page - 1) * size
      return {
        code: 0,
        message: 'success',
        data: {
          items: mockApiKeys.slice(start, start + size).map((k) => ({ ...k })),
          total: mockApiKeys.length,
          page,
          page_size: size,
        },
      }
    },
  },
  {
    url: '/api/v1/auth/apikeys',
    method: 'post',
    response: ({ body }: { body: Record<string, unknown> }) => {
      const name = String(body.name ?? '').trim()
      const expiresInDays = Number(body.expires_in_days) || 0
      const rawKey = generateRawKey()
      const now = formatDateTime(new Date())
      const expiresAt =
        expiresInDays > 0 ? formatDateTime(new Date(Date.now() + expiresInDays * DAY_MS)) : ''
      apiKeySeq += 1
      const newKey = {
        id: apiKeySeq,
        name: name || 'Untitled Key',
        prefix: rawKey.substring(0, 8),
        created_at: now,
        last_used_at: '',
        expires_at: expiresAt,
        status: 'active',
        raw_key: rawKey,
      }
      mockApiKeys.unshift({ ...newKey })
      return {
        code: 0,
        message: 'success',
        data: newKey,
      }
    },
  },
  {
    url: '/api/v1/auth/apikeys/:id',
    method: 'delete',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const key = mockApiKeys.find((k) => k.id === id)
      if (key) {
        key.status = 'revoked'
      }
      return {
        code: 0,
        message: 'success',
        data: null,
      }
    },
  },
] as MockMethod[]
