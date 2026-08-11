// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import UpgradeBanner from '../../components/UpgradeBanner.vue'
import { useDashboardCharts } from './useCharts'
import { formatDate, DataTable } from '@tickraft/core'
import { getGlobalStats, getRuntimeInfo } from '../../../../api/system'
import type { GlobalStats, RuntimeInfo } from '../../../../api/system'
import { getAlertRecords } from '../../../../api/prism'
import type { AlertRecord } from '../../../../api/prism'
import type { AlertSeverity } from '../../../../api/prism'

const { t } = useI18n()
const router = useRouter()

const loading = ref(false)
const globalStats = ref<GlobalStats | null>(null)
const runtimeInfo = ref<RuntimeInfo | null>(null)
const recentAlerts = ref<AlertRecord[]>([])

const { trendChartRef, donutChartRef, barChartRef, trendLegend, setChartData } = useDashboardCharts()
// Chart container refs are bound via string refs (`ref="trendChartRef"`) in the
// template; vue-tsc does not count string refs as usage, so mark them as read.
void trendChartRef
void donutChartRef
void barChartRef

/** Active time range for the segmented control */
const activeRange = ref<'today' | '7d' | '30d'>('today')
const rangeOptions = [
  { key: 'today' as const, labelKey: 'common.dashboard.rangeToday' },
  { key: '7d' as const, labelKey: 'common.dashboard.range7d' },
  { key: '30d' as const, labelKey: 'common.dashboard.range30d' },
]

/** Today's date for the eyebrow indicator */
const todayDate = new Date().toISOString().split('T')[0]

/**
 * Build alert trend chart data from recent alert records.
 * Groups records by date and severity — the backend does not yet expose
 * a dedicated trend endpoint, so we derive a lightweight chart from
 * the recent records page.
 */
function buildAlertTrend(records: AlertRecord[]): { date: string; critical: number; warning: number; info: number }[] {
  const byDate = new Map<string, { critical: number; warning: number; info: number }>()
  for (const r of records) {
    const d = r.firedAt ? new Date(r.firedAt) : null
    if (!d || isNaN(d.getTime())) continue
    const key = `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
    const entry = byDate.get(key) ?? { critical: 0, warning: 0, info: 0 }
    const sev = (r.severity ?? 'info') as 'critical' | 'warning' | 'info'
    if (sev in entry) entry[sev]++
    else entry.info++
    byDate.set(key, entry)
  }
  return Array.from(byDate.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([date, counts]) => ({ date, ...counts }))
}

/**
 * Build asset status distribution donut chart data from global stats.
 * The backend GlobalStats does not break down status, so we show a
 * single "total" segment when no detailed breakdown is available.
 */
function buildStatusDist(stats: GlobalStats | null): { status: string; count: number }[] {
  const total = stats?.totalDevices ?? 0
  if (total === 0) return []
  return [{ status: 'normal', count: total }]
}

/** Fetch all dashboard data from backend APIs */
async function fetchDashboardData(): Promise<void> {
  loading.value = true
  try {
    const [stats, info, alerts] = await Promise.all([
      getGlobalStats(),
      getRuntimeInfo(),
      getAlertRecords({ page: 1, pageSize: 50 }),
    ])
    globalStats.value = stats
    runtimeInfo.value = info
    recentAlerts.value = (alerts.items || []).slice(0, 8)

    // Feed derived chart data
    setChartData({
      alertTrend: buildAlertTrend(alerts.items || []),
      statusDist: buildStatusDist(stats),
    })
  } catch {
    // Errors are handled centrally by the interceptor
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchDashboardData()
})

/** Reload data when the time range changes */
watch(activeRange, () => {
  fetchDashboardData()
})

/** Stat card definition */
interface StatCard {
  index: string
  labelKey: string
  value: string
  unit?: string
  delta: string
  deltaDir: 'up' | 'down' | 'flat'
  deltaLabelKey: string
  icon: string
  iconClass: string
  accentClass: string
}

const statCards = computed<StatCard[]>(() => {
  const s = globalStats.value
  const firingAlerts = recentAlerts.value.filter((a) => a.status === 'firing').length
  return [
    {
      index: '01',
      labelKey: 'common.dashboard.totalAssets',
      value: String(s?.totalDevices ?? 0),
      delta: '—',
      deltaDir: 'flat',
      deltaLabelKey: 'common.dashboard.vsLastWeek',
      icon: 'i-ep-monitor',
      iconClass: 'tk-dash-stat__icon--primary',
      accentClass: 'tk-dash-card--accent-primary',
    },
    {
      index: '02',
      labelKey: 'common.dashboard.activeTasks',
      value: String(s?.totalTasks ?? 0),
      delta: '—',
      deltaDir: 'flat',
      deltaLabelKey: 'common.dashboard.vsYesterday',
      icon: 'i-ep-data-line',
      iconClass: 'tk-dash-stat__icon--success',
      accentClass: 'tk-dash-card--accent-success',
    },
    {
      index: '03',
      labelKey: 'common.dashboard.firingAlerts',
      value: String(firingAlerts),
      delta: '—',
      deltaDir: 'flat',
      deltaLabelKey: 'common.dashboard.vsYesterday',
      icon: 'i-ep-bell',
      iconClass: 'tk-dash-stat__icon--danger',
      accentClass: 'tk-dash-card--accent-danger',
    },
    {
      index: '04',
      labelKey: 'common.dashboard.todayExecutions',
      value: (s?.todayExecutions ?? 0).toLocaleString(),
      delta: `${(s?.todaySuccessRate ?? 0).toFixed(1)}%`,
      deltaDir: 'up',
      deltaLabelKey: 'common.dashboard.successRate',
      icon: 'i-ep-timer',
      iconClass: 'tk-dash-stat__icon--warning',
      accentClass: 'tk-dash-card--accent-warning',
    },
  ]
})

/** Quick action definition */
interface QuickAction {
  labelKey: string
  hintKey: string
  icon: string
  iconClass: string
  route: string
}

const quickActions: QuickAction[] = [
  {
    labelKey: 'common.dashboard.newAsset',
    hintKey: 'common.dashboard.newAssetHint',
    icon: 'i-ep-plus',
    iconClass: 'tk-dash-action__icon--primary',
    route: '/asset/create',
  },
  {
    labelKey: 'common.dashboard.newProber',
    hintKey: 'common.dashboard.newProberHint',
    icon: 'i-ep-position',
    iconClass: 'tk-dash-action__icon--success',
    route: '/telemetry/monitor/create',
  },
  {
    labelKey: 'common.dashboard.newTask',
    hintKey: 'common.dashboard.newTaskHint',
    icon: 'i-ep-timer',
    iconClass: 'tk-dash-action__icon--warning',
    route: '/task/create',
  },
  {
    labelKey: 'common.dashboard.viewAlerts',
    hintKey: 'common.dashboard.viewAlertsHint',
    icon: 'i-ep-bell',
    iconClass: 'tk-dash-action__icon--danger',
    route: '/prism/record/list',
  },
]

/** Dashboard alerts table columns */
const alertColumns = computed(() => [
  { prop: 'severity', label: t('common.dashboard.alertSeverity'), width: 110, slot: 'severity' },
  { prop: 'ruleName', label: t('common.dashboard.alertAsset'), minWidth: 160, slot: 'ruleName' },
  { prop: 'message', label: t('common.dashboard.alertMessage'), minWidth: 200, slot: 'message' },
  { prop: 'status', label: t('common.dashboard.alertStatus'), width: 110, slot: 'status' },
  { prop: 'firedAt', label: t('common.dashboard.alertFiredAt'), width: 170, slot: 'firedAt' },
])

/** Severity CSS class mapping */
function severityClass(severity?: string): string {
  const valid: AlertSeverity[] = ['critical', 'warning', 'info']
  const s = valid.includes(severity as AlertSeverity) ? severity : 'info'
  return `tk-dash-severity--${s}`
}

/** Severity i18n key mapping */
function severityLabelKey(severity?: string): string {
  const map: Record<string, string> = {
    critical: 'common.dashboard.severityCritical',
    warning: 'common.dashboard.severityWarning',
    info: 'common.dashboard.severityInfo',
  }
  return map[severity ?? 'info'] ?? map.info
}

/** Status tag type for el-tag */
function statusTagType(status: 'firing' | 'resolved' | 'acknowledged' | string): 'danger' | 'success' | 'warning' {
  if (status === 'firing') return 'danger'
  if (status === 'resolved') return 'success'
  return 'warning'
}

/** System health items derived from real runtime info and global stats */
const systemHealthItems = computed(() => {
  const info = runtimeInfo.value
  const s = globalStats.value
  const successRate = s?.todaySuccessRate ?? 0
  return [
    { labelKey: 'common.dashboard.health.version', value: info?.version ?? '—', state: '' as const },
    { labelKey: 'common.dashboard.health.uptime', value: info?.uptime ?? '—', state: '' as const },
    { labelKey: 'common.dashboard.health.activeTasks', value: String(s?.totalTasks ?? 0), state: 'ok' as const },
    {
      labelKey: 'common.dashboard.health.successRate',
      value: `${successRate.toFixed(1)}%`,
      state: successRate >= 95 ? 'ok' : ('warn' as const),
    },
  ]
})

/** Health item value state class */
function healthStateClass(state: string): string {
  return state ? `tk-dash-health__value--${state}` : ''
}

/** Navigate to a route */
function navigateTo(route: string): void {
  router.push(route)
}

/** Navigate to alert detail */
function navigateToAlert(id: number): void {
  router.push(`/prism/record/detail/${id}`)
}

/** Handle alert table row click */
function handleAlertRowClick(row: { id: number }): void {
  navigateToAlert(row.id)
}

/** Select a time range */
function selectRange(key: 'today' | '7d' | '30d'): void {
  activeRange.value = key
}
</script>

<template>
  <div class="tk-dash tk-page-container">
    <!-- Page header -->
    <header class="tk-dash__header">
      <div class="tk-dash__heading">
        <span class="tk-dash__eyebrow">
          {{ t('common.dashboard.eyebrow') }} · {{ todayDate }}
        </span>
        <h1 class="tk-dash__title">{{ t('common.dashboard.overview') }}</h1>
        <p class="tk-dash__subtitle">{{ t('common.dashboard.subtitle') }}</p>
      </div>
      <div class="tk-dash__toolbar">
        <div
          class="tk-dash__range"
          role="tablist"
          :aria-label="t('common.dashboard.rangeToday')"
        >
          <button
            v-for="opt in rangeOptions"
            :key="opt.key"
            class="tk-dash__range-btn"
            :class="{ 'is-active': activeRange === opt.key }"
            type="button"
            role="tab"
            :aria-selected="activeRange === opt.key"
            @click="selectRange(opt.key)"
          >
            {{ t(opt.labelKey) }}
          </button>
        </div>
        <el-button
          type="default"
          size="default"
          :loading="loading"
          @click="fetchDashboardData"
        >
          <i class="i-ep-refresh" />
          <span>{{ t('common.app.refresh') }}</span>
        </el-button>
      </div>
    </header>

    <!-- Upgrade banner: promotes the professional edition (replaces scattered locked placeholders) -->
    <UpgradeBanner />

    <!-- Row 1 · Stat cards -->
    <section
      class="tk-dash__bento"
      :aria-label="t('common.dashboard.totalAssets')"
    >
      <article
        v-for="card in statCards"
        :key="card.index"
        class="tk-dash-card tk-dash-card--stat tk-dash-card--hover"
        :class="card.accentClass"
      >
        <div class="tk-dash-stat">
          <div class="tk-dash-stat__top">
            <span class="tk-dash-stat__label">{{ t(card.labelKey) }}</span>
            <span
              class="tk-dash-stat__icon"
              :class="card.iconClass"
            >
              <i :class="card.icon" />
            </span>
          </div>
          <div class="tk-dash-stat__value">
            {{ card.value }}
            <span
              v-if="card.unit"
              class="tk-dash-stat__unit"
            >{{ card.unit }}</span>
          </div>
          <div
            class="tk-dash-stat__delta"
            :class="`tk-dash-stat__delta--${card.deltaDir}`"
          >
            <span>{{ card.deltaDir === 'up' ? '↑' : card.deltaDir === 'down' ? '↓' : '→' }} {{ card.delta }}</span>
            <span class="tk-dash-stat__delta-label">{{ t(card.deltaLabelKey) }}</span>
          </div>
        </div>
      </article>
    </section>

    <!-- Row 2 · Trend + Donut -->
    <section
      class="tk-dash__bento"
      :aria-label="t('common.dashboard.alertTrend')"
    >
      <article class="tk-dash-card tk-dash-card--trend">
        <div class="tk-dash-card__head">
          <div class="tk-dash-card__label">
            <span class="tk-dash-card__index">05</span>
            <h2 class="tk-dash-card__title">{{ t('common.dashboard.alertTrendTitle') }}</h2>
          </div>
          <div class="tk-dash-card__tools">
            <a
              class="tk-dash-card__link"
              href="javascript:void(0)"
              @click="navigateTo('/prism/record/list')"
            >{{ t('common.dashboard.viewAll') }}</a>
          </div>
        </div>
        <div class="tk-dash-card__body">
          <div
            ref="trendChartRef"
            class="tk-dash-chart"
          />
          <div class="tk-dash-chart__legend">
            <span
              v-for="item in trendLegend"
              :key="item.name"
              class="tk-dash-chart__legend-item"
            >
              <span
                class="tk-dash-chart__legend-dot"
                :style="{ backgroundColor: item.color }"
              />
              <span>{{ item.name }} · {{ item.total }} {{ t('common.dashboard.times') }}</span>
            </span>
          </div>
        </div>
      </article>

      <article class="tk-dash-card tk-dash-card--donut">
        <div class="tk-dash-card__head">
          <div class="tk-dash-card__label">
            <span class="tk-dash-card__index">06</span>
            <h2 class="tk-dash-card__title">{{ t('common.dashboard.assetDistribution') }}</h2>
          </div>
          <div class="tk-dash-card__tools">
            <a
              class="tk-dash-card__link"
              href="javascript:void(0)"
              @click="navigateTo('/asset/list')"
            >{{ t('common.dashboard.viewDetail') }}</a>
          </div>
        </div>
        <div class="tk-dash-card__body">
          <div
            ref="donutChartRef"
            class="tk-dash-chart"
          />
        </div>
      </article>
    </section>

    <!-- Row 3 · Bar + Health -->
    <section
      class="tk-dash__bento"
      :aria-label="t('common.dashboard.taskExecTrend')"
    >
      <article class="tk-dash-card tk-dash-card--bar">
        <div class="tk-dash-card__head">
          <div class="tk-dash-card__label">
            <span class="tk-dash-card__index">07</span>
            <h2 class="tk-dash-card__title">{{ t('common.dashboard.taskExecTrend') }}</h2>
          </div>
          <div class="tk-dash-card__tools">
            <a
              class="tk-dash-card__link"
              href="javascript:void(0)"
              @click="navigateTo('/task/log/list')"
            >{{ t('common.dashboard.viewLogs') }}</a>
          </div>
        </div>
        <div class="tk-dash-card__body">
          <div
            ref="barChartRef"
            class="tk-dash-chart"
          />
        </div>
      </article>

      <article class="tk-dash-card tk-dash-card--health">
        <div class="tk-dash-card__head">
          <div class="tk-dash-card__label">
            <span class="tk-dash-card__index">08</span>
            <h2 class="tk-dash-card__title">{{ t('common.dashboard.systemHealth') }}</h2>
          </div>
          <div class="tk-dash-card__tools">
            <a
              class="tk-dash-card__link"
              href="javascript:void(0)"
              @click="navigateTo('/system/settings/general')"
            >{{ t('common.dashboard.viewConfig') }}</a>
          </div>
        </div>
        <div class="tk-dash-card__body">
          <div class="tk-dash-health">
            <div
              v-for="item in systemHealthItems"
              :key="item.labelKey"
              class="tk-dash-health__item"
            >
              <span class="tk-dash-health__icon">
                <i class="i-ep-odometer" />
              </span>
              <div class="tk-dash-health__meta">
                <div class="tk-dash-health__label">{{ t(item.labelKey) }}</div>
                <div
                  class="tk-dash-health__value"
                  :class="healthStateClass(item.state)"
                >{{ item.value }}</div>
              </div>
            </div>
          </div>
        </div>
      </article>
    </section>

    <!-- Row 4 · Recent alerts table -->
    <section
      class="tk-dash__bento"
      :aria-label="t('common.dashboard.recentAlerts')"
    >
      <article class="tk-dash-card tk-dash-card--alerts">
        <div class="tk-dash-card__head">
          <div class="tk-dash-card__label">
            <span class="tk-dash-card__index">09</span>
            <h2 class="tk-dash-card__title">{{ t('common.dashboard.recentAlerts') }}</h2>
          </div>
          <div class="tk-dash-card__tools">
            <a
              class="tk-dash-card__link"
              href="javascript:void(0)"
              @click="navigateTo('/prism/record/list')"
            >{{ t('common.dashboard.viewAll') }}</a>
          </div>
        </div>
        <div class="tk-dash-card__body">
          <DataTable
            :data="recentAlerts"
            :columns="alertColumns"
            :pagination="false"
            :resizable="true"
            density="compact"
            @row-click="handleAlertRowClick"
          >
            <template #severity="{ row }">
              <span
                class="tk-dash-severity"
                :class="severityClass(row.severity)"
              >
                <span class="tk-dash-severity__dot" />
                {{ t(severityLabelKey(row.severity)) }}
              </span>
            </template>
            <template #ruleName="{ row }">
              <span class="tk-dash-asset">
                <span class="tk-dash-asset__icon"><i class="i-ep-monitor" /></span>
                <span class="tk-dash-asset__name">{{ row.ruleName }}</span>
              </span>
            </template>
            <template #message="{ row }">
              <span class="tk-dash-message">{{ row.message }}</span>
            </template>
            <template #status="{ row }">
              <el-tag
                :type="statusTagType(row.status)"
                size="small"
                effect="light"
              >
                {{ t(`prism.record.list.${row.status}`) }}
              </el-tag>
            </template>
            <template #firedAt="{ row }">
              <span class="tk-dash-time">{{ formatDate(row.firedAt) }}</span>
            </template>
          </DataTable>
        </div>
      </article>
    </section>

    <!-- Quick actions -->
    <section :aria-label="t('common.dashboard.quickActions')">
      <div class="tk-dash-actions">
        <a
          v-for="action in quickActions"
          :key="action.labelKey"
          class="tk-dash-action"
          href="javascript:void(0)"
          @click="navigateTo(action.route)"
        >
          <span
            class="tk-dash-action__icon"
            :class="action.iconClass"
          >
            <i :class="action.icon" />
          </span>
          <span class="tk-dash-action__body">
            <span class="tk-dash-action__label">{{ t(action.labelKey) }}</span>
            <span class="tk-dash-action__hint">{{ t(action.hintKey) }}</span>
          </span>
        </a>
      </div>
    </section>
  </div>
</template>

<style scoped lang="scss">
.tk-dash {
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-lg);

  &__header {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-lg);
    align-items: flex-end;
    justify-content: space-between;
  }

  &__heading {
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-xs);
    min-width: 0;
  }

  &__eyebrow {
    display: inline-flex;
    gap: var(--tk-spacing-xs);
    align-items: center;
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;

    &::before {
      width: 6px;
      height: 6px;
      content: '';
      background-color: var(--tk-success-color);
      border-radius: var(--tk-border-radius-circle);
      box-shadow: 0 0 0 3px var(--tk-success-color-bg);
      animation: tk-dash-pulse 2s var(--tk-ease-in-out) infinite;
    }
  }

  &__title {
    margin: 0;
    font-family: var(--tk-font-family);
    font-size: var(--tk-font-size-2xl);
    font-weight: var(--tk-font-weight-bold);
    line-height: 1.1;
    color: var(--tk-text-primary);
    letter-spacing: -0.02em;
  }

  &__subtitle {
    margin: 0;
    font-size: var(--tk-font-size-sm);
    line-height: var(--tk-line-height-normal);
    color: var(--tk-text-secondary);
  }

  &__toolbar {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
  }

  &__range {
    display: inline-flex;
    padding: 3px;
    background-color: var(--tk-fill-color-light);
    border: 1px solid var(--tk-border-color);
    border-radius: var(--tk-radius-md);
  }

  &__range-btn {
    padding: 6px var(--tk-spacing-md);
    font-family: var(--tk-font-family);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-medium);
    color: var(--tk-text-secondary);
    cursor: pointer;
    background: transparent;
    border: none;
    border-radius: var(--tk-radius-sm);
    transition: background-color var(--tk-transition-fast), color var(--tk-transition-fast);

    &:hover { color: var(--tk-text-primary); }

    &.is-active {
      color: var(--tk-primary-color);
      background-color: var(--tk-bg-surface);
    }

    &:focus-visible {
      outline: 2px solid var(--tk-primary-color);
      outline-offset: 2px;
    }
  }
}

@keyframes tk-dash-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.55; }
}

// Bento grid
.tk-dash__bento {
  display: grid;
  grid-template-columns: repeat(12, 1fr);
  gap: var(--tk-spacing-md);
}

// Card primitive
.tk-dash-card {
  position: relative;
  display: flex;
  flex-direction: column;
  padding: var(--tk-spacing-lg);
  overflow: hidden;
  background-color: var(--tk-bg-surface);
  border: 1px solid var(--tk-border-color);
  border-radius: var(--tk-border-radius-lg);
  transition: border-color var(--tk-transition-fast), transform var(--tk-transition-fast);

  &--hover:hover {
    border-color: var(--tk-border-strong);
    transform: translateY(-1px);
  }

  &__head {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--tk-spacing-md);
  }

  &__label {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: baseline;
    min-width: 0;
  }

  &__index {
    flex-shrink: 0;
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-placeholder);
    letter-spacing: 0.04em;
  }

  &__title {
    margin: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    font-family: var(--tk-font-family);
    font-size: var(--tk-font-size-base);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
    letter-spacing: -0.02em;
    white-space: nowrap;
  }

  &__link {
    display: inline-flex;
    gap: var(--tk-spacing-xs);
    align-items: center;
    font-family: var(--tk-font-family);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-medium);
    color: var(--tk-primary-color);
    text-decoration: none;

    &:hover { text-decoration: underline; text-underline-offset: 3px; }
    &:focus-visible { outline: 2px solid var(--tk-primary-color); outline-offset: 2px; }
  }

  &__body { flex: 1; min-height: 0; }

  &--stat { grid-column: span 3; }
  &--trend { grid-column: span 8; }
  &--donut { grid-column: span 4; }
  &--bar { grid-column: span 6; }
  &--health { grid-column: span 6; }
  &--alerts { grid-column: span 12; }
}

// Left accent strips
.tk-dash-card--accent-primary { box-shadow: inset 3px 0 0 0 var(--tk-primary-color); }
.tk-dash-card--accent-success { box-shadow: inset 3px 0 0 0 var(--tk-success-color); }
.tk-dash-card--accent-warning { box-shadow: inset 3px 0 0 0 var(--tk-warning-color); }
.tk-dash-card--accent-danger { box-shadow: inset 3px 0 0 0 var(--tk-danger-color); }

// Stat card
.tk-dash-stat {
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-sm);
  height: 100%;

  &__top {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
    justify-content: space-between;
  }

  &__icon {
    display: inline-flex;
    flex-shrink: 0;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    border-radius: var(--tk-radius-md);

    i { font-size: 18px; }

    &--primary { color: var(--tk-primary-color); background-color: var(--tk-primary-color-bg); }
    &--success { color: var(--tk-success-color); background-color: var(--tk-success-color-bg); }
    &--warning { color: var(--tk-warning-color); background-color: var(--tk-warning-color-bg); }
    &--danger { color: var(--tk-danger-color); background-color: var(--tk-danger-color-bg); }
  }

  &__label {
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  &__value {
    display: flex;
    gap: var(--tk-spacing-xs);
    align-items: baseline;
    font-family: var(--tk-font-family);
    font-size: var(--tk-font-size-3xl);
    font-weight: var(--tk-font-weight-bold);
    line-height: 1;
    color: var(--tk-text-primary);
    letter-spacing: -0.02em;
  }

  &__unit {
    font-size: var(--tk-font-size-base);
    font-weight: var(--tk-font-weight-medium);
    color: var(--tk-text-secondary);
  }

  &__delta {
    display: inline-flex;
    gap: var(--tk-spacing-xs);
    align-items: center;
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);

    &--up { color: var(--tk-success-color); }
    &--down { color: var(--tk-danger-color); }
    &--flat { color: var(--tk-text-secondary); }
  }

  &__delta-label {
    font-weight: var(--tk-font-weight-medium);
    color: var(--tk-text-placeholder);
  }
}

// Chart
.tk-dash-chart {
  width: 100%;
  height: 320px;

  &__legend {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-md);
    padding-top: var(--tk-spacing-sm);
    margin-top: var(--tk-spacing-sm);
    border-top: 1px solid var(--tk-border-color-lighter);
  }

  &__legend-item {
    display: inline-flex;
    gap: var(--tk-spacing-xs);
    align-items: center;
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
  }

  &__legend-dot {
    flex-shrink: 0;
    width: 8px;
    height: 8px;
    border-radius: 2px;
  }
}

// System health
.tk-dash-health {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: var(--tk-spacing-sm);

  &__item {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
    padding: var(--tk-spacing-md);
    background-color: var(--tk-neutral-50);
    border: 1px solid var(--tk-border-color-lighter);
    border-radius: var(--tk-radius-md);
    transition: border-color var(--tk-transition-fast);

    &:hover { border-color: var(--tk-border-color); }
  }

  &__icon {
    display: inline-flex;
    flex-shrink: 0;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    color: var(--tk-text-secondary);
    background-color: var(--tk-bg-surface);
    border: 1px solid var(--tk-border-color-lighter);
    border-radius: var(--tk-radius-sm);

    i { font-size: 16px; }
  }

  &__meta { flex: 1; min-width: 0; }

  &__label {
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
  }

  &__value {
    margin-top: 2px;
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-base);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);

    &--ok { color: var(--tk-success-color); }
    &--warn { color: var(--tk-warning-color); }
  }
}

// Alerts table
.tk-dash-alerts-table {
  width: 100%;

  :deep(.el-table) {
    --el-table-border-color: var(--tk-border-color-lighter);
    --el-table-header-bg-color: transparent;
    --el-table-row-hover-bg-color: var(--tk-bg-hover);
  }

  :deep(.el-table th) {
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-secondary);
  }

  :deep(.el-table td) {
    color: var(--tk-text-regular);
  }

  :deep(.el-table__row) {
    cursor: pointer;
  }
}

.tk-dash-severity {
  display: inline-flex;
  gap: var(--tk-spacing-xs);
  align-items: center;
  font-family: var(--tk-font-family-mono);
  font-size: var(--tk-font-size-xs);
  font-weight: var(--tk-font-weight-semibold);
  text-transform: uppercase;
  letter-spacing: 0.04em;

  &__dot {
    flex-shrink: 0;
    width: 6px;
    height: 6px;
    border-radius: var(--tk-border-radius-circle);
  }

  &--critical { color: var(--tk-danger-color); }
  &--critical .tk-dash-severity__dot { background-color: var(--tk-danger-color); }
  &--warning { color: var(--tk-warning-color); }
  &--warning .tk-dash-severity__dot { background-color: var(--tk-warning-color); }
  &--info { color: var(--tk-info-color); }
  &--info .tk-dash-severity__dot { background-color: var(--tk-info-color); }
}

.tk-dash-asset {
  display: inline-flex;
  gap: var(--tk-spacing-sm);
  align-items: center;

  &__icon {
    display: inline-flex;
    flex-shrink: 0;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    color: var(--tk-text-secondary);
    background-color: var(--tk-fill-color-light);
    border-radius: var(--tk-radius-sm);

    i { font-size: 13px; }
  }

  &__name {
    font-weight: var(--tk-font-weight-medium);
    color: var(--tk-text-primary);
  }
}

.tk-dash-message {
  font-size: var(--tk-font-size-sm);
  color: var(--tk-text-regular);
}

.tk-dash-time {
  font-family: var(--tk-font-family-mono);
  font-size: var(--tk-font-size-xs);
  color: var(--tk-text-secondary);
}

// Quick actions
.tk-dash-actions {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--tk-spacing-md);
}

.tk-dash-action {
  position: relative;
  display: flex;
  gap: var(--tk-spacing-md);
  align-items: center;
  padding: var(--tk-spacing-lg);
  overflow: hidden;
  color: var(--tk-text-primary);
  text-decoration: none;
  background-color: var(--tk-bg-surface);
  border: 1px solid var(--tk-border-color);
  border-radius: var(--tk-border-radius-lg);
  transition: border-color var(--tk-transition-fast), background-color var(--tk-transition-fast), transform var(--tk-transition-fast);

  &:hover {
    background-color: var(--tk-bg-hover);
    border-color: var(--tk-border-strong);
    transform: translateY(-1px);
  }

  &:focus-visible {
    outline: 2px solid var(--tk-primary-color);
    outline-offset: 2px;
  }

  &__icon {
    display: inline-flex;
    flex-shrink: 0;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    border-radius: var(--tk-radius-md);

    i { font-size: 20px; }

    &--primary { color: var(--tk-primary-color); background-color: var(--tk-primary-color-bg); }
    &--success { color: var(--tk-success-color); background-color: var(--tk-success-color-bg); }
    &--warning { color: var(--tk-warning-color); background-color: var(--tk-warning-color-bg); }
    &--danger { color: var(--tk-danger-color); background-color: var(--tk-danger-color-bg); }
  }

  &__body {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  &__label {
    font-family: var(--tk-font-family);
    font-size: var(--tk-font-size-base);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
  }

  &__hint {
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
  }
}

// Responsive
@media (max-width: 1199px) {
  .tk-dash-card--stat { grid-column: span 6; }
  .tk-dash-card--trend { grid-column: span 12; }
  .tk-dash-card--donut { grid-column: span 12; }
  .tk-dash-card--bar { grid-column: span 12; }
  .tk-dash-card--health { grid-column: span 12; }
  .tk-dash-actions { grid-template-columns: repeat(2, 1fr); }
}

// Mid-range: let the toolbar wrap onto its own row below the heading.
@media (max-width: 879px) {
  .tk-dash__header {
    align-items: flex-start;
  }

  .tk-dash__toolbar {
    justify-content: flex-start;
    width: 100%;
  }
}

@media (max-width: 639px) {
  .tk-dash-card--stat { grid-column: span 12; }
  .tk-dash-actions { grid-template-columns: 1fr; }
  .tk-dash-health { grid-template-columns: 1fr; }
  .tk-dash-chart { height: 260px; }

  .tk-dash__header {
    flex-direction: column;
    align-items: stretch;
  }
  .tk-dash__toolbar { width: 100%; }
  .tk-dash__range { flex: 1; }
  .tk-dash__range-btn { flex: 1; }
}
</style>
