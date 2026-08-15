// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { MockMethod } from './types'

/** Alert rules (8 items, covering 3 severity levels and 5 conditions, aligned with storyboard mock-data.js) */
const mockAlertRules = [
  {
    id: 1,
    name: 'HTTP 5xx Error Rate',
    metric: 'http_5xx_rate',
    condition: 'gt',
    threshold: 5,
    duration: 60,
    severity: 'critical',
    channels: ['webhook', 'email'],
    enabled: true,
    description: 'Triggered when HTTP 5xx error rate sustained above threshold',
    created_at: '2026-06-01 10:00:00',
  },
  {
    id: 2,
    name: 'Host CPU Usage',
    metric: 'cpu_usage',
    condition: 'gt',
    threshold: 80,
    duration: 300,
    severity: 'warning',
    channels: ['webhook'],
    enabled: true,
    description: 'Triggered when host CPU usage sustained above threshold',
    created_at: '2026-06-02 10:00:00',
  },
  {
    id: 3,
    name: 'Host Memory Usage',
    metric: 'mem_usage',
    condition: 'gte',
    threshold: 90,
    duration: 180,
    severity: 'critical',
    channels: ['webhook', 'sms'],
    enabled: true,
    description: 'Triggered when host memory usage reaches threshold',
    created_at: '2026-06-03 10:00:00',
  },
  {
    id: 4,
    name: 'Disk Free Space',
    metric: 'disk_free',
    condition: 'lt',
    threshold: 10,
    duration: 600,
    severity: 'warning',
    channels: ['email'],
    enabled: true,
    description: 'Triggered when disk free space falls below threshold',
    created_at: '2026-06-04 10:00:00',
  },
  {
    id: 5,
    name: 'TCP Port Connectivity',
    metric: 'tcp_connect',
    condition: 'eq',
    threshold: 0,
    duration: 60,
    severity: 'critical',
    channels: ['webhook', 'sms'],
    enabled: true,
    description: 'Triggered when TCP port probe fails (returns 0)',
    created_at: '2026-06-05 10:00:00',
  },
  {
    id: 6,
    name: 'ICMP Packet Loss Rate',
    metric: 'icmp_loss',
    condition: 'gt',
    threshold: 10,
    duration: 120,
    severity: 'warning',
    channels: ['webhook'],
    enabled: true,
    description: 'Triggered when ICMP probe packet loss rate exceeds threshold',
    created_at: '2026-06-06 10:00:00',
  },
  {
    id: 7,
    name: 'Task Failure Count',
    metric: 'task_failed',
    condition: 'gte',
    threshold: 3,
    duration: 3600,
    severity: 'info',
    channels: ['email'],
    enabled: false,
    description: 'Triggered when task failure count reaches threshold (disabled)',
    created_at: '2026-06-07 10:00:00',
  },
  {
    id: 8,
    name: 'HTTPS Certificate Days Remaining',
    metric: 'cert_days',
    condition: 'lte',
    threshold: 14,
    duration: 86400,
    severity: 'info',
    channels: ['email'],
    enabled: true,
    description: 'Triggered when HTTPS certificate days remaining falls below threshold',
    created_at: '2026-06-08 10:00:00',
  },
]

/** Alert records (22 items, covering 3 severity levels and 2 statuses, aligned with storyboard mock-data.js) */
const mockAlertRecords = [
  { id: 1, rule_id: 1, rule_name: 'HTTP 5xx Error Rate', asset_id: 4, asset_name: 'Payment Callback API', metric: 'http_5xx_rate', condition: 'gt', threshold: 5, current_value: 8.2, severity: 'critical', status: 'firing', message: '5xx error rate reached 8.2%, exceeding threshold 5%', fired_at: '2026-06-30 13:32:00', resolved_at: '' },
  { id: 2, rule_id: 5, rule_name: 'TCP Port Connectivity', asset_id: 9, asset_name: 'prod-es-01', metric: 'tcp_connect', condition: 'eq', threshold: 0, current_value: 0, severity: 'critical', status: 'firing', message: 'ES 9200 port connection failed', fired_at: '2026-06-30 13:15:00', resolved_at: '' },
  { id: 3, rule_id: 3, rule_name: 'Host Memory Usage', asset_id: 3, asset_name: 'prod-api-03', metric: 'mem_usage', condition: 'gte', threshold: 90, current_value: 92.5, severity: 'critical', status: 'firing', message: 'Memory usage 92.5%, exceeding threshold 90%', fired_at: '2026-06-30 12:45:00', resolved_at: '' },
  { id: 4, rule_id: 2, rule_name: 'Host CPU Usage', asset_id: 3, asset_name: 'prod-api-03', metric: 'cpu_usage', condition: 'gt', threshold: 80, current_value: 85.3, severity: 'warning', status: 'firing', message: 'CPU usage 85.3%, exceeding threshold 80%', fired_at: '2026-06-30 11:20:00', resolved_at: '' },
  { id: 5, rule_id: 6, rule_name: 'ICMP Packet Loss Rate', asset_id: 10, asset_name: 'cdn-edge-01', metric: 'icmp_loss', condition: 'gt', threshold: 10, current_value: 15, severity: 'warning', status: 'firing', message: 'CDN node packet loss rate 15%, exceeding threshold 10%', fired_at: '2026-06-30 10:50:00', resolved_at: '' },
  { id: 6, rule_id: 5, rule_name: 'TCP Port Connectivity', asset_id: 7, asset_name: 'prod-cache-01', metric: 'tcp_connect', condition: 'eq', threshold: 0, current_value: 0, severity: 'critical', status: 'firing', message: 'Redis 6379 port connection failed', fired_at: '2026-06-30 10:30:00', resolved_at: '' },
  { id: 7, rule_id: 1, rule_name: 'HTTP 5xx Error Rate', asset_id: 11, asset_name: 'www.tickraft.io', metric: 'http_5xx_rate', condition: 'gt', threshold: 5, current_value: 6.1, severity: 'critical', status: 'resolved', message: '5xx error rate recovered', fired_at: '2026-06-30 09:45:00', resolved_at: '2026-06-30 10:00:00' },
  { id: 8, rule_id: 2, rule_name: 'Host CPU Usage', asset_id: 1, asset_name: 'prod-web-01', metric: 'cpu_usage', condition: 'gt', threshold: 80, current_value: 82, severity: 'warning', status: 'resolved', message: 'CPU usage recovered', fired_at: '2026-06-30 09:00:00', resolved_at: '2026-06-30 09:15:00' },
  { id: 9, rule_id: 4, rule_name: 'Disk Free Space', asset_id: 15, asset_name: 'Backup Storage', metric: 'disk_free', condition: 'lt', threshold: 10, current_value: 8, severity: 'warning', status: 'firing', message: 'Disk free 8%, below threshold 10%', fired_at: '2026-06-30 08:30:00', resolved_at: '' },
  { id: 10, rule_id: 3, rule_name: 'Host Memory Usage', asset_id: 2, asset_name: 'prod-web-02', metric: 'mem_usage', condition: 'gte', threshold: 90, current_value: 91, severity: 'critical', status: 'resolved', message: 'Memory usage recovered', fired_at: '2026-06-30 07:00:00', resolved_at: '2026-06-30 07:30:00' },
  { id: 11, rule_id: 6, rule_name: 'ICMP Packet Loss Rate', asset_id: 12, asset_name: 'Intranet Gateway', metric: 'icmp_loss', condition: 'gt', threshold: 10, current_value: 12, severity: 'warning', status: 'resolved', message: 'Packet loss rate recovered', fired_at: '2026-06-29 22:00:00', resolved_at: '2026-06-29 22:15:00' },
  { id: 12, rule_id: 8, rule_name: 'HTTPS Certificate Days Remaining', asset_id: 11, asset_name: 'www.tickraft.io', metric: 'cert_days', condition: 'lte', threshold: 14, current_value: 12, severity: 'info', status: 'firing', message: 'HTTPS certificate has 12 days remaining', fired_at: '2026-06-29 09:00:00', resolved_at: '' },
  { id: 13, rule_id: 5, rule_name: 'TCP Port Connectivity', asset_id: 9, asset_name: 'prod-es-01', metric: 'tcp_connect', condition: 'eq', threshold: 0, current_value: 0, severity: 'critical', status: 'firing', message: 'ES 9200 port connection failed', fired_at: '2026-06-29 09:00:00', resolved_at: '' },
  { id: 14, rule_id: 2, rule_name: 'Host CPU Usage', asset_id: 6, asset_name: 'prod-db-03', metric: 'cpu_usage', condition: 'gt', threshold: 80, current_value: 81.5, severity: 'warning', status: 'resolved', message: 'CPU usage recovered', fired_at: '2026-06-29 18:00:00', resolved_at: '2026-06-29 18:30:00' },
  { id: 15, rule_id: 7, rule_name: 'Task Failure Count', asset_id: 3, asset_name: 'prod-api-03', metric: 'task_failed', condition: 'gte', threshold: 3, current_value: 3, severity: 'info', status: 'resolved', message: 'Task failure count returned to normal', fired_at: '2026-06-29 15:00:00', resolved_at: '2026-06-29 16:00:00' },
  { id: 16, rule_id: 1, rule_name: 'HTTP 5xx Error Rate', asset_id: 4, asset_name: 'Payment Callback API', metric: 'http_5xx_rate', condition: 'gt', threshold: 5, current_value: 7.5, severity: 'critical', status: 'resolved', message: '5xx error rate recovered', fired_at: '2026-06-29 14:00:00', resolved_at: '2026-06-29 14:30:00' },
  { id: 17, rule_id: 3, rule_name: 'Host Memory Usage', asset_id: 5, asset_name: 'prod-db-02', metric: 'mem_usage', condition: 'gte', threshold: 90, current_value: 93, severity: 'critical', status: 'resolved', message: 'Memory usage recovered', fired_at: '2026-06-29 10:00:00', resolved_at: '2026-06-29 10:30:00' },
  { id: 18, rule_id: 6, rule_name: 'ICMP Packet Loss Rate', asset_id: 1, asset_name: 'prod-web-01', metric: 'icmp_loss', condition: 'gt', threshold: 10, current_value: 11, severity: 'warning', status: 'resolved', message: 'Packet loss rate recovered', fired_at: '2026-06-29 08:00:00', resolved_at: '2026-06-29 08:30:00' },
  { id: 19, rule_id: 4, rule_name: 'Disk Free Space', asset_id: 15, asset_name: 'Backup Storage', metric: 'disk_free', condition: 'lt', threshold: 10, current_value: 7, severity: 'warning', status: 'resolved', message: 'Disk free space recovered after cleanup', fired_at: '2026-06-28 22:00:00', resolved_at: '2026-06-28 23:00:00' },
  { id: 20, rule_id: 2, rule_name: 'Host CPU Usage', asset_id: 14, asset_name: 'Monitoring Host', metric: 'cpu_usage', condition: 'gt', threshold: 80, current_value: 84, severity: 'warning', status: 'resolved', message: 'CPU usage recovered', fired_at: '2026-06-28 16:00:00', resolved_at: '2026-06-28 16:30:00' },
  { id: 21, rule_id: 5, rule_name: 'TCP Port Connectivity', asset_id: 8, asset_name: 'prod-kafka-01', metric: 'tcp_connect', condition: 'eq', threshold: 0, current_value: 0, severity: 'critical', status: 'resolved', message: 'Kafka 9092 port recovered', fired_at: '2026-06-28 10:00:00', resolved_at: '2026-06-28 10:15:00' },
  { id: 22, rule_id: 8, rule_name: 'HTTPS Certificate Days Remaining', asset_id: 10, asset_name: 'cdn-edge-01', metric: 'cert_days', condition: 'lte', threshold: 14, current_value: 8, severity: 'info', status: 'firing', message: 'CDN certificate has 8 days remaining', fired_at: '2026-06-28 09:00:00', resolved_at: '' },
]

/** Mutable copy for demo of start/stop, delete, create, resolve and other write operations */
const records = mockAlertRecords.map((r) => ({ ...r }))
const rules = mockAlertRules.map((r) => ({ ...r }))

/** Current alert rule auto-increment ID */
let ruleSeq = mockAlertRules.length

/** Mock notification channels (CE supports webhook type) */
const mockChannels = [
  {
    id: 1,
    name: 'Default Webhook',
    type: 'webhook',
    config: JSON.stringify({
      url: 'https://hooks.example.com/alerts',
      method: 'POST',
      timeout: '10s',
      headers: { 'Content-Type': 'application/json' },
    }),
    enabled: true,
    last_used_at: '2026-07-01 14:30:00',
    created_at: '2026-06-01 10:00:00',
    updated_at: '2026-06-15 12:00:00',
  },
  {
    id: 2,
    name: 'PagerDuty Integration',
    type: 'webhook',
    config: JSON.stringify({
      url: 'https://events.pagerduty.com/v2/enqueue',
      method: 'POST',
      timeout: '15s',
      headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer pd-key-xxx' },
    }),
    enabled: true,
    last_used_at: '2026-07-02 09:15:00',
    created_at: '2026-06-05 14:00:00',
    updated_at: '2026-06-20 16:30:00',
  },
  {
    id: 3,
    name: 'Slack Notifications',
    type: 'webhook',
    config: JSON.stringify({
      url: 'https://hooks.slack.com/services/T000/B000/XXXX',
      method: 'POST',
      timeout: '5s',
      headers: {},
    }),
    enabled: false,
    last_used_at: null,
    created_at: '2026-06-10 11:00:00',
    updated_at: '2026-06-10 11:00:00',
  },
]

const channels = mockChannels.map((c) => ({ ...c }))

/** Current channel auto-increment ID */
let channelSeq = mockChannels.length

/** Mock remediation rules (CE supports webhook/http executor types) */
const mockRemediationRules = [
  {
    id: 1,
    name: 'Auto-Restart Web Service on 5xx Spike',
    description: 'When HTTP 5xx error rate fires, trigger a webhook to restart the web service',
    asset_id: 4,
    trigger_event_type: 'metric',
    condition_expr: 'severity == "critical" && metric == "http_5xx_rate"',
    executor_type: 'webhook',
    executor_config: JSON.stringify({
      url: 'https://hooks.example.com/restart-web',
      method: 'POST',
      timeout: '10s',
      headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer restart-token-xxx' },
    }),
    cooldown: 300,
    circuit_breaker_threshold: 3,
    enabled: true,
    status: 'idle',
    last_run_at: '2026-07-01 14:30:00',
    created_at: '2026-06-01 10:00:00',
    updated_at: '2026-06-15 12:00:00',
  },
  {
    id: 2,
    name: 'Scale Out on High CPU',
    description: 'When host CPU usage reaches critical threshold, call HTTP API to scale out',
    asset_id: 3,
    trigger_event_type: 'log',
    condition_expr: 'metric == "cpu_usage" && current_value > 85',
    executor_type: 'http',
    executor_config: JSON.stringify({
      url: 'https://api.scaling.example.com/v1/scale-out',
      method: 'POST',
      timeout: '15s',
      headers: { 'Content-Type': 'application/json', 'X-Tickraft-Auth': 'scale-key-xxx' },
      body: '{"replicas": 1, "reason": "high_cpu_remediation"}',
    }),
    cooldown: 600,
    circuit_breaker_threshold: 5,
    enabled: true,
    status: 'idle',
    last_run_at: '2026-07-02 09:15:00',
    created_at: '2026-06-05 14:00:00',
    updated_at: '2026-06-20 16:30:00',
  },
  {
    id: 3,
    name: 'Clear Cache on Memory Pressure',
    description: 'When memory usage fires, trigger a webhook to flush Redis cache',
    asset_id: 3,
    trigger_event_type: 'status_change',
    condition_expr: 'metric == "mem_usage"',
    executor_type: 'webhook',
    executor_config: JSON.stringify({
      url: 'https://hooks.example.com/flush-cache',
      method: 'POST',
      timeout: '5s',
      headers: { 'Content-Type': 'application/json' },
    }),
    cooldown: 180,
    circuit_breaker_threshold: 2,
    enabled: false,
    status: 'idle',
    last_run_at: null,
    created_at: '2026-06-10 11:00:00',
    updated_at: '2026-06-10 11:00:00',
  },
]

const remediationRules = mockRemediationRules.map((r) => ({ ...r }))

/** Current remediation rule auto-increment ID */
let remediationRuleSeq = mockRemediationRules.length

/** Extract trailing numeric ID from URL (compatible with multi-segment paths like `/records/:id/resolve`) */
function extractId(url: string): number {
  const match = url.match(/\/(\d+)(?:\/|$)/)
  return match ? Number(match[1]) : 0
}

/** Parse date or datetime string into timestamp (compatible with YYYY-MM-DD and YYYY-MM-DD HH:mm:ss) */
function parseTs(value: string): number {
  if (!value) return 0
  return new Date(value.length === 10 ? `${value}T00:00:00` : value.replace(' ', 'T')).getTime()
}

/** Filter alert records (severity/status/from/to, aligned with the API contract) */
function filterRecords(query: Record<string, string>) {
  const { severity, status, from, to } = query
  const fromTs = from ? parseTs(from) : 0
  const toTs = to ? parseTs(to) : 0
  return records.filter((r) => {
    if (severity && r.severity !== severity) return false
    if (status && r.status !== status) return false
    const firedTs = parseTs(r.fired_at)
    if (fromTs && firedTs < fromTs) return false
    if (toTs && firedTs > toTs) return false
    return true
  })
}

/** Mock remediation execution records (aligned with backend remediation Record DTO) */
const remediationRecords = [
  { id: 1, rule_id: 1, rule_name: 'Auto-Restart Web Service on 5xx Spike', asset_id: 4, asset_key: 'payment-api', run_id: 'run-20260701-1430-a1b2', trigger: 'telemetry.metric_exceeded', status: 'completed', error: '', started_at: '2026-07-01 14:30:05', finished_at: '2026-07-01 14:30:08', created_at: '2026-07-01 14:30:05' },
  { id: 2, rule_id: 2, rule_name: 'Scale Out on High CPU', asset_id: 3, asset_key: 'prod-api-03', run_id: 'run-20260702-0915-c3d4', trigger: 'telemetry.log_matched', status: 'failed', error: 'executor returned HTTP 502', started_at: '2026-07-02 09:15:10', finished_at: '2026-07-02 09:15:26', created_at: '2026-07-02 09:15:10' },
  { id: 3, rule_id: 1, rule_name: 'Auto-Restart Web Service on 5xx Spike', asset_id: 4, asset_key: 'payment-api', run_id: 'run-20260703-0801-e5f6', trigger: 'telemetry.metric_exceeded', status: 'started', error: '', started_at: '2026-07-03 08:01:00', finished_at: '', created_at: '2026-07-03 08:01:00' },
  { id: 4, rule_id: 3, rule_name: 'Clear Cache on Memory Pressure', asset_id: 3, asset_key: 'prod-api-03', run_id: 'run-20260703-1102-a7b8', trigger: 'asset.status_changed', status: 'skipped', error: '', started_at: '', finished_at: '', created_at: '2026-07-03 11:02:00' },
]

export default [
  // ── Remediation records ──
  {
    url: '/api/v1/prism/remediation/records',
    method: 'get',
    response: ({ query }: { query: Record<string, string> }) => {
      const page = Number(query.page) || 1
      const size = Number(query.page_size) || 10
      const status = query.status || ''
      const filtered = status ? remediationRecords.filter((r) => r.status === status) : remediationRecords
      const start = (page - 1) * size
      return {
        code: 0,
        message: 'success',
        data: {
          items: filtered.slice(start, start + size),
          total: filtered.length,
          page,
          page_size: size,
        },
      }
    },
  },
  // ── Alert records ──
  {
    url: '/api/v1/prism/alert/records',
    method: 'get',
    response: ({ query }: { query: Record<string, string> }) => {
      const page = Number(query.page) || 1
      const size = Number(query.page_size) || 15
      const filtered = filterRecords(query)
      const start = (page - 1) * size
      return {
        code: 0,
        message: 'success',
        data: {
          items: filtered.slice(start, start + size),
          total: filtered.length,
          page,
          page_size: size,
        },
      }
    },
  },
  {
    url: '/api/v1/prism/alert/records/:id',
    method: 'get',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const record = records.find((r) => r.id === id)
      return {
        code: 0,
        message: 'success',
        data: record ?? records[0],
      }
    },
  },
  {
    url: '/api/v1/prism/alert/records/:id/resolve',
    method: 'put',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const record = records.find((r) => r.id === id)
      if (record && record.status === 'firing') {
        record.status = 'resolved'
        record.resolved_at = new Date().toISOString().replace('T', ' ').substring(0, 19)
      }
      return { code: 0, message: 'success', data: record ?? records[0] }
    },
  },
  {
    url: '/api/v1/prism/alert/records/:id/acknowledge',
    method: 'put',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const record = records.find((r) => r.id === id)
      if (record && record.status === 'firing') {
        record.status = 'acknowledged'
        ;(record as Record<string, unknown>).acknowledged_at = new Date().toISOString().replace('T', ' ').substring(0, 19)
      }
      return { code: 0, message: 'success', data: record ?? records[0] }
    },
  },
  // ── Alert rules ──
  {
    url: '/api/v1/prism/alert/rules',
    method: 'get',
    response: ({ query }: { query: Record<string, string> }) => {
      const page = Number(query.page) || 1
      const size = Number(query.page_size) || 10
      const start = (page - 1) * size
      return {
        code: 0,
        message: 'success',
        data: {
          items: rules.slice(start, start + size),
          total: rules.length,
          page,
          page_size: size,
        },
      }
    },
  },
  {
    url: '/api/v1/prism/alert/rules/:id',
    method: 'get',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const rule = rules.find((r) => r.id === id)
      return { code: 0, message: 'success', data: rule ?? rules[0] }
    },
  },
  {
    url: '/api/v1/prism/alert/rules',
    method: 'post',
    response: ({ body }: { body: Record<string, unknown> }) => {
      ruleSeq += 1
      const rule = {
        id: ruleSeq,
        name: String(body.name ?? ''),
        metric: String(body.metric ?? ''),
        condition: (body.condition as string) ?? 'gt',
        threshold: Number(body.threshold ?? 0),
        duration: Number(body.duration ?? 60),
        severity: (body.severity as string) ?? 'warning',
        channels: (body.channels as string[]) ?? ['webhook'],
        enabled: body.enabled !== false,
        description: String(body.description ?? ''),
        created_at: new Date().toISOString().replace('T', ' ').substring(0, 19),
      }
      rules.unshift(rule)
      return { code: 0, message: 'success', data: rule }
    },
  },
  {
    url: '/api/v1/prism/alert/rules/:id',
    method: 'put',
    response: ({ url, body }: { url: string; body: Record<string, unknown> }) => {
      const id = extractId(url)
      const idx = rules.findIndex((r) => r.id === id)
      if (idx !== -1) {
        rules[idx] = {
          ...rules[idx],
          name: String(body.name ?? rules[idx].name),
          metric: String(body.metric ?? rules[idx].metric),
          condition: (body.condition as string) ?? rules[idx].condition,
          threshold: Number(body.threshold ?? rules[idx].threshold),
          duration: Number(body.duration ?? rules[idx].duration),
          severity: (body.severity as string) ?? rules[idx].severity,
          channels: (body.channels as string[]) ?? rules[idx].channels,
          enabled: body.enabled !== undefined ? Boolean(body.enabled) : rules[idx].enabled,
          description: String(body.description ?? rules[idx].description),
        }
        return { code: 0, message: 'success', data: rules[idx] }
      }
      return { code: 0, message: 'success', data: rules[0] }
    },
  },
  {
    url: '/api/v1/prism/alert/rules/:id',
    method: 'delete',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const idx = rules.findIndex((r) => r.id === id)
      if (idx !== -1) rules.splice(idx, 1)
      return { code: 0, message: 'success', data: null }
    },
  },
  // ── Notification channels ──
  {
    url: '/api/v1/prism/channels',
    method: 'get',
    response: ({ query }: { query: Record<string, string> }) => {
      const page = Number(query.page) || 1
      const size = Number(query.page_size) || 10
      const keyword = query.keyword || ''
      const filtered = keyword
        ? channels.filter((c) => c.name.toLowerCase().includes(keyword.toLowerCase()))
        : channels
      const start = (page - 1) * size
      return {
        code: 0,
        message: 'success',
        data: {
          items: filtered.slice(start, start + size),
          total: filtered.length,
          page,
          page_size: size,
        },
      }
    },
  },
  {
    url: '/api/v1/prism/channels/:id',
    method: 'get',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const channel = channels.find((c) => c.id === id)
      return { code: 0, message: 'success', data: channel ?? channels[0] }
    },
  },
  {
    url: '/api/v1/prism/channels',
    method: 'post',
    response: ({ body }: { body: Record<string, unknown> }) => {
      channelSeq += 1
      const now = new Date().toISOString().replace('T', ' ').substring(0, 19)
      const channel = {
        id: channelSeq,
        name: String(body.name ?? ''),
        type: String(body.type ?? 'webhook'),
        config: String(body.config ?? '{}'),
        enabled: body.enabled !== false,
        last_used_at: null,
        created_at: now,
        updated_at: now,
      }
      channels.unshift(channel)
      return { code: 0, message: 'success', data: channel }
    },
  },
  {
    url: '/api/v1/prism/channels/:id',
    method: 'put',
    response: ({ url, body }: { url: string; body: Record<string, unknown> }) => {
      const id = extractId(url)
      const idx = channels.findIndex((c) => c.id === id)
      if (idx !== -1) {
        const now = new Date().toISOString().replace('T', ' ').substring(0, 19)
        channels[idx] = {
          ...channels[idx],
          name: String(body.name ?? channels[idx].name),
          type: String(body.type ?? channels[idx].type),
          config: String(body.config ?? channels[idx].config),
          enabled: body.enabled !== undefined ? Boolean(body.enabled) : channels[idx].enabled,
          updated_at: now,
        }
        return { code: 0, message: 'success', data: channels[idx] }
      }
      return { code: 0, message: 'success', data: channels[0] }
    },
  },
  {
    url: '/api/v1/prism/channels/:id',
    method: 'delete',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const idx = channels.findIndex((c) => c.id === id)
      if (idx !== -1) channels.splice(idx, 1)
      return { code: 0, message: 'success', data: null }
    },
  },
  {
    url: '/api/v1/prism/channels/:id/test',
    method: 'post',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const idx = channels.findIndex((c) => c.id === id)
      if (idx !== -1) {
        channels[idx].last_used_at = new Date().toISOString().replace('T', ' ').substring(0, 19)
      }
      return { code: 0, message: 'success', data: null }
    },
  },
  // ── Remediation rules ──
  {
    url: '/api/v1/prism/remediation/rules',
    method: 'get',
    response: ({ query }: { query: Record<string, string> }) => {
      const page = Number(query.page) || 1
      const size = Number(query.page_size) || 10
      const keyword = query.keyword || ''
      const filtered = keyword
        ? remediationRules.filter((r) => r.name.toLowerCase().includes(keyword.toLowerCase()))
        : remediationRules
      const start = (page - 1) * size
      return {
        code: 0,
        message: 'success',
        data: {
          items: filtered.slice(start, start + size),
          total: filtered.length,
          page,
          page_size: size,
        },
      }
    },
  },
  {
    url: '/api/v1/prism/remediation/rules/:id',
    method: 'get',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const rule = remediationRules.find((r) => r.id === id)
      return { code: 0, message: 'success', data: rule ?? remediationRules[0] }
    },
  },
  {
    url: '/api/v1/prism/remediation/rules',
    method: 'post',
    response: ({ body }: { body: Record<string, unknown> }) => {
      remediationRuleSeq += 1
      const now = new Date().toISOString().replace('T', ' ').substring(0, 19)
      const rule = {
        id: remediationRuleSeq,
        name: String(body.name ?? ''),
        description: String(body.description ?? ''),
        asset_id: Number(body.asset_id ?? 0),
        trigger_event_type: String(body.trigger_event_type ?? 'metric'),
        condition_expr: String(body.condition_expr ?? ''),
        executor_type: String(body.executor_type ?? 'webhook'),
        executor_config: String(body.executor_config ?? '{}'),
        cooldown: Number(body.cooldown ?? 300),
        circuit_breaker_threshold: Number(body.circuit_breaker_threshold ?? 3),
        enabled: body.enabled !== false,
        status: 'idle',
        last_run_at: null,
        created_at: now,
        updated_at: now,
      }
      remediationRules.unshift(rule)
      return { code: 0, message: 'success', data: rule }
    },
  },
  {
    url: '/api/v1/prism/remediation/rules/:id',
    method: 'put',
    response: ({ url, body }: { url: string; body: Record<string, unknown> }) => {
      const id = extractId(url)
      const idx = remediationRules.findIndex((r) => r.id === id)
      if (idx !== -1) {
        const now = new Date().toISOString().replace('T', ' ').substring(0, 19)
        remediationRules[idx] = {
          ...remediationRules[idx],
          name: String(body.name ?? remediationRules[idx].name),
          description: String(body.description ?? remediationRules[idx].description),
          asset_id: Number(body.asset_id ?? remediationRules[idx].asset_id),
          trigger_event_type: String(body.trigger_event_type ?? remediationRules[idx].trigger_event_type),
          condition_expr: String(body.condition_expr ?? remediationRules[idx].condition_expr),
          executor_type: String(body.executor_type ?? remediationRules[idx].executor_type),
          executor_config: String(body.executor_config ?? remediationRules[idx].executor_config),
          cooldown: Number(body.cooldown ?? remediationRules[idx].cooldown),
          circuit_breaker_threshold: Number(body.circuit_breaker_threshold ?? remediationRules[idx].circuit_breaker_threshold),
          enabled: body.enabled !== undefined ? Boolean(body.enabled) : remediationRules[idx].enabled,
          updated_at: now,
        }
        return { code: 0, message: 'success', data: remediationRules[idx] }
      }
      return { code: 0, message: 'success', data: remediationRules[0] }
    },
  },
  {
    url: '/api/v1/prism/remediation/rules/:id',
    method: 'delete',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const idx = remediationRules.findIndex((r) => r.id === id)
      if (idx !== -1) remediationRules.splice(idx, 1)
      return { code: 0, message: 'success', data: null }
    },
  },
] as MockMethod[]
