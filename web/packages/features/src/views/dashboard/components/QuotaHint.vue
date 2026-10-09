// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * QuotaHint - dashboard quota near-limit hint (open-core strategy §6.2-1).
 *
 * Surfaces count-based quota types as they approach their ceilings:
 * - ratio >= 0.8: mild hint bar, dismissible, resurfaces after 7 days
 * - ratio >= 1.0: strong hint bar, not dismissible (creation is already
 *   being rejected with 409 at this point)
 *
 * Data comes from GET /quota/usage — a read-only aggregate with no edition
 * logic on the client; unlimited rows (ceiling 0) never appear.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getQuotaUsage } from '../../../api/system'
import type { QuotaUsageItem } from '../../../api/system'
import { getStorage, setStorage } from '@tickraft/core'

/** External link to the editions comparison page */
const EDITIONS_URL = 'https://tickraft.io/editions'

/** localStorage key holding the last dismiss timestamp (ms since epoch) */
const DISMISS_KEY = 'tk-quota-hint-dismissed-at'

/** Dismissed hints resurface after 7 days */
const DISMISS_TTL_MS = 7 * 24 * 60 * 60 * 1000

/** Mild-hint threshold */
const NEAR_RATIO = 0.8

const { t } = useI18n()

const items = ref<QuotaUsageItem[]>([])
const dismissedAt = ref<number>(getStorage<number>(DISMISS_KEY) ?? 0)

/** Count-based rows sorted worst-first; unlimited rows are dropped */
const nearItems = computed<QuotaUsageItem[]>(() =>
  items.value
    .filter((it) => it.ceiling > 0 && it.ratio >= NEAR_RATIO)
    .sort((a, b) => b.ratio - a.ratio),
)

/** At-ceiling rows (ratio >= 1) always render and cannot be dismissed away */
const fullItems = computed(() => nearItems.value.filter((it) => it.ratio >= 1))

/** Approaching rows (0.8 <= ratio < 1) render until dismissed within 7 days */
const approachingItems = computed(() => nearItems.value.filter((it) => it.ratio < 1))

const dismissedRecently = computed(
  () => Date.now() - dismissedAt.value < DISMISS_TTL_MS,
)

const visible = computed(
  () => fullItems.value.length > 0
    || (approachingItems.value.length > 0 && !dismissedRecently.value),
)

/** Per-type i18n label key; unknown types fall back to the raw type string */
const typeLabelKeys: Record<string, string> = {
  device: 'dashboard.quota.typeDevice',
  probe: 'dashboard.quota.typeProbe',
  task: 'dashboard.quota.typeTask',
  remediation: 'dashboard.quota.typeRemediation',
  contact: 'dashboard.quota.typeContact',
}

function typeLabel(type: string): string {
  const key = typeLabelKeys[type]
  return key ? t(key) : type
}

/** Percent display, e.g. 85% */
function percent(ratio: number): string {
  return `${Math.floor(ratio * 100)}%`
}

function dismiss(): void {
  dismissedAt.value = Date.now()
  setStorage(DISMISS_KEY, dismissedAt.value)
}

onMounted(async () => {
  try {
    items.value = await getQuotaUsage()
  } catch {
    // The hint is best-effort; load errors surface through the central
    // interceptor and must not break the dashboard.
  }
})
</script>

<template>
  <section
    v-if="visible"
    class="tk-quota-hint"
    :class="{ 'tk-quota-hint--full': fullItems.length > 0 }"
    role="status"
    :aria-label="t('dashboard.quota.title')"
  >
    <div class="tk-quota-hint__body">
      <div class="tk-quota-hint__head">
        <i
          class="tk-quota-hint__icon"
          :class="fullItems.length > 0 ? 'i-ep-warning-filled' : 'i-ep-info-filled'"
        />
        <span class="tk-quota-hint__title">
          {{ fullItems.length > 0 ? t('dashboard.quota.fullTitle') : t('dashboard.quota.nearTitle') }}
        </span>
      </div>
      <ul class="tk-quota-hint__items">
        <li
          v-for="it in nearItems"
          :key="it.type"
          class="tk-quota-hint__item"
          :class="{ 'tk-quota-hint__item--full': it.ratio >= 1 }"
        >
          <span class="tk-quota-hint__item-name">{{ typeLabel(it.type) }}</span>
          <span class="tk-quota-hint__item-usage">{{ it.used }}/{{ it.ceiling }}</span>
          <span class="tk-quota-hint__item-ratio">{{ percent(it.ratio) }}</span>
        </li>
      </ul>
    </div>
    <div class="tk-quota-hint__actions">
      <a
        class="tk-quota-hint__link"
        :href="EDITIONS_URL"
        target="_blank"
        rel="noopener"
      >
        {{ t('dashboard.quota.viewEditions') }}
        <i class="i-ep-arrow-right" />
      </a>
      <button
        v-if="fullItems.length === 0"
        class="tk-quota-hint__dismiss"
        type="button"
        :aria-label="t('dashboard.quota.dismiss')"
        @click="dismiss"
      >
        <i class="i-ep-close" />
      </button>
    </div>
  </section>
</template>

<style scoped lang="scss">
.tk-quota-hint {
  display: flex;
  gap: var(--tk-spacing-md);
  align-items: center;
  justify-content: space-between;
  padding: var(--tk-spacing-md) var(--tk-spacing-lg);
  background-color: var(--tk-warning-color-bg);
  border: 1px solid var(--tk-warning-color);
  border-radius: var(--tk-border-radius-lg);

  // At-ceiling variant: stronger danger accent, cannot be dismissed
  &--full {
    background-color: var(--tk-danger-color-bg);
    border-color: var(--tk-danger-color);
  }

  &__body {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-sm) var(--tk-spacing-lg);
    align-items: center;
    min-width: 0;
  }

  &__head {
    display: inline-flex;
    gap: var(--tk-spacing-xs);
    align-items: center;
  }

  &__icon {
    font-size: 16px;
    color: var(--tk-warning-color);
  }

  &--full &__icon {
    color: var(--tk-danger-color);
  }

  &__title {
    font-size: var(--tk-font-size-sm);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
  }

  &__items {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-xs);
    padding: 0;
    margin: 0;
    list-style: none;
  }

  &__item {
    display: inline-flex;
    gap: var(--tk-spacing-xs);
    align-items: center;
    padding: 2px var(--tk-spacing-sm);
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-regular);
    background-color: var(--tk-bg-surface);
    border: 1px solid var(--tk-border-color);
    border-radius: var(--tk-radius-full);

    &--full {
      color: var(--tk-danger-color);
      border-color: var(--tk-danger-color);
    }
  }

  &__item-name {
    font-family: var(--tk-font-family);
    font-weight: var(--tk-font-weight-medium);
  }

  &__item-ratio {
    font-weight: var(--tk-font-weight-semibold);
  }

  &__actions {
    display: inline-flex;
    flex-shrink: 0;
    gap: var(--tk-spacing-sm);
    align-items: center;
  }

  &__link {
    display: inline-flex;
    gap: var(--tk-spacing-xs);
    align-items: center;
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-primary-color);
    text-decoration: none;

    &:hover { text-decoration: underline; text-underline-offset: 3px; }
    &:focus-visible { outline: 2px solid var(--tk-primary-color); outline-offset: 2px; }
  }

  &__dismiss {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    color: var(--tk-text-secondary);
    cursor: pointer;
    background: transparent;
    border: none;
    border-radius: var(--tk-radius-sm);

    &:hover {
      color: var(--tk-text-primary);
      background-color: var(--tk-fill-color-light);
    }

    &:focus-visible {
      outline: 2px solid var(--tk-primary-color);
      outline-offset: 2px;
    }
  }
}
</style>
