// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * Preset alert rule templates catalog for the Prism templates page.
 *
 * These are built-in threshold recipes (a static catalog), aligned with the
 * storyboard `alertTemplates` mock. Field names follow the project's
 * AlertRule conventions (`metric`, `condition` enum, `duration` in seconds)
 * so a template can be applied directly to the rule edit form.
 */
import type { AlertCondition, AlertSeverity } from '../../../api/prism'

/** Monitor/resource type a template applies to (scenario category) */
export type MonitorType =
  | 'host'
  | 'network'
  | 'web'
  | 'data'
  | 'cert'
  | 'custom'

/** Preset alert rule template */
export interface AlertTemplate {
  id: number
  /** Unique template key (machine-readable) */
  key: string
  /** i18n key for the display name */
  nameKey: string
  /** i18n key for the human-readable description */
  descriptionKey: string
  /** Applicable scenario / resource type */
  monitorType: MonitorType
  /** Metric key name (matches rule metric field) */
  metric: string
  /** Trigger condition operator */
  condition: AlertCondition
  /** Threshold value */
  threshold: number
  /** Duration in seconds */
  duration: number
  /** Severity level */
  severity: AlertSeverity
  /** Number of rules referencing this template (catalog metadata) */
  usage: number
}

/** Monitor type filter options (value + i18n label key) */
export const MONITOR_TYPE_OPTIONS: Array<{ value: MonitorType; labelKey: string }> = [
  { value: 'host', labelKey: 'prism.templates.monitorType.host' },
  { value: 'network', labelKey: 'prism.templates.monitorType.network' },
  { value: 'web', labelKey: 'prism.templates.monitorType.web' },
  { value: 'data', labelKey: 'prism.templates.monitorType.data' },
  { value: 'cert', labelKey: 'prism.templates.monitorType.cert' },
  { value: 'custom', labelKey: 'prism.templates.monitorType.custom' },
]

/** Built-in preset templates (aligned with storyboard alertTemplates) */
export const TEMPLATES: AlertTemplate[] = [
  {
    id: 1,
    key: 'cpu_high',
    nameKey: 'prism.templates.catalog.cpu_high.name',
    descriptionKey: 'prism.templates.catalog.cpu_high.description',
    monitorType: 'host',
    metric: 'cpu_usage',
    condition: 'gt',
    threshold: 90,
    duration: 300,
    severity: 'critical',
    usage: 2,
  },
  {
    id: 2,
    key: 'memory_high',
    nameKey: 'prism.templates.catalog.memory_high.name',
    descriptionKey: 'prism.templates.catalog.memory_high.description',
    monitorType: 'host',
    metric: 'memory_usage',
    condition: 'gt',
    threshold: 85,
    duration: 300,
    severity: 'warning',
    usage: 1,
  },
  {
    id: 3,
    key: 'disk_full',
    nameKey: 'prism.templates.catalog.disk_full.name',
    descriptionKey: 'prism.templates.catalog.disk_full.description',
    monitorType: 'host',
    metric: 'disk_usage',
    condition: 'gt',
    threshold: 90,
    duration: 600,
    severity: 'critical',
    usage: 1,
  },
  {
    id: 4,
    key: 'network_unreachable',
    nameKey: 'prism.templates.catalog.network_unreachable.name',
    descriptionKey: 'prism.templates.catalog.network_unreachable.description',
    monitorType: 'network',
    metric: 'tcp_connect',
    condition: 'eq',
    threshold: 0,
    duration: 60,
    severity: 'critical',
    usage: 3,
  },
  {
    id: 5,
    key: 'service_down',
    nameKey: 'prism.templates.catalog.service_down.name',
    descriptionKey: 'prism.templates.catalog.service_down.description',
    monitorType: 'web',
    metric: 'status_code',
    condition: 'eq',
    threshold: 0,
    duration: 60,
    severity: 'critical',
    usage: 1,
  },
  {
    id: 6,
    key: 'http_error_rate',
    nameKey: 'prism.templates.catalog.http_error_rate.name',
    descriptionKey: 'prism.templates.catalog.http_error_rate.description',
    monitorType: 'web',
    metric: 'http_5xx_rate',
    condition: 'gt',
    threshold: 5,
    duration: 180,
    severity: 'warning',
    usage: 1,
  },
  {
    id: 7,
    key: 'connection_pool',
    nameKey: 'prism.templates.catalog.connection_pool.name',
    descriptionKey: 'prism.templates.catalog.connection_pool.description',
    monitorType: 'data',
    metric: 'conn_pool_usage',
    condition: 'gt',
    threshold: 80,
    duration: 120,
    severity: 'warning',
    usage: 0,
  },
  {
    id: 8,
    key: 'cert_expiring',
    nameKey: 'prism.templates.catalog.cert_expiring.name',
    descriptionKey: 'prism.templates.catalog.cert_expiring.description',
    monitorType: 'cert',
    metric: 'cert_days',
    condition: 'lte',
    threshold: 7,
    duration: 86400,
    severity: 'warning',
    usage: 1,
  },
  {
    id: 9,
    key: 'log_keyword',
    nameKey: 'prism.templates.catalog.log_keyword.name',
    descriptionKey: 'prism.templates.catalog.log_keyword.description',
    monitorType: 'custom',
    metric: 'log_keyword_match',
    condition: 'gt',
    threshold: 0,
    duration: 60,
    severity: 'warning',
    usage: 0,
  },
  {
    id: 10,
    key: 'custom_metric',
    nameKey: 'prism.templates.catalog.custom_metric.name',
    descriptionKey: 'prism.templates.catalog.custom_metric.description',
    monitorType: 'custom',
    metric: 'custom_metric',
    condition: 'gte',
    threshold: 100,
    duration: 300,
    severity: 'info',
    usage: 0,
  },
]

/** Find a preset template by id */
export function findTemplate(id: number): AlertTemplate | undefined {
  return TEMPLATES.find((t) => t.id === id)
}
