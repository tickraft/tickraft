// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { RouteRecordRaw } from 'vue-router'
import { DefaultLayout } from '@tickraft/core'

const routes: RouteRecordRaw[] = [
  // Legacy short URL kept as redirect so existing bookmarks keep working
  { path: '/dashboard', redirect: '/dashboard/overview' },
  {
    path: '/dashboard/overview',
    component: DefaultLayout,
    children: [
      {
        path: '',
        name: 'Dashboard',
        component: () => import('../views/dashboard/overview/index/Index.vue'),
        meta: { title: 'common.app.title' },
      },
    ],
  },
]

export default routes
