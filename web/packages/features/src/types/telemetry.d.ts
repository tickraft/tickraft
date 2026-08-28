// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * Prober type enum (aligns with backend ProberRegistry)
 */
export type ProberType =
  | 'icmp'
  | 'tcp'
  | 'http'
  | 'dns'
  | 'udp'
  | 'redis'
  | 'ssh'
  | 'snmp'
  | 'database'
  | 'ssl'

/**
 * Listener type enum (aligns with backend ListenerRegistry)
 */
export type ListenerType = 'webhook' | 'syslog' | 'snmp' | 'mqtt'

/**
 * Monitor mode enum — aligns with backend MonitorPoint.Mode.
 * "active" = probed by ProberService; "passive" = receives via listener.
 */
export type MonitorMode = 'active' | 'passive'

/**
 * Monitor type enum — active points use prober types, passive points use
 * listener types. CE supports: icmp, tcp, http, udp (active), webhook (passive).
 */
export type MonitorType = ProberType | ListenerType

/**
 * Monitor point model — aligns with backend telemetry.MonitorPoint.
 * Unified model merging prober (active) and listener (passive) into a single
 * table with a Mode field.
 */
export interface MonitorPoint {
  id: number
  name: string
  description?: string
  /** Asset type (host, service, website, device) */
  assetType: string
  /** Linked asset ID; passive history/logs and active probe records
   *  are queried through the linked asset / point */
  assetId?: number
  /** Monitoring mode: "active" (probed) or "passive" (receives) */
  mode: MonitorMode
  /** Prober executor type or listener type */
  type: MonitorType
  /** Schedule: cron expression or interval string */
  schedule: string
  enabled: boolean
  /** Derived runtime status (active/inactive/error), maintained by the
   *  backend probe loop for active points; read-only */
  status?: string
  /** Type-specific configuration (host, port, url, etc.) */
  config?: Record<string, unknown>
  createdAt: string
  updatedAt: string
}

/**
 * Monitor point creation parameters — matches backend MonitorPoint request body
 * for POST /api/v1/telemetry/monitors.
 */
export interface MonitorCreateParams {
  name: string
  description?: string
  assetType: string
  /** Linked asset ID (sent as asset_id) */
  assetId?: number
  mode: MonitorMode
  type: MonitorType
  schedule: string
  enabled: boolean
  config?: Record<string, unknown>
}

/**
 * Monitor point update parameters — matches backend MonitorPoint request body
 * for PUT /api/v1/telemetry/monitors/:id. The backend replaces all fields,
 * so all required fields must be provided.
 */
export interface MonitorUpdateParams {
  name: string
  description?: string
  assetType: string
  /** Linked asset ID (sent as asset_id) */
  assetId?: number
  mode: MonitorMode
  type: MonitorType
  schedule: string
  enabled: boolean
  config?: Record<string, unknown>
}

/**
 * Monitor status response — aligns with backend monitorStatus.
 */
export interface MonitorStatus {
  id: number
  name: string
  enabled: boolean
  status: string
  /** Timestamp of the latest probe (active points with probe history) */
  lastProbeAt?: string
  /** Latest probe latency in milliseconds (active points) */
  latencyMs?: number
}

/**
 * Monitor point summary counts — aligns with backend telemetry.PointSummary
 * returned by GET /api/v1/telemetry/monitors/summary (counts span all
 * pages and tabs, unlike the former per-current-page chip computation).
 */
export interface MonitorSummary {
  active: number
  passive: number
  enabled: number
  disabled: number
}

/**
 * Prober type metadata — aligns with backend ProberType struct returned by
 * GET /api/v1/telemetry/probers.
 */
export interface ProberTypeInfo {
  /** Prober identifier (icmp, tcp, http, udp) */
  type: string
  /** Human-readable display name */
  name: string
  /** Short summary of the prober capability */
  description?: string
}

/**
 * Listener type metadata — aligns with backend ListenerType struct returned by
 * GET /api/v1/telemetry/listeners.
 */
export interface ListenerTypeInfo {
  /** Listener identifier (webhook) */
  type: string
  /** Human-readable display name */
  name: string
  /** Short summary of the listener capability */
  description?: string
}

/**
 * Monitor history entry — aligns with backend monitorHistoryEntry returned by
 * GET /api/v1/telemetry/monitors/:id/history (paginated).
 */
export interface MonitorHistoryEntry {
  /** Timestamp of the data point */
  timestamp: string
  /** Measured value */
  value: unknown
  /** Probe result status (asset vocabulary: normal/abnormal/offline/unknown);
   * empty for passive metric rows */
  status: string
  /** Name of the measured quantity: "latency_ms" for active probe rows, the
   * collected metric name for passive rows */
  metric?: string
}

/**
 * Monitor log entry — aligns with backend monitorLogEntry returned by
 * GET /api/v1/telemetry/monitors/:id/logs (paginated).
 */
export interface MonitorLog {
  /** Timestamp of the log entry */
  timestamp: string
  /** Log level (info, warning, error, etc.) */
  level: string
  /** Log message */
  message: string
}

/**
 * Metric data point (for asset metric trend)
 */
export interface MetricPoint {
  /** Time label */
  timestamp: string
  /** Average value */
  valueAvg: number
  /** Max value */
  valueMax: number
  /** Min value */
  valueMin: number
}

/**
 * Telemetry template model (pre-configured probe/monitor recipe) — aligns with
 * backend templateResponse. Field names are camelCase; the case conversion
 * layer transparently handles snake_case ↔ camelCase at the API boundary.
 */
export interface TelemetryTemplate {
  id: number
  name: string
  description: string
  category: string
  executorType: string
  config: Record<string, unknown>
  isBuiltin: boolean
  createdAt: string
  updatedAt: string
}

/**
 * Apply template parameters — matches backend applyTemplateRequest for
 * POST /api/v1/telemetry/templates/:id/apply. Both fields are optional;
 * when omitted, the template defaults are used.
 */
export interface ApplyTemplateParams {
  /** Override the monitoring point name */
  name?: string
  /** Merge into the template config to customise the resulting monitor */
  overrides?: Record<string, unknown>
}
