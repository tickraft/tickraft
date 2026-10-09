// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { request } from '@tickraft/core'

/** Component status vocabulary (backend pkg/status) */
export type StatusLevel = 'operational' | 'degraded' | 'partial_outage' | 'major_outage' | 'unknown'

/** Status page component mapping (aligned with backend status.Component) */
export interface StatusComponent {
  name: string
  description?: string
  /** Monitor point IDs aggregated into this component */
  pointIds?: number[]
}

/** Status page configuration (aligned with backend status.Config) */
export interface StatusConfig {
  title: string
  description?: string
  enabled: boolean
  components?: StatusComponent[]
}

/** Rendered component in the public view (aligned with backend status.ComponentStatus) */
export interface StatusComponentView {
  name: string
  description?: string
  status: StatusLevel
  monitorCount: number
  lastChecked?: string
}

/** Public status view (aligned with backend status.PublicView) */
export interface StatusPublicView {
  title: string
  description?: string
  overall: StatusLevel
  components: StatusComponentView[]
  updatedAt: string
}

/**
 * Get the public status view. The endpoint is unauthenticated; it answers
 * 404 while the page is disabled.
 */
export function getPublicStatus(): Promise<StatusPublicView> {
  return request<StatusPublicView>({
    url: '/status',
    method: 'get',
  })
}

/**
 * Get the status page configuration (management endpoint).
 */
export function getStatusConfig(): Promise<StatusConfig> {
  return request<StatusConfig>({
    url: '/status/config',
    method: 'get',
  })
}

/**
 * Update the status page configuration (management endpoint).
 */
export function updateStatusConfig(params: StatusConfig): Promise<StatusConfig> {
  return request<StatusConfig>({
    url: '/status/config',
    method: 'put',
    data: params,
  })
}
