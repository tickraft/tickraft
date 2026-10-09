// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// ── Route aggregation ──
export { baseRoutes } from './routes'

// ── Locale messages aggregation ──
export { baseMessages } from './i18n'

// ── Menu aggregation ──
export { baseMenus } from './menus'

// ── API namespace ──
export * as assetApi from './api/asset'
export * as authApi from './api/auth'
export * as channelApi from './api/channel'
export * as contactApi from './api/contact'
export * as prismApi from './api/prism'
export * as statusApi from './api/status'
export * as systemApi from './api/system'
export * as taskApi from './api/task'
export * as telemetryApi from './api/telemetry'

// ── Business types ──
export type {
  AssetType,
  Asset,
  AssetStatus,
} from './types/asset'
export type {
  ScheduleType,
  ExecutorType,
  RetryPolicy,
  CatchupPolicy,
  SleepWindow,
  TaskModel,
  TaskCreateParams,
  TaskUpdateParams,
  TaskFormData,
  LogModel,
} from './types/task'
export type {
  ProberType,
  ListenerType,
  MonitorMode,
  MonitorType,
  MonitorPoint,
  MonitorCreateParams,
  MonitorUpdateParams,
  MonitorStatus,
  ProberTypeInfo,
  ListenerTypeInfo,
  MonitorHistoryEntry,
  MonitorLog,
  TelemetryTemplate,
  ApplyTemplateParams,
} from './types/telemetry'
