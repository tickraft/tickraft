// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * Open-source base menu definitions.
 *
 * Aligned with docs/frontend/navigation-design.md sections 5.1 and 6.2.
 *
 * - Menu titles use standardized menu.* i18n keys (see features i18n menu.json)
 * - Icons use Element Plus icon component names per doc section 3.3
 * - Paths match existing vue-router route paths for correct active highlight
 * - extension injects extra menus via DefaultLayout extraMenus prop
 *
 * Only menus with corresponding views are included. Documentation-specified
 * menus without views will be added when their views are implemented.
 */
import type { MenuItem } from '@tickraft/core'

export const baseMenus: readonly MenuItem[] = [
  {
    path: '/dashboard/overview',
    title: 'menu.dashboard',
    icon: 'Odometer',
  },
  {
    path: '/asset',
    title: 'menu.asset.title',
    icon: 'Box',
    children: [
      { path: '/asset/list', title: 'asset.list.title' },
    ],
  },
  {
    path: '/telemetry',
    title: 'menu.telemetry.title',
    icon: 'Monitor',
    children: [
      { path: '/telemetry/monitor/list', title: 'telemetry.monitor.list.title' },
      { path: '/telemetry/monitor/templates', title: 'telemetry.monitor.templates.title' },
    ],
  },
  {
    path: '/task',
    title: 'menu.task.title',
    icon: 'Calendar',
    children: [
      { path: '/task/list', title: 'menu.task.task' },
      { path: '/task/log/list', title: 'menu.task.log' },
    ],
  },
  {
    path: '/prism',
    title: 'menu.prism.title',
    icon: 'Bell',
    children: [
      { path: '/prism/record/list', title: 'menu.prism.record' },
      { path: '/prism/rule/list', title: 'prism.rule.list.title' },
      { path: '/prism/templates', title: 'prism.templates.title' },
      { path: '/prism/channel/list', title: 'prism.channel.list.title' },
      { path: '/prism/contact/list', title: 'prism.contact.list.title' },
      { path: '/prism/remediation/rule/list', title: 'prism.remediation.rule.list.title' },
      { path: '/prism/remediation/records', title: 'prism.remediation.list.title' },
    ],
  },
  {
    path: '/system',
    title: 'menu.system.title',
    icon: 'Setting',
    children: [
      { path: '/system/settings/general', title: 'menu.system.basic' },
      { path: '/system/status', title: 'menu.system.status' },
      { path: '/system/api-keys/list', title: 'system.apiKeys.title' },
      { path: '/system/info/overview', title: 'menu.system.info' },
    ],
  },
] as const
