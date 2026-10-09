// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * TrendChartCanvas - the ECharts-backed chart renderer.
 *
 * This is the heavy chart implementation. It is intentionally split out from
 * TrendChart.vue so that TrendChart.vue can lazy-load it via
 * `defineAsyncComponent`, keeping it out of the main bundle until a chart
 * actually renders. ECharts itself is loaded on demand via the shared
 * `ensureEcharts` dynamic import, so the library stays in a single lazy chunk.
 *
 * Responsibilities:
 * - option prop to pass a full ECharts config directly (takes precedence over convenience props)
 * - automatic tooltip color update on light/dark theme switch
 * - ResizeObserver for container size adaptation
 * - automatic ECharts instance disposal on unmount
 *
 * Loading skeleton and empty state overlays are handled by the TrendChart wrapper.
 */
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import type * as echarts from 'echarts/core'
import { useAppStore } from '../stores/app'
import { useEventListener } from '../composables/useEventListener'
import { ensureEcharts } from '../composables/useChart'

interface TrendChartProps {
  /** ECharts config; when provided, takes precedence over convenience props like data/title */
  option?: echarts.EChartsCoreOption
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
  /** Whether the parent wrapper is showing the loading skeleton (used to trigger resize when it ends) */
  loading?: boolean
  /** Whether the parent wrapper is showing the empty state (used to trigger resize when it ends) */
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

const appStore = useAppStore()

const chartRef = ref<HTMLElement | null>(null)
let chartInstance: echarts.ECharts | null = null
let resizeObserver: ResizeObserver | null = null
/** Whether the owning component has been unmounted (guards against init after unmount) */
let disposed = false

/** Read the current value of a CSS variable (ECharts renders on canvas, so values must be resolved to actual colors) */
function getCssVar(name: string): string {
  if (typeof window === 'undefined') return ''
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

/** Build tooltip style (follows light/dark theme switching) */
function buildTooltip(): Record<string, unknown> {
  return {
    backgroundColor: getCssVar('--tk-bg-color-overlay') || '#ffffff',
    borderColor: getCssVar('--tk-border-color') || '#e2e8f0',
    borderWidth: 1,
    padding: 8,
    textStyle: {
      color: getCssVar('--tk-text-primary') || '#0f172a',
      fontSize: 13,
    },
    extraCssText: 'border-radius: 6px;',
  }
}

/** Build ECharts config from convenience props */
function buildDataOption(): echarts.EChartsCoreOption {
  return {
    title: props.title ? { text: props.title, textStyle: { fontSize: 14 } } : undefined,
    grid: { left: 50, right: 20, top: 40, bottom: 40 },
    xAxis: {
      type: 'category',
      data: props.data.map((d) => d.time),
      boundaryGap: false,
    },
    yAxis: {
      type: 'value',
      axisLabel: { formatter: `{value}${props.unit}` },
    },
    series: [
      {
        type: 'line',
        data: props.data.map((d) => d.value),
        smooth: true,
        areaStyle: { opacity: 0.15 },
        markLine: props.showThreshold
          ? { data: [{ yAxis: props.thresholdValue, name: 'Threshold' }] }
          : undefined,
      },
    ],
    tooltip: {
      trigger: 'axis',
      valueFormatter: (val: unknown) => `${String(val)}${props.unit}`,
    },
  }
}

/** Base config: option prop takes precedence; otherwise built from convenience props */
const baseOption = computed<echarts.EChartsCoreOption>(() => props.option ?? buildDataOption())

/** Full config after merging tooltip theme styles */
function buildMergedOption(): echarts.EChartsCoreOption {
  const base = baseOption.value as Record<string, unknown>
  const baseTooltip = (base.tooltip as Record<string, unknown> | undefined) ?? {}
  return {
    ...base,
    tooltip: { ...buildTooltip(), ...baseTooltip },
  }
}

/** Initialize the chart instance (async — ECharts is loaded on demand via ensureEcharts) */
async function initChart() {
  if (!chartRef.value || disposed) return
  const core = await ensureEcharts()
  // Guard against unmount during the async import
  if (disposed || !chartRef.value) return
  chartInstance = core.init(chartRef.value)
  chartInstance.setOption(buildMergedOption())
}

/** Apply config updates (called when option changes or theme switches) */
function applyOption() {
  if (!chartInstance) return
  chartInstance.setOption(buildMergedOption(), { notMerge: false })
}

/** Redraw on window resize */
function handleWindowResize() {
  chartInstance?.resize()
}

/** Watch base config changes */
watch(baseOption, () => applyOption(), { deep: true })

/** Watch theme changes: re-read CSS variables to update tooltip and redraw */
watch(
  () => appStore.theme,
  () => {
    applyOption()
    chartInstance?.resize()
  },
)

/** Ensure the chart is sized correctly after loading/empty ends */
watch(
  () => [props.loading, props.empty],
  () => {
    if (!props.loading && !props.empty) {
      chartInstance?.resize()
    }
  },
)

onMounted(() => {
  // initChart is async (dynamically imports ECharts); fire and forget — the
  // internal `disposed` guard handles cleanup if the component unmounts first.
  void initChart()
  if (chartRef.value && typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => chartInstance?.resize())
    resizeObserver.observe(chartRef.value)
  }
})

// useEventListener handles attach-on-mount and detach-on-unmount automatically.
useEventListener(window, 'resize', handleWindowResize)

onUnmounted(() => {
  disposed = true
  resizeObserver?.disconnect()
  resizeObserver = null
  chartInstance?.dispose()
  chartInstance = null
})
</script>

<template>
  <div
    ref="chartRef"
    class="tk-trend-chart__canvas"
  />
</template>

<style scoped lang="scss">
.tk-trend-chart__canvas {
  width: 100%;
  height: 100%;
}
</style>
