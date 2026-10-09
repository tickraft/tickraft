// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * Executor type enum (aligned with backend handler.Task.Executor)
 *
 * Built-in executors: http / tcp / icmp / mqtt_probe / local / webhook
 * Extension executors: ssh / mysql / redis (extended by the extension)
 */
export type ExecutorType =
  | 'http'
  | 'tcp'
  | 'icmp'
  | 'mqtt_probe'
  | 'local'
  | 'webhook'
  | 'ssh'
  | 'mysql'
  | 'redis'

/**
 * Executor type metadata — aligns with the registry-derived catalog returned
 * by GET /api/v1/executors. The list reflects the runtime's actual
 * task-executable executors (the same capability predicate the backend task
 * CRUD precheck uses), so plugin-provided executors appear automatically.
 */
export interface ExecutorTypeInfo {
  /** Executor identifier (http, tcp, icmp, local, webhook, ...) */
  type: string
  /** Human-readable display name */
  name: string
  /** Short summary of the executor capability */
  description?: string
}

/**
 * Schedule type for form UI state only.
 * The backend uses a single `schedule` string; this type helps the form
 * distinguish between cron expression, fixed interval, and event-driven
 * (empty schedule) for UI rendering. It is NOT sent to the backend.
 */
export type ScheduleType = 'cron' | 'interval' | 'event'

/**
 * Retry policy enum (aligned with backend handler.Task.RetryPolicy)
 */
export type RetryPolicy = 'fixed' | 'exponential'

/**
 * Catch-up policy: what to do with schedule slots missed while the task was
 * paused or the server was down (cron/interval schedules only).
 * - skip: discard missed slots (default)
 * - once: replay only the latest missed slot
 * - all: replay all missed slots (server caps at 10)
 */
export type CatchupPolicy = 'skip' | 'once' | 'all'

/**
 * A recurring dispatch suppression window (backend task.SleepWindow).
 * `days` are ISO weekdays 0 (Sunday) .. 6 (Saturday); `start`/`end` are
 * strict HH:MM in the server's local timezone; end may be "24:00".
 * A window with start > end crosses midnight and belongs to its start day.
 */
export interface SleepWindow {
  start: string
  end: string
  days: number[]
}

/**
 * Task model (aligned with backend task.Task wire fields)
 */
export interface TaskModel {
  id: number
  name: string
  description?: string
  executorType: string
  schedule: string
  enabled: boolean
  /** Mode A: schedule only dispatches; the remote reports the outcome */
  reportStatus?: boolean
  config?: Record<string, unknown>
  /** Task-level execution timeout in seconds */
  timeout?: number
  /** Retry attempts after a failed execution; 0 disables retries */
  maxRetries?: number
  /** Delay between retry attempts in seconds */
  retryInterval?: number
  group?: string
  tags?: string[]
  runId?: string
  retryPolicy?: string
  concurrency?: number
  /** Catch-up policy for missed schedule slots (cron/interval only) */
  catchupPolicy?: string
  /** Recurring dispatch suppression windows */
  sleepWindows?: SleepWindow[]
  createdAt: string
  updatedAt: string
}

/**
 * Execution log model (aligned with backend handler.Execution)
 */
export interface LogModel {
  id: number
  taskId: number
  status: string // success, failed, running, timeout, unknown
  output: string
  error?: string
  startedAt: string
  finishedAt?: string
  /** Execution context (absent on legacy rows) */
  triggerType?: string // schedule | manual | event
  triggeredAt?: string
  node?: string
  exitCode?: number
  /** Display-only fields enriched by backend list joins */
  taskName?: string
  executorType?: string
  duration?: number
  statusCode?: number
  retryCount?: number
}

/** One day of the daily execution series (server-local calendar date, YYYY-MM-DD) */
export interface DailyStat {
  date: string
  total: number
  success: number
  failed: number
}

/**
 * Execution stats (aligned with backend handler.ExecutionStats)
 */
export interface ExecutionStats {
  totalExecutions: number
  successCount: number
  failureCount: number
  successRate: number
  averageDurationMs: number
  /** Contiguous zero-filled daily series, present only when days was requested */
  daily?: DailyStat[]
}

/**
 * Task creation parameters (aligned with backend task.Task request body)
 */
export interface TaskCreateParams {
  name: string
  description?: string
  executorType: string
  schedule: string
  enabled: boolean
  /** Mode A: schedule only dispatches; the remote reports the outcome */
  reportStatus?: boolean
  config?: Record<string, unknown>
  /** Task-level execution timeout in seconds */
  timeout?: number
  /** Retry attempts after a failed execution; 0 disables retries */
  maxRetries?: number
  /** Delay between retry attempts in seconds */
  retryInterval?: number
  group?: string
  tags?: string[]
  retryPolicy?: string
  concurrency?: number
  /**
   * Catch-up policy for missed schedule slots (cron/interval only).
   * Omitted on update to preserve the stored policy; explicit 'skip' resets.
   */
  catchupPolicy?: CatchupPolicy
  /** Omitted on update to preserve the stored windows; empty array clears. */
  sleepWindows?: SleepWindow[]
}

/**
 * Task update parameters (same fields as create; id is in the URL path)
 */
export type TaskUpdateParams = TaskCreateParams

/**
 * Task form data (shared by Create/Edit)
 *
 * Contains backend-compatible fields plus form-only UI fields used to
 * compose the `schedule` string. The form-only fields (scheduleType,
 * cronExpr, interval) are stripped by the parent component before
 * sending the API request.
 */
export interface TaskFormData {
  // Backend-compatible fields
  name: string
  description: string
  executorType: ExecutorType
  schedule: string
  config: Record<string, unknown>
  /** Task-level execution timeout in seconds */
  timeout: number
  /** Retry attempts after a failed execution; 0 disables retries */
  maxRetries: number
  /** Delay between retry attempts in seconds */
  retryInterval: number
  group: string
  tags: string[]
  enabled: boolean
  /** Mode A: schedule only dispatches; the remote reports the outcome */
  reportStatus: boolean
  retryPolicy: string
  concurrency: number
  catchupPolicy: CatchupPolicy
  sleepWindows: SleepWindow[]

  // Form-only UI fields (not sent to backend)
  scheduleType: ScheduleType
  cronExpr: string
  interval: number
}
