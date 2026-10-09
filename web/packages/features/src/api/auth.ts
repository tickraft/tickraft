// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { request } from '@tickraft/core'
import type { LoginParams, LoginData, PageData, PageParams } from '@tickraft/core'

/** API Key status: 1 = active, 0 = revoked/disabled (aligned with backend apikey.StatusActive/StatusRevoked) */
export const API_KEY_STATUS_ACTIVE = 1
export const API_KEY_STATUS_REVOKED = 0

/** API Key model (aligned with backend user.APIKey) */
export interface ApiKey {
  id: number
  name: string
  /** Key prefix (first chars only, e.g. tk_abc1) */
  keyPrefix: string
  /** Status: 1 = active, 0 = revoked/disabled */
  status: number
  /** IP whitelist (optional, returned when set) */
  ipWhitelist?: string
  /** Permission level (optional, returned when set) */
  permissionLevel?: string
  /** Creation time (RFC3339) */
  createdAt: string
  /** Expiry time (RFC3339), undefined means never expires */
  expiredAt?: string
  /** Revocation time (RFC3339), present when the key has been revoked */
  revokedAt?: string
}

/** Create API Key parameters (aligned with backend createAPIKeyRequest) */
export interface ApiKeyCreateParams {
  name: string
  /** Expiry time as RFC3339 timestamp; omit for never-expiring key */
  expiredAt?: string
}

/** Create API Key result (includes one-time raw key, aligned with backend apiKeyData) */
export interface ApiKeyCreateResult extends ApiKey {
  /** Full key, only returned once on creation */
  rawKey: string
}

/** Check whether an API key is currently active (status=1 and not revoked). */
export function isApiKeyActive(key: ApiKey): boolean {
  return key.status === API_KEY_STATUS_ACTIVE && !key.revokedAt
}

/**
 * User login
 */
export function login(params: LoginParams): Promise<LoginData> {
  return request<LoginData>({
    url: '/auth/login',
    method: 'post',
    data: params,
  })
}

/**
 * User logout
 */
export function logout(): Promise<void> {
  return request<void>({
    url: '/auth/logout',
    method: 'post',
  })
}

/**
 * Change password
 */
export function changePassword(params: { oldPassword: string; newPassword: string }): Promise<void> {
  return request<void>({
    url: '/auth/password',
    method: 'put',
    data: params,
  })
}

/**
 * Get API Key list (paginated, aligned with backend ListAPIKeys → PageData).
 * keyword is a server-side substring filter on the key name.
 */
export function getApiKeys(params: PageParams & { keyword?: string }): Promise<PageData<ApiKey>> {
  return request<PageData<ApiKey>>({
    url: '/auth/apikeys',
    method: 'get',
    params,
  })
}

/**
 * Create API Key
 */
export function createApiKey(params: ApiKeyCreateParams): Promise<ApiKeyCreateResult> {
  return request<ApiKeyCreateResult>({
    url: '/auth/apikeys',
    method: 'post',
    data: params,
  })
}

/**
 * Revoke API Key
 */
export function revokeApiKey(id: number): Promise<void> {
  return request<void>({
    url: `/auth/apikeys/${id}`,
    method: 'delete',
  })
}
