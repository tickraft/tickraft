// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * useInterval - declarative setInterval with lifecycle safety.
 *
 * The timer is created on mount and cleared on unmount, so no interval ever
 * outlives its component. Pass `immediate` to run the callback once right
 * away, and toggle `controls.active` to pause/resume without tearing down.
 */
import { onBeforeUnmount, onMounted, reactive } from 'vue'

export interface UseIntervalControls {
  /** Whether the interval is currently running */
  active: boolean
  /** Pause the interval (no-op if already paused) */
  pause: () => void
  /** Resume the interval (no-op if already running) */
  resume: () => void
  /** Re-run the callback immediately without resetting the cadence timer */
  fire: () => void
}

export interface UseIntervalOptions {
  /** Run the callback immediately on mount (default false) */
  immediate?: boolean
  /** Whether the interval should start active (default true) */
  immediateStart?: boolean
}

/**
 * Run `handler` every `delay` milliseconds with automatic cleanup.
 *
 * @param handler - callback invoked each tick
 * @param delay - interval delay in ms (must be >= 0)
 * @param options - interval options
 * @returns controls for pausing/resuming
 */
export function useInterval(
  handler: () => void,
  delay: number,
  options: UseIntervalOptions = {},
): UseIntervalControls {
  const { immediate = false, immediateStart = true } = options
  const controls = reactive({
    active: immediateStart,
    pause: () => {
      if (!controls.active) return
      controls.active = false
      clearCurrent()
    },
    resume: () => {
      if (controls.active) return
      controls.active = true
      schedule()
    },
    fire: () => {
      handler()
    },
  }) as UseIntervalControls

  let timer: ReturnType<typeof setInterval> | null = null

  function clearCurrent(): void {
    if (timer !== null) {
      clearInterval(timer)
      timer = null
    }
  }

  function schedule(): void {
    clearCurrent()
    if (delay < 0) return
    timer = setInterval(() => {
      handler()
    }, delay)
  }

  onMounted(() => {
    if (immediate) handler()
    if (controls.active) schedule()
  })

  onBeforeUnmount(() => {
    clearCurrent()
  })

  return controls
}
