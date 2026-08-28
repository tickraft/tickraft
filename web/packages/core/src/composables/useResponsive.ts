// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * Responsive breakpoint composable.
 *
 * Aligned with
 *
 * Breakpoint definitions:
 * - xs: < 768px (mobile, drawer sidebar)
 * - sm: 768px - 1023px (tablet, drawer sidebar)
 * - md: 1024px - 1279px (small desktop, collapsed sidebar 64px)
 * - lg: 1280px - 1535px (desktop, expanded sidebar 240px)
 * - xl: >= 1536px (large desktop, expanded sidebar 240px)
 */
import { computed, onMounted, ref, type Ref } from 'vue'
import { useEventListener } from './useEventListener'

export type Breakpoint = 'xs' | 'sm' | 'md' | 'lg' | 'xl'

const BREAKPOINTS: Record<Breakpoint, number> = {
  xs: 0,
  sm: 768,
  md: 1024,
  lg: 1280,
  xl: 1536,
}

/**
 * Resolve breakpoint from viewport width.
 */
function resolveBreakpoint(width: number): Breakpoint {
  if (width >= BREAKPOINTS.xl) return 'xl'
  if (width >= BREAKPOINTS.lg) return 'lg'
  if (width >= BREAKPOINTS.md) return 'md'
  if (width >= BREAKPOINTS.sm) return 'sm'
  return 'xs'
}

export interface UseResponsiveReturn {
  /** Current breakpoint, reactive */
  currentBreakpoint: Ref<Breakpoint>
  /** Whether viewport is mobile/tablet (drawer sidebar mode) */
  isMobile: Ref<boolean>
  /** Whether viewport is desktop (collapsed or expanded sidebar mode) */
  isDesktop: Ref<boolean>
}

/**
 * Track viewport breakpoint changes.
 *
 * Automatically adds resize listener on mount and removes on unmount.
 * Defaults to `lg` on SSR or when window is unavailable.
 */
export function useResponsive(): UseResponsiveReturn {
  const currentBreakpoint = ref<Breakpoint>(
    typeof window === 'undefined' ? 'lg' : resolveBreakpoint(window.innerWidth),
  )

  const updateBreakpoint = (): void => {
    if (typeof window === 'undefined') return
    currentBreakpoint.value = resolveBreakpoint(window.innerWidth)
  }

  onMounted(() => {
    updateBreakpoint()
  })

  // useEventListener handles attach-on-mount and detach-on-unmount automatically.
  useEventListener(window, 'resize', updateBreakpoint)

  const isMobile = computed(
    () => currentBreakpoint.value === 'xs' || currentBreakpoint.value === 'sm',
  )
  const isDesktop = computed(
    () =>
      currentBreakpoint.value === 'md' ||
      currentBreakpoint.value === 'lg' ||
      currentBreakpoint.value === 'xl',
  )

  return { currentBreakpoint, isMobile, isDesktop }
}
