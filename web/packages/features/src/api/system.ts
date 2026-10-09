// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { request } from '@tickraft/core'

/** System configuration (aligned with backend SystemConfig) */
export interface SystemSettings {
  /** Site display name */
  siteName?: string
  /** Log level: debug/info/warn/error */
  logLevel: string
  /** Default language code (e.g. zh-Hans, en-US) */
  defaultLang: string
  /** Data retention days */
  retentionDays: number
  /** Network environment: internet (full rendering) | isolated (plain-text notifications) */
  networkEnvironment: string
}

/** Runtime info (aligned with backend SystemInfo) */
export interface RuntimeInfo {
  version: string
  buildTags: string
  startTime: string
  uptime: string
}

/** Global dashboard statistics (aligned with backend GlobalStats) */
export interface GlobalStats {
  totalTasks: number
  totalDevices: number
  todayExecutions: number
  todaySuccessRate: number
  /** Asset counts grouped by status (normal/abnormal/offline/unknown) */
  assetStatusCounts?: Record<string, number>
}

/** Quota usage row (aligned with backend quota.UsageItem; ceiling 0 = unlimited) */
export interface QuotaUsageItem {
  /** Count-based quota type: device/probe/task/remediation/contact */
  type: string
  used: number
  ceiling: number
  /** used/ceiling; 0 when unlimited */
  ratio: number
}

/** User profile (aligned with backend UserProfile) */
export interface UserProfile {
  id: number
  username: string
  nickname?: string
  email?: string
  /** Role: 0=viewer, 1=developer, 2=admin */
  role: number
  language: string
  alertFormatStyle: string
}

/** Update profile request (aligned with backend UpdateProfileRequest).
 *  Only the fields present in the request are updated. */
export interface UpdateProfileRequest {
  nickname?: string
  email?: string
  language?: string
  alertFormatStyle?: string
}

/**
 * Get system settings
 */
export function getSettings(): Promise<SystemSettings> {
  return request<SystemSettings>({
    url: '/system/config',
    method: 'get',
  })
}

/**
 * Update system settings
 */
export function updateSettings(params: Partial<SystemSettings>): Promise<SystemSettings> {
  return request<SystemSettings>({
    url: '/system/config',
    method: 'put',
    data: params,
  })
}

/**
 * Get system info (version, build tags, start time, uptime)
 */
export function getRuntimeInfo(): Promise<RuntimeInfo> {
  return request<RuntimeInfo>({
    url: '/system/info',
    method: 'get',
  })
}

/**
 * Get global dashboard statistics (total tasks, devices, today's executions and success rate)
 */
export function getGlobalStats(): Promise<GlobalStats> {
  return request<GlobalStats>({
    url: '/system/stats',
    method: 'get',
  })
}

/**
 * Get the count-based quota usage aggregate (type/used/ceiling/ratio) that
 * backs the dashboard near-limit hint. A failing counter is omitted by the
 * backend rather than failing the whole request.
 */
export function getQuotaUsage(): Promise<QuotaUsageItem[]> {
  return request<QuotaUsageItem[]>({
    url: '/quota/usage',
    method: 'get',
  })
}

/**
 * Get the current user's profile
 */
export function getProfile(): Promise<UserProfile> {
  return request<UserProfile>({
    url: '/system/profile',
    method: 'get',
  })
}

/**
 * Update the current user's profile (partial update)
 */
export function updateProfile(params: UpdateProfileRequest): Promise<UserProfile> {
  return request<UserProfile>({
    url: '/system/profile',
    method: 'put',
    data: params,
  })
}
