// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { RouteRecordRaw } from 'vue-router'
import authRoutes from './auth'
import dashboardRoutes from './dashboard'
import assetRoutes from './asset'
import taskRoutes from './task'
import telemetryRoutes from './telemetry'
import prismRoutes from './prism'
import systemRoutes from './system'
import statusRoutes from './status'

/**
 * Open-source base routes.
 *
 * Only contains open-source routes: root redirect and seven open-source modules.
 * Extension merges via `createRouter([...baseRoutes, ...routes])`.
 */
export const baseRoutes: RouteRecordRaw[] = [
  // Redirect root to dashboard to avoid blank page when authenticated users visit /
  { path: '/', redirect: '/dashboard' },
  ...authRoutes,
  ...dashboardRoutes,
  ...assetRoutes,
  ...taskRoutes,
  ...telemetryRoutes,
  ...prismRoutes,
  ...systemRoutes,
  ...statusRoutes,
]
