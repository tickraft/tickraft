// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * useFormGuard - guard against leaving a route/page with unsaved form changes.
 *
 * Provides two layers of protection:
 * 1. `beforeunload` event — warns when the user closes the tab or refreshes
 *    the page with unsaved changes (browser-native dialog).
 * 2. `onBeforeRouteLeave` — a component-scoped vue-router guard that warns
 *    when the user navigates to another in-app route with unsaved changes.
 *    Uses `window.confirm` for the confirmation dialog.
 *
 * The caller drives `isDirty` reactively (typically from `useForm().isDirty`).
 * Call `release()` after a successful save to clear the dirty flag and allow
 * navigation without prompting.
 */
import { onBeforeUnmount, onMounted, type Ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'

export interface UseFormGuardOptions {
  /** Reactive flag indicating the form has unsaved changes */
  isDirty: Ref<boolean>
  /** Confirmation message shown in the beforeunload dialog */
  message?: string
  /** i18n message resolver (called when route-leave guard triggers) */
  confirmMessage?: () => string
}

export interface UseFormGuardReturn {
  /** Force-release the guard (call after a successful save) */
  release: () => void
}

/**
 * Install a form-leave guard.
 *
 * Must be called within a component `setup()` context (uses
 * `onBeforeRouteLeave` which requires a component-scoped route guard).
 *
 * @param options - guard options
 */
export function useFormGuard(options: UseFormGuardOptions): UseFormGuardReturn {
  const {
    isDirty,
    message = 'You have unsaved changes. Leave anyway?',
    confirmMessage,
  } = options

  // ── Layer 1: beforeunload (tab close / refresh) ──
  function onBeforeUnload(evt: BeforeUnloadEvent): string | undefined {
    if (isDirty.value) {
      evt.preventDefault()
      evt.returnValue = message
      return message
    }
    return undefined
  }

  onMounted(() => {
    window.addEventListener('beforeunload', onBeforeUnload)
  })

  onBeforeUnmount(() => {
    window.removeEventListener('beforeunload', onBeforeUnload)
  })

  // ── Layer 2: onBeforeRouteLeave (in-app navigation) ──
  // Component-scoped guard: only triggers when navigating AWAY from the
  // current component's route, unlike router.beforeEach which is global.
  onBeforeRouteLeave((_to, _from) => {
    if (!isDirty.value) return true
    const msg = confirmMessage ? confirmMessage() : message
     
    if (window.confirm(msg)) {
      return true
    }
    return false
  })

  function release(): void {
    isDirty.value = false
  }

  return { release }
}
