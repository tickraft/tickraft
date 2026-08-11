// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * ConfirmDialog - confirmation dialog component.
 *
 * Built on el-dialog, supporting:
 * - Dangerous action confirmation (danger type, confirm button in red var(--tk-danger-color))
 * - Secondary input confirmation (require-input; the user must type the specified text to confirm)
 * - Async confirm loading state, preventing duplicate clicks
 * - Custom icon and icon color
 * - Content slot (default) and custom button group slot (footer)
 */
import { computed, ref, watch } from 'vue'
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
import type { Component } from 'vue'

interface ConfirmDialogProps {
  /** Controls dialog visibility (v-model) */
  modelValue: boolean
  /** Dialog title */
  title?: string
  /** Prompt content text */
  content?: string
  /** Dialog type: default for regular confirmation, danger for hazardous actions (confirm button in red) */
  type?: 'default' | 'danger'
  /** Confirm button text; falls back to the i18n default when empty */
  confirmText?: string
  /** Cancel button text; falls back to the i18n default when empty */
  cancelText?: string
  /** Verification string that must be typed before confirming */
  requireInput?: string
  /** Async confirm loading state; the confirm button shows loading and is disabled to prevent duplicate clicks */
  loading?: boolean
  /** Custom icon name (iconify format ep:warning-filled or PascalCase) */
  icon?: string
  /** Custom icon color (CSS color value or var(--tk-*)) */
  iconColor?: string
}

interface ConfirmDialogEmits {
  (e: 'update:modelValue', value: boolean): void
  (e: 'confirm'): void
  (e: 'cancel'): void
}

const props = withDefaults(defineProps<ConfirmDialogProps>(), {
  title: '',
  content: '',
  type: 'default',
  confirmText: '',
  cancelText: '',
  requireInput: '',
  loading: false,
  icon: '',
  iconColor: '',
})

const emit = defineEmits<ConfirmDialogEmits>()

/** Verification text entered by the user */
const inputValue = ref('')

/** Whether input verification is required */
const needInput = computed(() => !!props.requireInput)

/** Whether the input verification has passed */
const isInputValid = computed(() => !needInput.value || inputValue.value === props.requireInput)

/** Whether the confirm button is clickable */
const canConfirm = computed(() => isInputValid.value && !props.loading)

/** Whether this is a danger-typed dialog */
const isDanger = computed(() => props.type === 'danger')

/** Normalize an icon name to an Element Plus icon component (PascalCase) */
function resolveIcon(name: string): Component | undefined {
  if (!name) return undefined
  const icons = ElementPlusIconsVue as unknown as Record<string, Component>
  // iconify format "ep:warning-filled" -> "WarningFilled"
  if (name.includes(':')) {
    const [, iconName] = name.split(':')
    const pascal = iconName
      .split('-')
      .filter(Boolean)
      .map((s) => s.charAt(0).toUpperCase() + s.slice(1))
      .join('')
    return icons[pascal]
  }
  // PascalCase: direct lookup
  return icons[name]
}

/** Currently displayed icon component; when not customized, picked based on type */
const iconComponent = computed<Component | undefined>(() => {
  const resolved = resolveIcon(props.icon)
  if (resolved) return resolved
  return isDanger.value ? ElementPlusIconsVue.CircleCloseFilled : ElementPlusIconsVue.InfoFilled
})

/** Inline icon style (applies iconColor) */
const iconStyle = computed(() => (props.iconColor ? { color: props.iconColor } : undefined))

/** Reset input and emit cancel when the dialog closes */
function handleClose() {
  emit('update:modelValue', false)
  emit('cancel')
}

/** Confirm action */
function handleConfirm() {
  if (!canConfirm.value) return
  emit('confirm')
}

/** Reset the input when the dialog opens */
watch(
  () => props.modelValue,
  (visible) => {
    if (visible) {
      inputValue.value = ''
    }
  },
)
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    :title="title"
    width="420px"
    :class="['tk-confirm-dialog', { 'tk-confirm-dialog--danger': isDanger }]"
    @close="handleClose"
  >
    <div class="tk-confirm-dialog__content">
      <el-icon
        v-if="iconComponent"
        class="tk-confirm-dialog__icon"
        :class="{ 'tk-confirm-dialog__icon--danger': isDanger }"
        :style="iconStyle"
        aria-hidden="true"
      >
        <component :is="iconComponent" />
      </el-icon>
      <div class="tk-confirm-dialog__body">
        <slot>
          <p
            v-if="content"
            class="tk-confirm-dialog__message"
          >
            {{ content }}
          </p>
        </slot>
        <div
          v-if="needInput"
          class="tk-confirm-dialog__verify"
        >
          <span class="tk-confirm-dialog__verify-hint">
            {{ $t('common.app.confirm') }}
            <code class="tk-confirm-dialog__verify-code">{{ requireInput }}</code>
          </span>
          <el-input
            v-model="inputValue"
            :placeholder="requireInput"
            :aria-label="`Type ${requireInput} to confirm`"
            class="tk-confirm-dialog__verify-input"
          />
        </div>
      </div>
    </div>
    <template #footer>
      <slot name="footer">
        <el-button
          :disabled="loading"
          @click="handleClose"
        >
          {{ cancelText || $t('common.app.cancel') }}
        </el-button>
        <el-button
          :type="isDanger ? 'danger' : 'primary'"
          :loading="loading"
          :disabled="!canConfirm"
          @click="handleConfirm"
        >
          {{ confirmText || $t('common.app.confirm') }}
        </el-button>
      </slot>
    </template>
  </el-dialog>
</template>

<style scoped lang="scss">
.tk-confirm-dialog__content {
  display: flex;
  gap: var(--tk-spacing-sm);
  align-items: flex-start;
}

.tk-confirm-dialog__body {
  flex: 1;
  min-width: 0;
}

.tk-confirm-dialog__message {
  margin: 0;
  font-size: var(--tk-font-size-base);
  line-height: var(--tk-line-height-normal);
  color: var(--tk-text-regular);
  word-break: break-all;
}

.tk-confirm-dialog__icon {
  flex-shrink: 0;
  margin-top: 2px;
  font-size: 20px;
  color: var(--tk-info-color);
}

.tk-confirm-dialog__icon--danger {
  color: var(--tk-danger-color-text);
}

/* Danger-typed dialog title turns red */
.tk-confirm-dialog--danger {
  :deep(.el-dialog__title) {
    color: var(--tk-danger-color-text);
  }
}

.tk-confirm-dialog__verify {
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-xs);
  margin-top: var(--tk-spacing-md);
}

.tk-confirm-dialog__verify-hint {
  font-size: var(--tk-font-size-sm);
  color: var(--tk-text-secondary);
}

.tk-confirm-dialog__verify-code {
  display: inline-block;
  padding: 2px var(--tk-spacing-xs);
  margin-left: var(--tk-spacing-xs);
  font-family: var(--tk-font-family-mono);
  font-size: var(--tk-font-size-sm);
  color: var(--tk-danger-color-text);
  background-color: var(--tk-danger-color-bg);
  border: 1px solid var(--tk-danger-color-border);
  border-radius: var(--tk-radius-sm);
}

.tk-confirm-dialog__verify-input {
  width: 100%;
}
</style>
