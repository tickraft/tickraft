// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * useFocusRestore - save and restore focus for overlay components.
 *
 * When a dialog/drawer opens, the element that had focus (the trigger) is
 * recorded. When the overlay closes, focus is returned to that element so
 * screen-reader and keyboard users resume where they left off.
 *
 * Element Plus's el-dialog / el-drawer already trap Tab focus inside the
 * overlay via their internal ElFocusTrap (enabled by aria-modal). This
 * composable complements that by handling focus *restoration* on close,
 * which Element Plus does not guarantee for the triggering element.
 */
import { ref, type Ref } from 'vue'

export interface UseFocusRestoreReturn {
  /** The element that had focus when the overlay opened (readonly) */
  triggerElement: Readonly<Ref<HTMLElement | null>>
  /** Call on overlay open — records the currently focused element */
  saveFocus: () => void
  /** Call on overlay closed — restores focus to the saved element */
  restoreFocus: () => void
  /** Clear the saved trigger without restoring focus */
  clear: () => void
}

/**
 * Track and restore focus for an overlay component.
 */
export function useFocusRestore(): UseFocusRestoreReturn {
  const triggerElement = ref<HTMLElement | null>(null)

  function saveFocus(): void {
    if (typeof document === 'undefined') return
    const active = document.activeElement
    triggerElement.value = active instanceof HTMLElement ? active : null
  }

  function restoreFocus(): void {
    const el = triggerElement.value
    if (!el) return
    // Only restore if the element is still connected to the DOM and focusable.
    if (el.isConnected) {
      // Use requestAnimationFrame so the overlay has fully unmounted and the
      // trigger is visible before we move focus back.
      requestAnimationFrame(() => {
        try {
          el.focus()
        } catch {
          // Element may no longer be focusable; ignore silently.
        }
      })
    }
    triggerElement.value = null
  }

  function clear(): void {
    triggerElement.value = null
  }

  return {
    triggerElement: triggerElement as Readonly<Ref<HTMLElement | null>>,
    saveFocus,
    restoreFocus,
    clear,
  }
}
