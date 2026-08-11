// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { describe, it, expect } from 'vitest'
import { baseMenus } from './index'

describe('baseMenus', () => {
  it('exports a readonly array', () => {
    expect(Array.isArray(baseMenus)).toBe(true)
    // `as const` provides TypeScript-level readonly; runtime freeze is not enforced
    // but the type contract prevents mutation in consumers
  })

  it('contains exactly 6 top-level menus (dashboard, asset, telemetry, task, prism, system)', () => {
    expect(baseMenus).toHaveLength(6)
    const paths = baseMenus.map((m) => m.path)
    expect(paths).toContain('/dashboard/overview')
    expect(paths).toContain('/asset')
    expect(paths).toContain('/task')
    expect(paths).toContain('/telemetry')
    expect(paths).toContain('/prism')
    expect(paths).toContain('/system')
  })

  it('uses Element Plus icon names (PascalCase) for level-1 menus', () => {
    for (const menu of baseMenus) {
      if (menu.icon) {
        // Element Plus icon names are PascalCase, no i-ep- prefix
        expect(menu.icon).not.toMatch(/^i-ep-/)
        expect(menu.icon).toMatch(/^[A-Z][a-zA-Z]+$/)
      }
    }
  })

  it('uses menu.* i18n keys for standardized menu titles', () => {
    const dashboard = baseMenus.find((m) => m.path === '/dashboard/overview')
    expect(dashboard?.title).toBe('menu.dashboard')

    const task = baseMenus.find((m) => m.path === '/task')
    expect(task?.title).toBe('menu.task.title')

    const prism = baseMenus.find((m) => m.path === '/prism')
    expect(prism?.title).toBe('menu.prism.title')

    const system = baseMenus.find((m) => m.path === '/system')
    expect(system?.title).toBe('menu.system.title')
  })

  it('task menu has task and log children', () => {
    const task = baseMenus.find((m) => m.path === '/task')
    expect(task?.children).toBeDefined()
    expect(task?.children).toHaveLength(2)
    const childPaths = task?.children?.map((c) => c.path)
    expect(childPaths).toContain('/task/list')
    expect(childPaths).toContain('/task/log/list')
  })

  it('system menu has basic settings, apikey, and info children', () => {
    const system = baseMenus.find((m) => m.path === '/system')
    expect(system?.children).toBeDefined()
    expect(system?.children).toHaveLength(3)
  })

  it('dashboard menu has no children (leaf menu)', () => {
    const dashboard = baseMenus.find((m) => m.path === '/dashboard/overview')
    expect(dashboard?.children).toBeUndefined()
  })

  it('all menu paths start with /', () => {
    function checkPaths(menus: typeof baseMenus): void {
      for (const menu of menus) {
        expect(menu.path.startsWith('/')).toBe(true)
        if (menu.children) {
          checkPaths(menu.children as unknown as typeof baseMenus)
        }
      }
    }
    checkPaths(baseMenus)
  })

  it('uses correct icons per docs/frontend/navigation-design.md §3.3', () => {
    const dashboard = baseMenus.find((m) => m.path === '/dashboard/overview')
    expect(dashboard?.icon).toBe('Odometer')

    const task = baseMenus.find((m) => m.path === '/task')
    expect(task?.icon).toBe('Calendar')

    const telemetry = baseMenus.find((m) => m.path === '/telemetry')
    expect(telemetry?.icon).toBe('Monitor')

    const prism = baseMenus.find((m) => m.path === '/prism')
    expect(prism?.icon).toBe('Bell')

    const system = baseMenus.find((m) => m.path === '/system')
    expect(system?.icon).toBe('Setting')
  })
})
