// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { request } from '@tickraft/core'
import type {
  ApplyTemplateParams,
  ListenerTypeInfo,
  MonitorCreateParams,
  MonitorHistoryEntry,
  MonitorLog,
  MonitorMode,
  MonitorPoint,
  MonitorStatus,
  MonitorSummary,
  MonitorUpdateParams,
  ProberTypeInfo,
  TelemetryTemplate,
} from '../types/telemetry'
import type { PageData, PageParams } from '@tickraft/core'

// ---------------------------------------------------------------------------
// Unified Monitor Point API — aligns with backend /api/v1/telemetry/monitors
// ---------------------------------------------------------------------------

/**
 * Get monitor point list with optional mode filter.
 *
 * Backend ListTelemetry only supports the `mode` query parameter for filtering
 * ("active", "passive", or omitted for all). Keyword and enabled filtering are
 * not supported by the backend.
 *
 * @param params Pagination + optional mode filter
 */
export function getMonitors(
  params: PageParams & { mode?: MonitorMode },
): Promise<PageData<MonitorPoint>> {
  return request<PageData<MonitorPoint>>({
    url: '/telemetry/monitors',
    method: 'get',
    params,
  })
}

/**
 * Get monitor point summary counts (active/passive/enabled/disabled).
 *
 * The counts span the whole dataset, not the current page, so the list
 * page's summary chips stay correct under pagination.
 */
export function getMonitorSummary(): Promise<MonitorSummary> {
  return request<MonitorSummary>({
    url: '/telemetry/monitors/summary',
    method: 'get',
  })
}

/**
 * Get a single monitor point by ID.
 */
export function getMonitor(id: number): Promise<MonitorPoint> {
  return request<MonitorPoint>({
    url: `/telemetry/monitors/${id}`,
    method: 'get',
  })
}

/**
 * Create a new monitor point.
 */
export function createMonitor(params: MonitorCreateParams): Promise<MonitorPoint> {
  return request<MonitorPoint>({
    url: '/telemetry/monitors',
    method: 'post',
    data: params,
  })
}

/**
 * Update an existing monitor point.
 *
 * The backend replaces all fields, so the full MonitorPoint body must be provided.
 */
export function updateMonitor(id: number, params: MonitorUpdateParams): Promise<MonitorPoint> {
  return request<MonitorPoint>({
    url: `/telemetry/monitors/${id}`,
    method: 'put',
    data: params,
  })
}

/**
 * Delete a monitor point.
 */
export function deleteMonitor(id: number): Promise<void> {
  return request<void>({
    url: `/telemetry/monitors/${id}`,
    method: 'delete',
  })
}

/**
 * Enable a monitor point.
 */
export function enableMonitor(id: number): Promise<MonitorPoint> {
  return request<MonitorPoint>({
    url: `/telemetry/monitors/${id}/enable`,
    method: 'put',
  })
}

/**
 * Disable a monitor point.
 */
export function disableMonitor(id: number): Promise<MonitorPoint> {
  return request<MonitorPoint>({
    url: `/telemetry/monitors/${id}/disable`,
    method: 'put',
  })
}

/**
 * Get monitor point status.
 */
export function getMonitorStatus(id: number): Promise<MonitorStatus> {
  return request<MonitorStatus>({
    url: `/telemetry/monitors/${id}/status`,
    method: 'get',
  })
}

/**
 * Trigger an immediate probe of a monitor point.
 */
export function probeMonitor(id: number): Promise<MonitorStatus> {
  return request<MonitorStatus>({
    url: `/telemetry/monitors/${id}/probe`,
    method: 'post',
  })
}

/**
 * Get monitor point history (paginated).
 *
 * Replaces the former getProbeRecords function. Returns historical data points
 * for the monitoring point.
 */
export function getMonitorHistory(
  id: number,
  params: PageParams,
): Promise<PageData<MonitorHistoryEntry>> {
  return request<PageData<MonitorHistoryEntry>>({
    url: `/telemetry/monitors/${id}/history`,
    method: 'get',
    params,
  })
}

/**
 * Get monitor point logs (paginated).
 */
export function getMonitorLogs(
  id: number,
  params: PageParams,
): Promise<PageData<MonitorLog>> {
  return request<PageData<MonitorLog>>({
    url: `/telemetry/monitors/${id}/logs`,
    method: 'get',
    params,
  })
}

// ---------------------------------------------------------------------------
// Prober / Listener Type Metadata — aligns with backend
// GET /api/v1/telemetry/probers and /api/v1/telemetry/listeners
// ---------------------------------------------------------------------------

/**
 * Get supported prober types (active monitoring point types).
 *
 * Returns the list of prober types supported by the current runtime,
 * derived from the executor registry (ICMP, TCP, HTTP for CE; extensions
 * may add more via Plugin SPI).
 */
export function getProbers(): Promise<ProberTypeInfo[]> {
  return request<ProberTypeInfo[]>({
    url: '/telemetry/probers',
    method: 'get',
  })
}

/**
 * Get supported listener types (passive monitoring point types).
 *
 * Returns the list of listener types supported by the current runtime
 * (Webhook for CE; extension may add Syslog/SNMP/MQTT via Plugin SPI).
 */
export function getListeners(): Promise<ListenerTypeInfo[]> {
  return request<ListenerTypeInfo[]>({
    url: '/telemetry/listeners',
    method: 'get',
  })
}

// ---------------------------------------------------------------------------
// Telemetry Template API — aligns with backend /api/v1/telemetry/templates
// ---------------------------------------------------------------------------

/**
 * Get telemetry templates (pre-configured probe/monitor recipes).
 *
 * Backend ListTemplates returns a plain array (not paginated) via api.Success.
 * An optional category query parameter filters by template category.
 */
export function getTelemetryTemplates(
  params?: { category?: string },
): Promise<TelemetryTemplate[]> {
  return request<TelemetryTemplate[]>({
    url: '/telemetry/templates',
    method: 'get',
    params,
  })
}

/**
 * Delete a custom (non-builtin) telemetry template.
 */
export function deleteTelemetryTemplate(id: number): Promise<void> {
  return request<void>({
    url: `/telemetry/templates/${id}`,
    method: 'delete',
  })
}

/**
 * Apply a template to create a new monitor point.
 *
 * Loads the template, merges any overrides from the request body, and creates
 * a new telemetry monitoring point via the TelemetryService. When name is
 * empty, defaults to "<template name> (applied)".
 */
export function applyTemplate(
  id: number,
  params?: ApplyTemplateParams,
): Promise<MonitorPoint> {
  return request<MonitorPoint>({
    url: `/telemetry/templates/${id}/apply`,
    method: 'post',
    data: params ?? {},
  })
}
