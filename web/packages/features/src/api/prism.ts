// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { request } from '@tickraft/core'
import type { PageData, PageParams, AlertStatus } from '@tickraft/core'

/** Alert severity level (aligned with backend alert.Severity) */
export type AlertSeverity = 'critical' | 'warning' | 'info'

/** Alert trigger condition operator (aligned with backend alert.Condition) */
export type AlertCondition = 'gt' | 'lt' | 'eq' | 'gte' | 'lte'

/** Alert record (aligned with backend handler.AlertRecord) */
export interface AlertRecord {
  id: number
  ruleId: number
  ruleName: string
  /** info, warning, critical */
  severity?: string
  /** Value at trigger time */
  value: number
  /** firing, acknowledged, resolved */
  status: AlertStatus
  message: string
  firedAt: string
  acknowledgedAt?: string | null
  resolvedAt?: string | null
}

/** Alert rule (aligned with backend handler.AlertRule) */
export interface AlertRule {
  id: number
  name: string
  description?: string
  /** Scene: task, probe, metric, remediation */
  scene: string
  /** expr-lang source text */
  expression: string
  /** Higher priority fires first */
  priority?: number
  enabled: boolean
  createdAt: string
  updatedAt: string
}

/** Alert rule create/edit payload */
export type AlertRulePayload = Omit<AlertRule, 'id' | 'createdAt' | 'updatedAt'>

/**
 * Fetch alert record list (backend only supports page/size pagination)
 */
export function getAlertRecords(
  params: PageParams,
): Promise<PageData<AlertRecord>> {
  return request<PageData<AlertRecord>>({
    url: '/prism/alert/records',
    method: 'get',
    params,
  })
}

/**
 * Fetch alert record detail
 */
export function getAlertRecord(id: number): Promise<AlertRecord> {
  return request<AlertRecord>({
    url: `/prism/alert/records/${id}`,
    method: 'get',
  })
}

/**
 * Mark alert record as resolved
 */
export function resolveAlertRecord(id: number): Promise<AlertRecord> {
  return request<AlertRecord>({
    url: `/prism/alert/records/${id}/resolve`,
    method: 'put',
  })
}

/**
 * Acknowledge an alert record (transitions status to "acknowledged")
 */
export function acknowledgeAlertRecord(id: number): Promise<AlertRecord> {
  return request<AlertRecord>({
    url: `/prism/alert/records/${id}/acknowledge`,
    method: 'put',
  })
}

/**
 * Fetch alert rule list
 */
export function getAlertRules(params: PageParams): Promise<PageData<AlertRule>> {
  return request<PageData<AlertRule>>({
    url: '/prism/alert/rules',
    method: 'get',
    params,
  })
}

/**
 * Fetch alert rule detail
 */
export function getAlertRule(id: number): Promise<AlertRule> {
  return request<AlertRule>({
    url: `/prism/alert/rules/${id}`,
    method: 'get',
  })
}

/**
 * Create alert rule
 */
export function createAlertRule(params: AlertRulePayload): Promise<AlertRule> {
  return request<AlertRule>({
    url: '/prism/alert/rules',
    method: 'post',
    data: params,
  })
}

/**
 * Update alert rule (PUT semantics — sends the full rule object)
 */
export function updateAlertRule(
  id: number,
  params: AlertRulePayload,
): Promise<AlertRule> {
  return request<AlertRule>({
    url: `/prism/alert/rules/${id}`,
    method: 'put',
    data: params,
  })
}

/**
 * Delete alert rule
 */
export function deleteAlertRule(id: number): Promise<void> {
  return request<void>({
    url: `/prism/alert/rules/${id}`,
    method: 'delete',
  })
}

// ── Notification channels ──

/** Notification channel type (CE supports "webhook"; extensible via SPI) */
export type ChannelType = 'webhook' | string

/** Webhook channel configuration (stored as JSON in NotificationChannel.config) */
export interface WebhookConfig {
  /** Target endpoint URL */
  url: string
  /** HTTP method (POST or PUT) */
  method: 'POST' | 'PUT'
  /** Request timeout as a duration string (e.g. "10s") */
  timeout: string
  /** Custom HTTP headers */
  headers: Record<string, string>
}

/** Notification channel (aligned with backend handler.NotificationChannel) */
export interface NotificationChannel {
  id: number
  name: string
  type: ChannelType
  /** JSON-encoded channel config payload (e.g. WebhookConfig) */
  config: string
  enabled: boolean
  /** Last successful delivery time (nullable until first use) */
  lastUsedAt: string | null
  createdAt: string
  updatedAt: string
}

/** Channel list query parameters (backend only supports page/size) */
export type ChannelListParams = PageParams

/** Channel create/update payload */
export interface ChannelPayload {
  name: string
  type: ChannelType
  config: string
  enabled: boolean
}

/**
 * Fetch notification channel list (paginated)
 */
export function getChannels(
  params: ChannelListParams,
): Promise<PageData<NotificationChannel>> {
  return request<PageData<NotificationChannel>>({
    url: '/prism/channels',
    method: 'get',
    params,
  })
}

/**
 * Fetch a single notification channel by ID
 */
export function getChannel(id: number): Promise<NotificationChannel> {
  return request<NotificationChannel>({
    url: `/prism/channels/${id}`,
    method: 'get',
  })
}

/**
 * Create a new notification channel
 */
export function createChannel(
  params: ChannelPayload,
): Promise<NotificationChannel> {
  return request<NotificationChannel>({
    url: '/prism/channels',
    method: 'post',
    data: params,
  })
}

/**
 * Update an existing notification channel
 */
export function updateChannel(
  id: number,
  params: ChannelPayload,
): Promise<NotificationChannel> {
  return request<NotificationChannel>({
    url: `/prism/channels/${id}`,
    method: 'put',
    data: params,
  })
}

/**
 * Delete a notification channel
 */
export function deleteChannel(id: number): Promise<void> {
  return request<void>({
    url: `/prism/channels/${id}`,
    method: 'delete',
  })
}

/**
 * Send a test notification through the given channel
 */
export function testChannel(id: number): Promise<void> {
  return request<void>({
    url: `/prism/channels/${id}/test`,
    method: 'post',
  })
}

// ── Remediation rules ──

/** Remediation rule executor type (CE supports "webhook" and "http"; extensible via SPI) */
export type RemediationExecutorType = 'webhook' | 'http' | string

/** Remediation rule trigger event type (CE supports alert.firing / alert.critical) */
export type RemediationTriggerType = 'alert.firing' | 'alert.critical' | string

/** Remediation rule (aligned with backend handler.RemediationRule) */
export interface RemediationRule {
  id: number
  name: string
  description: string
  assetId: number
  triggerEventType: string
  conditionExpr: string
  executorType: string
  /** JSON-encoded executor config payload (e.g. WebhookExecutorConfig) */
  executorConfig: string
  /** Cooldown duration in seconds */
  cooldown: number
  circuitBreakerThreshold: number
  enabled: boolean
  /** Rule status (e.g. "idle", "running", "circuit_open") */
  status: string
  /** Last execution time (nullable until first run) */
  lastRunAt: string | null
  createdAt: string
  updatedAt: string
}

/** Remediation rule list query parameters (backend only supports page/size) */
export type RemediationRuleListParams = PageParams

/** Remediation rule create/update payload */
export interface RemediationRulePayload {
  name: string
  description?: string
  assetId?: number
  triggerEventType: string
  conditionExpr?: string
  executorType: string
  executorConfig: string
  cooldown?: number
  circuitBreakerThreshold?: number
  enabled: boolean
}

/** Webhook/http executor configuration (stored as JSON in RemediationRule.executorConfig) */
export interface RemediationExecutorConfig {
  /** Target endpoint URL */
  url: string
  /** HTTP method (POST or PUT) */
  method: 'POST' | 'PUT' | 'GET'
  /** Request timeout as a duration string (e.g. "10s") */
  timeout: string
  /** Custom HTTP headers */
  headers: Record<string, string>
  /** Request body (only used by http executor type) */
  body?: string
}

/**
 * Fetch remediation rule list (paginated)
 */
export function getRemediationRules(
  params: RemediationRuleListParams,
): Promise<PageData<RemediationRule>> {
  return request<PageData<RemediationRule>>({
    url: '/prism/remediation/rules',
    method: 'get',
    params,
  })
}

/**
 * Fetch a single remediation rule by ID
 */
export function getRemediationRule(id: number): Promise<RemediationRule> {
  return request<RemediationRule>({
    url: `/prism/remediation/rules/${id}`,
    method: 'get',
  })
}

/**
 * Create a new remediation rule
 */
export function createRemediationRule(
  params: RemediationRulePayload,
): Promise<RemediationRule> {
  return request<RemediationRule>({
    url: '/prism/remediation/rules',
    method: 'post',
    data: params,
  })
}

/**
 * Update an existing remediation rule
 */
export function updateRemediationRule(
  id: number,
  params: RemediationRulePayload,
): Promise<RemediationRule> {
  return request<RemediationRule>({
    url: `/prism/remediation/rules/${id}`,
    method: 'put',
    data: params,
  })
}

/**
 * Delete a remediation rule
 */
export function deleteRemediationRule(id: number): Promise<void> {
  return request<void>({
    url: `/prism/remediation/rules/${id}`,
    method: 'delete',
  })
}
