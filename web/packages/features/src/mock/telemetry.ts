// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { MockMethod } from './types'

/** Base time (ISO format), aligned with storyboard mock-data.js */
const baseTime = '2026-06-30T14:00:00+08:00'

/** Format ISO time (simplified, aligned with storyboard static time strings) */
function ts(hoursAgo: number): string {
  const d = new Date(baseTime)
  d.setHours(d.getHours() - hoursAgo)
  const pad = (n: number): string => (n < 10 ? `0${n}` : `${n}`)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

/** Monitor point seed model (snake_case wire format; the request layer camelizes) */
interface MonitorSeed {
  id: number
  name: string
  description: string
  asset_type: string
  mode: 'active' | 'passive'
  type: string
  schedule: string
  enabled: boolean
  config: Record<string, unknown>
  created_at: string
  updated_at: string
}

/**
 * Unified monitor point dataset (13 items: 8 active probes across icmp/tcp/http
 * and 5 passive webhook listeners, with varied schedules and some disabled),
 * aligned with backend TelemetryTask model.
 */
const mockMonitors: MonitorSeed[] = [
  // Active probing (8)
  { id: 1, name: 'prod-web-01 ICMP Ping', description: 'ICMP connectivity probe for prod-web-01', asset_type: 'host', mode: 'active', type: 'icmp', schedule: '30s', enabled: true, config: { host: '10.0.1.11', count: 4 }, created_at: '2026-06-01T10:30:00+08:00', updated_at: '2026-06-15T09:30:00+08:00' },
  { id: 2, name: 'prod-api-03 ICMP Ping', description: 'ICMP connectivity probe for prod-api-03', asset_type: 'host', mode: 'active', type: 'icmp', schedule: '30s', enabled: true, config: { host: '10.0.1.13', count: 4 }, created_at: '2026-06-01T10:30:00+08:00', updated_at: '2026-06-28T11:00:00+08:00' },
  { id: 3, name: 'prod-db-02 MySQL Port', description: 'TCP port probe for MySQL', asset_type: 'host', mode: 'active', type: 'tcp', schedule: '60s', enabled: true, config: { host: '10.0.2.21', port: 3306 }, created_at: '2026-06-02T11:30:00+08:00', updated_at: '2026-06-12T14:00:00+08:00' },
  { id: 4, name: 'prod-cache-01 Redis Port', description: 'TCP port probe for Redis', asset_type: 'service', mode: 'active', type: 'tcp', schedule: '30s', enabled: true, config: { host: '10.0.3.31', port: 6379 }, created_at: '2026-06-05T14:30:00+08:00', updated_at: '2026-06-22T11:45:00+08:00' },
  { id: 5, name: 'prod-kafka-01 Kafka Port', description: 'TCP port probe for Kafka', asset_type: 'service', mode: 'active', type: 'tcp', schedule: '90s', enabled: true, config: { host: '10.0.4.41', port: 9092 }, created_at: '2026-06-08T10:30:00+08:00', updated_at: '2026-06-25T11:00:00+08:00' },
  { id: 6, name: 'cdn-edge-01 Health Check', description: 'HTTP health check for CDN edge', asset_type: 'website', mode: 'active', type: 'http', schedule: '60s', enabled: true, config: { method: 'GET', url: 'https://cdn.tickraft.io/health', expect_code: 200 }, created_at: '2026-06-03T09:30:00+08:00', updated_at: '2026-06-20T16:30:00+08:00' },
  { id: 7, name: 'www.tickraft.io Health Check', description: 'HTTP health check for main site', asset_type: 'website', mode: 'active', type: 'http', schedule: '60s', enabled: true, config: { method: 'GET', url: 'https://www.tickraft.io', expect_code: 200 }, created_at: '2026-06-01T10:30:00+08:00', updated_at: '2026-06-28T16:00:00+08:00' },
  { id: 8, name: 'prod-es-01 ES Health', description: 'HTTP health check for Elasticsearch cluster', asset_type: 'service', mode: 'active', type: 'http', schedule: '300s', enabled: false, config: { method: 'GET', url: 'http://10.0.5.51:9200/_cluster/health', expect_code: 200 }, created_at: '2026-06-10T09:30:00+08:00', updated_at: '2026-06-29T10:00:00+08:00' },
  // Passive receiving (5)
  { id: 9, name: 'External Webhook Receiver', description: 'Webhook listener for external data ingestion', asset_type: 'service', mode: 'passive', type: 'webhook', schedule: 'on-demand', enabled: true, config: { auth_type: 'hmac', secret: 'wh_********************abc123' }, created_at: '2026-06-01T10:00:00+08:00', updated_at: '2026-06-15T09:30:00+08:00' },
  { id: 10, name: 'CI/CD Pipeline Webhook', description: 'Webhook for CI/CD pipeline status reporting', asset_type: 'task', mode: 'passive', type: 'webhook', schedule: 'on-demand', enabled: false, config: { auth_type: 'asset-key' }, created_at: '2026-06-10T14:00:00+08:00', updated_at: '2026-06-27T15:00:00+08:00' },
  { id: 11, name: 'Grafana Alert Webhook', description: 'Webhook receiver for Grafana alert notifications', asset_type: 'service', mode: 'passive', type: 'webhook', schedule: 'on-demand', enabled: true, config: { auth_type: 'hmac', secret: 'wh_********************grafana' }, created_at: '2026-06-12T09:00:00+08:00', updated_at: '2026-06-26T10:20:00+08:00' },
  { id: 12, name: 'Alertmanager Webhook', description: 'Webhook receiver for Prometheus Alertmanager alerts', asset_type: 'service', mode: 'passive', type: 'webhook', schedule: 'on-demand', enabled: true, config: { auth_type: 'asset-key' }, created_at: '2026-06-14T11:30:00+08:00', updated_at: '2026-06-28T13:45:00+08:00' },
  { id: 13, name: 'Backup Job Reporter', description: 'Webhook for backup job result reporting', asset_type: 'task', mode: 'passive', type: 'webhook', schedule: 'on-demand', enabled: true, config: { auth_type: 'hmac' }, created_at: '2026-06-16T15:20:00+08:00', updated_at: '2026-06-29T08:10:00+08:00' },
]

/**
 * Telemetry templates (pre-configured probe/monitor recipes).
 * The delete route splices items out of the array during a dev session
 * (splice mutates in place, so the binding can stay const).
 */
interface TemplateSeed {
  id: number
  name: string
  description: string
  category: string
  executor_type: string
  config: Record<string, unknown>
  is_builtin: boolean
  created_at: string
  updated_at: string
}

const mockTemplates: TemplateSeed[] = [
  { id: 1, name: 'ICMP Ping', description: 'Basic ICMP connectivity probe', category: 'network', executor_type: 'icmp', config: { count: 4, timeout: 3 }, is_builtin: true, created_at: '2026-06-01 10:00:00', updated_at: '2026-06-01 10:00:00' },
  { id: 2, name: 'TCP Port Check', description: 'TCP port connectivity probe', category: 'network', executor_type: 'tcp', config: { timeout: 5 }, is_builtin: true, created_at: '2026-06-01 10:00:00', updated_at: '2026-06-01 10:00:00' },
  { id: 3, name: 'HTTP Health Check', description: 'HTTP endpoint health check with status code validation', category: 'web', executor_type: 'http', config: { method: 'GET', expect_code: 200, timeout: 10 }, is_builtin: true, created_at: '2026-06-01 10:00:00', updated_at: '2026-06-01 10:00:00' },
  { id: 4, name: 'HTTPS Health Check', description: 'HTTPS endpoint health check with TLS verification', category: 'web', executor_type: 'http', config: { method: 'GET', expect_code: 200, timeout: 10, verify_tls: true }, is_builtin: true, created_at: '2026-06-01 10:00:00', updated_at: '2026-06-01 10:00:00' },
  { id: 5, name: 'MySQL Port Probe', description: 'Probe MySQL service port availability', category: 'database', executor_type: 'tcp', config: { port: 3306, timeout: 5 }, is_builtin: true, created_at: '2026-06-01 10:00:00', updated_at: '2026-06-01 10:00:00' },
  { id: 6, name: 'PostgreSQL Port Probe', description: 'Probe PostgreSQL service port availability', category: 'database', executor_type: 'tcp', config: { port: 5432, timeout: 5 }, is_builtin: true, created_at: '2026-06-01 10:00:00', updated_at: '2026-06-01 10:00:00' },
  { id: 7, name: 'Redis Port Probe', description: 'Probe Redis service port availability', category: 'database', executor_type: 'tcp', config: { port: 6379, timeout: 3 }, is_builtin: true, created_at: '2026-06-01 10:00:00', updated_at: '2026-06-01 10:00:00' },
  { id: 8, name: 'Webhook Receiver', description: 'Passive webhook listener for external data ingestion', category: 'listener', executor_type: 'webhook', config: { auth_type: 'hmac' }, is_builtin: true, created_at: '2026-06-01 10:00:00', updated_at: '2026-06-01 10:00:00' },
  { id: 9, name: 'Kafka Port Probe', description: 'Probe Kafka broker port availability', category: 'middleware', executor_type: 'tcp', config: { port: 9092, timeout: 5 }, is_builtin: true, created_at: '2026-06-01 10:00:00', updated_at: '2026-06-01 10:00:00' },
  { id: 10, name: 'Elasticsearch Health', description: 'HTTP health check for Elasticsearch cluster endpoint', category: 'middleware', executor_type: 'http', config: { method: 'GET', expect_code: 200, timeout: 10 }, is_builtin: true, created_at: '2026-06-01 10:00:00', updated_at: '2026-06-01 10:00:00' },
]

/** Monitors seeded with rich history/logs so detail pages look full for screenshots */
const richDetailMonitorIds = new Set([6, 9])

/** Monitors currently reporting failures (error status, failed history entries) */
const unhealthyMonitorIds = new Set([2])

/** Next ID for mock monitor creation */
let monitorNextId = mockMonitors.length + 1

/** Extract numeric ID from URL */
function extractId(url: string): number {
  const match = url.match(/\/(\d+)(?:\/|$)/)
  return match ? Number(match[1]) : 0
}

/** Derive the monitor status string the detail page expects (active/inactive/error) */
function monitorStatus(monitor: MonitorSeed | undefined): string {
  if (!monitor || !monitor.enabled) return 'inactive'
  return unhealthyMonitorIds.has(monitor.id) ? 'error' : 'active'
}

/** Deterministic pseudo-latency in ms for a given sample index */
function sampleLatency(index: number, base: number): number {
  return Math.round((base + Math.sin(index / 3) * base * 0.4) * 10) / 10
}

/** Build a measured value for one history sample, shaped by the monitor type */
function historyValue(type: string, index: number): unknown {
  switch (type) {
    case 'icmp':
      return sampleLatency(index, 1.2)
    case 'tcp':
      return sampleLatency(index, 0.9)
    case 'http':
      return sampleLatency(index, 24.5)
    default:
      // Passive listeners report the number of received events per window
      return 20 + ((index * 7) % 23)
  }
}

/** Build a log message for one sample, shaped by the monitor type and outcome */
function logMessage(type: string, isError: boolean, index: number): string {
  if (isError) {
    switch (type) {
      case 'icmp':
        return 'destination host unreachable'
      case 'tcp':
        return 'connection refused'
      case 'http':
        return 'HTTP request failed: dial timeout'
      default:
        return 'webhook event rejected: invalid signature'
    }
  }
  switch (type) {
    case 'icmp':
      return `4 packets transmitted, 4 received, avg ${sampleLatency(index, 1.2)}ms`
    case 'tcp':
      return `tcp connect success in ${sampleLatency(index, 0.9)}ms`
    case 'http':
      return `HTTP 200 in ${sampleLatency(index, 24.5)}ms`
    default:
      return `webhook event received (source: ${['grafana', 'ci-pipeline', 'backup-job'][index % 3]})`
  }
}

/**
 * Build history entries for a monitor. Rich monitors (active HTTP CDN edge +
 * passive webhook receiver) get 26 samples every 3 hours (~3 recent days) so
 * the detail page looks full; other monitors get 10 hourly samples.
 * Status values align with what the detail page renders: 'success' (green)
 * and 'error' (red).
 */
function buildHistory(monitor: MonitorSeed): Array<{ timestamp: string; value: unknown; status: string }> {
  const rich = richDetailMonitorIds.has(monitor.id)
  const failing = unhealthyMonitorIds.has(monitor.id)
  const count = rich ? 26 : 10
  const stepHours = rich ? 3 : 1
  return Array.from({ length: count }, (_, i) => {
    const isError = failing ? i % 4 === 1 : i === 7
    return {
      timestamp: ts(i * stepHours),
      value: isError ? 0 : historyValue(monitor.type, i),
      status: isError ? 'error' : 'success',
    }
  })
}

/**
 * Build log entries for a monitor. Rich monitors get 18 entries every 4 hours
 * (~3 recent days); other monitors get 8 entries.
 */
function buildLogs(monitor: MonitorSeed): Array<{ timestamp: string; level: string; message: string }> {
  const rich = richDetailMonitorIds.has(monitor.id)
  const failing = unhealthyMonitorIds.has(monitor.id)
  const count = rich ? 18 : 8
  const stepHours = rich ? 4 : 2
  return Array.from({ length: count }, (_, i) => {
    const isError = failing ? i % 4 === 1 : false
    const level = isError ? 'error' : i === 5 ? 'warning' : 'info'
    return {
      timestamp: ts(i * stepHours),
      level,
      message: logMessage(monitor.type, isError, i),
    }
  })
}

export default [
  // ---------------------------------------------------------------------------
  // Unified Monitor Point API — /api/v1/telemetry/monitors
  // NOTE: the /monitors/:id wildcard routes are intentionally placed AFTER the
  // specific collection route. The mock server matches routes in array order.
  // ---------------------------------------------------------------------------
  // Monitor list (with optional mode filter)
  {
    url: '/api/v1/telemetry/monitors',
    method: 'get',
    response: ({ query }: { query: { page?: string; page_size?: string; mode?: string; keyword?: string; enabled?: string } }) => {
      const page = Number(query?.page) || 1
      const size = Number(query?.page_size) || 20
      let filtered = [...mockMonitors]
      if (query?.mode) {
        filtered = filtered.filter((m) => m.mode === query.mode)
      }
      if (query?.keyword) {
        const kw = query.keyword.toLowerCase()
        filtered = filtered.filter((m) => m.name.toLowerCase().includes(kw))
      }
      if (query?.enabled) {
        filtered = filtered.filter((m) => String(m.enabled) === query.enabled)
      }
      return {
        code: 0,
        message: 'success',
        data: {
          items: filtered,
          total: filtered.length,
          page,
          page_size: size,
        },
      }
    },
  },
  // Create monitor
  {
    url: '/api/v1/telemetry/monitors',
    method: 'post',
    response: ({ body }: { body: Record<string, unknown> }) => {
      const id = monitorNextId++
      const now = new Date().toISOString()
      const monitor: MonitorSeed = {
        id,
        name: (body.name as string) ?? '',
        description: (body.description as string) ?? '',
        asset_type: (body.asset_type as string) ?? 'host',
        mode: (body.mode as 'active' | 'passive') ?? 'active',
        type: (body.type as string) ?? 'icmp',
        schedule: (body.schedule as string) ?? '60s',
        enabled: (body.enabled as boolean) ?? true,
        config: (body.config as Record<string, unknown>) ?? {},
        created_at: now,
        updated_at: now,
      }
      mockMonitors.push(monitor)
      return {
        code: 0,
        message: 'success',
        data: monitor,
      }
    },
  },
  // Get monitor by ID
  {
    url: '/api/v1/telemetry/monitors/:id',
    method: 'get',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const monitor = mockMonitors.find((m) => m.id === id)
      return {
        code: 0,
        message: 'success',
        data: monitor || mockMonitors[0],
      }
    },
  },
  // Update monitor
  {
    url: '/api/v1/telemetry/monitors/:id',
    method: 'put',
    response: ({ url, body }: { url: string; body: Record<string, unknown> }) => {
      const id = extractId(url)
      const monitor = mockMonitors.find((m) => m.id === id) || mockMonitors[0]
      return {
        code: 0,
        message: 'success',
        data: { ...monitor, ...body, id, updated_at: new Date().toISOString() },
      }
    },
  },
  // Delete monitor
  {
    url: '/api/v1/telemetry/monitors/:id',
    method: 'delete',
    response: () => ({
      code: 0,
      message: 'success',
      data: null,
    }),
  },
  // Enable monitor
  {
    url: '/api/v1/telemetry/monitors/:id/enable',
    method: 'put',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const monitor = mockMonitors.find((m) => m.id === id) || mockMonitors[0]
      return {
        code: 0,
        message: 'success',
        data: { ...monitor, enabled: true, updated_at: new Date().toISOString() },
      }
    },
  },
  // Disable monitor
  {
    url: '/api/v1/telemetry/monitors/:id/disable',
    method: 'put',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const monitor = mockMonitors.find((m) => m.id === id) || mockMonitors[0]
      return {
        code: 0,
        message: 'success',
        data: { ...monitor, enabled: false, updated_at: new Date().toISOString() },
      }
    },
  },
  // Get monitor status
  {
    url: '/api/v1/telemetry/monitors/:id/status',
    method: 'get',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const monitor = mockMonitors.find((m) => m.id === id)
      return {
        code: 0,
        message: 'success',
        data: {
          id,
          name: monitor?.name ?? '',
          enabled: monitor?.enabled ?? false,
          status: monitorStatus(monitor),
        },
      }
    },
  },
  // Probe monitor (trigger immediate probe)
  {
    url: '/api/v1/telemetry/monitors/:id/probe',
    method: 'post',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const monitor = mockMonitors.find((m) => m.id === id)
      return {
        code: 0,
        message: 'success',
        data: {
          id,
          name: monitor?.name ?? '',
          enabled: monitor?.enabled ?? false,
          status: monitorStatus(monitor),
        },
      }
    },
  },
  // Monitor point history (paginated)
  {
    url: '/api/v1/telemetry/monitors/:id/history',
    method: 'get',
    response: ({ url, query }: { url: string; query: Record<string, string> }) => {
      const id = extractId(url)
      const page = Number(query?.page) || 1
      const size = Number(query?.page_size) || 20
      const monitor = mockMonitors.find((m) => m.id === id) ?? mockMonitors[0]
      const points = buildHistory(monitor)
      const start = (page - 1) * size
      return {
        code: 0,
        message: 'success',
        data: {
          items: points.slice(start, start + size),
          total: points.length,
          page,
          page_size: size,
          monitor_id: id,
        },
      }
    },
  },
  // Monitor point logs (paginated)
  {
    url: '/api/v1/telemetry/monitors/:id/logs',
    method: 'get',
    response: ({ url, query }: { url: string; query: Record<string, string> }) => {
      const id = extractId(url)
      const monitor = mockMonitors.find((m) => m.id === id) ?? mockMonitors[0]
      const page = Number(query?.page) || 1
      const size = Number(query?.page_size) || 20
      const logs = buildLogs(monitor)
      const start = (page - 1) * size
      return {
        code: 0,
        message: 'success',
        data: {
          items: logs.slice(start, start + size),
          total: logs.length,
          page,
          page_size: size,
        },
      }
    },
  },
  // Prober types (active monitoring point types supported by this runtime).
  // CE supports icmp/tcp/http/udp; extensions may add DNS/SSL via Plugin SPI.
  {
    url: '/api/v1/telemetry/probers',
    method: 'get',
    response: () => ({
      code: 0,
      message: 'success',
      data: [
        { type: 'icmp', name: 'ICMP Ping', description: 'Measure connectivity and latency via ICMP echo' },
        { type: 'tcp', name: 'TCP Port', description: 'Check TCP port connectivity' },
        { type: 'http', name: 'HTTP', description: 'HTTP endpoint probe with status code validation' },
        { type: 'udp', name: 'UDP', description: 'UDP port probe' },
      ],
    }),
  },
  // Listener types (passive monitoring point types supported by this runtime)
  {
    url: '/api/v1/telemetry/listeners',
    method: 'get',
    response: () => ({
      code: 0,
      message: 'success',
      data: [
        { type: 'webhook', name: 'Webhook', description: 'Receive telemetry pushed from external systems via webhook' },
      ],
    }),
  },
  // ---------------------------------------------------------------------------
  // Telemetry templates (pre-configured probe/monitor recipes)
  // Must be placed before /telemetry/templates/:id to avoid :id shadowing.
  // ---------------------------------------------------------------------------
  {
    url: '/api/v1/telemetry/templates',
    method: 'get',
    response: ({ query }: { query: { category?: string } }) => {
      let filtered = [...mockTemplates]
      if (query?.category) {
        filtered = filtered.filter((t) => t.category === query.category)
      }
      // Backend ListTemplates returns a plain array (not paginated)
      return { code: 0, message: 'success', data: filtered }
    },
  },
  // Built-in templates only (must be before templates/:id so "builtin" is not matched as an ID)
  {
    url: '/api/v1/telemetry/templates/builtin',
    method: 'get',
    response: () => ({
      code: 0,
      message: 'success',
      data: mockTemplates.filter((t) => t.is_builtin),
    }),
  },
  // Apply a template — create a new monitor point from the recipe
  // (must be before templates/:id so "apply" is not matched as an ID)
  {
    url: '/api/v1/telemetry/templates/:id/apply',
    method: 'post',
    response: ({ url, body }: { url: string; body: Record<string, unknown> }) => {
      const id = extractId(url)
      const tpl = mockTemplates.find((t) => t.id === id) || mockTemplates[0]
      const now = new Date().toISOString()
      const monitor: MonitorSeed = {
        id: monitorNextId++,
        name: (body.name as string) || `${tpl.name} (applied)`,
        description: tpl.description,
        asset_type: 'host',
        mode: tpl.executor_type === 'webhook' ? 'passive' : 'active',
        type: tpl.executor_type,
        schedule: '60s',
        enabled: true,
        config: { ...tpl.config, ...(body.config as Record<string, unknown> | undefined) },
        created_at: now,
        updated_at: now,
      }
      mockMonitors.push(monitor)
      return { code: 0, message: 'success', data: monitor }
    },
  },
  // Create custom template (builtin templates cannot be created via this endpoint)
  {
    url: '/api/v1/telemetry/templates',
    method: 'post',
    response: ({ body }: { body: Record<string, unknown> }) => {
      const now = new Date().toISOString()
      const tpl = {
        id: Math.max(...mockTemplates.map((t) => t.id)) + 1,
        name: String(body.name ?? ''),
        description: String(body.description ?? ''),
        category: String(body.category ?? 'custom'),
        executor_type: String(body.executor_type ?? 'http'),
        config: (body.config as Record<string, unknown>) ?? {},
        is_builtin: false,
        created_at: now,
        updated_at: now,
      }
      mockTemplates.push(tpl)
      return { code: 0, message: 'success', data: tpl }
    },
  },
  // Get single template
  {
    url: '/api/v1/telemetry/templates/:id',
    method: 'get',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const tpl = mockTemplates.find((t) => t.id === id)
      return { code: 0, message: 'success', data: tpl || mockTemplates[0] }
    },
  },
  // Update custom template (builtin templates are read-only)
  {
    url: '/api/v1/telemetry/templates/:id',
    method: 'put',
    response: ({ url, body }: { url: string; body: Record<string, unknown> }) => {
      const id = extractId(url)
      const tpl = mockTemplates.find((t) => t.id === id)
      if (!tpl || tpl.is_builtin) {
        return { code: 40300, message: 'builtin template is read-only', data: null }
      }
      Object.assign(tpl, {
        name: String(body.name ?? tpl.name),
        description: String(body.description ?? tpl.description),
        category: String(body.category ?? tpl.category),
        executor_type: String(body.executor_type ?? tpl.executor_type),
        config: (body.config as Record<string, unknown>) ?? tpl.config,
        updated_at: new Date().toISOString(),
      })
      return { code: 0, message: 'success', data: tpl }
    },
  },
  {
    url: '/api/v1/telemetry/templates/:id',
    method: 'delete',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const idx = mockTemplates.findIndex((t) => t.id === id)
      if (idx >= 0 && !mockTemplates[idx].is_builtin) mockTemplates.splice(idx, 1)
      return { code: 0, message: 'success', data: null }
    },
  },
] as MockMethod[]
