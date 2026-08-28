// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * Menu type definitions.
 *
 * Describes sidebar menu item data structure, shared by core base menus
 * and extension injected menus. Aligned with
 */

/**
 * MenuBadge - sidebar menu badge configuration.
 *
 * Used for unread alert count, new feature markers, system status indicators.
 */
export interface MenuBadge {
  /** Badge type */
  type: 'dot' | 'count' | 'text'
  /** Badge value: number when type=count, string when type=text */
  value?: number | string
  /** Badge color using Element Plus status colors */
  color?: 'primary' | 'success' | 'warning' | 'danger' | 'info'
  /** Whether to show animation effect (e.g. alert pulse) */
  isAnimated?: boolean
}

/**
 * MenuItem - sidebar navigation menu node.
 *
 * Supports up to two-level nesting. Extension menus share this type
 * with core base menus to ensure rendering compatibility.
 */
export interface MenuItem {
  /** Route path, must match vue-router path for correct active highlight */
  path: string
 /** Menu display title (i18n key or static text, see docs i18n) */
  title: string
  /** Element Plus icon component name, required only for level-1 menus */
  icon?: string
  /** Child menu items, up to one level of nesting */
  children?: MenuItem[]
  /** Feature flag identifier, controls visibility via backend feature list */
  feature?: string
  /** Whether to hide from sidebar (detail/edit pages etc.) */
  hidden?: boolean
  /** External link URL, opens in new tab when set */
  externalLink?: string
  /** Badge configuration (e.g. unread alert count) */
  badge?: MenuBadge
}
