// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Drawer - slide-out panel with focus trap and focus restoration.
 *
 * Wrapper over el-drawer providing:
 * - Proper ARIA roles (role="dialog", aria-modal="true", aria-labelledby)
 * - Focus trapping via Element Plus's built-in ElFocusTrap (enabled by
 *   aria-modal)
 * - Focus restoration: on open the currently focused element (the trigger)
 *   is saved; on close focus is returned to that element so keyboard and
 *   screen-reader users resume where they left off
 * - Accessible close button label
 *
 * Use this component instead of raw el-drawer whenever the drawer is
 * triggered by a user action (button click) to guarantee focus management.
 */
import { computed, useId } from 'vue'
import { useFocusRestore } from '../composables/useFocusRestore'

/** Drawer slide direction */
type DrawerDirection = 'rtl' | 'ltr' | 'ttb' | 'btt'

/** Component props */
interface Props {
  /** v-model:visible — controls drawer visibility */
  visible: boolean
  /** Drawer title */
  title?: string
  /** Drawer size (width for ltr/rtl, height for ttb/btt) */
  size?: string
  /** Slide direction */
  direction?: DrawerDirection
  /** Whether clicking the backdrop closes the drawer */
  closeOnClickModal?: boolean
  /** Whether pressing ESC closes the drawer */
  closeOnPressEscape?: boolean
  /** Whether to show the built-in close button */
  showClose?: boolean
  /** Whether to destroy on close (frees DOM when hidden) */
  destroyOnClose?: boolean
  /** Whether the drawer content is loading */
  loading?: boolean
  /** Accessible label for the close button */
  closeAriaLabel?: string
  /** Custom class appended to the drawer */
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
  (e: 'update:visible', value: boolean): void
  (e: 'close'): void
  (e: 'open'): void
  (e: 'opened'): void
  (e: 'closed'): void
}>()

/** Stable id for the title element, used by aria-labelledby */
const titleId = useId()

/** Focus save/restore for accessibility */
const { saveFocus, restoreFocus } = useFocusRestore()

const drawerVisible = computed({
  get: () => props.visible,
  set: (val: boolean) => emit('update:visible', val),
})

function handleOpen(): void {
  // Record the element that triggered the drawer before focus moves inside.
  saveFocus()
  emit('open')
}

function handleClose(): void {
  emit('update:visible', false)
  emit('close')
}

function handleClosed(): void {
  // After the close transition completes, return focus to the trigger.
  restoreFocus()
  emit('closed')
}
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
    :class="['tk-ui-drawer', customClass]"
    :aria-labelledby="title ? titleId : undefined"
    role="dialog"
    aria-modal="true"
    @close="handleClose"
    @open="handleOpen"
    @opened="emit('opened')"
    @closed="handleClosed"
  >
    <template
      v-if="title"
      #header
    >
      <span
        :id="titleId"
        class="tk-ui-drawer__title"
        role="heading"
        aria-level="2"
      >
        {{ title }}
      </span>
    </template>
    <div
      class="tk-ui-drawer__body"
      :aria-busy="loading ? 'true' : undefined"
    >
      <slot />
    </div>
    <template
      v-if="$slots.footer"
      #footer
    >
      <div class="tk-ui-drawer__footer">
        <slot name="footer" />
      </div>
    </template>
  </el-drawer>
</template>

<style scoped lang="scss">
.tk-ui-drawer {
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
    gap: 12px;
    justify-content: flex-end;
  }
}
</style>
