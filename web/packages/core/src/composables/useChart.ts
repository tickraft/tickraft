// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { ref, computed, onMounted, onBeforeUnmount, watch, toValue } from 'vue'
import type { Ref, ComputedRef, MaybeRefOrGetter } from 'vue'
import type * as echarts from 'echarts/core'
import { useAppStore } from '../stores/app'

/**
 * Lazy-load and register the required ECharts modules once.
 *
 * ECharts is dynamically imported on first use (via the `./echarts`
 * registration module, which uses static named imports so the library is
 * tree-shakeable) and the resulting core is cached, keeping it out of the main
 * bundle until a chart actually mounts. Subsequent calls reuse the cached core.
 *
 * Shared by `useChart` and `TrendChartCanvas` so that ECharts is loaded via a
 * single dynamic import path, allowing Rollup to split it into one lazy chunk.
 */
let echartsCore: typeof import('echarts/core') | null = null
let echartsPromise: Promise<typeof import('echarts/core')> | null = null

export function ensureEcharts(): Promise<typeof import('echarts/core')> {
  if (echartsCore) return Promise.resolve(echartsCore)
  if (!echartsPromise) {
    // Dynamically import the registration module; static named imports inside
    // it keep ECharts tree-shakeable, while the dynamic boundary keeps it lazy.
    echartsPromise = import('./echarts').then((m) => {
      echartsCore = m.echarts
      return m.echarts
    })
  }
  return echartsPromise
}

/** CSS variable name for the theme grid line color */
const THEME_GRID_VAR = '--tk-border-color-light'
/** CSS variable name for the theme text color */
const THEME_TEXT_VAR = '--tk-text-regular'
/** Grid line color fallback (used when reading the CSS variable fails) */
const FALLBACK_GRID_COLOR = 'rgba(148,163,184,0.2)'
/** Text color fallback */
const FALLBACK_TEXT_COLOR = '#94a3b8'
/** Resize debounce (ms) */
const RESIZE_DEBOUNCE = 100

/**
 * Read the computed value of a CSS variable.
 *
 * ECharts renders to canvas and cannot directly resolve `var(--xxx)` strings,
 * so getComputedStyle is used to read the actual color value under the current
 * theme, allowing chart colors to follow light/dark theme switches.
 * @param name - CSS variable name
 * @param fallback - fallback value when reading fails
 */
function readCssVar(name: string, fallback: string): string {
  try {
    const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
    return value || fallback
  } catch {
    return fallback
  }
}

/** Build the ECharts color config fragment for the current theme */
function buildThemeOption(): { gridColor: string; textColor: string } {
  return {
    gridColor: readCssVar(THEME_GRID_VAR, FALLBACK_GRID_COLOR),
    textColor: readCssVar(THEME_TEXT_VAR, FALLBACK_TEXT_COLOR),
  }
}

/** Debounce helper (avoids frequent resize calls) */
function debounce<T extends (...args: never[]) => void>(
  fn: T,
  wait: number,
): (...args: Parameters<T>) => void {
  let timer: ReturnType<typeof setTimeout> | null = null
  return (...args: Parameters<T>) => {
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => fn(...args), wait)
  }
}

/**
 * Determine whether the ECharts option represents empty data.
 * @param option - ECharts option
 * @returns true when the data is empty
 */
function isOptionEmpty(option: echarts.EChartsCoreOption): boolean {
  const series = (option as { series?: Array<{ data?: unknown }> }).series
  if (!Array.isArray(series) || series.length === 0) {
    return true
  }
  return series.every((item) => {
    const seriesData = item.data
    return seriesData == null || (Array.isArray(seriesData) && seriesData.length === 0)
  })
}

/** useChart return type */
export interface UseChartReturn {
  /** Chart container ref, bound to the target element in the template */
  chartRef: Ref<HTMLElement | null>
  /** Loading state (the component renders a skeleton while loading) */
  loading: Ref<boolean>
  /** Empty data state (the component renders an empty state) */
  empty: Ref<boolean>
  /** Empty state text */
  emptyText: Ref<string>
  /** Whether the data is empty (computed, includes auto detection and manual setting) */
  isEmpty: ComputedRef<boolean>
  /** Set the chart option */
  setOption: (option: echarts.EChartsCoreOption, notMerge?: boolean) => void
  /** Set the loading state */
  setLoading: (value: boolean) => void
  /** Set the empty data state */
  setEmpty: (value: boolean, text?: string) => void
  /** Resize the chart */
  resize: () => void
  /** Dispose the chart instance */
  dispose: () => void
}

/**
 * ECharts initialization, theme sync and disposal composable.
 *
 * - Theme sync: watches `useAppStore().theme` and reads the actual values of
 *   the `--tk-border-color-light` and `--tk-text-regular` CSS
 *   variables to refresh chart colors.
 * - Size observation: uses ResizeObserver to listen for container size changes,
 *   debounced at 100ms before resize.
 * - Resource release: disposes the chart instance and disconnects the
 *   ResizeObserver on unmount to prevent memory leaks.
 * - State control: exposes loading / empty states for the component to render
 *   skeleton and empty placeholders.
 * @param option - initial option; supports Ref / Getter / plain object
 * @returns chart ref and control methods
 */
export function useChart(option?: MaybeRefOrGetter<echarts.EChartsCoreOption>): UseChartReturn {
  const appStore = useAppStore()

  /** Chart container ref */
  const chartRef: Ref<HTMLElement | null> = ref(null)
  /** Loading state */
  const loading = ref(false)
  /** Empty data state */
  const empty = ref(false)
  /** Empty state text */
  const emptyText = ref('')
  /** Current option */
  const currentOption = ref<echarts.EChartsCoreOption>(option ? toValue(option) : {})
  /** Whether the data is empty (manual setting takes precedence; otherwise auto-detected from the data) */
  const isEmpty: ComputedRef<boolean> = computed(
    () => empty.value || isOptionEmpty(currentOption.value),
  )

  let chartInstance: echarts.ECharts | null = null
  let resizeObserver: ResizeObserver | null = null
  /** Whether the owning component has been unmounted (guards against init after unmount) */
  let disposed = false

  /**
   * Apply the current theme colors (reads CSS variables then merges into the instance)
   */
  function applyTheme(): void {
    if (!chartInstance) return
    const { gridColor, textColor } = buildThemeOption()
    const current = chartInstance.getOption() as {
      xAxis?: unknown[]
      yAxis?: unknown[]
    } | undefined
    const hasAxis =
      (Array.isArray(current?.xAxis) && (current?.xAxis?.length ?? 0) > 0) ||
      (Array.isArray(current?.yAxis) && (current?.yAxis?.length ?? 0) > 0)

    const themePatch: Record<string, unknown> = {
      textStyle: { color: textColor },
      grid: { borderColor: gridColor },
    }
    if (hasAxis) {
      themePatch.xAxis = {
        axisLine: { lineStyle: { color: gridColor } },
        axisLabel: { color: textColor },
        splitLine: { lineStyle: { color: gridColor } },
      }
      themePatch.yAxis = {
        axisLine: { lineStyle: { color: gridColor } },
        axisLabel: { color: textColor },
        splitLine: { lineStyle: { color: gridColor } },
      }
    }
    chartInstance.setOption(themePatch as echarts.EChartsCoreOption)
  }

  /**
   * Initialize the chart instance.
   *
   * ECharts is loaded on demand via `ensureEcharts` (dynamic import), so
   * initialization is asynchronous. The latest `currentOption` is applied once
   * the library is ready; if the component unmounts while the import is in
   * flight, initialization is skipped via the `disposed` guard.
   */
  async function initChart(): Promise<void> {
    if (!chartRef.value || disposed) return
    const core = await ensureEcharts()
    // Guard against unmount during the async import
    if (disposed || !chartRef.value) return
    chartInstance = core.init(chartRef.value)
    chartInstance.setOption(currentOption.value)
    applyTheme()
    observeResize()
  }

  /**
   * Observe container size changes (100ms debounce)
   */
  function observeResize(): void {
    if (!chartRef.value || typeof ResizeObserver === 'undefined') return
    const debouncedResize = debounce((): void => {
      chartInstance?.resize()
    }, RESIZE_DEBOUNCE)
    resizeObserver = new ResizeObserver(() => {
      debouncedResize()
    })
    resizeObserver.observe(chartRef.value)
  }

  /**
   * Set the chart option
   * @param newOption - ECharts option
   * @param notMerge - whether not to merge (default false, i.e. merge)
   */
  function setOption(newOption: echarts.EChartsCoreOption, notMerge = false): void {
    currentOption.value = newOption
    chartInstance?.setOption(newOption, { notMerge })
  }

  /**
   * Resize the chart
   */
  function resize(): void {
    chartInstance?.resize()
  }

  /**
   * Dispose the chart instance and disconnect the size observer
   */
  function dispose(): void {
    disposed = true
    if (resizeObserver) {
      resizeObserver.disconnect()
      resizeObserver = null
    }
    chartInstance?.dispose()
    chartInstance = null
  }

  /**
   * Set the loading state
   * @param value - whether loading
   */
  function setLoading(value: boolean): void {
    loading.value = value
  }

  /**
   * Set the empty data state
   * @param value - whether empty
   * @param text - empty state text (optional)
   */
  function setEmpty(value: boolean, text?: string): void {
    empty.value = value
    if (text !== undefined) {
      emptyText.value = text
    } else if (value && !emptyText.value) {
      emptyText.value = 'No data'
    }
  }

  onMounted(() => {
    // initChart is async (dynamically imports ECharts); fire and forget — the
    // internal `disposed` guard handles cleanup if the component unmounts first.
    void initChart()
  })

  // Refresh colors when the theme changes (CSS variable values change with the theme switch)
  watch(
    () => appStore.theme,
    () => {
      if (chartInstance) {
        applyTheme()
      }
    },
  )

  // Auto-update when the passed option changes
  if (option !== undefined) {
    watch(
      () => toValue(option),
      (newOption) => {
        setOption(newOption)
      },
      { deep: true },
    )
  }

  // Dispose the chart instance on unmount to prevent memory leaks
  onBeforeUnmount(() => {
    dispose()
  })

  return {
    chartRef,
    loading,
    empty,
    emptyText,
    isEmpty,
    setOption,
    setLoading,
    setEmpty,
    resize,
    dispose,
  }
}
