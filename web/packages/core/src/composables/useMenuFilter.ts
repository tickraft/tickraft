// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * Menu feature-flag filtering composable.
 *
 * Aligned with
 *
 * Filtering rules:
 * - Menu items without `feature` field are always visible (open-source base capabilities).
 * - Menu items with `feature` field are visible only when the feature exists in the feature list.
 * - Parent menus with no remaining children after filtering are automatically hidden.
 * - `hidden: true` menu items are always excluded from sidebar rendering.
 * - Returns new arrays, never mutates the original menu constants.
 */
import type { MenuItem } from '../types/menu'

/**
 * Filter menu items by feature flag list.
 *
 * @param menus - source menu items (readonly, never mutated)
 * @param features - feature flag identifiers granted by backend
 * @returns filtered menu items as a new array
 */
export function filterMenusByFeature(
  menus: readonly MenuItem[],
  features: readonly string[],
): MenuItem[] {
  return menus
    .map((menu) => filterMenuItem(menu, features))
    .filter((menu): menu is MenuItem => menu !== null)
}

/**
 * Filter a single menu item.
 *
 * Returns null when the item should be hidden, otherwise returns the item
 * (with recursively filtered children when applicable).
 */
function filterMenuItem(
  menu: MenuItem,
  features: readonly string[],
): MenuItem | null {
  // Hidden items (detail/edit pages) are always excluded from sidebar
  if (menu.hidden) {
    return null
  }

  // Item with feature flag not present in granted features is hidden
  if (menu.feature && !features.includes(menu.feature)) {
    return null
  }

  // Recursively filter children; parent auto-hidden when no children remain
  if (menu.children && menu.children.length > 0) {
    const filteredChildren = filterMenusByFeature(menu.children, features)
    if (filteredChildren.length === 0) {
      return null
    }
    return { ...menu, children: filteredChildren }
  }

  return menu
}
