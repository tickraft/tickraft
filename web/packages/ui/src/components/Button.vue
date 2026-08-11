// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Button - shared primary action button.
 *
 * Thin, consistently-styled wrapper over el-button enforcing the tk- design
 * tokens and a unified API across frontends. Renders an accessible button:
 * when `loading` is true the element is disabled and annotated with
 * aria-busy so assistive technology announces the in-progress state.
 */
import { computed } from 'vue'
import type { Component } from 'vue'

/** Visual style variant */
type ButtonVariant = 'primary' | 'secondary' | 'success' | 'warning' | 'danger' | 'info' | 'text' | 'link'

/** Button size */
type ButtonSize = 'large' | 'default' | 'small'

/** Native button type */
type ButtonType = 'button' | 'submit' | 'reset'

/** Component props */
interface Props {
  /** Visual variant */
  variant?: ButtonVariant
  /** Button size */
  size?: ButtonSize
  /** Native button type */
  nativeType?: ButtonType
  /** Whether the button is loading (shows spinner, disables interaction) */
  loading?: boolean
  /** Whether the button is disabled */
  disabled?: boolean
  /** Whether to show a plain style (outlined) */
  plain?: boolean
  /** Whether to render a round button */
  round?: boolean
  /** Whether the button should take full width */
  block?: boolean
  /** Optional leading icon component */
  icon?: Component
  /** Accessible label for icon-only buttons (required when no text slot) */
  ariaLabel?: string
}

const props = withDefaults(defineProps<Props>(), {
  variant: 'primary',
  size: 'default',
  nativeType: 'button',
  loading: false,
  disabled: false,
  plain: false,
  round: false,
  block: false,
  icon: undefined,
  ariaLabel: undefined,
})

/** Element Plus type mapping (text/link map directly, others map to type) */
const elType = computed(() => {
  if (props.variant === 'text' || props.variant === 'link') return 'primary'
  return props.variant
})

/** Whether the button renders as text/link (uses el-button text/link props) */
const isText = computed(() => props.variant === 'text')
const isLink = computed(() => props.variant === 'link')

const emit = defineEmits<{
  /** Click event (suppressed while loading or disabled) */
  (e: 'click', evt: MouseEvent): void
}>()

function handleClick(evt: MouseEvent): void {
  if (props.loading || props.disabled) return
  emit('click', evt)
}
</script>

<template>
  <el-button
    :type="elType"
    :size="size"
    :native-type="nativeType"
    :loading="loading"
    :disabled="disabled"
    :plain="plain"
    :round="round"
    :text="isText"
    :link="isLink"
    :icon="icon"
    :class="['tk-ui-button', { 'tk-ui-button--block': block }]"
    :aria-label="ariaLabel"
    :aria-busy="loading ? 'true' : undefined"
    :aria-disabled="disabled ? 'true' : undefined"
    @click="handleClick"
  >
    <slot />
  </el-button>
</template>

<style scoped lang="scss">
.tk-ui-button {
  &--block {
    display: flex;
    width: 100%;
  }
}
</style>
