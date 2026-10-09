// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { RouteRecordRaw } from 'vue-router'
import { DefaultLayout } from '@tickraft/core'

/**
 * Status page module routes.
 *
 * - /status is the public status page: a standalone full-screen view (no
 *   DefaultLayout shell) declared with `meta.public` so the core router
 *   guard bypasses the token check. It renders the aggregated backend
 *   view and a "Powered by Tickraft" footer.
 * - /system/status is the management settings page behind the normal
 *   authenticated layout.
 */
const routes: RouteRecordRaw[] = [
  {
    path: '/status',
    name: 'PublicStatus',
    component: () => import('../views/status/public/PublicStatus.vue'),
    meta: { title: 'status.public.title', public: true },
  },
  {
    path: '/system/status',
    component: DefaultLayout,
    children: [
      {
        path: '',
        name: 'SystemStatus',
        component: () => import('../views/system/status/settings/Settings.vue'),
        meta: { title: 'status.settings.title' },
      },
    ],
  },
]

export default routes
