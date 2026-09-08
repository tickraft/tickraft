// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// common namespace is provided by @tickraft/core; business namespaces are provided by features.
import { common, type Messages } from '@tickraft/core'

// zh-Hans business locale packs
import zhHansAuth from './locales/zh-Hans/auth.json'
import zhHansTask from './locales/zh-Hans/task.json'
import zhHansTelemetry from './locales/zh-Hans/telemetry.json'
import zhHansPrism from './locales/zh-Hans/prism.json'
import zhHansSystem from './locales/zh-Hans/system.json'
import zhHansMenu from './locales/zh-Hans/menu.json'
import zhHansDashboard from './locales/zh-Hans/dashboard.json'
import zhHansAsset from './locales/zh-Hans/asset.json'
import zhHansStatus from './locales/zh-Hans/status.json'

// en-US business locale packs
import enUSAuth from './locales/en-US/auth.json'
import enUSTask from './locales/en-US/task.json'
import enUSTelemetry from './locales/en-US/telemetry.json'
import enUSPrism from './locales/en-US/prism.json'
import enUSSystem from './locales/en-US/system.json'
import enUSMenu from './locales/en-US/menu.json'
import enUSDashboard from './locales/en-US/dashboard.json'
import enUSAsset from './locales/en-US/asset.json'
import enUSStatus from './locales/en-US/status.json'

/**
 * Open-source base locale packs.
 *
 * Contains only open-source zh-Hans/en-US messages: common namespace from @tickraft/core,
 * business namespaces (auth/asset/task/telemetry/prism/system/menu/dashboard) from features.
 * Extension merges via `createI18n(mergeMessages(baseMessages, extensionMessages))`.
 */
export const baseMessages = {
  'zh-Hans': {
    common: common['zh-Hans'],
    auth: zhHansAuth,
    asset: zhHansAsset,
    task: zhHansTask,
    telemetry: zhHansTelemetry,
    prism: zhHansPrism,
    system: zhHansSystem,
    menu: zhHansMenu,
    dashboard: zhHansDashboard,
    status: zhHansStatus,
  },
  'en-US': {
    common: common['en-US'],
    auth: enUSAuth,
    asset: enUSAsset,
    task: enUSTask,
    telemetry: enUSTelemetry,
    prism: enUSPrism,
    system: enUSSystem,
    menu: enUSMenu,
    dashboard: enUSDashboard,
    status: enUSStatus,
  },
} as Messages
