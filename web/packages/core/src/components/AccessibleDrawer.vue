// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * AccessibleDrawer - slide-over panel with guaranteed focus management.
 *
 * Wraps `el-drawer` and, on top of Element Plus's built-in focus trap,
 * guarantees focus *restoration*: the element that had focus when the drawer
 * opened (the trigger button) receives focus again after the drawer closes.
 *
 * - `:trap-focus="true"` explicitly enables Element Plus's internal
 *   `ElFocusTrap` so Tab/Shift-Tab stay within the drawer while open.
 * - `role="dialog"` + `aria-modal="true"` are set by el-drawer; this
 *   component additionally wires `aria-labelledby` to the title so screen
 *   readers announce the drawer purpose.
 * - On open the currently focused element is recorded; on `closed` (after the
 *   leave transition) focus is returned to it.
 *
 * Prefer this component over raw `el-drawer` whenever the drawer is opened
 * from a user action (button click) to keep keyboard/screen-reader users
 * oriented.
 */
import { computed, onBeforeUnmount, useId } from 'vue'

/** Drawer slide direction */
type DrawerDirection = 'rtl' | 'ltr' | 'ttb' | 'btt'

/** Component props */
interface Props {
  /** v-model controlling drawer visibility */
  modelValue: boolean
  /** Drawer title (also used as the accessible name via aria-labelledby) */
  title?: string
  /** Drawer size (CSS length for the sliding dimension) */
  size?: string
  /** Slide direction (default `rtl` — right to left) */
  direction?: DrawerDirection
  /** Whether clicking the backdrop closes the drawer (default true) */
  closeOnClickModal?: boolean
  /** Whether pressing ESC closes the drawer (default true) */
  closeOnPressEscape?: boolean
  /** Whether to show the built-in close button (default true) */
  showClose?: boolean
  /** Destroy drawer content when closed (default false) */
  destroyOnClose?: boolean
  /** Whether the drawer body is in a loading state (sets aria-busy) */
  loading?: boolean
  /** Accessible label for the close button */
  closeAriaLabel?: string
  /** Extra class appended to the drawer */
  customClass?: string
}

const props = withDefaults(defineProps<Props>(), {
  title: undefined,
  size: '320px',
  direction: 'rtl',
  closeOnClickModal: true,
  closeOnPressEscape: true,
  showClose: true,
  destroyOnClose: false,
  loading: false,
  closeAriaLabel: 'Close panel',
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

/** The element that had focus when the drawer opened (the trigger). */
let triggerElement: HTMLElement | null = null

const drawerVisible = computed({
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
  <el-drawer
    v-model="drawerVisible"
    :title="title"
    :size="size"
    :direction="direction"
    :close-on-click-modal="closeOnClickModal"
    :close-on-press-escape="closeOnPressEscape"
    :show-close="showClose"
    :destroy-on-close="destroyOnClose"
    :trap-focus="true"
    :class="['tk-accessible-drawer', customClass]"
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
        class="tk-accessible-drawer__title"
        role="heading"
        aria-level="2"
      >
        {{ title }}
      </span>
    </template>
    <div
      class="tk-accessible-drawer__body"
      :aria-busy="loading ? 'true' : undefined"
    >
      <slot />
    </div>
    <template
      v-if="$slots.footer"
      #footer
    >
      <div class="tk-accessible-drawer__footer">
        <slot name="footer" />
      </div>
    </template>
  </el-drawer>
</template>

<style scoped lang="scss">
.tk-accessible-drawer {
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
