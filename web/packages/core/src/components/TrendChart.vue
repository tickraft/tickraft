// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * TrendChart - trend chart component.
 *
 * Thin async wrapper around TrendChartCanvas. The ECharts library (~300KB)
 * is loaded on demand via `defineAsyncComponent`, so it is kept out of the
 * main bundle until a chart actually renders.
 *
 * Wrapped on top of ECharts, supporting:
 * - option prop to pass a full ECharts config directly (takes precedence over convenience props)
 * - loading skeleton / empty state overlays
 * - automatic tooltip color update on light/dark theme switch (delegated to the canvas)
 * - ResizeObserver for container size adaptation (delegated to the canvas)
 * - automatic ECharts instance disposal on unmount (delegated to the canvas)
 */
import { computed, defineAsyncComponent } from 'vue'
import type { EChartsCoreOption } from 'echarts/core'
import PageEmpty from './PageEmpty.vue'

// Lazy-load the canvas (which statically imports ECharts) so the ECharts
// library (~300KB) is only fetched when a chart actually renders, not when
// the consumer view chunk loads.
const TrendChartCanvas = defineAsyncComponent(() => import('./TrendChartCanvas.vue'))

interface TrendChartProps {
  /** ECharts config; when provided, takes precedence over convenience props like data/title */
  option?: EChartsCoreOption
  /** Data source (convenience prop; used when option is not provided) */
  data?: Array<{ time: string; value: number }>
  /** Chart title (convenience prop) */
  title?: string
  /** Whether to show the threshold line (convenience prop) */
  showThreshold?: boolean
  /** Threshold value (convenience prop) */
  thresholdValue?: number
  /** Value unit (convenience prop) */
  unit?: string
  /** Whether to show the loading skeleton */
  loading?: boolean
  /** Whether to show the empty state */
  empty?: boolean
  /** Container height; accepts a string (e.g. '300px') or a number (auto-appended with px) */
  height?: string | number
}

const props = withDefaults(defineProps<TrendChartProps>(), {
  option: undefined,
  data: () => [],
  title: '',
  showThreshold: false,
  thresholdValue: 0,
  unit: '',
  loading: false,
  empty: false,
  height: '300px',
})

/** Container height style (a number is auto-appended with px) */
const heightStyle = computed(() => {
  const h = props.height
  return typeof h === 'number' ? `${h}px` : h
})
</script>

<template>
  <div
    class="tk-trend-chart"
    :style="{ height: heightStyle }"
    role="img"
    :aria-label="title || 'Chart'"
  >
    <TrendChartCanvas v-bind="$props" />
    <div
      v-if="loading"
      class="tk-trend-chart__loading"
      aria-busy="true"
      aria-live="polite"
    >
      <el-skeleton
        :rows="5"
        animated
      />
    </div>
    <div
      v-if="empty"
      class="tk-trend-chart__empty"
    >
      <slot name="empty">
        <PageEmpty />
      </slot>
    </div>
  </div>
</template>

<style scoped lang="scss">
.tk-trend-chart {
  position: relative;
  width: 100%;
}

.tk-trend-chart__loading,
.tk-trend-chart__empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: var(--tk-bg-surface);
}

.tk-trend-chart__loading {
  padding: var(--tk-spacing-md);
}

/* Accessibility: respect the user's reduced-motion preference, disable skeleton animation */
@media (prefers-reduced-motion: reduce) {
  .tk-trend-chart__loading :deep(.el-skeleton) {
    --el-skeleton-to-color: var(--tk-bg-active);

    animation: none;
  }
}
</style>
