// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { MockMethod } from './types'

/** Alert rule seed (snake_case response shape of handler.AlertRule) */
interface MockAlertRule {
  id: number
  name: string
  description: string
  scene: string
  expression: string
  priority: number
  enabled: boolean
  created_at: string
  updated_at: string
}

/** Alert record seed (snake_case response shape of handler.AlertRecord) */
interface MockAlertRecord {
  id: number
  rule_id: number
  rule_name: string
  severity: 'critical' | 'warning' | 'info'
  /** Value at trigger time */
  value: number
  status: 'firing' | 'acknowledged' | 'resolved'
  message: string
  fired_at: string
  acknowledged_at: string | null
  resolved_at: string | null
}

/** Alert rules (8 items covering all 4 scenes: task / probe / metric / remediation) */
const mockAlertRules: MockAlertRule[] = [
  {
    id: 1,
    name: 'HTTP 5xx Error Rate',
    description: 'Fires when the reported HTTP 5xx error rate exceeds 5%',
    scene: 'metric',
    expression: 'event.metrics["http_5xx_rate"] > 5',
    priority: 100,
    enabled: true,
    created_at: '2026-06-01 10:00:00',
    updated_at: '2026-07-12 09:30:00',
  },
  {
    id: 2,
    name: 'Host CPU Usage',
    description: 'Fires when host CPU usage is sustained above 80%',
    scene: 'metric',
    expression: 'event.metrics["cpu_usage"] > 80',
    priority: 60,
    enabled: true,
    created_at: '2026-06-02 10:00:00',
    updated_at: '2026-06-20 15:10:00',
  },
  {
    id: 3,
    name: 'Host Memory Usage',
    description: 'Fires when host memory usage reaches 90%',
    scene: 'metric',
    expression: 'event.metrics["memory_usage"] >= 90',
    priority: 90,
    enabled: true,
    created_at: '2026-06-03 10:00:00',
    updated_at: '2026-06-25 11:40:00',
  },
  {
    id: 4,
    name: 'Disk Free Space',
    description: 'Fires when disk free space falls below 10%',
    scene: 'metric',
    expression: 'event.metrics["disk_free"] < 10',
    priority: 50,
    enabled: true,
    created_at: '2026-06-04 10:00:00',
    updated_at: '2026-06-18 16:25:00',
  },
  {
    id: 5,
    name: 'TCP Port Connectivity',
    description: 'Fires when a TCP probe fails or the asset turns abnormal',
    scene: 'probe',
    expression: 'event.metrics["tcp_connect"] == 0 || event.status == "abnormal"',
    priority: 80,
    enabled: true,
    created_at: '2026-06-05 10:00:00',
    updated_at: '2026-07-01 08:55:00',
  },
  {
    id: 6,
    name: 'ICMP Packet Loss Rate',
    description: 'Fires when ICMP probe packet loss rate exceeds 10%',
    scene: 'probe',
    expression: 'event.metrics["icmp_loss"] > 10',
    priority: 40,
    enabled: true,
    created_at: '2026-06-06 10:00:00',
    updated_at: '2026-06-22 14:05:00',
  },
  {
    id: 7,
    name: 'Task Failure Surge',
    description: 'Fires when local-executor task failures reach 3 within a window (disabled)',
    scene: 'task',
    expression: 'event.metrics["task_failed"] >= 3 && event.executor_type == "local"',
    priority: 20,
    enabled: false,
    created_at: '2026-06-07 10:00:00',
    updated_at: '2026-07-08 10:20:00',
  },
  {
    id: 8,
    name: 'Critical Alert Remediation Escalation',
    description: 'Escalates to a human channel when a critical alert value stays above 80',
    scene: 'remediation',
    expression: 'event.metric_value > 80 && event.severity == "critical"',
    priority: 120,
    enabled: true,
    created_at: '2026-06-08 10:00:00',
    updated_at: '2026-07-15 13:45:00',
  },
]

/** Build a `YYYY-MM-DD HH:mm:ss` string for N days ago at the given local time */
function daysAgo(days: number, time: string): string {
  const d = new Date()
  d.setDate(d.getDate() - days)
  const pad = (n: number): string => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${time}`
}

/**
 * Alert records (22 items spread across the last 12 days so the dashboard
 * alert-trend chart shows a multi-day stacked area; mixed severities per day).
 */
const mockAlertRecords: MockAlertRecord[] = [
  // Day 0 (today)
  { id: 1, rule_id: 3, rule_name: 'Host Memory Usage', severity: 'critical', value: 92.5, status: 'firing', message: 'Memory usage on prod-api-03 at 92.5%, threshold 90%', fired_at: daysAgo(0, '09:58:00'), acknowledged_at: null, resolved_at: null },
  { id: 2, rule_id: 5, rule_name: 'TCP Port Connectivity', severity: 'critical', value: 0, status: 'firing', message: 'TCP probe failed: prod-es-01:9200 unreachable (tcp_connect=0)', fired_at: daysAgo(0, '09:21:00'), acknowledged_at: null, resolved_at: null },
  { id: 3, rule_id: 6, rule_name: 'ICMP Packet Loss Rate', severity: 'warning', value: 15.4, status: 'firing', message: 'ICMP packet loss to cdn-edge-01 reached 15.4%, threshold 10%', fired_at: daysAgo(0, '08:47:00'), acknowledged_at: null, resolved_at: null },
  // Day 1
  { id: 4, rule_id: 2, rule_name: 'Host CPU Usage', severity: 'warning', value: 85.3, status: 'firing', message: 'CPU usage on prod-api-03 at 85.3%, threshold 80%', fired_at: daysAgo(1, '16:05:00'), acknowledged_at: null, resolved_at: null },
  { id: 5, rule_id: 4, rule_name: 'Disk Free Space', severity: 'warning', value: 8.2, status: 'acknowledged', message: 'Disk free space on backup storage down to 8.2%, threshold 10%', fired_at: daysAgo(1, '11:30:00'), acknowledged_at: daysAgo(1, '12:02:00'), resolved_at: null },
  // Day 2
  { id: 6, rule_id: 1, rule_name: 'HTTP 5xx Error Rate', severity: 'critical', value: 8.2, status: 'firing', message: 'HTTP 5xx error rate on payment-api reached 8.2%, threshold 5%', fired_at: daysAgo(2, '14:18:00'), acknowledged_at: null, resolved_at: null },
  { id: 7, rule_id: 8, rule_name: 'Critical Alert Remediation Escalation', severity: 'critical', value: 92, status: 'acknowledged', message: 'Escalating critical alert: trigger value 92 stays above 80', fired_at: daysAgo(2, '10:40:00'), acknowledged_at: daysAgo(2, '10:55:00'), resolved_at: null },
  // Day 3
  { id: 8, rule_id: 5, rule_name: 'TCP Port Connectivity', severity: 'critical', value: 0, status: 'firing', message: 'TCP probe failed: prod-cache-01:6379 unreachable (tcp_connect=0)', fired_at: daysAgo(3, '22:10:00'), acknowledged_at: null, resolved_at: null },
  { id: 9, rule_id: 2, rule_name: 'Host CPU Usage', severity: 'warning', value: 82.7, status: 'resolved', message: 'CPU usage on prod-web-01 at 82.7%, threshold 80%', fired_at: daysAgo(3, '09:12:00'), acknowledged_at: daysAgo(3, '09:20:00'), resolved_at: daysAgo(3, '09:40:00') },
  // Day 4
  { id: 10, rule_id: 3, rule_name: 'Host Memory Usage', severity: 'critical', value: 93.1, status: 'resolved', message: 'Memory usage on prod-web-02 at 93.1%, threshold 90%', fired_at: daysAgo(4, '18:26:00'), acknowledged_at: daysAgo(4, '18:40:00'), resolved_at: daysAgo(4, '19:00:00') },
  { id: 11, rule_id: 7, rule_name: 'Task Failure Surge', severity: 'info', value: 3, status: 'firing', message: 'Local-executor task failures reached 3 within one hour', fired_at: daysAgo(4, '15:03:00'), acknowledged_at: null, resolved_at: null },
  // Day 5
  { id: 12, rule_id: 1, rule_name: 'HTTP 5xx Error Rate', severity: 'critical', value: 6.1, status: 'resolved', message: 'HTTP 5xx error rate on www.tickraft.io reached 6.1%, threshold 5%', fired_at: daysAgo(5, '13:45:00'), acknowledged_at: null, resolved_at: daysAgo(5, '14:10:00') },
  { id: 13, rule_id: 6, rule_name: 'ICMP Packet Loss Rate', severity: 'warning', value: 11.2, status: 'resolved', message: 'ICMP packet loss on intranet gateway reached 11.2%, threshold 10%', fired_at: daysAgo(5, '08:20:00'), acknowledged_at: null, resolved_at: daysAgo(5, '08:50:00') },
  // Day 6
  { id: 14, rule_id: 4, rule_name: 'Disk Free Space', severity: 'warning', value: 9.1, status: 'acknowledged', message: 'Disk free space on prod-db-02 down to 9.1%, threshold 10%', fired_at: daysAgo(6, '20:32:00'), acknowledged_at: daysAgo(6, '21:00:00'), resolved_at: null },
  { id: 15, rule_id: 2, rule_name: 'Host CPU Usage', severity: 'warning', value: 81.5, status: 'resolved', message: 'CPU usage on prod-db-03 at 81.5%, threshold 80%', fired_at: daysAgo(6, '11:15:00'), acknowledged_at: null, resolved_at: daysAgo(6, '11:45:00') },
  // Day 7
  { id: 16, rule_id: 3, rule_name: 'Host Memory Usage', severity: 'critical', value: 91.4, status: 'resolved', message: 'Memory usage on prod-db-02 at 91.4%, threshold 90%', fired_at: daysAgo(7, '17:08:00'), acknowledged_at: daysAgo(7, '17:20:00'), resolved_at: daysAgo(7, '17:35:00') },
  { id: 17, rule_id: 7, rule_name: 'Task Failure Surge', severity: 'info', value: 4, status: 'resolved', message: 'Local-executor task failures reached 4 within one hour', fired_at: daysAgo(7, '10:26:00'), acknowledged_at: null, resolved_at: daysAgo(7, '11:00:00') },
  // Day 8
  { id: 18, rule_id: 6, rule_name: 'ICMP Packet Loss Rate', severity: 'warning', value: 12.6, status: 'resolved', message: 'ICMP packet loss on prod-web-01 reached 12.6%, threshold 10%', fired_at: daysAgo(8, '21:40:00'), acknowledged_at: null, resolved_at: daysAgo(8, '22:05:00') },
  { id: 19, rule_id: 5, rule_name: 'TCP Port Connectivity', severity: 'critical', value: 0, status: 'resolved', message: 'TCP probe failed: prod-kafka-01:9092 unreachable (tcp_connect=0)', fired_at: daysAgo(8, '09:35:00'), acknowledged_at: daysAgo(8, '09:42:00'), resolved_at: daysAgo(8, '09:50:00') },
  // Day 9
  { id: 20, rule_id: 2, rule_name: 'Host CPU Usage', severity: 'warning', value: 84.9, status: 'resolved', message: 'CPU usage on monitoring host at 84.9%, threshold 80%', fired_at: daysAgo(9, '14:22:00'), acknowledged_at: null, resolved_at: daysAgo(9, '14:55:00') },
  // Day 10
  { id: 21, rule_id: 7, rule_name: 'Task Failure Surge', severity: 'info', value: 5, status: 'resolved', message: 'Local-executor task failures reached 5 within one hour', fired_at: daysAgo(10, '12:10:00'), acknowledged_at: null, resolved_at: daysAgo(10, '12:38:00') },
  // Day 11
  { id: 22, rule_id: 1, rule_name: 'HTTP 5xx Error Rate', severity: 'critical', value: 9.4, status: 'resolved', message: 'HTTP 5xx error rate on payment-api reached 9.4%, threshold 5%', fired_at: daysAgo(11, '15:52:00'), acknowledged_at: null, resolved_at: daysAgo(11, '16:20:00') },
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
      if (record && record.status !== 'resolved') {
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
        record.acknowledged_at = new Date().toISOString().replace('T', ' ').substring(0, 19)
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
      const now = new Date().toISOString().replace('T', ' ').substring(0, 19)
      const rule: MockAlertRule = {
        id: ruleSeq,
        name: String(body.name ?? ''),
        description: String(body.description ?? ''),
        scene: String(body.scene ?? 'metric'),
        expression: String(body.expression ?? ''),
        priority: Number(body.priority ?? 0),
        enabled: body.enabled !== false,
        created_at: now,
        updated_at: now,
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
        const now = new Date().toISOString().replace('T', ' ').substring(0, 19)
        rules[idx] = {
          ...rules[idx],
          name: String(body.name ?? rules[idx].name),
          description: String(body.description ?? ''),
          scene: String(body.scene ?? rules[idx].scene),
          expression: String(body.expression ?? rules[idx].expression),
          priority: Number(body.priority ?? rules[idx].priority),
          enabled: body.enabled !== undefined ? Boolean(body.enabled) : rules[idx].enabled,
          updated_at: now,
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
