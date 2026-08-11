// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * TrendTab - Execution trend sub-component for task detail page.
 *
 * Displays 3 summary tiles (success / failed / max duration) and a
 * TrendChart line chart visualising the last 20 execution durations.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { TrendChart } from '@tickraft/core'
import { formatDuration } from '@tickraft/core'
import type { LogModel } from '../../../../types/task'

const props = defineProps<{
  logs: LogModel[]
}>()

const { t } = useI18n()

/** Compute duration in milliseconds from startedAt and finishedAt */
function computeDurationMs(log: LogModel): number {
  if (!log.finishedAt) return 0
  const start = new Date(log.startedAt).getTime()
  const end = new Date(log.finishedAt).getTime()
  return Math.max(0, end - start)
}

/** Last 20 logs sorted by start time ascending */
const recentLogs = computed<LogModel[]>(() => {
  return [...props.logs]
    .sort((a, b) => new Date(a.startedAt).getTime() - new Date(b.startedAt).getTime())
    .slice(-20)
})

const successCount = computed(() =>
  recentLogs.value.filter((l) => l.status === 'success').length,
)
const failedCount = computed(() =>
  recentLogs.value.filter((l) => l.status === 'failed').length,
)
const maxDuration = computed(() => {
  if (recentLogs.value.length === 0) return 0
  return Math.max(...recentLogs.value.map((l) => computeDurationMs(l)))
})

const chartData = computed(() => {
  return recentLogs.value.map((log, i) => ({
    time: `#${i + 1}`,
    value: computeDurationMs(log),
  }))
})

const isEmpty = computed(() => recentLogs.value.length === 0)
</script>

<template>
  <div class="tk-trend-tab">
    <!-- Summary tiles -->
    <div class="tk-trend-tab__summary">
      <div class="tk-trend-tab__tile">
        <span class="tk-trend-tab__tile-label">
          {{ t('task.task.detail.trendSuccess') }}
        </span>
        <span class="tk-trend-tab__tile-value tk-trend-tab__tile-value--ok">
          {{ successCount }}
        </span>
      </div>
      <div class="tk-trend-tab__tile">
        <span class="tk-trend-tab__tile-label">
          {{ t('task.task.detail.trendFailed') }}
        </span>
        <span class="tk-trend-tab__tile-value tk-trend-tab__tile-value--fail">
          {{ failedCount }}
        </span>
      </div>
      <div class="tk-trend-tab__tile">
        <span class="tk-trend-tab__tile-label">
          {{ t('task.task.detail.trendMaxDuration') }}
        </span>
        <span class="tk-trend-tab__tile-value">
          {{ formatDuration(maxDuration) }}
        </span>
      </div>
    </div>

    <!-- Chart toolbar -->
    <div class="tk-trend-tab__toolbar">
      <span class="tk-trend-tab__toolbar-label">
        {{ t('task.task.detail.executionTrend') }}
      </span>
      <div class="tk-trend-tab__legend">
        <span class="tk-trend-tab__legend-item">
          <span class="tk-trend-tab__legend-dot tk-trend-tab__legend-dot--duration" />
          {{ t('task.task.detail.trend') }}
        </span>
        <span class="tk-trend-tab__legend-item">
          <span class="tk-trend-tab__legend-dot tk-trend-tab__legend-dot--success" />
          {{ t('common.status.success') }}
        </span>
        <span class="tk-trend-tab__legend-item">
          <span class="tk-trend-tab__legend-dot tk-trend-tab__legend-dot--failed" />
          {{ t('common.status.failed') }}
        </span>
      </div>
    </div>

    <!-- Trend chart -->
    <TrendChart
      :data="chartData"
      :empty="isEmpty"
      height="300px"
      unit="ms"
    />
  </div>
</template>

<style scoped lang="scss">
.tk-trend-tab {
  &__summary {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: var(--tk-spacing-4);
    margin-bottom: var(--tk-spacing-8);
  }

  &__tile {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: var(--tk-spacing-5) var(--tk-spacing-6);
    background: var(--tk-bg-fill-light);
    border: 1px solid var(--tk-border-color-light);
    border-radius: var(--tk-radius-md);
  }

  &__tile-label {
    font-family: var(--tk-font-mono);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: var(--tk-letter-wide);
  }

  &__tile-value {
    font-family: var(--tk-font-display);
    font-size: var(--tk-font-size-lg);
    font-weight: var(--tk-font-weight-bold);
    font-variant-numeric: tabular-nums;
    color: var(--tk-text-primary);

    &--ok { color: var(--tk-success-color); }
    &--fail { color: var(--tk-danger-color); }
  }

  &__toolbar {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-4);
    align-items: center;
    margin-bottom: var(--tk-spacing-5);
  }

  &__toolbar-label {
    font-family: var(--tk-font-mono);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: var(--tk-letter-wide);
  }

  &__legend {
    display: inline-flex;
    gap: var(--tk-spacing-4);
    align-items: center;
    margin-left: auto;
  }

  &__legend-item {
    display: inline-flex;
    gap: 4px;
    align-items: center;
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
  }

  &__legend-dot {
    width: 8px;
    height: 8px;
    border-radius: var(--tk-radius-circle);

    &--duration { background-color: var(--tk-primary-color); }
    &--success { background-color: var(--tk-success-color); }
    &--failed { background-color: var(--tk-danger-color); }
  }
}

@media (max-width: 960px) {
  .tk-trend-tab__summary {
    grid-template-columns: 1fr;
  }
}
</style>
