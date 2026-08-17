// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { MockMethod } from './types'

/**
 * Task seed shape (snake_case wire format mirroring backend handler.Task).
 * The request layer camelizes response keys, so the frontend receives
 * { id, name, executor, schedule, config, group, tags, runId, retryPolicy,
 *   concurrency, enabled, createdAt, updatedAt }.
 */
interface TaskSeed {
  id: number
  name: string
  description: string
  executor: string
  schedule: string
  enabled: boolean
  config: Record<string, unknown>
  group: string
  tags: string[]
  run_id?: string
  retry_policy?: string
  concurrency?: number
  created_at: string
  updated_at: string
}

/**
 * Task dataset (14 items, covering the 5 open-source executors and the 3
 * schedule forms: cron expression / Go duration interval / empty event-driven)
 */
const mockTasks: TaskSeed[] = [
  {
    id: 1, name: 'API health check', description: 'Probe the public API health endpoint every minute',
    executor: 'http', schedule: '60s', enabled: true,
    config: { url: 'https://api.tickraft.io/health', method: 'GET', headers: '', timeout: 10 },
    group: 'web', tags: ['critical', 'production'], retry_policy: 'fixed', concurrency: 3, run_id: 'run-20260630-135800',
    created_at: '2026-06-01 10:00:00', updated_at: '2026-06-15 09:30:00',
  },
  {
    id: 2, name: 'MySQL master-slave sync check', description: 'Check MySQL master-slave port connectivity every 5 minutes',
    executor: 'tcp', schedule: '*/5 * * * *', enabled: true,
    config: { host: 'prod-db-02', port: 3306, timeout: 5 },
    group: 'database', tags: ['production'], retry_policy: 'fixed',
    created_at: '2026-06-02 11:20:00', updated_at: '2026-06-12 14:00:00',
  },
  {
    id: 3, name: 'CDN edge ICMP probe', description: 'Ping the CDN edge node every 30 seconds to measure latency',
    executor: 'icmp', schedule: '30s', enabled: true,
    config: { host: 'cdn-edge-01.tickraft.io', count: 4, timeout: 3 },
    group: 'web', tags: ['edge'], retry_policy: 'fixed', run_id: 'run-20260630-140030',
    created_at: '2026-06-03 09:00:00', updated_at: '2026-06-20 16:30:00',
  },
  {
    id: 4, name: 'Nightly log archive', description: 'Archive previous day logs at 02:00 every day',
    executor: 'local', schedule: '0 2 * * *', enabled: true,
    config: { interpreter: 'bash', source: '/opt/tickraft/scripts/archive_logs.sh --keep-days 30', timeout: 600 },
    group: 'infra', tags: ['nightly'], retry_policy: 'fixed', concurrency: 1,
    created_at: '2026-05-28 18:00:00', updated_at: '2026-06-25 09:15:00',
  },
  {
    id: 5, name: 'Webhook alert forwarding', description: 'Forward alert payloads to the external webhook endpoint on demand',
    executor: 'webhook', schedule: '', enabled: true,
    config: { url: 'https://hooks.example.com/alert', method: 'POST', headers: 'Authorization: Bearer wh_*****' },
    group: 'alerting', tags: ['critical'], retry_policy: 'exponential',
    created_at: '2026-06-05 14:00:00', updated_at: '2026-06-22 11:45:00',
  },
  {
    id: 6, name: 'Redis cache cleanup', description: 'Clean expired cache keys every 6 hours',
    executor: 'local', schedule: '0 */6 * * *', enabled: true,
    config: { interpreter: 'bash', source: 'redis-cli --scan --pattern "tmp:*" | xargs -r redis-cli del', timeout: 120 },
    group: 'database', tags: ['maintenance'], retry_policy: 'fixed',
    created_at: '2026-05-30 10:00:00', updated_at: '2026-06-18 13:00:00',
  },
  {
    id: 7, name: 'API benchmark smoke test', description: 'One-shot benchmark against the staging API (disabled)',
    executor: 'http', schedule: '', enabled: false,
    config: { url: 'http://prod-api-03:8080/benchmark', method: 'POST', headers: '', timeout: 300 },
    group: 'web', tags: ['benchmark'], retry_policy: 'fixed', concurrency: 5,
    created_at: '2026-06-25 14:30:00', updated_at: '2026-06-28 11:00:00',
  },
  {
    id: 8, name: 'Weekly backup upload to OSS', description: 'Upload archived backups to OSS at 03:00 every Sunday',
    executor: 'local', schedule: '0 3 * * 0', enabled: true,
    config: { interpreter: 'bash', source: '/opt/tickraft/scripts/upload_oss.sh --bucket=backup', timeout: 1800 },
    group: 'infra', tags: ['backup', 'weekly'], retry_policy: 'fixed',
    created_at: '2026-05-25 09:00:00', updated_at: '2026-06-26 10:00:00',
  },
  {
    id: 9, name: 'DNS resolution probe', description: 'Resolve the public API domain via the intranet DNS server every 5 minutes',
    executor: 'local', schedule: '5m', enabled: true,
    config: { interpreter: 'bash', source: 'dig +short api.tickraft.io @10.0.0.53', timeout: 15 },
    group: 'network', tags: ['dns'], retry_policy: 'fixed',
    created_at: '2026-06-07 13:00:00', updated_at: '2026-06-24 16:00:00',
  },
  {
    id: 10, name: 'PostgreSQL port listen check', description: 'Check the PostgreSQL 5432 port every 2 minutes',
    executor: 'tcp', schedule: '120s', enabled: true,
    config: { host: 'prod-db-03', port: 5432, timeout: 5 },
    group: 'database', tags: [], retry_policy: 'fixed',
    created_at: '2026-06-04 10:00:00', updated_at: '2026-06-21 14:30:00',
  },
  {
    id: 11, name: 'Intranet gateway ICMP probe', description: 'Ping the intranet gateway every minute',
    executor: 'icmp', schedule: '60s', enabled: true,
    config: { host: '10.0.0.1', count: 4, timeout: 3 },
    group: 'network', tags: ['critical'], retry_policy: 'fixed', run_id: 'run-20260630-140000',
    created_at: '2026-06-06 11:00:00', updated_at: '2026-06-23 09:00:00',
  },
  {
    id: 12, name: 'Config file sync', description: 'Triggered by config change events, sync to all nodes (disabled)',
    executor: 'local', schedule: '', enabled: false,
    config: { interpreter: 'bash', source: '/opt/tickraft/scripts/sync_config.sh --all-nodes', timeout: 60 },
    group: 'infra', tags: [], retry_policy: 'fixed',
    created_at: '2026-05-29 14:00:00', updated_at: '2026-06-27 15:30:00',
  },
  {
    id: 13, name: 'Payment callback availability check', description: 'Check the payment callback API availability every 10 minutes',
    executor: 'http', schedule: '*/10 * * * *', enabled: true,
    config: { url: 'https://api.tickraft.io/pay/callback', method: 'GET', headers: '', timeout: 8 },
    group: 'web', tags: ['critical', 'payment'], retry_policy: 'exponential', concurrency: 5,
    created_at: '2026-06-08 13:00:00', updated_at: '2026-06-26 16:00:00',
  },
  {
    id: 14, name: 'Kafka broker port check', description: 'Check the Kafka 9092 port every 90 seconds (disabled)',
    executor: 'tcp', schedule: '90s', enabled: false,
    config: { host: 'prod-kafka-01', port: 9092, timeout: 5 },
    group: 'middleware', tags: ['production'], retry_policy: 'fixed',
    created_at: '2026-06-09 10:30:00', updated_at: '2026-06-25 11:00:00',
  },
]

/**
 * Execution log seed shape (snake_case wire format mirroring backend
 * handler.Execution). `finished_at` is omitted on running rows and `error`
 * is only present on failed rows.
 */
interface LogSeed {
  id: number
  task_id: number
  status: string
  output: string
  error?: string
  started_at: string
  finished_at?: string
  task_name: string
  executor_type: string
  duration: number
  status_code?: number
  retry_count: number
}

/**
 * Execution log dataset (28 items, status limited to success/failed/running).
 * Task 1 carries 12 alternating-duration runs to feed the detail trend chart.
 */
const mockLogs: LogSeed[] = [
  { id: 1, task_id: 1, status: 'success', output: 'HTTP 200, response time: 2.0s', started_at: '2026-06-30 13:47:00', finished_at: '2026-06-30 13:47:02', task_name: 'API health check', executor_type: 'http', duration: 2000, status_code: 200, retry_count: 0 },
  { id: 2, task_id: 1, status: 'success', output: 'HTTP 200, response time: 5.0s', started_at: '2026-06-30 13:48:00', finished_at: '2026-06-30 13:48:05', task_name: 'API health check', executor_type: 'http', duration: 5000, status_code: 200, retry_count: 0 },
  { id: 3, task_id: 1, status: 'success', output: 'HTTP 200, response time: 1.0s', started_at: '2026-06-30 13:49:00', finished_at: '2026-06-30 13:49:01', task_name: 'API health check', executor_type: 'http', duration: 1000, status_code: 200, retry_count: 0 },
  { id: 4, task_id: 1, status: 'success', output: 'HTTP 200, response time: 7.0s', started_at: '2026-06-30 13:50:00', finished_at: '2026-06-30 13:50:07', task_name: 'API health check', executor_type: 'http', duration: 7000, status_code: 200, retry_count: 0 },
  { id: 5, task_id: 1, status: 'success', output: 'HTTP 200, response time: 3.0s', started_at: '2026-06-30 13:51:00', finished_at: '2026-06-30 13:51:03', task_name: 'API health check', executor_type: 'http', duration: 3000, status_code: 200, retry_count: 0 },
  { id: 6, task_id: 1, status: 'failed', output: '', error: 'HTTP 500 Internal Server Error: upstream connect error', started_at: '2026-06-30 13:52:00', finished_at: '2026-06-30 13:52:06', task_name: 'API health check', executor_type: 'http', duration: 6000, status_code: 500, retry_count: 2 },
  { id: 7, task_id: 1, status: 'success', output: 'HTTP 200, response time: 2.0s', started_at: '2026-06-30 13:53:00', finished_at: '2026-06-30 13:53:02', task_name: 'API health check', executor_type: 'http', duration: 2000, status_code: 200, retry_count: 0 },
  { id: 8, task_id: 1, status: 'success', output: 'HTTP 200, response time: 8.0s', started_at: '2026-06-30 13:54:00', finished_at: '2026-06-30 13:54:08', task_name: 'API health check', executor_type: 'http', duration: 8000, status_code: 200, retry_count: 0 },
  { id: 9, task_id: 1, status: 'success', output: 'HTTP 200, response time: 3.0s', started_at: '2026-06-30 13:55:00', finished_at: '2026-06-30 13:55:03', task_name: 'API health check', executor_type: 'http', duration: 3000, status_code: 200, retry_count: 0 },
  { id: 10, task_id: 1, status: 'success', output: 'HTTP 200, response time: 1.0s', started_at: '2026-06-30 13:56:00', finished_at: '2026-06-30 13:56:01', task_name: 'API health check', executor_type: 'http', duration: 1000, status_code: 200, retry_count: 0 },
  { id: 11, task_id: 1, status: 'success', output: 'HTTP 200, response time: 4.0s', started_at: '2026-06-30 13:57:00', finished_at: '2026-06-30 13:57:04', task_name: 'API health check', executor_type: 'http', duration: 4000, status_code: 200, retry_count: 0 },
  { id: 12, task_id: 1, status: 'success', output: 'HTTP 200, response time: 2.0s', started_at: '2026-06-30 13:58:00', finished_at: '2026-06-30 13:58:02', task_name: 'API health check', executor_type: 'http', duration: 2000, status_code: 200, retry_count: 0 },
  { id: 13, task_id: 2, status: 'success', output: 'TCP connect success', started_at: '2026-06-30 13:55:00', finished_at: '2026-06-30 13:55:01', task_name: 'MySQL master-slave sync check', executor_type: 'tcp', duration: 500, retry_count: 0 },
  { id: 14, task_id: 2, status: 'failed', output: '', error: 'dial tcp 10.0.2.21:3306: connect: connection refused', started_at: '2026-06-30 13:50:00', finished_at: '2026-06-30 13:50:03', task_name: 'MySQL master-slave sync check', executor_type: 'tcp', duration: 3000, retry_count: 3 },
  { id: 15, task_id: 3, status: 'success', output: '4 packets transmitted, 4 received, 0% loss, avg 12.5ms', started_at: '2026-06-30 13:59:30', finished_at: '2026-06-30 13:59:35', task_name: 'CDN edge ICMP probe', executor_type: 'icmp', duration: 5000, retry_count: 0 },
  { id: 16, task_id: 4, status: 'success', output: 'archived 1024 files, total size 5.2GB', started_at: '2026-06-30 02:00:00', finished_at: '2026-06-30 02:05:20', task_name: 'Nightly log archive', executor_type: 'local', duration: 320000, retry_count: 0 },
  { id: 17, task_id: 5, status: 'success', output: 'webhook delivered, HTTP 200', started_at: '2026-06-30 13:30:00', finished_at: '2026-06-30 13:30:01', task_name: 'Webhook alert forwarding', executor_type: 'webhook', duration: 800, retry_count: 0 },
  { id: 18, task_id: 6, status: 'failed', output: 'redis-cli: connection refused', error: 'Could not connect to Redis at 10.0.3.31:6379: Connection refused', started_at: '2026-06-30 12:00:00', finished_at: '2026-06-30 12:00:08', task_name: 'Redis cache cleanup', executor_type: 'local', duration: 8000, retry_count: 2 },
  { id: 19, task_id: 7, status: 'failed', output: '', error: 'context deadline exceeded after 300s', started_at: '2026-06-28 10:00:00', finished_at: '2026-06-28 10:05:00', task_name: 'API benchmark smoke test', executor_type: 'http', duration: 300000, status_code: 0, retry_count: 1 },
  { id: 20, task_id: 8, status: 'success', output: 'uploaded 5.2GB to oss://backup/2026-06-29', started_at: '2026-06-29 03:00:00', finished_at: '2026-06-29 03:08:00', task_name: 'Weekly backup upload to OSS', executor_type: 'local', duration: 480000, retry_count: 0 },
  { id: 21, task_id: 9, status: 'success', output: '93.184.216.34', started_at: '2026-06-30 13:55:00', finished_at: '2026-06-30 13:55:01', task_name: 'DNS resolution probe', executor_type: 'local', duration: 420, retry_count: 0 },
  { id: 22, task_id: 10, status: 'success', output: 'TCP connect success', started_at: '2026-06-30 13:58:00', finished_at: '2026-06-30 13:58:01', task_name: 'PostgreSQL port listen check', executor_type: 'tcp', duration: 600, retry_count: 0 },
  { id: 23, task_id: 11, status: 'running', output: '', started_at: '2026-06-30 14:00:00', task_name: 'Intranet gateway ICMP probe', executor_type: 'icmp', duration: 0, retry_count: 0 },
  { id: 24, task_id: 12, status: 'success', output: 'synced config to 6 nodes', started_at: '2026-06-27 15:00:00', finished_at: '2026-06-27 15:00:02', task_name: 'Config file sync', executor_type: 'local', duration: 2100, retry_count: 0 },
  { id: 25, task_id: 13, status: 'failed', output: '', error: 'HTTP 503 Service Unavailable', started_at: '2026-06-30 13:50:00', finished_at: '2026-06-30 13:50:03', task_name: 'Payment callback availability check', executor_type: 'http', duration: 3000, status_code: 503, retry_count: 3 },
  { id: 26, task_id: 13, status: 'success', output: 'HTTP 200, response time: 0.8s', started_at: '2026-06-30 13:40:00', finished_at: '2026-06-30 13:40:01', task_name: 'Payment callback availability check', executor_type: 'http', duration: 800, status_code: 200, retry_count: 0 },
  { id: 27, task_id: 14, status: 'success', output: 'TCP connect success', started_at: '2026-06-30 13:58:30', finished_at: '2026-06-30 13:58:31', task_name: 'Kafka broker port check', executor_type: 'tcp', duration: 700, retry_count: 0 },
  { id: 28, task_id: 3, status: 'running', output: '', started_at: '2026-06-30 14:00:30', task_name: 'CDN edge ICMP probe', executor_type: 'icmp', duration: 0, retry_count: 0 },
]

/** Extract numeric ID from URL (compatible with multi-segment paths like /tasks/:id/trigger) */
function extractId(url: string): number {
  const match = url.match(/\/(\d+)(?:\/|$)/)
  return match ? Number(match[1]) : 0
}

/** Extract task ID from sub-resource URL like /api/v1/tasks/:id/executions */
function extractTaskId(url: string): number {
  const match = url.match(/\/tasks\/(\d+)\/executions/)
  return match ? Number(match[1]) : 0
}

/** Extract execution ID from sub-resource URL like /api/v1/tasks/:id/executions/:execId */
function extractExecId(url: string): number {
  const match = url.match(/\/executions\/(\d+)/)
  return match ? Number(match[1]) : 0
}

/** Read a plain object body field as a config object, defaulting to {} */
function asConfig(value: unknown): Record<string, unknown> {
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    return { ...(value as Record<string, unknown>) }
  }
  return {}
}

/** Read a body field as a string array, dropping non-string entries */
function asStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is string => typeof item === 'string')
}

export default [
  // Task list (filters: page/page_size/group/tags)
  {
    url: '/api/v1/tasks',
    method: 'get',
    response: ({ query }: { query: Record<string, string> }) => {
      const page = Number(query?.page) || 1
      const size = Number(query?.page_size) || 20
      let filtered = [...mockTasks]
      if (query?.group) {
        const group = query.group.trim().toLowerCase()
        filtered = filtered.filter((t) => (t.group || '').trim().toLowerCase() === group)
      }
      if (query?.tags) {
        const wanted = query.tags
          .split(',')
          .map((tag) => tag.trim().toLowerCase())
          .filter(Boolean)
        if (wanted.length > 0) {
          filtered = filtered.filter((t) => (t.tags || []).some((tag) => wanted.includes(tag.toLowerCase())))
        }
      }
      const total = filtered.length
      const start = (page - 1) * size
      const items = filtered.slice(start, start + size)
      return { code: 0, message: 'success', data: { items, total, page, page_size: size } }
    },
  },
  // Create task (body: TaskCreateParams in snake_case)
  {
    url: '/api/v1/tasks',
    method: 'post',
    response: ({ body }: { body: Record<string, unknown> }) => {
      const now = new Date().toISOString()
      const task: TaskSeed = {
        id: Math.max(0, ...mockTasks.map((t) => t.id)) + 1,
        name: String(body.name ?? ''),
        description: String(body.description ?? ''),
        executor: String(body.executor ?? 'local'),
        schedule: String(body.schedule ?? ''),
        enabled: body.enabled === undefined ? true : Boolean(body.enabled),
        config: asConfig(body.config),
        group: String(body.group ?? ''),
        tags: asStringArray(body.tags),
        retry_policy: String(body.retry_policy ?? 'fixed'),
        concurrency: Number(body.concurrency ?? 0) || 0,
        created_at: now,
        updated_at: now,
      }
      mockTasks.push(task)
      return { code: 0, message: 'success', data: task }
    },
  },
  // Execution stats for an optional time range.
  // NOTE: must stay above /tasks/:id — the mock server matches routes in
  // array order and ":id" would otherwise capture the literal "stats".
  {
    url: '/api/v1/tasks/stats',
    method: 'get',
    response: ({ query }: { query: Record<string, string> }) => {
      const fromTs = query?.from ? new Date(query.from).getTime() : 0
      const toTs = query?.to ? new Date(query.to).getTime() : 0
      const inRange = mockLogs.filter((l) => {
        const ts = new Date(l.started_at.replace(' ', 'T')).getTime()
        if (fromTs && ts < fromTs) return false
        if (toTs && ts > toTs) return false
        return true
      })
      const success = inRange.filter((l) => l.status === 'success').length
      const failed = inRange.filter((l) => l.status === 'failed').length
      const total = inRange.length
      const avgDuration = total
        ? Math.round(inRange.reduce((sum, l) => sum + (l.duration || 0), 0) / total)
        : 0
      return {
        code: 0,
        message: 'success',
        data: {
          total_executions: total,
          success_count: success,
          failure_count: failed,
          success_rate: total ? Math.round((success / total) * 10000) / 100 : 0,
          average_duration_ms: avgDuration,
        },
      }
    },
  },
  // Task detail
  {
    url: '/api/v1/tasks/:id',
    method: 'get',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const task = mockTasks.find((t) => t.id === id)
      return { code: 0, message: 'success', data: task || mockTasks[0] }
    },
  },
  // Update task (body: TaskUpdateParams in snake_case)
  {
    url: '/api/v1/tasks/:id',
    method: 'put',
    response: ({ url, body }: { url: string; body: Record<string, unknown> }) => {
      const id = extractId(url)
      const task = mockTasks.find((t) => t.id === id) || mockTasks[0]
      Object.assign(task, body, { updated_at: new Date().toISOString() })
      return { code: 0, message: 'success', data: task }
    },
  },
  // Delete task
  {
    url: '/api/v1/tasks/:id',
    method: 'delete',
    response: () => ({ code: 0, message: 'success', data: null }),
  },
  // Trigger task
  {
    url: '/api/v1/tasks/:id/trigger',
    method: 'post',
    response: () => ({ code: 0, message: 'success', data: null }),
  },
  // Pause task (remove from scheduling wheel, config preserved)
  {
    url: '/api/v1/tasks/:id/pause',
    method: 'post',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const task = mockTasks.find((t) => t.id === id)
      if (task) task.enabled = false
      return { code: 0, message: 'success', data: null }
    },
  },
  // Resume a paused task
  {
    url: '/api/v1/tasks/:id/resume',
    method: 'post',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const task = mockTasks.find((t) => t.id === id)
      if (task) task.enabled = true
      return { code: 0, message: 'success', data: null }
    },
  },
  // Copy a task (clone configuration into a new task)
  {
    url: '/api/v1/tasks/:id/copy',
    method: 'post',
    response: ({ url, body }: { url: string; body: Record<string, unknown> }) => {
      const id = extractId(url)
      const source = mockTasks.find((t) => t.id === id) || mockTasks[0]
      const now = new Date().toISOString()
      const customName = typeof body.name === 'string' ? body.name.trim() : ''
      const clone: TaskSeed = {
        ...source,
        id: Math.max(0, ...mockTasks.map((t) => t.id)) + 1,
        name: customName || `${source.name} (copy)`,
        enabled: false,
        created_at: now,
        updated_at: now,
      }
      delete clone.run_id
      mockTasks.push(clone)
      return { code: 0, message: 'success', data: clone }
    },
  },
  // Execution log list (sub-resource: /tasks/:id/executions).
  // Filters: page/page_size/task_name/executor/status; taskId=0 means all tasks.
  {
    url: '/api/v1/tasks/:id/executions',
    method: 'get',
    response: ({ url, query }: { url: string; query: Record<string, string> }) => {
      const taskId = extractTaskId(url)
      const page = Number(query?.page) || 1
      const size = Number(query?.page_size) || 20
      let filtered = [...mockLogs]
      if (taskId > 0) {
        filtered = filtered.filter((l) => l.task_id === taskId)
      }
      if (query?.task_name) {
        const keyword = query.task_name.trim().toLowerCase()
        filtered = filtered.filter((l) => (l.task_name || '').toLowerCase().includes(keyword))
      }
      if (query?.executor) {
        filtered = filtered.filter((l) => l.executor_type === query.executor)
      }
      if (query?.status) {
        filtered = filtered.filter((l) => l.status === query.status)
      }
      const total = filtered.length
      const start = (page - 1) * size
      const items = filtered.slice(start, start + size)
      return { code: 0, message: 'success', data: { items, total, page, page_size: size } }
    },
  },
  // Log detail (sub-resource: /tasks/:id/executions/:execId)
  {
    url: '/api/v1/tasks/:id/executions/:execId',
    method: 'get',
    response: ({ url }: { url: string }) => {
      const execId = extractExecId(url)
      const log = mockLogs.find((l) => l.id === execId)
      return { code: 0, message: 'success', data: log || mockLogs[0] }
    },
  },
] as MockMethod[]
