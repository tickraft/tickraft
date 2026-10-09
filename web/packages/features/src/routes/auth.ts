// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { RouteRecordRaw } from 'vue-router'

/**
 * Authentication module routes.
 *
 * Page routes use clean top-level paths (/login, /change-password) with no
 * /auth/session prefix. The 4-level file structure is retained under
 * views/auth/session/<page>/<Component>.vue; only the URL is flattened.
 *
 * - Login uses split layout (left brand area + right form area)
 * - ChangePassword uses center layout (centered card)
 *
 * Both routes declare `meta.public` so the core router guard bypasses the token
 * check for them.
 */
const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/auth/session/login/Login.vue'),
    meta: { title: 'auth.login.title', public: true },
  },
  {
    path: '/change-password',
    name: 'ChangePassword',
    component: () => import('../views/auth/session/change-password/ChangePassword.vue'),
    meta: { title: 'auth.changePassword.title', public: true },
  },
]

export default routes
