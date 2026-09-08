// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * PublicStatus - standalone public status page.
 *
 * Renders the aggregated backend view (GET /api/v1/status, unauthenticated)
 * as a full-screen page with no DefaultLayout shell: page header with the
 * overall banner, one row per component, and a "Powered by Tickraft"
 * footer. The view auto-refreshes every 60 seconds. While the page is
 * disabled on the server (404) a neutral unavailable notice is shown.
 */
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getPublicStatus } from '../../../api/status'
import type { StatusLevel, StatusPublicView } from '../../../api/status'

const REFRESH_INTERVAL_MS = 60_000

const { t, d } = useI18n()

const view = ref<StatusPublicView | null>(null)
const unavailable = ref(false)
const loading = ref(true)

let timer: ReturnType<typeof setInterval> | null = null

/** Status level → banner/row tone (CSS class suffix) */
const statusTone: Record<StatusLevel, string> = {
  operational: 'operational',
  degraded: 'degraded',
  partial_outage: 'partial',
  major_outage: 'major',
  unknown: 'unknown',
}

const overallTone = computed(() => (view.value ? statusTone[view.value.overall] ?? 'unknown' : 'unknown'))

async function refresh(): Promise<void> {
  try {
    view.value = await getPublicStatus()
    unavailable.value = false
  } catch {
    // 404 while disabled, or a transient network error: both render the
    // neutral unavailable notice instead of a raw error page.
    unavailable.value = true
  } finally {
    loading.value = false
  }
}

function formatTime(iso?: string): string {
  if (!iso)
    return t('status.public.neverChecked')
  return d(new Date(iso), 'datetime')
}

onMounted(() => {
  void refresh()
  timer = setInterval(() => void refresh(), REFRESH_INTERVAL_MS)
})

onUnmounted(() => {
  if (timer)
    clearInterval(timer)
})
</script>

<template>
  <div class="tk-public-status">
    <main class="tk-public-status__main">
      <!-- Loading skeleton -->
      <div
        v-if="loading"
        class="tk-public-status__state"
      >
        <p>{{ t('status.public.loading') }}</p>
      </div>

      <!-- Disabled / unreachable -->
      <div
        v-else-if="unavailable || !view"
        class="tk-public-status__state"
      >
        <h1 class="tk-public-status__title">
          {{ t('status.public.unavailableTitle') }}
        </h1>
        <p class="tk-public-status__desc">
          {{ t('status.public.unavailableDesc') }}
        </p>
      </div>

      <template v-else>
        <header class="tk-public-status__header">
          <h1 class="tk-public-status__title">
            {{ view.title }}
          </h1>
          <p
            v-if="view.description"
            class="tk-public-status__desc"
          >
            {{ view.description }}
          </p>
          <div
            class="tk-public-status__banner"
            :class="`tk-public-status__banner--${overallTone}`"
            role="status"
          >
            {{ t(`status.level.${view.overall}`) }}
          </div>
        </header>

        <ul class="tk-public-status__list">
          <li
            v-for="c in view.components"
            :key="c.name"
            class="tk-public-status__row"
          >
            <div class="tk-public-status__row-info">
              <span class="tk-public-status__row-name">{{ c.name }}</span>
              <span
                v-if="c.description"
                class="tk-public-status__row-desc"
              >{{ c.description }}</span>
              <span class="tk-public-status__row-meta">
                {{ t('status.public.lastChecked', { time: formatTime(c.lastChecked) }) }}
              </span>
            </div>
            <span
              class="tk-public-status__pill"
              :class="`tk-public-status__pill--${statusTone[c.status] ?? 'unknown'}`"
            >
              {{ t(`status.level.${c.status}`) }}
            </span>
          </li>
        </ul>

        <p class="tk-public-status__updated">
          {{ t('status.public.updatedAt', { time: formatTime(view.updatedAt) }) }}
        </p>
      </template>
    </main>

    <footer class="tk-public-status__footer">
      <p>Powered by Tickraft</p>
    </footer>
  </div>
</template>

<style scoped lang="scss">
.tk-public-status {
  display: flex;
  flex-direction: column;
  min-height: 100vh;
  background-color: var(--tk-bg-color-page);

  &__main {
    flex: 1;
    width: 100%;
    max-width: 720px;
    padding: 48px 24px 32px;
    margin: 0 auto;
  }

  &__state {
    padding: 64px 0;
    text-align: center;
  }

  &__header {
    margin-bottom: 24px;
  }

  &__title {
    margin: 0 0 8px;
    font-size: 28px;
    font-weight: 600;
    color: var(--tk-text-primary);
  }

  &__desc {
    margin: 0 0 16px;
    font-size: var(--tk-font-size-base);
    color: var(--tk-text-secondary);
  }

  &__banner {
    display: inline-block;
    padding: 8px 20px;
    margin-bottom: 8px;
    font-size: var(--tk-font-size-base);
    font-weight: 600;
    color: #fff;
    border-radius: var(--tk-radius-md, 8px);

    &--operational { background-color: var(--el-color-success); }
    &--degraded { background-color: var(--el-color-warning); }
    &--partial { background-color: var(--el-color-warning); }
    &--major { background-color: var(--el-color-danger); }
    &--unknown { background-color: var(--el-color-info); }
  }

  &__list {
    padding: 0;
    margin: 0;
    list-style: none;
    background-color: var(--tk-bg-color);
    border: 1px solid var(--tk-border-color);
    border-radius: var(--tk-radius-md, 8px);
  }

  &__row {
    display: flex;
    gap: 16px;
    align-items: center;
    justify-content: space-between;
    padding: 14px 20px;

    & + & {
      border-top: 1px solid var(--tk-border-color);
    }
  }

  &__row-info {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  &__row-name {
    font-size: var(--tk-font-size-base);
    font-weight: 500;
    color: var(--tk-text-primary);
  }

  &__row-desc {
    font-size: var(--tk-font-size-sm);
    color: var(--tk-text-secondary);
  }

  &__row-meta {
    font-size: var(--tk-font-size-sm);
    color: var(--tk-text-placeholder, var(--tk-text-secondary));
  }

  &__pill {
    flex-shrink: 0;
    padding: 4px 12px;
    font-size: var(--tk-font-size-sm);
    color: #fff;
    border-radius: 12px;

    &--operational { background-color: var(--el-color-success); }
    &--degraded { background-color: var(--el-color-warning); }
    &--partial { background-color: var(--el-color-warning); }
    &--major { background-color: var(--el-color-danger); }
    &--unknown { background-color: var(--el-color-info); }
  }

  &__updated {
    margin-top: 16px;
    font-size: var(--tk-font-size-sm);
    color: var(--tk-text-placeholder, var(--tk-text-secondary));
    text-align: right;
  }

  &__footer {
    padding: var(--tk-spacing-md);
    font-size: var(--tk-font-size-sm);
    color: var(--tk-text-placeholder, var(--tk-text-secondary));
    text-align: center;
  }
}
</style>
