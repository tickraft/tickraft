// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { request, PageData } from '@tickraft/core'

/** Channel type enum (every type has a backend adapter; "sms" is an
 * extension type injected via the registry SPI, not a kernel built-in) */
export type ChannelType =
  | 'webhook'
  | 'email'
  | 'dingtalk'
  | 'feishu'
  | 'wecom'
  | 'slack'
  | 'discord'
  | 'telegram'
  | 'teams'
  | 'sms'

/** Delivery status enum. The backend never persists a retrying state:
 * retries are synchronous and the record reflects the latest attempt. */
export type DeliveryStatus = 'success' | 'failed'

/** Channel configuration (API response, sensitive fields masked) */
export interface ChannelConfig {
  id: number
  tenantId: number
  name: string
  type: ChannelType
  /** JSON-encoded config object, sensitive fields masked as ****xxxx */
  config: string
  enabled: boolean
  /** Last test time (ISO, empty means never tested) */
  lastTestAt: string
  /** Last test result */
  lastTestResult: 'success' | 'failed' | 'none'
  createdAt: string
  updatedAt: string
}

/** A single delivery attempt: the original send is entry 0 and each
 * manual retry appends the next entry. */
export interface DeliveryAttempt {
  time: string
  n: number
  result: DeliveryStatus
  error?: string
  durationMs: number
}

/** Delivery record (sys_prism_delivery) */
export interface DeliveryRecord {
  id: number
  tenantId: number
  /** Owning channel config row; 0 for environment-built channels */
  channelId: number
  channelName: string
  channelType: ChannelType
  alertType: string
  alertTitle: string
  /** Stable alert event correlation ID */
  eventId: string
  status: DeliveryStatus
  error: string
  responseCode: number
  durationMs: number
  /** Serialized alert event; backs the detail drawer and retry replay */
  requestPayload?: string
  attempts: DeliveryAttempt[]
  sentAt: string
}

/** Test result (includes receipt detail, used for test receipt display) */
export interface TestResult {
  status: 'success' | 'failed'
  error?: string
  responseCode?: number
  latency?: number
  requestId?: string
}

/** Create channel request */
interface CreateChannelRequest {
  name: string
  type: ChannelType
  config: Record<string, unknown>
  enabled?: boolean
}

/** Update channel request */
interface UpdateChannelRequest {
  name?: string
  type?: ChannelType
  config?: Record<string, unknown>
  enabled?: boolean
}

/** Test channel request: either reference a saved channel by id or
 * supply an inline type + config pair. */
export interface TestChannelRequest {
  id?: number
  type?: ChannelType
  config?: Record<string, unknown>
}

/** Delivery record query params */
export interface DeliveryQueryParams {
  page?: number
  size?: number
  channelId?: number
  status?: string
  alertTitle?: string
  startTime?: string
  endTime?: string
}

/**
 * Get channel list
 */
export function getChannels(): Promise<{ items: ChannelConfig[] }> {
  return request<{ items: ChannelConfig[] }>({
    url: '/prism/channels',
    method: 'get',
  })
}

/**
 * Get a single channel detail
 */
export function getChannel(id: number): Promise<ChannelConfig> {
  return request<ChannelConfig>({
    url: `/prism/channels/${id}`,
    method: 'get',
  })
}

/**
 * Create channel
 */
export function createChannel(data: CreateChannelRequest): Promise<ChannelConfig> {
  return request<ChannelConfig>({
    url: '/prism/channels',
    method: 'post',
    data,
  })
}

/**
 * Update channel
 */
export function updateChannel(id: number, data: UpdateChannelRequest): Promise<ChannelConfig> {
  return request<ChannelConfig>({
    url: `/prism/channels/${id}`,
    method: 'put',
    data,
  })
}

/**
 * Delete channel
 */
export function deleteChannel(id: number): Promise<void> {
  return request<void>({
    url: `/prism/channels/${id}`,
    method: 'delete',
  })
}

/**
 * Test channel configuration
 *
 * The backend route is POST /prism/channels/test with the channel id or
 * an inline type + config pair in the request body.
 */
export function testChannel(data: TestChannelRequest): Promise<TestResult> {
  return request<TestResult>({
    url: '/prism/channels/test',
    method: 'post',
    data,
  })
}

/**
 * Get channel delivery records
 *
 * Pass id=0 to query delivery records across all channels.
 */
export function getDeliveries(
  id: number,
  params: DeliveryQueryParams,
): Promise<PageData<DeliveryRecord>> {
  return request<PageData<DeliveryRecord>>({
    url: id > 0 ? `/prism/channels/${id}/deliveries` : '/prism/channels/deliveries',
    method: 'get',
    params,
  })
}

/**
 * Toggle channel enabled status (PUT update, no separate toggle endpoint)
 */
export function toggleChannel(id: number, enabled: boolean): Promise<ChannelConfig> {
  return request<ChannelConfig>({
    url: `/prism/channels/${id}`,
    method: 'put',
    data: { enabled },
  })
}

/**
 * Retry delivery (only failed status can be retried)
 */
export function retryDelivery(deliveryId: number): Promise<DeliveryRecord> {
  return request<DeliveryRecord>({
    url: `/prism/channels/deliveries/${deliveryId}/retry`,
    method: 'post',
  })
}

/** Channel compact option (for alert rule multi-select) */
export interface ChannelOption {
  id: number
  name: string
  type: ChannelType
  enabled: boolean
}

/**
 * Get channel options (compact, for alert config multi-select)
 */
export function getChannelOptions(): Promise<{ items: ChannelOption[] }> {
  return request<{ items: ChannelOption[] }>({
    url: '/prism/channels/options',
    method: 'get',
  })
}
