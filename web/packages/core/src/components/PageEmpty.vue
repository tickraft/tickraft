// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * PageEmpty - empty state component.
 *
 * Visuals align with the .tk-empty prototype: 64px icon, title,
 * description and action button.
 * Supports a default slot for custom overall content and an #action slot for
 * a custom action button.
 */
import { computed } from 'vue'
import type { Component } from 'vue'
import { Box } from '@element-plus/icons-vue'

interface PageEmptyProps {
  /** Custom icon component; uses the default icon when not set */
  icon?: Component
  /** Title text */
  title?: string
  /** Description text; falls back to the i18n "No data" message when empty and no title is set */
  description?: string
  /** Action button text; renders the default action button when set */
  actionText?: string
}

interface PageEmptyEmits {
  (e: 'action'): void
}

const props = withDefaults(defineProps<PageEmptyProps>(), {
  icon: undefined,
  title: '',
  description: '',
  actionText: '',
})

const emit = defineEmits<PageEmptyEmits>()

/** Currently displayed icon component */
const iconComponent = computed<Component>(() => props.icon ?? Box)

/** Whether to show the description row: shown when there is a description or no title */
const showDescription = computed(() => !!props.description || !props.title)

function handleAction() {
  emit('action')
}
</script>

<template>
  <div class="tk-empty">
    <slot>
      <div class="tk-empty__icon">
        <component :is="iconComponent" />
      </div>
      <p
        v-if="title"
        class="tk-empty__title"
      >
        {{ title }}
      </p>
      <p
        v-if="showDescription"
        class="tk-empty__description"
      >
        {{ description || $t('common.app.noData') }}
      </p>
    </slot>
    <div
      v-if="$slots.action || actionText"
      class="tk-empty__action"
    >
      <slot name="action">
        <el-button
          type="primary"
          @click="handleAction"
        >
          {{ actionText }}
        </el-button>
      </slot>
    </div>
  </div>
</template>

<style scoped lang="scss">
.tk-empty {
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-md);
  align-items: center;
  justify-content: center;
  padding: var(--tk-spacing-2xl) var(--tk-spacing-md);
  text-align: center;
}

.tk-empty__icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 64px;
  height: 64px;
  font-size: 64px;
  line-height: 1;
  color: var(--tk-gray-7);
  user-select: none;
}

.tk-empty__icon :deep(svg) {
  width: 100%;
  height: 100%;
  fill: currentcolor;
}

.tk-empty__title {
  margin: 0;
  font-size: var(--tk-font-size-lg);
  font-weight: var(--tk-font-weight-medium);
  line-height: var(--tk-line-height-normal);
  color: var(--tk-text-primary);
}

.tk-empty__description {
  max-width: 360px;
  margin: 0;
  font-size: var(--tk-font-size-base);
  line-height: var(--tk-line-height-relaxed);
  color: var(--tk-text-secondary);
  word-break: break-all;
}

.tk-empty__action {
  display: flex;
  gap: var(--tk-spacing-sm);
  margin-top: var(--tk-spacing-lg);
}
</style>
