// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { MockMethod } from './types'
import { mockAssets } from './asset'

/** Base time (ISO format), aligned with storyboard mock-data.js */
const baseTime = '2026-06-30T14:00:00+08:00'

/** Format ISO time (simplified, aligned with storyboard static time strings) */
function ts(hoursAgo: number): string {
  const d = new Date(baseTime)
  d.setHours(d.getHours() - hoursAgo)
  const pad = (n: number): string => (n < 10 ? `0${n}` : `${n}`)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

/** Telemetry config dataset (11 items, covering 3 probe types, aligned with storyboard mock-data.js) */
const mockTelemetryConfigs = [
  { id: 1, tenant_id: 1, asset_id: 1, asset_type: 'host', asset_name: 'prod-web-01', collect_type: 'icmp', collect_config: '{"host":"10.0.1.11","count":4}', timeout: 3, probe_interval: 30, enable: true, last_probe: ts(0), created_at: '2026-06-01 10:30:00', updated_at: '2026-06-15 09:30:00' },
  { id: 2, tenant_id: 1, asset_id: 2, asset_type: 'host', asset_name: 'prod-web-02', collect_type: 'icmp', collect_config: '{"host":"10.0.1.12","count":4}', timeout: 3, probe_interval: 30, enable: true, last_probe: ts(0), created_at: '2026-06-01 10:30:00', updated_at: '2026-06-15 09:30:00' },
  { id: 3, tenant_id: 1, asset_id: 3, asset_type: 'host', asset_name: 'prod-api-03', collect_type: 'icmp', collect_config: '{"host":"10.0.1.13","count":4}', timeout: 3, probe_interval: 30, enable: true, last_probe: ts(0), created_at: '2026-06-01 10:30:00', updated_at: '2026-06-28 11:00:00' },
  { id: 4, tenant_id: 1, asset_id: 5, asset_type: 'host', asset_name: 'prod-db-02', collect_type: 'tcp', collect_config: '{"host":"10.0.2.21","port":3306}', timeout: 5, probe_interval: 60, enable: true, last_probe: ts(0), created_at: '2026-06-02 11:30:00', updated_at: '2026-06-12 14:00:00' },
  { id: 5, tenant_id: 1, asset_id: 6, asset_type: 'host', asset_name: 'prod-db-03', collect_type: 'tcp', collect_config: '{"host":"10.0.2.22","port":5432}', timeout: 5, probe_interval: 60, enable: true, last_probe: ts(0), created_at: '2026-06-04 10:30:00', updated_at: '2026-06-21 14:30:00' },
  { id: 6, tenant_id: 1, asset_id: 7, asset_type: 'service', asset_name: 'prod-cache-01', collect_type: 'tcp', collect_config: '{"host":"10.0.3.31","port":6379}', timeout: 3, probe_interval: 30, enable: true, last_probe: ts(0), created_at: '2026-06-05 14:30:00', updated_at: '2026-06-22 11:45:00' },
  { id: 7, tenant_id: 1, asset_id: 8, asset_type: 'service', asset_name: 'prod-kafka-01', collect_type: 'tcp', collect_config: '{"host":"10.0.4.41","port":9092}', timeout: 5, probe_interval: 90, enable: true, last_probe: ts(0), created_at: '2026-06-08 10:30:00', updated_at: '2026-06-25 11:00:00' },
  { id: 8, tenant_id: 1, asset_id: 10, asset_type: 'website', asset_name: 'cdn-edge-01', collect_type: 'http', collect_config: '{"url":"https://cdn.tickraft.io/health","expect_code":200}', timeout: 10, probe_interval: 60, enable: true, last_probe: ts(0), created_at: '2026-06-03 09:30:00', updated_at: '2026-06-20 16:30:00' },
  { id: 9, tenant_id: 1, asset_id: 11, asset_type: 'website', asset_name: 'www.tickraft.io', collect_type: 'http', collect_config: '{"url":"https://www.tickraft.io","expect_code":200}', timeout: 10, probe_interval: 60, enable: true, last_probe: ts(0), created_at: '2026-06-01 10:30:00', updated_at: '2026-06-28 16:00:00' },
  { id: 10, tenant_id: 1, asset_id: 12, asset_type: 'device', asset_name: 'Intranet Gateway', collect_type: 'icmp', collect_config: '{"host":"10.0.0.1","count":4}', timeout: 3, probe_interval: 60, enable: true, last_probe: ts(0), created_at: '2026-06-06 11:30:00', updated_at: '2026-06-23 09:00:00' },
  { id: 11, tenant_id: 1, asset_id: 9, asset_type: 'service', asset_name: 'prod-es-01', collect_type: 'http', collect_config: '{"url":"http://10.0.5.51:9200/_cluster/health","expect_code":200}', timeout: 10, probe_interval: 300, enable: false, last_probe: ts(29), created_at: '2026-06-10 09:30:00', updated_at: '2026-06-29 10:00:00' },
]

/** Probe records (12 items, aligned with storyboard mock-data.js) */
const mockProbeRecords = [
  { id: 1, config_id: 1, asset_id: 1, prober_type: 'icmp', asset_name: 'prod-web-01', status: 'success', latency: 1.2, detail: '4 packets received, avg 1.2ms', created_at: ts(0) },
  { id: 2, config_id: 2, asset_id: 2, prober_type: 'icmp', asset_name: 'prod-web-02', status: 'success', latency: 1.5, detail: '4 packets received, avg 1.5ms', created_at: ts(0) },
  { id: 3, config_id: 3, asset_id: 3, prober_type: 'icmp', asset_name: 'prod-api-03', status: 'error', latency: 0, detail: 'destination host unreachable', created_at: ts(0) },
  { id: 4, config_id: 4, asset_id: 5, prober_type: 'tcp', asset_name: 'prod-db-02', status: 'success', latency: 0.8, detail: 'tcp connect success', created_at: ts(0) },
  { id: 5, config_id: 5, asset_id: 6, prober_type: 'tcp', asset_name: 'prod-db-03', status: 'success', latency: 1.0, detail: 'tcp connect success', created_at: ts(0) },
  { id: 6, config_id: 6, asset_id: 7, prober_type: 'tcp', asset_name: 'prod-cache-01', status: 'error', latency: 0, detail: 'connection refused', created_at: ts(0) },
  { id: 7, config_id: 7, asset_id: 8, prober_type: 'tcp', asset_name: 'prod-kafka-01', status: 'success', latency: 1.1, detail: 'tcp connect success', created_at: ts(0) },
  { id: 8, config_id: 8, asset_id: 10, prober_type: 'http', asset_name: 'cdn-edge-01', status: 'success', latency: 25.3, detail: 'HTTP 200, response time: 25.3ms', created_at: ts(0) },
  { id: 9, config_id: 9, asset_id: 11, prober_type: 'http', asset_name: 'www.tickraft.io', status: 'success', latency: 18.5, detail: 'HTTP 200, response time: 18.5ms', created_at: ts(0) },
  { id: 10, config_id: 10, asset_id: 12, prober_type: 'icmp', asset_name: 'Intranet Gateway', status: 'success', latency: 0.6, detail: '4 packets received, avg 0.6ms', created_at: ts(0) },
  { id: 11, config_id: 11, asset_id: 9, prober_type: 'http', asset_name: 'prod-es-01', status: 'error', latency: 0, detail: 'connection refused', created_at: ts(29) },
  { id: 12, config_id: 1, asset_id: 1, prober_type: 'icmp', asset_name: 'prod-web-01', status: 'success', latency: 1.4, detail: '4 packets received, avg 1.4ms', created_at: ts(0) },
]

/** Webhook listener config */
const mockWebhookConfig = {
  id: 1,
  path: 'http://localhost:9090/telemetry/report',
  secret: 'wh_********************abc123',
  enable: true,
  auth_type: 'hmac' as const,
  created_at: '2026-06-01 10:00:00',
}

/**
 * Unified monitor point dataset (13 items, covering active/passive modes and
 * multiple types, aligned with backend TelemetryTask model).
 */
const mockMonitors = [
  { id: 1, name: 'prod-web-01 ICMP Ping', description: 'ICMP connectivity probe for prod-web-01', asset_type: 'host', mode: 'active', type: 'icmp', schedule: '30s', enabled: true, config: { host: '10.0.1.11', count: 4 }, created_at: '2026-06-01T10:30:00+08:00', updated_at: '2026-06-15T09:30:00+08:00' },
  { id: 2, name: 'prod-web-02 ICMP Ping', description: 'ICMP connectivity probe for prod-web-02', asset_type: 'host', mode: 'active', type: 'icmp', schedule: '30s', enabled: true, config: { host: '10.0.1.12', count: 4 }, created_at: '2026-06-01T10:30:00+08:00', updated_at: '2026-06-15T09:30:00+08:00' },
  { id: 3, name: 'prod-api-03 ICMP Ping', description: 'ICMP connectivity probe for prod-api-03', asset_type: 'host', mode: 'active', type: 'icmp', schedule: '30s', enabled: true, config: { host: '10.0.1.13', count: 4 }, created_at: '2026-06-01T10:30:00+08:00', updated_at: '2026-06-28T11:00:00+08:00' },
  { id: 4, name: 'prod-db-02 MySQL Port', description: 'TCP port probe for MySQL', asset_type: 'host', mode: 'active', type: 'tcp', schedule: '60s', enabled: true, config: { host: '10.0.2.21', port: 3306 }, created_at: '2026-06-02T11:30:00+08:00', updated_at: '2026-06-12T14:00:00+08:00' },
  { id: 5, name: 'prod-db-03 PostgreSQL Port', description: 'TCP port probe for PostgreSQL', asset_type: 'host', mode: 'active', type: 'tcp', schedule: '60s', enabled: true, config: { host: '10.0.2.22', port: 5432 }, created_at: '2026-06-04T10:30:00+08:00', updated_at: '2026-06-21T14:30:00+08:00' },
  { id: 6, name: 'prod-cache-01 Redis Port', description: 'TCP port probe for Redis', asset_type: 'service', mode: 'active', type: 'tcp', schedule: '30s', enabled: true, config: { host: '10.0.3.31', port: 6379 }, created_at: '2026-06-05T14:30:00+08:00', updated_at: '2026-06-22T11:45:00+08:00' },
  { id: 7, name: 'prod-kafka-01 Kafka Port', description: 'TCP port probe for Kafka', asset_type: 'service', mode: 'active', type: 'tcp', schedule: '90s', enabled: true, config: { host: '10.0.4.41', port: 9092 }, created_at: '2026-06-08T10:30:00+08:00', updated_at: '2026-06-25T11:00:00+08:00' },
  { id: 8, name: 'cdn-edge-01 Health Check', description: 'HTTP health check for CDN edge', asset_type: 'website', mode: 'active', type: 'http', schedule: '60s', enabled: true, config: { method: 'GET', url: 'https://cdn.tickraft.io/health', expect_code: 200 }, created_at: '2026-06-03T09:30:00+08:00', updated_at: '2026-06-20T16:30:00+08:00' },
  { id: 9, name: 'www.tickraft.io Health Check', description: 'HTTP health check for main site', asset_type: 'website', mode: 'active', type: 'http', schedule: '60s', enabled: true, config: { method: 'GET', url: 'https://www.tickraft.io', expect_code: 200 }, created_at: '2026-06-01T10:30:00+08:00', updated_at: '2026-06-28T16:00:00+08:00' },
  { id: 10, name: 'Intranet Gateway ICMP Ping', description: 'ICMP probe for gateway', asset_type: 'device', mode: 'active', type: 'icmp', schedule: '60s', enabled: true, config: { host: '10.0.0.1', count: 4 }, created_at: '2026-06-06T11:30:00+08:00', updated_at: '2026-06-23T09:00:00+08:00' },
  { id: 11, name: 'prod-es-01 ES Health', description: 'HTTP health check for Elasticsearch cluster', asset_type: 'service', mode: 'active', type: 'http', schedule: '300s', enabled: false, config: { method: 'GET', url: 'http://10.0.5.51:9200/_cluster/health', expect_code: 200 }, created_at: '2026-06-10T09:30:00+08:00', updated_at: '2026-06-29T10:00:00+08:00' },
  { id: 12, name: 'External Webhook Receiver', description: 'Webhook listener for external data ingestion', asset_type: 'service', mode: 'passive', type: 'webhook', schedule: 'on-demand', enabled: true, config: { auth_type: 'hmac', secret: 'wh_********************abc123' }, created_at: '2026-06-01T10:00:00+08:00', updated_at: '2026-06-15T09:30:00+08:00' },
  { id: 13, name: 'CI/CD Pipeline Webhook', description: 'Webhook for CI/CD pipeline status reporting', asset_type: 'task', mode: 'passive', type: 'webhook', schedule: 'on-demand', enabled: false, config: { auth_type: 'asset-key' }, created_at: '2026-06-10T14:00:00+08:00', updated_at: '2026-06-27T15:00:00+08:00' },
]

/**
 * Telemetry templates (pre-configured probe/monitor recipes).
 * The delete route splices items out of the array during a dev session
 * (splice mutates in place, so the binding can stay const).
 */
const mockTemplates = [
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

/** Next ID for mock monitor creation */
let monitorNextId = mockMonitors.length + 1

/** Extract numeric ID from URL */
function extractId(url: string): number {
  const match = url.match(/\/(\d+)(?:\/|$)/)
  return match ? Number(match[1]) : 0
}

export default [
  // Telemetry configs
  {
    url: '/api/v1/telemetry',
    method: 'get',
    response: ({ query }: { query: { page?: string; size?: string; asset_id?: string; collect_type?: string; asset_name?: string; enable?: string } }) => {
      const page = Number(query?.page) || 1
      const size = Number(query?.page_size) || 20
      let filtered = [...mockTelemetryConfigs]
      if (query?.asset_id) {
        filtered = filtered.filter((c) => c.asset_id === Number(query.asset_id))
      }
      if (query?.collect_type) {
        filtered = filtered.filter((c) => c.collect_type === query.collect_type)
      }
      if (query?.asset_name) {
        const kw = query.asset_name.toLowerCase()
        filtered = filtered.filter((c) => (c.asset_name ?? '').toLowerCase().includes(kw))
      }
      if (query?.enable) {
        filtered = filtered.filter((c) => String(c.enable) === query.enable)
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
  {
    url: '/api/v1/telemetry',
    method: 'post',
    response: ({ body }: { body: Record<string, unknown> }) => ({
      code: 0,
      message: 'success',
      data: {
        id: mockTelemetryConfigs.length + 1,
        tenant_id: 1,
        asset_id: body.asset_id ?? 1,
        asset_type: 'host',
        asset_name: mockAssets.find((r) => r.id === Number(body.asset_id ?? 1))?.name ?? '',
        collect_type: body.collect_type ?? 'icmp',
        collect_config: body.collect_config ?? '{}',
        timeout: body.timeout ?? 10,
        probe_interval: body.probe_interval ?? 60,
        enable: body.enable ?? true,
        last_probe: '',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    }),
  },
  // NOTE: /api/v1/telemetry/:id routes are intentionally placed AFTER all
  // specific sub-path routes (monitors, probe-records, templates, listeners)
  // at the end of this file. The mock server matches routes in array order,
  // so the :id parameter would otherwise shadow specific paths like
  // /telemetry/monitors (matching "monitors" as an :id value).
  // Probe records
  {
    url: '/api/v1/telemetry/probe-records',
    method: 'get',
    response: ({ query }: { query: { page?: string; size?: string; config_id?: string; prober_id?: string } }) => {
      const page = Number(query?.page) || 1
      const size = Number(query?.page_size) || 20
      let records = [...mockProbeRecords]
      const filterId = query?.config_id ?? query?.prober_id
      if (filterId) {
        records = records.filter((r) => r.config_id === Number(filterId))
      }
      return {
        code: 0,
        message: 'success',
        data: {
          items: records,
          total: records.length,
          page,
          page_size: size,
        },
      }
    },
  },
  // Webhook config
  {
    url: '/api/v1/telemetry/listeners/webhook',
    method: 'get',
    response: () => ({
      code: 0,
      message: 'success',
      data: mockWebhookConfig,
    }),
  },
  {
    url: '/api/v1/telemetry/listeners/webhook',
    method: 'put',
    response: ({ body }: { body: Record<string, unknown> }) => ({
      code: 0,
      message: 'success',
      data: { ...mockWebhookConfig, ...body, updated_at: new Date().toISOString() },
    }),
  },
  // ---------------------------------------------------------------------------
  // Unified Monitor Point API — /api/v1/telemetry/monitors
  // ---------------------------------------------------------------------------
  // Monitor list (with optional mode filter)
  {
    url: '/api/v1/telemetry/monitors',
    method: 'get',
    response: ({ query }: { query: { page?: string; size?: string; mode?: string; keyword?: string; enabled?: string } }) => {
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
      const monitor = {
        id,
        name: (body.name as string) ?? '',
        description: (body.description as string) ?? '',
        asset_type: (body.asset_type as string) ?? 'host',
        mode: (body.mode as string) ?? 'active',
        type: (body.type as string) ?? 'icmp',
        schedule: (body.schedule as string) ?? '60s',
        enabled: (body.enabled as boolean) ?? true,
        config: (body.config as Record<string, unknown>) ?? {},
        created_at: now,
        updated_at: now,
      }
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
          status: monitor?.enabled ? 'active' : 'inactive',
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
          status: monitor?.enabled ? 'active' : 'inactive',
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
      const points = Array.from({ length: 24 }, (_, i) => ({
        timestamp: ts(i),
        value: Math.round((12 + Math.sin(i / 3) * 6) * 10) / 10,
        status: i === 7 ? 'abnormal' : 'normal',
      }))
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
      const monitor = mockMonitors.find((m) => m.id === id)
      const page = Number(query?.page) || 1
      const size = Number(query?.page_size) || 20
      const logs = Array.from({ length: 18 }, (_, i) => ({
        timestamp: ts(i),
        level: i === 5 ? 'warning' : 'info',
        message: `${monitor?.name ?? 'monitor'} probe completed`,
      }))
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
  // Prober types (active monitoring point types supported by this runtime)
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
  // Must be placed before /telemetry/:id to avoid :id shadowing.
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
      const monitor = {
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
  // ---------------------------------------------------------------------------
  // Telemetry config by ID — MUST be placed AFTER all specific sub-path routes
  // (monitors, probe-records, templates, listeners) to avoid the :id parameter
  // shadowing those paths. The mock server matches routes in array order.
  // ---------------------------------------------------------------------------
  {
    url: '/api/v1/telemetry/:id',
    method: 'get',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const config = mockTelemetryConfigs.find((c) => c.id === id)
      return {
        code: 0,
        message: 'success',
        data: config || mockTelemetryConfigs[0],
      }
    },
  },
  {
    url: '/api/v1/telemetry/:id',
    method: 'put',
    response: ({ url, body }: { url: string; body: Record<string, unknown> }) => {
      const id = extractId(url)
      const config = mockTelemetryConfigs.find((c) => c.id === id) || mockTelemetryConfigs[0]
      return {
        code: 0,
        message: 'success',
        data: { ...config, ...body, updated_at: new Date().toISOString() },
      }
    },
  },
  {
    url: '/api/v1/telemetry/:id',
    method: 'delete',
    response: () => ({
      code: 0,
      message: 'success',
      data: null,
    }),
  },
] as MockMethod[]
