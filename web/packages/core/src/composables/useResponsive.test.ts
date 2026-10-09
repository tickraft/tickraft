// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useResponsive } from './useResponsive'

describe('useResponsive', () => {
  const originalInnerWidth = window.innerWidth

  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    Object.defineProperty(window, 'innerWidth', {
      writable: true,
      configurable: true,
      value: originalInnerWidth,
    })
  })

  function setViewportWidth(width: number): void {
    Object.defineProperty(window, 'innerWidth', {
      writable: true,
      configurable: true,
      value: width,
    })
  }

  it('returns lg breakpoint for 1280px viewport', () => {
    setViewportWidth(1280)
    const { currentBreakpoint } = useResponsive()
    expect(currentBreakpoint.value).toBe('lg')
  })

  it('returns xl breakpoint for 1536px viewport', () => {
    setViewportWidth(1536)
    const { currentBreakpoint } = useResponsive()
    expect(currentBreakpoint.value).toBe('xl')
  })

  it('returns md breakpoint for 1024px viewport', () => {
    setViewportWidth(1024)
    const { currentBreakpoint } = useResponsive()
    expect(currentBreakpoint.value).toBe('md')
  })

  it('returns sm breakpoint for 768px viewport', () => {
    setViewportWidth(768)
    const { currentBreakpoint } = useResponsive()
    expect(currentBreakpoint.value).toBe('sm')
  })

  it('returns xs breakpoint for 767px viewport', () => {
    setViewportWidth(767)
    const { currentBreakpoint } = useResponsive()
    expect(currentBreakpoint.value).toBe('xs')
  })

  it('returns xs breakpoint for 0px viewport', () => {
    setViewportWidth(0)
    const { currentBreakpoint } = useResponsive()
    expect(currentBreakpoint.value).toBe('xs')
  })

  it('isMobile is true for xs and sm breakpoints', () => {
    setViewportWidth(500)
    const { isMobile } = useResponsive()
    expect(isMobile.value).toBe(true)

    setViewportWidth(900)
    // Need to create a new instance since onMounted won't re-run
    const { isMobile: isMobileSm } = useResponsive()
    expect(isMobileSm.value).toBe(true)
  })

  it('isDesktop is true for md, lg, xl breakpoints', () => {
    setViewportWidth(1100)
    const { isDesktop } = useResponsive()
    expect(isDesktop.value).toBe(true)

    setViewportWidth(1400)
    const { isDesktop: isDesktopLg } = useResponsive()
    expect(isDesktopLg.value).toBe(true)

    setViewportWidth(2000)
    const { isDesktop: isDesktopXl } = useResponsive()
    expect(isDesktopXl.value).toBe(true)
  })

  it('isMobile is false for desktop breakpoints', () => {
    setViewportWidth(1400)
    const { isMobile } = useResponsive()
    expect(isMobile.value).toBe(false)
  })

  it('isDesktop is false for mobile breakpoints', () => {
    setViewportWidth(500)
    const { isDesktop } = useResponsive()
    expect(isDesktop.value).toBe(false)
  })
})
