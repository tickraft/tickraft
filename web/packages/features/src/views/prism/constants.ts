// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { AlertCondition, AlertSeverity } from '../../api/prism'

/** Mapping of condition operators to symbols */
export const CONDITION_SYMBOL: Record<AlertCondition, string> = {
  gt: '>',
  lt: '<',
  eq: '=',
  gte: '≥',
  lte: '≤',
}

/** Condition dropdown options */
export const CONDITION_OPTIONS: Array<{ value: AlertCondition; labelKey: string }> = [
  { value: 'gt', labelKey: 'prism.condition.gt' },
  { value: 'lt', labelKey: 'prism.condition.lt' },
  { value: 'eq', labelKey: 'prism.condition.eq' },
  { value: 'gte', labelKey: 'prism.condition.gte' },
  { value: 'lte', labelKey: 'prism.condition.lte' },
]

/** Severity dropdown options */
export const SEVERITY_OPTIONS: Array<{ value: AlertSeverity; labelKey: string }> = [
  { value: 'critical', labelKey: 'prism.severity.critical' },
  { value: 'warning', labelKey: 'prism.severity.warning' },
  { value: 'info', labelKey: 'prism.severity.info' },
]

/** el-tag type for each severity level */
export const SEVERITY_TAG_TYPE: Record<AlertSeverity, 'danger' | 'warning' | 'info'> = {
  critical: 'danger',
  warning: 'warning',
  info: 'info',
}

/** Metric dropdown options (common metrics; allow-create is used as a fallback during editing) */
export const METRIC_OPTIONS: Array<{ value: string; label: string }> = [
  { value: 'cpu_usage', label: 'cpu_usage' },
  { value: 'memory_usage', label: 'memory_usage' },
  { value: 'disk_usage', label: 'disk_usage' },
  { value: 'network_in', label: 'network_in' },
  { value: 'network_out', label: 'network_out' },
  { value: 'response_time', label: 'response_time' },
  { value: 'status_code', label: 'status_code' },
  { value: 'probe_latency', label: 'probe_latency' },
  { value: 'http_5xx_rate', label: 'http_5xx_rate' },
  { value: 'mem_usage', label: 'mem_usage' },
  { value: 'disk_free', label: 'disk_free' },
  { value: 'tcp_connect', label: 'tcp_connect' },
  { value: 'icmp_loss', label: 'icmp_loss' },
  { value: 'task_failed', label: 'task_failed' },
  { value: 'cert_days', label: 'cert_days' },
]

/** Parse time string to Date (supports 'YYYY-MM-DD HH:mm:ss' and ISO) */
export function parseDate(value: string): Date {
  return new Date(value.includes(' ') ? value.replace(' ', 'T') : value)
}
