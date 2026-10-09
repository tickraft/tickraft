// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * Shared injection keys for provide/inject pattern.
 *
 * Used to break circular dependency: core DefaultLayout needs baseMenus
 * from features, but features depends on core. App root provides baseMenus
 * via app.provide(), DefaultLayout injects it.
 */
import type { InjectionKey } from 'vue'
import type { MenuItem } from './types/menu'

/**
 * Injection key for base menus.
 *
 * App root provides readonly MenuItem[] via `app.provide(BASE_MENUS_KEY, baseMenus)`.
 * DefaultLayout injects it with an empty-array fallback for standalone usage.
 */
export const BASE_MENUS_KEY: InjectionKey<readonly MenuItem[]> = Symbol('baseMenus')
