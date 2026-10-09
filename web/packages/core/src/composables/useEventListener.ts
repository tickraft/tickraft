// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * useEventListener - declaratively attach a window/element event listener.
 *
 * Attaches the listener on mount and removes it on unmount, so callers never
 * leak listeners. When `target` is a ref, the listener re-binds if the
 * resolved element changes.
 */
import { onBeforeUnmount, onMounted, watch, type Ref } from 'vue'

/** Resolvable event target: an element, a ref to an element, or window/document */
type EventTargetSource<T extends EventTarget> = T | Ref<T | null | undefined>

export interface UseEventListenerOptions {
  /** Whether to capture the event (bubbling vs capturing phase) */
  capture?: boolean
  /** Whether to call preventDefault automatically */
  passive?: boolean
  /** Whether the listener should be active (false detaches it) */
  enabled?: Ref<boolean>
}

/**
 * Attach an event listener with automatic lifecycle cleanup.
 *
 * @param target - event target (element, ref to element, window, or document)
 * @param event - event name (e.g. 'click', 'keydown')
 * @param handler - event handler
 * @param options - listener options
 */
export function useEventListener<T extends EventTarget>(
  target: EventTargetSource<T>,
  event: string,
  handler: (evt: Event) => void,
  options: UseEventListenerOptions = {},
): void {
  const { capture = false, passive = true, enabled } = options

  let attachedTarget: T | null = null
  let attachedHandler: ((evt: Event) => void) | null = null

  function resolveTarget(src: EventTargetSource<T>): T | null {
    if (src === null || src === undefined) return null
    if (typeof src === 'object' && 'value' in (src as Ref<T>)) {
      return (src as Ref<T>).value ?? null
    }
    return src as T
  }

  function bind(t: T | null): void {
    if (attachedTarget && attachedHandler) {
      attachedTarget.removeEventListener(event, attachedHandler, { capture })
      attachedTarget = null
      attachedHandler = null
    }
    if (!t) return
    if (enabled && !enabled.value) return
    attachedHandler = handler
    attachedTarget = t
    t.addEventListener(event, handler, { capture, passive })
  }

  onMounted(() => {
    bind(resolveTarget(target))
  })

  // Re-bind when a ref target changes
  if (typeof target === 'object' && target !== null && 'value' in target) {
    watch(
      () => (target as Ref<T | null | undefined>).value,
      (newVal) => {
        bind(newVal ?? null)
      },
    )
  }

  // Re-bind when enabled flag toggles
  if (enabled) {
    watch(
      enabled,
      (isActive) => {
        if (isActive) {
          bind(resolveTarget(target))
        } else if (attachedTarget && attachedHandler) {
          attachedTarget.removeEventListener(event, attachedHandler, { capture })
          attachedTarget = null
          attachedHandler = null
        }
      },
    )
  }

  onBeforeUnmount(() => {
    if (attachedTarget && attachedHandler) {
      attachedTarget.removeEventListener(event, attachedHandler, { capture })
      attachedTarget = null
      attachedHandler = null
    }
  })
}
