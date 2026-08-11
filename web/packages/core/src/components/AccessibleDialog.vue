// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * AccessibleDialog - modal dialog with guaranteed focus management.
 *
 * Wraps `el-dialog` and, on top of Element Plus's built-in focus trap,
 * guarantees focus *restoration*: the element that had focus when the dialog
 * opened (the trigger button) receives focus again after the dialog closes.
 *
 * - `:trap-focus="true"` explicitly enables Element Plus's internal
 *   `ElFocusTrap` so Tab/Shift-Tab stay within the dialog while open.
 * - `role="dialog"` + `aria-modal="true"` are set by el-dialog; this
 *   component additionally wires `aria-labelledby` to the title so screen
 *   readers announce the dialog purpose.
 * - On open the currently focused element is recorded; on `closed` (after the
 *   leave transition) focus is returned to it.
 *
 * Prefer this component over raw `el-dialog` whenever the dialog is opened
 * from a user action (button click) to keep keyboard/screen-reader users
 * oriented.
 */
import { computed, onBeforeUnmount, useId } from 'vue'

/** Component props */
interface Props {
  /** v-model controlling dialog visibility */
  modelValue: boolean
  /** Dialog title (also used as the accessible name via aria-labelledby) */
  title?: string
  /** Dialog width (CSS length) */
  width?: string
  /** Whether clicking the backdrop closes the dialog (default false) */
  closeOnClickModal?: boolean
  /** Whether pressing ESC closes the dialog (default true) */
  closeOnPressEscape?: boolean
  /** Whether to show the built-in close button (default true) */
  showClose?: boolean
  /** Whether to center the dialog content (default false) */
  center?: boolean
  /** Whether to center the dialog in the viewport (default false) */
  alignCenter?: boolean
  /** Destroy dialog content when closed (default false) */
  destroyOnClose?: boolean
  /** Whether the dialog body is in a loading state (sets aria-busy) */
  loading?: boolean
  /** Accessible label for the close button */
  closeAriaLabel?: string
  /** Extra class appended to the dialog */
  customClass?: string
}

const props = withDefaults(defineProps<Props>(), {
  title: undefined,
  width: '50%',
  closeOnClickModal: false,
  closeOnPressEscape: true,
  showClose: true,
  center: false,
  alignCenter: false,
  destroyOnClose: false,
  loading: false,
  closeAriaLabel: 'Close dialog',
  customClass: undefined,
})

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'open'): void
  (e: 'opened'): void
  (e: 'close'): void
  (e: 'closed'): void
}>()

/** Stable id for the title element, used by aria-labelledby */
const titleId = useId()

/** The element that had focus when the dialog opened (the trigger). */
let triggerElement: HTMLElement | null = null

const dialogVisible = computed({
  get: () => props.modelValue,
  set: (val: boolean) => emit('update:modelValue', val),
})

function handleOpen(): void {
  if (typeof document !== 'undefined') {
    const active = document.activeElement
    triggerElement = active instanceof HTMLElement ? active : null
  }
  emit('open')
}

function handleClose(): void {
  emit('update:modelValue', false)
  emit('close')
}

function handleClosed(): void {
  // After the close transition completes, return focus to the trigger so
  // keyboard/screen-reader users resume where they left off.
  const el = triggerElement
  triggerElement = null
  if (el && el.isConnected) {
    requestAnimationFrame(() => {
      try {
        el.focus()
      } catch {
        // Element may no longer be focusable; ignore silently.
      }
    })
  }
  emit('closed')
}

onBeforeUnmount(() => {
  triggerElement = null
})
</script>

<template>
  <el-dialog
    v-model="dialogVisible"
    :title="title"
    :width="width"
    :close-on-click-modal="closeOnClickModal"
    :close-on-press-escape="closeOnPressEscape"
    :show-close="showClose"
    :center="center"
    :align-center="alignCenter"
    :destroy-on-close="destroyOnClose"
    :trap-focus="true"
    :class="['tk-accessible-dialog', customClass]"
    :aria-labelledby="title ? titleId : undefined"
    role="dialog"
    aria-modal="true"
    @open="handleOpen"
    @opened="emit('opened')"
    @close="handleClose"
    @closed="handleClosed"
  >
    <template
      v-if="title"
      #header
    >
      <span
        :id="titleId"
        class="tk-accessible-dialog__title"
        role="heading"
        aria-level="2"
      >
        {{ title }}
      </span>
    </template>
    <div
      class="tk-accessible-dialog__body"
      :aria-busy="loading ? 'true' : undefined"
    >
      <slot />
    </div>
    <template
      v-if="$slots.footer"
      #footer
    >
      <div class="tk-accessible-dialog__footer">
        <slot name="footer" />
      </div>
    </template>
  </el-dialog>
</template>

<style scoped lang="scss">
.tk-accessible-dialog {
  &__title {
    font-size: var(--tk-font-size-lg, 18px);
    font-weight: 600;
    color: var(--tk-text-primary, #1f2937);
  }

  &__body {
    min-height: 40px;
  }

  &__footer {
    display: flex;
    gap: var(--tk-spacing-sm, 12px);
    justify-content: flex-end;
  }
}
</style>
