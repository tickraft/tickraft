// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { RouteRecordRaw } from 'vue-router'
import { DefaultLayout } from '@tickraft/core'

const routes: RouteRecordRaw[] = [
  {
    path: '/asset',
    component: DefaultLayout,
    children: [
      {
        path: 'list',
        name: 'AssetList',
        component: () => import('../views/asset/list/List.vue'),
        meta: { title: 'asset.list.title' },
      },
      {
        path: 'create',
        name: 'AssetCreate',
        component: () => import('../views/asset/create/Create.vue'),
        meta: { title: 'asset.create.title' },
      },
      {
        path: 'detail/:id',
        name: 'AssetDetail',
        component: () => import('../views/asset/detail/Detail.vue'),
        meta: { title: 'asset.detail.title' },
      },
      {
        path: 'edit/:id',
        name: 'AssetEdit',
        component: () => import('../views/asset/create/Create.vue'),
        meta: { title: 'asset.create.editTitle' },
      },
    ],
  },
]

export default routes
