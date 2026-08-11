// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Input - shared text input.
 *
 * Wrapper over el-input enforcing tk- design tokens and an accessible
 * label association. When `label` is provided it renders an associated
 * <label> element wired to the input via for/id so screen readers announce
 * the field purpose; otherwise callers should supply aria-label.
 */
import { computed, useId } from 'vue'
import type { Component } from 'vue'

/** Input size */
type InputSize = 'large' | 'default' | 'small'

/** Component props */
interface Props {
  /** v-model value */
  modelValue?: string | number
  /** Input type */
  type?: string
  /** Field label (rendered as an associated <label>) */
  label?: string
  /** Placeholder text */
  placeholder?: string
  /** Input size */
  size?: InputSize
  /** Whether the field is disabled */
  disabled?: boolean
  /** Whether the field is read-only */
  readonly?: boolean
  /** Whether the field is clearable */
  clearable?: boolean
  /** Whether to show a password toggle (for type=password) */
  showPassword?: boolean
  /** Maximum character length */
  maxlength?: number
  /** Whether to show the character counter (requires maxlength) */
  showWordLimit?: boolean
  /** Optional leading prefix icon component */
  prefixIcon?: Component
  /** Optional trailing suffix icon component */
  suffixIcon?: Component
  /** Accessible label (used when no visible label is provided) */
  ariaLabel?: string
  /** Whether the field is required (adds aria-required) */
  required?: boolean
  /** Error message (sets aria-invalid and aria-describedby) */
  error?: string
  /** Hint text shown below the input */
  hint?: string
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: '',
  type: 'text',
  label: undefined,
  placeholder: undefined,
  size: 'default',
  disabled: false,
  readonly: false,
  clearable: false,
  showPassword: false,
  maxlength: undefined,
  showWordLimit: false,
  prefixIcon: undefined,
  suffixIcon: undefined,
  ariaLabel: undefined,
  required: false,
  error: undefined,
  hint: undefined,
})

const emit = defineEmits<{
  (e: 'update:modelValue', value: string | number): void
  (e: 'input', value: string | number): void
  (e: 'change', value: string | number): void
  (e: 'clear'): void
  (e: 'blur', evt: FocusEvent): void
  (e: 'focus', evt: FocusEvent): void
}>()

/** Stable unique id for label association */
const fieldId = useId()
/** Stable unique id for the error/hint description */
const describedById = computed(() => `tk-ui-input-desc-${fieldId}`)

const describedBy = computed(() => {
  const parts: string[] = []
  if (props.error) parts.push(describedById.value)
  if (props.hint && !props.error) parts.push(describedById.value)
  return parts.length ? parts.join(' ') : undefined
})

function handleInput(value: string | number): void {
  emit('update:modelValue', value)
  emit('input', value)
}
</script>

<template>
  <div class="tk-ui-input">
    <label
      v-if="label"
      :for="fieldId"
      class="tk-ui-input__label"
    >
      {{ label }}
      <span
        v-if="required"
        class="tk-ui-input__required"
        aria-hidden="true"
      >*</span>
    </label>
    <el-input
      :id="fieldId"
      :model-value="modelValue"
      :type="type"
      :placeholder="placeholder"
      :size="size"
      :disabled="disabled"
      :readonly="readonly"
      :clearable="clearable"
      :show-password="showPassword"
      :maxlength="maxlength"
      :show-word-limit="showWordLimit"
      :prefix-icon="prefixIcon"
      :suffix-icon="suffixIcon"
      :aria-label="ariaLabel"
      :aria-required="required ? 'true' : undefined"
      :aria-invalid="error ? 'true' : undefined"
      :aria-describedby="describedBy"
      class="tk-ui-input__field"
      @update:model-value="handleInput"
      @change="(v: string | number) => emit('change', v)"
      @clear="emit('clear')"
      @blur="(e: FocusEvent) => emit('blur', e)"
      @focus="(e: FocusEvent) => emit('focus', e)"
    />
    <div
      v-if="error || hint"
      :id="describedById"
      class="tk-ui-input__message"
      :class="{ 'tk-ui-input__message--error': error }"
      :role="error ? 'alert' : 'note'"
    >
      {{ error || hint }}
    </div>
  </div>
</template>

<style scoped lang="scss">
.tk-ui-input {
  display: flex;
  flex-direction: column;
  gap: 6px;
  width: 100%;

  &__label {
    font-size: var(--tk-font-size-sm, 14px);
    font-weight: 500;
    color: var(--tk-text-primary, #1f2937);
  }

  &__required {
    margin-left: 2px;
    color: var(--tk-danger-color, #ef4444);
  }

  &__message {
    font-size: var(--tk-font-size-xs, 12px);
    color: var(--tk-text-secondary, #6b7280);

    &--error {
      color: var(--tk-danger-color, #ef4444);
    }
  }
}
</style>
