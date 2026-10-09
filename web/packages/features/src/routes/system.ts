// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { RouteRecordRaw } from 'vue-router'
import { DefaultLayout } from '@tickraft/core'

const routes: RouteRecordRaw[] = [
  {
    path: '/system',
    component: DefaultLayout,
    children: [
      // Backward-compatible redirects for previous short URLs
      { path: 'settings', redirect: 'settings/general' },
      { path: 'apikey', redirect: 'api-keys/list' },
      {
        path: 'settings/general',
        name: 'SystemSettings',
        component: () => import('../views/system/settings/general/Settings.vue'),
        meta: { title: 'system.settings.title' },
      },
      {
        path: 'api-keys/list',
        name: 'ApiKeys',
        component: () => import('../views/system/api-keys/list/List.vue'),
        meta: { title: 'system.apiKeys.title' },
      },
      {
        path: 'info/overview',
        name: 'SystemInfo',
        component: () => import('../views/system/info/overview/Info.vue'),
        meta: { title: 'system.info.title' },
      },
    ],
  },
]

export default routes
