// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Shared Prism list-page header.
 *
 * Renders the prototype-aligned page header structure:
 * a large title with subtitle on the left, a numeric count badge plus
 * an actions slot on the right, and an optional chips slot below.
 *
 * Visual intent mirrors storyboard `tk-prism-page__header` while using
 * only core design tokens (named spacing / typography / color tokens).
 */
withDefaults(
  defineProps<{
    /** Page title */
    title: string
    /** Subtitle shown under the title */
    subtitle?: string
    /** Numeric value rendered inside the count badge (omit to hide) */
    count?: number | string
    /** Mono uppercase label under the count number */
    countLabel?: string
  }>(),
  {
    subtitle: '',
    count: undefined,
    countLabel: '',
  },
)
</script>

<template>
  <header class="tk-prism-page-header">
    <div class="tk-prism-page-header__title-row">
      <div class="tk-prism-page-header__heading">
        <h1 class="tk-prism-page-header__title">
          {{ title }}
        </h1>
        <p
          v-if="subtitle"
          class="tk-prism-page-header__subtitle"
        >
          {{ subtitle }}
        </p>
      </div>

      <div class="tk-prism-page-header__actions">
        <div
          v-if="count !== undefined"
          class="tk-prism-page-header__count"
        >
          <span class="tk-prism-page-header__count-num">{{ count }}</span>
          <span
            v-if="countLabel"
            class="tk-prism-page-header__count-label"
          >{{ countLabel }}</span>
        </div>
        <slot name="actions" />
      </div>
    </div>

    <div
      v-if="$slots.chips"
      class="tk-prism-page-header__chips"
    >
      <slot name="chips" />
    </div>
  </header>
</template>

<style scoped lang="scss">
.tk-prism-page-header {
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-lg);
  padding-bottom: var(--tk-spacing-lg);
  margin-bottom: var(--tk-spacing-lg);
  border-bottom: 1px solid var(--tk-border-color);

  &__title-row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-lg);
    align-items: flex-end;
    justify-content: space-between;
  }

  &__heading {
    min-width: 0;
  }

  &__title {
    margin: 0;
    font-size: var(--tk-font-size-3xl);
    font-weight: var(--tk-font-weight-bold);
    line-height: 1.1;
    color: var(--tk-text-primary);
  }

  &__subtitle {
    margin: var(--tk-spacing-xs) 0 0;
    font-size: var(--tk-font-size-sm);
    line-height: var(--tk-line-height-normal);
    color: var(--tk-text-secondary);
  }

  &__actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-md);
    align-items: center;
  }

  &__count {
    display: flex;
    flex-direction: column;
    gap: 2px;
    align-items: flex-end;
    padding: var(--tk-spacing-sm) var(--tk-spacing-lg);
    background-color: var(--tk-bg-surface);
    border: 1px solid var(--tk-border-color);
    border-radius: var(--tk-radius-md);
  }

  &__count-num {
    font-size: var(--tk-font-size-3xl);
    font-weight: var(--tk-font-weight-bold);
    font-variant-numeric: tabular-nums;
    line-height: 1;
    color: var(--tk-text-primary);
  }

  &__count-label {
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  &__chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-sm);
    align-items: center;
  }
}
</style>
