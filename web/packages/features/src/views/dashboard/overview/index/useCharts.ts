// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details

import { computed, ref } from 'vue'
import type { ComputedRef, Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useChart, useAppStore } from '@tickraft/core'
import type { AlertRecord } from '../../../../api/prism'

/** Tooltip background color fallback */
const TOOLTIP_BG_FALLBACK = 'rgba(15, 20, 25, 0.92)'
/** Tooltip border color fallback */
const TOOLTIP_BORDER_FALLBACK = 'rgba(255,255,255,0.08)'
/** Tooltip text color fallback */
const TOOLTIP_TEXT_FALLBACK = '#e2e8f0'
/** Donut border color fallback */
const DONUT_BORDER_FALLBACK = '#ffffff'
/** Danger color fallback */
const DANGER_FALLBACK = '#ef4444'
/** Warning color fallback */
const WARNING_FALLBACK = '#f59e0b'
/** Info color fallback */
const INFO_FALLBACK = '#0ea5e9'
/** Success color fallback */
const SUCCESS_FALLBACK = '#16a34a'
/** Placeholder color fallback */
const PLACEHOLDER_FALLBACK = '#94a3b8'
/** Text primary fallback */
const TEXT_PRIMARY_FALLBACK = '#0f172a'
/** Text secondary fallback */
const TEXT_SECONDARY_FALLBACK = '#64748b'

/** Severity series color mapping */
type SeverityColor = {
  key: 'critical' | 'error' | 'warning' | 'info'
  cssVar: string
  fallback: string
  labelKey: string
}

/** Alert trend severity series definition. Error shares the danger hue with
 *  critical (the tag system maps both to danger); the legend label tells
 *  them apart. */
const TREND_SERIES: SeverityColor[] = [
  { key: 'critical', cssVar: '--tk-danger-color', fallback: DANGER_FALLBACK, labelKey: 'common.dashboard.severityCritical' },
  { key: 'error', cssVar: '--tk-danger-color', fallback: DANGER_FALLBACK, labelKey: 'common.dashboard.severityError' },
  { key: 'warning', cssVar: '--tk-warning-color', fallback: WARNING_FALLBACK, labelKey: 'common.dashboard.severityWarning' },
  { key: 'info', cssVar: '--tk-info-color', fallback: INFO_FALLBACK, labelKey: 'common.dashboard.severityInfo' },
]

/** Bar chart series definition */
const BAR_SERIES = [
  { key: 'success' as const, cssVar: '--tk-success-color', fallback: SUCCESS_FALLBACK, labelKey: 'common.status.success' },
  { key: 'failed' as const, cssVar: '--tk-danger-color', fallback: DANGER_FALLBACK, labelKey: 'common.status.failed' },
]

/**
 * Read the computed value of a CSS variable.
 *
 * ECharts renders to canvas and cannot parse `var(--xxx)` strings,
 * so getComputedStyle is used to read the actual color value under the current theme;
 * accessing `appStore.theme` inside the method establishes a reactive dependency,
 * triggering computed property re-evaluation on theme switch,
 * and useChart's internal watch synchronously refreshes chart colors
 * to enable light / dark theme linkage.
 */
function readCssVar(
  appStore: ReturnType<typeof useAppStore>,
  name: string,
  fallback: string,
): string {
  void appStore.theme
  try {
    const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
    return value || fallback
  } catch {
    return fallback
  }
}

/** Build tooltip configuration (theme-aware) */
function buildTooltip(appStore: ReturnType<typeof useAppStore>): Record<string, unknown> {
  return {
    backgroundColor: readCssVar(appStore, '--tk-bg-color-overlay', TOOLTIP_BG_FALLBACK),
    borderColor: readCssVar(appStore, '--tk-border-color', TOOLTIP_BORDER_FALLBACK),
    borderWidth: 1,
    padding: 8,
    textStyle: {
      color: readCssVar(appStore, '--tk-text-primary', TOOLTIP_TEXT_FALLBACK),
      fontSize: 12,
    },
    extraCssText: 'border-radius: 6px;',
  }
}

/** Alert trend data point (derived from alert records) */
interface AlertTrendPoint {
  date: string
  critical: number
  error: number
  warning: number
  info: number
}

/** Task execution trend data point (derived from execution stats) */
interface TaskExecTrendPoint {
  date: string
  success: number
  failed: number
}

/** Status distribution for donut chart */
interface StatusDistItem {
  status: string
  count: number
}

/**
 * Dashboard chart composable.
 *
 * Accepts reactive data from the parent component instead of reading hardcoded
 * mock arrays. All chart options are computed properties that read CSS variables
 * via `readCssVar` for theme linkage.
 */
export function useDashboardCharts(): {
  trendChartRef: ReturnType<typeof useChart>['chartRef']
  donutChartRef: ReturnType<typeof useChart>['chartRef']
  barChartRef: ReturnType<typeof useChart>['chartRef']
  trendLegend: ComputedRef<{ name: string; color: string; total: number }[]>
  setChartData: (data: {
    alertTrend?: AlertTrendPoint[]
    taskExecTrend?: TaskExecTrendPoint[]
    statusDist?: StatusDistItem[]
    alerts?: AlertRecord[]
  }) => void
} {
  const { t } = useI18n()
  const appStore = useAppStore()

  /** Reactive chart data sources (populated by setChartData) */
  const alertTrendData: Ref<AlertTrendPoint[]> = ref([])
  const taskExecTrendData: Ref<TaskExecTrendPoint[]> = ref([])
  const statusDistData: Ref<StatusDistItem[]> = ref([])

  /** Update chart data from external source */
  function setChartData(data: {
    alertTrend?: AlertTrendPoint[]
    taskExecTrend?: TaskExecTrendPoint[]
    statusDist?: StatusDistItem[]
    alerts?: AlertRecord[]
  }): void {
    if (data.alertTrend) alertTrendData.value = data.alertTrend
    if (data.taskExecTrend) taskExecTrendData.value = data.taskExecTrend
    if (data.statusDist) statusDistData.value = data.statusDist
  }

  /** Sum helper */
  const sum = (arr: number[]): number => arr.reduce((acc, val) => acc + (val || 0), 0)

  // ===== Alert trend stacked area chart =====
  const trendLegend = computed(() =>
    TREND_SERIES.map((s) => ({
      name: t(s.labelKey),
      color: readCssVar(appStore, s.cssVar, s.fallback),
      total: sum(alertTrendData.value.map((d) => d[s.key])),
    })),
  )

  const trendOption = computed(() => {
    const dates = alertTrendData.value.map((d) => d.date)
    const tooltip = buildTooltip(appStore)
    return {
      tooltip: {
        trigger: 'axis',
        ...tooltip,
      },
      legend: { show: false },
      grid: { left: 36, right: 16, top: 16, bottom: 24, containLabel: true },
      xAxis: {
        type: 'category',
        data: dates,
        boundaryGap: false,
        axisTick: { show: false },
      },
      yAxis: { type: 'value', minInterval: 1, splitNumber: 4 },
      series: TREND_SERIES.map((s, idx) => ({
        name: t(s.labelKey),
        type: 'line',
        stack: 'alert',
        smooth: true,
        showSymbol: false,
        areaStyle: { opacity: 0.55 - idx * 0.1 },
        lineStyle: { width: 1.5 },
        data: alertTrendData.value.map((d) => d[s.key]),
        itemStyle: { color: readCssVar(appStore, s.cssVar, s.fallback) },
      })),
    }
  })

  // ===== Status distribution donut chart =====
  const donutOption = computed(() => {
    const STATUS_META: Record<string, { cssVar: string; fallback: string; labelKey: string }> = {
      normal: { cssVar: '--tk-success-color', fallback: SUCCESS_FALLBACK, labelKey: 'common.dashboard.assetStatus.normal' },
      warning: { cssVar: '--tk-warning-color', fallback: WARNING_FALLBACK, labelKey: 'common.dashboard.assetStatus.warning' },
      error: { cssVar: '--tk-danger-color', fallback: DANGER_FALLBACK, labelKey: 'common.dashboard.assetStatus.error' },
      abnormal: { cssVar: '--tk-danger-color', fallback: DANGER_FALLBACK, labelKey: 'common.dashboard.assetStatus.error' },
      offline: { cssVar: '--tk-text-placeholder', fallback: PLACEHOLDER_FALLBACK, labelKey: 'common.dashboard.assetStatus.offline' },
      unknown: { cssVar: '--tk-text-disabled', fallback: PLACEHOLDER_FALLBACK, labelKey: 'common.dashboard.assetStatus.unknown' },
    }

    const seriesData = statusDistData.value
      .filter((s) => s.count > 0)
      .map((s) => {
        const meta = STATUS_META[s.status]
        const color = meta
          ? readCssVar(appStore, meta.cssVar, meta.fallback)
          : PLACEHOLDER_FALLBACK
        const name = meta ? t(meta.labelKey) : s.status
        return {
          name,
          value: s.count,
          itemStyle: { color },
        }
      })
    const total = seriesData.reduce((acc, item) => acc + item.value, 0)
    const tooltip = buildTooltip(appStore)

    return {
      tooltip: {
        trigger: 'item',
        formatter: '{b}: {c} ({d}%)',
        ...tooltip,
      },
      legend: { show: false },
      graphic: {
        type: 'group',
        left: 'center',
        top: 'center',
        children: [
          {
            type: 'text',
            style: {
              text: String(total),
              textAlign: 'center',
              fill: readCssVar(appStore, '--tk-text-primary', TEXT_PRIMARY_FALLBACK),
              fontSize: 28,
              fontWeight: 700,
              fontFamily: 'Inter, sans-serif',
            },
            top: -8,
          },
          {
            type: 'text',
            style: {
              text: t('common.dashboard.totalResources'),
              textAlign: 'center',
              fill: readCssVar(appStore, '--tk-text-secondary', TEXT_SECONDARY_FALLBACK),
              fontSize: 11,
              fontFamily: 'Inter, sans-serif',
            },
            top: 20,
          },
        ],
      },
      series: [
        {
          type: 'pie',
          radius: ['62%', '82%'],
          center: ['50%', '50%'],
          avoidLabelOverlap: true,
          itemStyle: {
            borderColor: readCssVar(appStore, '--tk-bg-color', DONUT_BORDER_FALLBACK),
            borderWidth: 2,
          },
          label: { show: false },
          labelLine: { show: false },
          emphasis: {
            scale: true,
            scaleSize: 6,
            label: {
              show: true,
              fontSize: 13,
              fontWeight: 600,
              formatter: '{b}\n{c} ({d}%)',
            },
          },
          data: seriesData,
        },
      ],
    }
  })

  // ===== Task execution trend grouped bar chart =====
  const barOption = computed(() => {
    const dates = taskExecTrendData.value.map((d) => d.date)
    const tooltip = buildTooltip(appStore)
    return {
      tooltip: {
        trigger: 'axis',
        axisPointer: { type: 'shadow' },
        ...tooltip,
      },
      legend: {
        show: true,
        bottom: 0,
        itemWidth: 10,
        itemHeight: 10,
        textStyle: {
          color: readCssVar(appStore, '--tk-text-secondary', TEXT_SECONDARY_FALLBACK),
          fontSize: 11,
        },
      },
      grid: { left: 36, right: 16, top: 16, bottom: 40, containLabel: true },
      xAxis: {
        type: 'category',
        data: dates,
        axisTick: { show: false },
      },
      yAxis: { type: 'value', minInterval: 1, splitNumber: 4 },
      series: BAR_SERIES.map((s) => ({
        name: t(s.labelKey),
        type: 'bar',
        barWidth: 10,
        barGap: '20%',
        itemStyle: {
          color: readCssVar(appStore, s.cssVar, s.fallback),
          borderRadius: [3, 3, 0, 0],
        },
        data: taskExecTrendData.value.map((d) => d[s.key]),
      })),
    }
  })

  const trendChart = useChart(trendOption)
  const donutChart = useChart(donutOption)
  const barChart = useChart(barOption)

  return {
    trendChartRef: trendChart.chartRef,
    donutChartRef: donutChart.chartRef,
    barChartRef: barChart.chartRef,
    trendLegend,
    setChartData,
  }
}
