// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/// <reference types="unplugin-icons/types/vue" />

import type { PageParams } from './api'

/**
 * Feature flag identifier type
 */
export type FeatureFlag = string

/**
 * Feature flag list
 */
export interface FeatureFlags {
  [key: string]: boolean
}

/**
 * User info
 */
export interface UserInfo {
  id: number
  username: string
  role: string
  features: FeatureFlags
}

/**
 * Theme mode
 *
 * - `light`: light theme
 * - `dark`: dark theme
 * - `auto`: follow system theme (auto-switches based on `prefers-color-scheme`)
 */
export type ThemeMode = 'light' | 'dark' | 'auto'

/**
 * Built-in locale type
 *
 * The core ships with zh-Hans and en-US; extension can extend more locales via `registerLocale`.
 */
export type BuiltinLocale = 'zh-Hans' | 'en-US'

/**
 * Locale type
 *
 * The core ships with zh-Hans / en-US, but any BCP 47 locale code can be registered
 * via `registerLocale`.
 */
export type LocaleType = string

/**
 * Sidebar state
 */
export interface SidebarState {
  collapsed: boolean
}

/**
 * Asset status enum (aligned with backend asset.Status)
 *
 * Generic status enum, the core StatusTag component depends on this type;
 * business asset types reuse it in the features layer.
 */
export type AssetStatus = 'normal' | 'abnormal' | 'offline' | 'unknown'

/**
 * Alert status enum (aligned with backend alert.Status)
 */
export type AlertStatus = 'firing' | 'acknowledged' | 'resolved'

/**
 * Task status enum (aligned with backend task.Status)
 */
export type TaskStatus = 'pending' | 'running' | 'success' | 'failed' | 'timeout'

/**
 * Log status enum (aligned with backend log.Status)
 */
export type LogStatus = 'success' | 'failed' | 'timeout' | 'running'

/**
 * Status category, used by StatusTag component to determine color mapping
 */
export type StatusCategory = 'asset' | 'alert' | 'task' | 'log'

/**
 * Status tag size
 */
export type StatusSize = 'sm' | 'md' | 'lg'
