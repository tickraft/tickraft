// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { createRouter as createVueRouter, createWebHistory, type Router, type RouteRecordRaw } from 'vue-router'
import { getToken } from '../utils/request'

/**
 * Router instance factory.
 *
 * Callers (open-source `main.ts` or extension `main.ts`) pass the fully merged route
 * table; the core uniformly mounts the navigation guard: routes declared with
 * `meta.public` bypass the token check; all other routes require a token and
 * redirect to `/login` when absent.
 *
 * @param routes - full route record array (typically `[...baseRoutes, ...extensionRoutes]`)
 * @returns Router instance with navigation guards configured
 */
export function createRouter(routes: RouteRecordRaw[]): Router {
  const router = createVueRouter({
    history: createWebHistory(),
    routes,
  })

  /** Navigation guard: bypass token check for public routes, else require token. */
  router.beforeEach((to) => {
    const token = getToken()
    if (to.meta.public) {
      return true
    }
    if (!token) {
      return '/login'
    }
    return true
  })

  return router
}
