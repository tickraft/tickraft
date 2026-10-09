// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { RouteRecordRaw } from 'vue-router'
import { DefaultLayout } from '@tickraft/core'

const routes: RouteRecordRaw[] = [
  {
    path: '/telemetry',
    component: DefaultLayout,
    children: [
      {
        path: 'monitor/list',
        name: 'MonitorList',
        component: () => import('../views/telemetry/monitor/list/List.vue'),
        meta: { title: 'telemetry.monitor.list.title' },
      },
      {
        path: 'monitor/templates',
        name: 'MonitorTemplates',
        component: () => import('../views/telemetry/monitor/templates/Templates.vue'),
        meta: { title: 'telemetry.monitor.templates.title' },
      },
      {
        path: 'monitor/create',
        name: 'MonitorCreate',
        component: () => import('../views/telemetry/monitor/create/Create.vue'),
        meta: { title: 'telemetry.monitor.create.title' },
      },
      {
        path: 'monitor/detail/:id',
        name: 'MonitorDetail',
        component: () => import('../views/telemetry/monitor/detail/Detail.vue'),
        meta: { title: 'telemetry.monitor.detail.title' },
      },
      {
        path: 'monitor/edit/:id',
        name: 'MonitorEdit',
        component: () => import('../views/telemetry/monitor/create/Create.vue'),
        meta: { title: 'telemetry.monitor.create.editTitle' },
      },
    ],
  },
]

export default routes
