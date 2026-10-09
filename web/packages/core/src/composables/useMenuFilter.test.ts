// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { describe, it, expect } from 'vitest'
import { filterMenusByFeature } from './useMenuFilter'
import type { MenuItem } from '../types/menu'

describe('filterMenusByFeature', () => {
  const menus: readonly MenuItem[] = [
    { path: '/dashboard', title: 'Dashboard' },
    {
      path: '/task',
      title: 'Task',
      children: [
        { path: '/task/list', title: 'Tasks' },
        { path: '/task/log', title: 'Logs' },
      ],
    },
    {
      path: '/team',
      title: 'Team',
      feature: 'team_management',
      children: [
        { path: '/team/member', title: 'Members', feature: 'team_management' },
        { path: '/team/audit', title: 'Audit', feature: 'audit_log' },
      ],
    },
    {
      path: '/system',
      title: 'System',
      children: [
        { path: '/system/settings', title: 'Settings' },
        { path: '/system/license', title: 'License', feature: 'license_management' },
      ],
    },
    {
      path: '/hidden-page',
      title: 'Hidden',
      hidden: true,
    },
  ] as const

  it('returns all non-feature, non-hidden menus when features list is empty', () => {
    const result = filterMenusByFeature(menus, [])
    // Dashboard, Task (no feature), System (no feature on parent)
    // Hidden page should be excluded
    expect(result).toHaveLength(3)
    expect(result.find((m) => m.path === '/dashboard')).toBeDefined()
    expect(result.find((m) => m.path === '/task')).toBeDefined()
    expect(result.find((m) => m.path === '/system')).toBeDefined()
    expect(result.find((m) => m.path === '/hidden-page')).toBeUndefined()
  })

  it('hides menus with feature flag not in features list', () => {
    const result = filterMenusByFeature(menus, [])
    // Team menu has feature 'team_management', should be hidden
    expect(result.find((m) => m.path === '/team')).toBeUndefined()
  })

  it('shows menus with feature flag present in features list', () => {
    const result = filterMenusByFeature(menus, ['team_management'])
    const team = result.find((m) => m.path === '/team')
    expect(team).toBeDefined()
    // Only /team/member should remain (has feature 'team_management'),
    // /team/audit has feature 'audit_log' which is not granted
    expect(team?.children).toHaveLength(1)
    expect(team?.children?.[0].path).toBe('/team/member')
  })

  it('shows all feature-gated children when all features granted', () => {
    const result = filterMenusByFeature(menus, ['team_management', 'audit_log'])
    const team = result.find((m) => m.path === '/team')
    expect(team?.children).toHaveLength(2)
  })

  it('filters child items by feature flag independently', () => {
    const result = filterMenusByFeature(menus, [])
    const system = result.find((m) => m.path === '/system')
    // /system/settings has no feature, should remain
    // /system/license has feature 'license_management', should be filtered
    expect(system?.children).toHaveLength(1)
    expect(system?.children?.[0].path).toBe('/system/settings')
  })

  it('hides parent menu when all children are filtered out', () => {
    const menusWithAllFeatureChildren: MenuItem[] = [
      {
        path: '/extension',
        title: 'Extension',
        children: [
          { path: '/extension/a', title: 'A', feature: 'feature_a' },
          { path: '/extension/b', title: 'B', feature: 'feature_b' },
        ],
      },
    ]
    const result = filterMenusByFeature(menusWithAllFeatureChildren, [])
    expect(result).toHaveLength(0)
  })

  it('does not mutate the original menus array', () => {
    const original = [...menus]
    filterMenusByFeature(menus, ['team_management'])
    // Original array should be unchanged
    expect(menus).toEqual(original)
  })

  it('returns empty array for empty input', () => {
    const result = filterMenusByFeature([], ['any_feature'])
    expect(result).toEqual([])
  })

  it('handles deeply nested hidden items', () => {
    const menusWithHiddenChild: MenuItem[] = [
      {
        path: '/parent',
        title: 'Parent',
        children: [
          { path: '/parent/visible', title: 'Visible' },
          { path: '/parent/hidden', title: 'Hidden', hidden: true },
        ],
      },
    ]
    const result = filterMenusByFeature(menusWithHiddenChild, [])
    const parent = result[0]
    expect(parent.children).toHaveLength(1)
    expect(parent.children?.[0].path).toBe('/parent/visible')
  })
})
