// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { MockMethod } from './types'

/**
 * API Key wire record (snake_case, aligned with backend user.APIKey JSON tags).
 * The axios response interceptor camelizes keys so the frontend receives the
 * ApiKey shape (keyPrefix / expiredAt / revokedAt / ...).
 * Fields: id, name, key_prefix, status (1=active, 0=revoked),
 * ip_whitelist?, permission_level?, created_at, expired_at?, revoked_at?
 */
interface MockApiKey {
  id: number
  name: string
  key_prefix: string
  status: number
  ip_whitelist?: string
  permission_level?: string
  created_at: string
  expired_at?: string
  revoked_at?: string
}

/**
 * API Key list (6 items: 5 active + 1 revoked, aligned with prototype mock-data.js).
 * All timestamps are RFC3339; expired_at is omitted for never-expiring keys.
 */
const mockApiKeys: MockApiKey[] = [
  {
    id: 1,
    name: 'Default API Key',
    key_prefix: 'tk_abc1',
    status: 1,
    created_at: '2026-06-01T10:00:00+08:00',
    expired_at: '2027-06-30T00:00:00+08:00',
  },
  {
    id: 2,
    name: 'Telemetry Report Key',
    key_prefix: 'tk_def2',
    status: 1,
    ip_whitelist: '10.0.0.0/8,192.168.1.0/24',
    permission_level: 'read',
    created_at: '2026-06-02T10:00:00+08:00',
    expired_at: '2027-06-30T00:00:00+08:00',
  },
  {
    id: 3,
    name: 'Admin Key',
    key_prefix: 'tk_ghi3',
    status: 1,
    permission_level: 'admin',
    created_at: '2026-06-03T10:00:00+08:00',
  },
  {
    id: 4,
    name: 'Third-Party Integration',
    key_prefix: 'tk_jkl4',
    status: 0,
    ip_whitelist: '203.0.113.0/24',
    created_at: '2026-05-15T10:00:00+08:00',
    expired_at: '2026-06-15T00:00:00+08:00',
    revoked_at: '2026-06-15T16:30:00+08:00',
  },
  {
    id: 5,
    name: 'Temporary Debug Key',
    key_prefix: 'tk_mno5',
    status: 1,
    ip_whitelist: '127.0.0.1',
    created_at: '2026-06-15T10:00:00+08:00',
    expired_at: '2026-09-15T00:00:00+08:00',
  },
  {
    id: 6,
    name: 'CI/CD Pipeline',
    key_prefix: 'tk_pqr6',
    status: 1,
    created_at: '2026-06-20T10:00:00+08:00',
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

/** Current time as an RFC3339 timestamp */
function nowRfc3339(): string {
  return new Date().toISOString()
}

/** Token expiry for login / refresh responses (2 hours ahead, RFC3339) */
function tokenExpiresAt(): string {
  return new Date(Date.now() + 2 * 3_600_000).toISOString()
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
        expires_at: tokenExpiresAt(),
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
        expires_at: tokenExpiresAt(),
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
      // The request layer snakeizes the outgoing payload, so ApiKeyCreateParams.expiredAt
      // (RFC3339 string, optional = never expires) arrives here as expired_at.
      const expiredAt = typeof body.expired_at === 'string' && body.expired_at ? body.expired_at : undefined
      const rawKey = generateRawKey()
      apiKeySeq += 1
      const newKey: MockApiKey = {
        id: apiKeySeq,
        name: name || 'Untitled Key',
        key_prefix: rawKey.substring(0, 8),
        status: 1,
        created_at: nowRfc3339(),
        expired_at: expiredAt,
      }
      mockApiKeys.unshift({ ...newKey })
      return {
        code: 0,
        message: 'success',
        data: { ...newKey, raw_key: rawKey },
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
        // Soft revoke: flip the status flag and stamp the revocation time.
        key.status = 0
        key.revoked_at = nowRfc3339()
      }
      return {
        code: 0,
        message: 'success',
        data: null,
      }
    },
  },
] as MockMethod[]
