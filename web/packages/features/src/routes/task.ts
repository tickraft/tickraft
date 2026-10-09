// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { RouteRecordRaw } from 'vue-router'
import { DefaultLayout } from '@tickraft/core'

const routes: RouteRecordRaw[] = [
  {
    path: '/task',
    component: DefaultLayout,
    children: [
      {
        path: 'list',
        name: 'TaskList',
        component: () => import('../views/task/task/list/List.vue'),
        meta: { title: 'task.task.list.title' },
      },
      {
        path: 'create',
        name: 'TaskCreate',
        component: () => import('../views/task/task/create/Create.vue'),
        meta: { title: 'task.task.create.title' },
      },
      {
        path: 'detail/:id',
        name: 'TaskDetail',
        component: () => import('../views/task/task/detail/Detail.vue'),
        meta: { title: 'task.task.detail.title' },
      },
      {
        path: 'edit/:id',
        name: 'TaskEdit',
        component: () => import('../views/task/task/edit/Edit.vue'),
        meta: { title: 'task.task.create.editTitle' },
      },
      {
        path: 'log/list',
        name: 'LogList',
        component: () => import('../views/task/log/list/List.vue'),
        meta: { title: 'task.log.list.title' },
      },
      {
        path: 'log/detail/:taskId/:execId',
        name: 'LogDetail',
        component: () => import('../views/task/log/detail/Detail.vue'),
        meta: { title: 'task.log.detail.title' },
      },
    ],
  },
]

export default routes
