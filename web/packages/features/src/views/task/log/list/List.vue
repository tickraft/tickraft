// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Refresh, Download, Search } from '@element-plus/icons-vue'
import { FeatureGuard, DataTable } from '@tickraft/core'
import { formatDuration, formatDate } from '@tickraft/core'
import type { LogModel, ExecutorType } from '../../../../types/task'
import { getLogs } from '../../../../api/task'

const router = useRouter()
const { t } = useI18n()

const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(15)
const total = ref(0)
const tableData = ref<LogModel[]>([])

/**
 * Full execution log dataset fetched from the backend. The backend
 * ListExecutions endpoint only supports page/size — no task name, executor
 * type, or status filtering — so we fetch a large page and apply filters
 * client-side before rendering the current page slice.
 */
const allLogs = ref<LogModel[]>([])

const filterTaskName = ref('')
const filterExecutor = ref<ExecutorType | ''>('')
const filterStatus = ref('')

/** Client-side filtered view of allLogs. */
const filteredLogs = computed(() => {
  let items = allLogs.value
  const taskName = filterTaskName.value.trim().toLowerCase()
  if (taskName) {
    items = items.filter((l) => (l.taskName ?? '').toLowerCase().includes(taskName))
  }
  if (filterExecutor.value) {
    items = items.filter((l) => l.executorType === filterExecutor.value)
  }
  if (filterStatus.value) {
    items = items.filter((l) => l.status === filterStatus.value)
  }
  return items
})

/** Recompute the paginated table slice whenever filters or page change. */
watch(
  [filteredLogs, currentPage, pageSize],
  () => {
    const items = filteredLogs.value
    total.value = items.length
    const start = (currentPage.value - 1) * pageSize.value
    tableData.value = items.slice(start, start + pageSize.value)
  },
  { immediate: true },
)

/** Log table columns */
const logColumns = computed(() => [
  { prop: 'id', label: t('task.log.list.logId'), width: 80, slot: 'id', align: 'center' as const },
  { prop: 'taskName', label: t('task.log.list.taskName'), minWidth: 180, slot: 'taskName', align: 'left' as const, showOverflowTooltip: true },
  { prop: 'executorType', label: t('task.log.list.executorType'), width: 110, slot: 'executorType', align: 'center' as const },
  { prop: 'status', label: t('task.log.list.status'), width: 100, slot: 'status', align: 'center' as const },
  { prop: 'startedAt', label: t('task.log.list.startedAt'), width: 160, slot: 'startedAt', align: 'center' as const },
  { prop: 'duration', label: t('task.log.list.duration'), width: 120, slot: 'duration', align: 'center' as const },
])

const EXECUTOR_LABELS = computed<Record<string, string>>(() => ({
  http: 'HTTP', tcp: 'TCP', icmp: 'ICMP', local: t('task.task.create.executorLocal'),
  webhook: 'Webhook', ssh: 'SSH', mysql: 'MYSQL', redis: 'REDIS',
}))

const summary = computed(() => {
  const items = tableData.value
  return {
    total: total.value,
    success: items.filter((l) => l.status === 'success').length,
    failed: items.filter((l) => l.status === 'failed').length,
    running: items.filter((l) => l.status === 'running').length,
    timeout: items.filter((l) => l.status === 'timeout').length,
  }
})

const maxDuration = computed(() => {
  if (tableData.value.length === 0) return 1
  return Math.max(...tableData.value.map((l) => l.duration || 0), 1)
})

function durationPercent(duration: number): number {
  return Math.round(((duration || 0) / maxDuration.value) * 100)
}

const countText = computed(() => `${total.value} ${t('task.log.list.title').toUpperCase().includes('LOG') ? 'LOGS' : ''}`)

const executorOptions = computed(() => [
  { label: 'HTTP', value: 'http' },
  { label: 'TCP', value: 'tcp' },
  { label: 'ICMP', value: 'icmp' },
  { label: t('task.task.create.executorLocal'), value: 'local' },
  { label: 'Webhook', value: 'webhook' },
])

const statusOptions = computed(() => [
  { label: t('common.status.success'), value: 'success' },
  { label: t('common.status.failed'), value: 'failed' },
  { label: t('common.status.timeout'), value: 'timeout' },
  { label: t('common.status.running'), value: 'running' },
])

async function fetchData(): Promise<void> {
  loading.value = true
  try {
    // Fetch a large page — the backend ListExecutions endpoint only supports
    // page/size pagination, so task name / executor type / status filtering
    // is applied client-side via the filteredLogs computed above.
    const res = await getLogs(0, { page: 1, pageSize: 1000 })
    allLogs.value = res.items || []
  } catch {
    allLogs.value = []
  } finally {
    loading.value = false
  }
}

function handleSearch(): void {
  currentPage.value = 1
  void fetchData()
}

function handleReset(): void {
  filterTaskName.value = ''
  filterExecutor.value = ''
  filterStatus.value = ''
  currentPage.value = 1
  void fetchData()
}

function handlePageChange(payload: { current: number; pageSize: number }): void {
  currentPage.value = payload.current
  void fetchData()
}

function handleRefresh(): void {
  void fetchData()
  ElMessage.success(t('task.log.list.refreshSuccess'))
}

function handleDetail(row: LogModel): void {
  router.push(`/task/log/detail/${row.taskId}/${row.id}`)
}

function handleExport(): void {
  ElMessage.info(t('common.feature.locked'))
}

onMounted(() => { void fetchData() })
</script>

<template>
  <div class="tk-log-list tk-page-container">
    <!-- Header: title + count badge + subtitle + actions -->
    <div class="tk-log-list__header">
      <div>
        <div class="tk-log-list__title-row">
          <h1 class="tk-log-list__title">{{ t('task.log.list.title') }}</h1>
          <span class="tk-log-list__count">{{ countText }}</span>
        </div>
        <p class="tk-log-list__subtitle">{{ t('task.log.list.subtitle') }}</p>
      </div>
      <div class="tk-log-list__actions">
        <el-button @click="handleRefresh"><el-icon><Refresh /></el-icon>{{ t('common.app.refresh') }}</el-button>
        <FeatureGuard feature="log_export">
          <el-button @click="handleExport"><el-icon><Download /></el-icon>{{ t('task.log.list.export') }}</el-button>
        </FeatureGuard>
      </div>
    </div>

    <!-- Summary strip: 5 cards with accent borders -->
    <div class="tk-log-summary">
      <div class="tk-log-summary__card tk-log-summary__card--total">
        <div class="tk-log-summary__label"><span class="tk-log-summary__dot" />{{ t('task.log.list.summaryTotal') }}</div>
        <div class="tk-log-summary__value">{{ summary.total }}</div>
      </div>
      <div class="tk-log-summary__card tk-log-summary__card--success">
        <div class="tk-log-summary__label"><span class="tk-log-summary__dot" />{{ t('task.log.list.summarySuccess') }}</div>
        <div class="tk-log-summary__value">{{ summary.success }}</div>
      </div>
      <div class="tk-log-summary__card tk-log-summary__card--failed">
        <div class="tk-log-summary__label"><span class="tk-log-summary__dot" />{{ t('task.log.list.summaryFailed') }}</div>
        <div class="tk-log-summary__value">{{ summary.failed }}</div>
      </div>
      <div class="tk-log-summary__card tk-log-summary__card--running">
        <div class="tk-log-summary__label"><span class="tk-log-summary__dot" />{{ t('task.log.list.summaryRunning') }}</div>
        <div class="tk-log-summary__value">{{ summary.running }}</div>
      </div>
      <div class="tk-log-summary__card tk-log-summary__card--timeout">
        <div class="tk-log-summary__label"><span class="tk-log-summary__dot" />{{ t('task.log.list.summaryTimeout') }}</div>
        <div class="tk-log-summary__value">{{ summary.timeout }}</div>
      </div>
    </div>

    <!-- Inline filter bar -->
    <div class="tk-log-filter">
      <el-input
        v-model="filterTaskName"
        :placeholder="t('task.log.list.taskNamePlaceholder')"
        :prefix-icon="Search"
        clearable
        class="tk-log-filter__search"
        @keyup.enter="handleSearch"
      />
      <el-select v-model="filterExecutor" :placeholder="t('task.log.list.executorType')" clearable class="tk-log-filter__select">
        <el-option v-for="opt in executorOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
      </el-select>
      <el-select v-model="filterStatus" :placeholder="t('task.log.list.status')" clearable class="tk-log-filter__select">
        <el-option v-for="opt in statusOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
      </el-select>
      <div class="tk-log-filter__divider" />
      <el-button text @click="handleReset">{{ t('common.app.reset') }}</el-button>
      <el-button type="primary" @click="handleSearch">{{ t('common.app.search') }}</el-button>
    </div>

    <!-- Table -->
    <div class="tk-log-list__table">
      <DataTable
        :data="tableData"
        :columns="logColumns"
        :loading="loading"
        :total="total"
        :current="currentPage"
        :page-size="pageSize"
        density="compact"
        @row-click="handleDetail"
        @page-change="handlePageChange"
      >
        <template #id="{ row }"><span class="tk-mono-id">#{{ (row as LogModel).id }}</span></template>
        <template #taskName="{ row }">
          <span class="tk-log-task">{{ (row as LogModel).taskName || '—' }}</span>
        </template>
        <template #executorType="{ row }">
          <span class="tk-executor-badge" :class="`tk-executor-badge--${(row as LogModel).executorType ?? ''}`">
            <span class="tk-executor-badge__dot" />
            {{ EXECUTOR_LABELS[(row as LogModel).executorType ?? ''] || (row as LogModel).executorType }}
          </span>
        </template>
        <template #status="{ row }">
          <span class="tk-status-tag" :class="`tk-status-tag--${(row as LogModel).status === 'success' ? 'success' : (row as LogModel).status === 'failed' ? 'failed' : (row as LogModel).status === 'running' ? 'running' : (row as LogModel).status === 'timeout' ? 'warning' : 'unknown'}`">
            <span class="tk-status-tag__dot" />{{ (row as LogModel).status }}
          </span>
        </template>
        <template #startedAt="{ row }"><span class="tk-mono-text">{{ formatDate((row as LogModel).startedAt) }}</span></template>
        <template #duration="{ row }">
          <div class="tk-duration-cell">
            <span class="tk-duration-cell__value">{{ formatDuration((row as LogModel).duration ?? 0) }}</span>
            <div class="tk-duration-cell__bar">
              <div class="tk-duration-cell__fill" :class="{ 'tk-duration-cell__fill--fail': (row as LogModel).status === 'failed' || (row as LogModel).status === 'timeout' }" :style="{ width: durationPercent((row as LogModel).duration ?? 0) + '%' }" />
            </div>
          </div>
        </template>
        <template #action-column>
          <el-table-column
            :label="t('task.log.list.action')"
            width="140"
            fixed="right"
            align="center"
            :resizable="false"
          >
            <template #default="{ row }">
              <div class="tk-log-actions" @click.stop>
                <el-button link type="primary" @click="handleDetail(row as LogModel)">
                  {{ t('task.log.list.detail') }}
                </el-button>
              </div>
            </template>
          </el-table-column>
        </template>
      </DataTable>
    </div>
  </div>
</template>

<style scoped lang="scss">
.tk-log-list {
  &__header { display: flex; flex-wrap: wrap; gap: var(--tk-spacing-12); align-items: flex-start; justify-content: space-between; margin-bottom: var(--tk-spacing-8); }
  &__title-row { display: flex; gap: var(--tk-spacing-6); align-items: baseline; }
  &__title { margin: 0; font-family: var(--tk-font-display); font-size: var(--tk-font-size-2xl); font-weight: var(--tk-font-weight-bold); line-height: 1.1; color: var(--tk-text-primary); letter-spacing: var(--tk-letter-tight); }
  &__count { display: inline-flex; align-items: center; padding: 2px var(--tk-spacing-6); font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); font-weight: var(--tk-font-weight-semibold); color: var(--tk-primary-color); letter-spacing: var(--tk-letter-wider); background-color: var(--tk-primary-color-bg); border: 1px solid var(--tk-primary-color-border); border-radius: var(--tk-radius-round); }
  &__subtitle { max-width: 540px; margin-top: var(--tk-spacing-2); font-size: var(--tk-font-size-sm); line-height: var(--tk-line-height-normal); color: var(--tk-text-secondary); }
  &__actions { display: flex; gap: var(--tk-spacing-4); align-items: center; }
  &__table { overflow: hidden; background-color: var(--tk-bg-surface); border: 1px solid var(--tk-border-color-base); border-radius: var(--tk-radius-lg); }
  &__footer { display: flex; gap: var(--tk-spacing-8); align-items: center; justify-content: space-between; padding: var(--tk-spacing-4) var(--tk-spacing-6); }
  &__showing { font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); color: var(--tk-text-secondary); letter-spacing: var(--tk-letter-wide); }
}

.tk-log-summary {
  display: grid; grid-template-columns: repeat(5, 1fr); gap: var(--tk-spacing-4); margin-bottom: var(--tk-spacing-8);

  &__card { position: relative; display: flex; flex-direction: column; gap: var(--tk-spacing-3); padding: var(--tk-spacing-6) var(--tk-spacing-8); overflow: hidden; background-color: var(--tk-bg-surface); border: 1px solid var(--tk-border-color-base); border-radius: var(--tk-radius-lg);
    &::before { position: absolute; top: 0; left: 0; width: 3px; height: 100%; content: ""; background-color: var(--tk-text-secondary); }
    &--total::before { background-color: var(--tk-primary-color); }
    &--success::before { background-color: var(--tk-success-color); }
    &--failed::before { background-color: var(--tk-danger-color); }
    &--running::before { background-color: var(--tk-info-color); }
    &--timeout::before { background-color: var(--tk-warning-color); }
  }
  &__label { display: flex; gap: var(--tk-spacing-2); align-items: center; font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); font-weight: var(--tk-font-weight-semibold); color: var(--tk-text-secondary); text-transform: uppercase; letter-spacing: var(--tk-letter-wider); }
  &__dot { flex-shrink: 0; width: 6px; height: 6px; border-radius: var(--tk-radius-circle); }
  &__card--total .tk-log-summary__dot { background-color: var(--tk-primary-color); }
  &__card--success .tk-log-summary__dot { background-color: var(--tk-success-color); }
  &__card--failed .tk-log-summary__dot { background-color: var(--tk-danger-color); }
  &__card--running .tk-log-summary__dot { background-color: var(--tk-info-color); }
  &__card--timeout .tk-log-summary__dot { background-color: var(--tk-warning-color); }
  &__value { font-family: var(--tk-font-display); font-size: var(--tk-font-size-2xl); font-weight: var(--tk-font-weight-extrabold); font-variant-numeric: tabular-nums; line-height: 1; color: var(--tk-text-primary); letter-spacing: var(--tk-letter-tight); }
}

.tk-log-filter {
  display: flex; flex-wrap: wrap; gap: var(--tk-spacing-4); align-items: center; padding: var(--tk-spacing-6) var(--tk-spacing-8); margin-bottom: var(--tk-spacing-8);
  background-color: var(--tk-bg-surface); border: 1px solid var(--tk-border-color-base); border-radius: var(--tk-radius-lg);
  &__search { flex: 1 1 240px; max-width: 320px; }
  &__select { width: 140px; }
  &__divider { width: 1px; height: 24px; background-color: var(--tk-border-color-base); }
}

.tk-executor-badge {
  display: inline-flex; gap: var(--tk-spacing-2); align-items: center; padding: 2px var(--tk-spacing-6);
  font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); font-weight: var(--tk-font-weight-semibold); text-transform: uppercase;
  letter-spacing: var(--tk-letter-wide); white-space: nowrap; border: 1px solid transparent; border-radius: var(--tk-radius-sm);
  &__dot { flex-shrink: 0; width: 5px; height: 5px; background-color: currentcolor; border-radius: var(--tk-radius-circle); }
  &--http { color: #2563eb; background-color: rgb(37 99 235 / 10%); border-color: rgb(37 99 235 / 25%); }
  &--tcp { color: #0891b2; background-color: rgb(8 145 178 / 10%); border-color: rgb(8 145 178 / 25%); }
  &--icmp { color: #7c3aed; background-color: rgb(124 58 237 / 10%); border-color: rgb(124 58 237 / 25%); }
  &--local { color: #475569; background-color: rgb(71 85 105 / 10%); border-color: rgb(71 85 105 / 25%); }
  &--ssh { color: #b45309; background-color: rgb(180 83 9 / 10%); border-color: rgb(180 83 9 / 25%); }
  &--mysql { color: #15803d; background-color: rgb(21 128 61 / 10%); border-color: rgb(21 128 61 / 25%); }
  &--redis { color: #dc2626; background-color: rgb(220 38 38 / 10%); border-color: rgb(220 38 38 / 25%); }
  &--webhook { color: #be185d; background-color: rgb(190 24 93 / 10%); border-color: rgb(190 24 93 / 25%); }
}

.tk-status-tag {
  display: inline-flex; gap: 6px; align-items: center; padding: 2px var(--tk-spacing-4);
  font-size: var(--tk-font-size-xs); font-weight: var(--tk-font-weight-medium); text-transform: capitalize; border: 1px solid transparent; border-radius: var(--tk-radius-sm);
  &__dot { flex-shrink: 0; width: 7px; height: 7px; border-radius: var(--tk-radius-circle); }
  &--success { color: var(--tk-success-color-text); background: var(--tk-success-color-bg); border-color: var(--tk-success-color-border); .tk-status-tag__dot { background-color: var(--tk-success-color); } }
  &--failed { color: var(--tk-danger-color-text); background: var(--tk-danger-color-bg); border-color: var(--tk-danger-color-border); .tk-status-tag__dot { background-color: var(--tk-danger-color); } }
  &--running { color: var(--tk-info-color-text); background: var(--tk-info-color-bg); border-color: var(--tk-info-color-border); .tk-status-tag__dot { background-color: var(--tk-info-color); } }
  &--warning { color: var(--tk-warning-color-text); background: var(--tk-warning-color-bg); border-color: var(--tk-warning-color-border); .tk-status-tag__dot { background-color: var(--tk-warning-color); } }
  &--unknown { color: var(--tk-text-secondary); background: var(--tk-bg-fill); border-color: var(--tk-border-color-base); .tk-status-tag__dot { background-color: var(--tk-text-placeholder); } }
}

.tk-mono-id { font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); font-variant-numeric: tabular-nums; color: var(--tk-text-secondary); }
.tk-mono-text { font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); color: var(--tk-text-secondary); }
.tk-log-task { font-weight: var(--tk-font-weight-medium); color: var(--tk-text-primary); cursor: pointer; transition: color var(--tk-duration-fast) var(--tk-ease-out); &:hover { color: var(--tk-primary-color); } }

.tk-duration-cell { display: flex; flex-direction: column; gap: 2px;
  &__value { font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); font-variant-numeric: tabular-nums; color: var(--tk-text-primary); }
  &__bar { width: 60px; height: 3px; overflow: hidden; background-color: var(--tk-bg-fill); border-radius: var(--tk-radius-round); }
  &__fill { height: 100%; background-color: var(--tk-primary-color); border-radius: var(--tk-radius-round); &--fail { background-color: var(--tk-danger-color); } }
}
.tk-log-actions { display: flex; gap: var(--tk-spacing-2); align-items: center; white-space: nowrap; }

.tk-log-empty { display: flex; flex-direction: column; gap: var(--tk-spacing-4); align-items: center; justify-content: center; padding: var(--tk-spacing-16) var(--tk-spacing-12); text-align: center;
  &__icon { display: flex; align-items: center; justify-content: center; width: 56px; height: 56px; color: var(--tk-text-secondary); background: var(--tk-bg-fill); border-radius: var(--tk-radius-lg); }
  &__title { font-family: var(--tk-font-display); font-size: var(--tk-font-size-md); font-weight: var(--tk-font-weight-semibold); color: var(--tk-text-primary); }
  &__desc { max-width: 320px; font-size: var(--tk-font-size-sm); color: var(--tk-text-secondary); }
}

:deep(.el-table thead th) { font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); font-weight: var(--tk-font-weight-semibold); color: var(--tk-text-secondary); text-transform: uppercase; letter-spacing: var(--tk-letter-wider); }
:deep(.el-table tbody tr) { cursor: pointer; }
:deep(.el-table tbody tr:hover td) { background-color: var(--tk-primary-color-bg); }

// Fixed right column: keep opaque on hover to prevent see-through to rows
// beneath. The hover rule above applies a semi-transparent primary tint to ALL
// cells; sticky fixed cells need an opaque base with the tint composited as a
// background-image overlay (same approach as the DataTable component).
:deep(td.el-table__cell.el-table-fixed-column--right) {
  background-color: var(--tk-bg-surface) !important;
}

:deep(.el-table__body tr:hover > td.el-table__cell.el-table-fixed-column--right),
:deep(.el-table__body tr.hover-row > td.el-table__cell.el-table-fixed-column--right) {
  background-color: var(--tk-bg-surface) !important;
  background-image: linear-gradient(var(--tk-primary-color-bg), var(--tk-primary-color-bg)) !important;
}

@media (max-width: 960px) { .tk-log-summary { grid-template-columns: repeat(2, 1fr); } }
</style>
