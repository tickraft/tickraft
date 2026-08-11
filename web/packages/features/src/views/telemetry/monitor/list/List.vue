// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Unified monitor point list page.
 *
 * Replaces the separate prober list and listener overview pages.
 * Displays all monitor points in a single table with Tab switching
 * (All / Active Probing / Passive Receiving) for mode filtering.
 *
 * Backend ListTelemetry only supports the `mode` query parameter for filtering;
 * keyword and enabled filtering are not supported and are handled client-side
 * via the summary bar and mode tabs.
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { DataTable, ConfirmDialog } from '@tickraft/core'
import type { MonitorMode, MonitorPoint } from '../../../../types/telemetry'
import { getMonitors, deleteMonitor, enableMonitor, disableMonitor } from '../../../../api/telemetry'

type ModeFilter = '' | MonitorMode

const router = useRouter()
const { t } = useI18n()

const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)
const tableData = ref<MonitorPoint[]>([])

const activeTab = ref<ModeFilter>('')

const deleteVisible = ref(false)
const deleteLoading = ref(false)
const deleteTarget = ref<MonitorPoint | null>(null)

/** Mode badge class mapping */
const MODE_BADGE_CLASS: Record<MonitorMode, string> = {
  active: 'tk-mode-badge--active',
  passive: 'tk-mode-badge--passive',
}

/** Type badge class mapping */
const TYPE_BADGE_CLASS: Record<string, string> = {
  icmp: 'tk-type-badge--icmp',
  tcp: 'tk-type-badge--tcp',
  http: 'tk-type-badge--http',
  webhook: 'tk-type-badge--webhook',
  dns: 'tk-type-badge--dns',
  udp: 'tk-type-badge--udp',
  ssl: 'tk-type-badge--ssl',
}

/** Tab items for mode filtering */
const tabItems = computed(() => [
  { value: '' as ModeFilter, label: t('telemetry.monitor.list.tabAll') },
  { value: 'active' as ModeFilter, label: t('telemetry.monitor.list.tabActive') },
  { value: 'passive' as ModeFilter, label: t('telemetry.monitor.list.tabPassive') },
])

/** Summary bar computed counts */
const summaryCounts = computed(() => {
  const data = tableData.value ?? []
  const enabled = data.filter((item) => item.enabled).length
  const active = data.filter((item) => item.mode === 'active').length
  const passive = data.filter((item) => item.mode === 'passive').length
  return { enabled, disabled: data.length - enabled, active, passive }
})

const summaryItems = computed(() => [
  {
    label: t('telemetry.monitor.list.summaryActive'),
    value: summaryCounts.value.active,
    dot: 'var(--tk-primary-color)',
  },
  {
    label: t('telemetry.monitor.list.summaryPassive'),
    value: summaryCounts.value.passive,
    dot: 'var(--tk-success-color)',
  },
  {
    label: t('telemetry.monitor.list.summaryEnabled'),
    value: summaryCounts.value.enabled,
    dot: 'var(--tk-success-color)',
  },
  {
    label: t('telemetry.monitor.list.summaryDisabled'),
    value: summaryCounts.value.disabled,
    dot: 'var(--tk-text-placeholder)',
  },
])

const columns = computed(() => [
  { prop: 'name', label: t('telemetry.monitor.list.name'), minWidth: 160 },
  { prop: 'mode', label: t('telemetry.monitor.list.mode'), minWidth: 120, slot: 'mode' },
  { prop: 'type', label: t('telemetry.monitor.list.type'), minWidth: 100, slot: 'type' },
  { prop: 'schedule', label: t('telemetry.monitor.list.interval'), minWidth: 100, slot: 'schedule' },
  { prop: 'enabled', label: t('telemetry.monitor.list.status'), minWidth: 100, slot: 'enabled' },
  { prop: 'createdAt', label: t('telemetry.monitor.list.createdAt'), minWidth: 180, align: 'center' as const },
])

async function fetchData(): Promise<void> {
  loading.value = true
  try {
    const res = await getMonitors({
      page: currentPage.value,
      pageSize: pageSize.value,
      mode: activeTab.value || undefined,
    })
    // Defensive: ensure items is always an array (guard against malformed responses)
    tableData.value = Array.isArray(res?.items) ? res.items : []
    total.value = res?.total ?? 0
  } catch {
    tableData.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

function handlePageChange({ current, pageSize: size }: { current: number; pageSize: number }): void {
  currentPage.value = current
  pageSize.value = size
  void fetchData()
}

function handleTabChange(tab: ModeFilter): void {
  activeTab.value = tab
  currentPage.value = 1
  void fetchData()
}

function handleCreate(): void {
  router.push('/telemetry/monitor/create')
}

function handleDetail(row: MonitorPoint): void {
  router.push(`/telemetry/monitor/detail/${row.id}`)
}

function handleEdit(row: MonitorPoint): void {
  router.push(`/telemetry/monitor/edit/${row.id}`)
}

function handleDelete(row: MonitorPoint): void {
  deleteTarget.value = row
  deleteVisible.value = true
}

async function handleToggleEnable(row: MonitorPoint, value: boolean): Promise<void> {
  try {
    // Use dedicated enable/disable endpoints — updateMonitor replaces all fields
    // and would zero out unsent data.
    if (value) {
      await enableMonitor(row.id)
    } else {
      await disableMonitor(row.id)
    }
    row.enabled = value
    ElMessage.success(value ? t('common.app.enabled') : t('common.app.disabled'))
  } catch {
    // Errors are handled centrally by the interceptor; revert UI state
    row.enabled = !value
  }
}

async function confirmDelete(): Promise<void> {
  if (!deleteTarget.value) return
  deleteLoading.value = true
  try {
    await deleteMonitor(deleteTarget.value.id)
    ElMessage.success(t('telemetry.monitor.list.deleteSuccess'))
    deleteVisible.value = false
    deleteTarget.value = null
    await fetchData()
  } catch {
    // Errors are handled centrally by the interceptor
  } finally {
    deleteLoading.value = false
  }
}

/** Format schedule for display (e.g. "60s" for a cron like star-slash-60 or interval) */
function formatSchedule(schedule: string): string {
  if (!schedule) return '-'
  // If it looks like a simple interval (e.g. "60s", "5m"), display as-is
  if (/^\d+[smh]$/.test(schedule)) return schedule
  // Otherwise display the raw schedule
  return schedule
}

onMounted(() => {
  void fetchData()
})
</script>

<template>
  <div class="tk-page-container">
    <!-- Page header -->
    <div class="tk-telemetry-header">
      <div class="tk-telemetry-header__left">
        <div class="tk-telemetry-header__title-row">
          <h1 class="tk-telemetry-header__title">
            {{ t('telemetry.monitor.list.title') }}
          </h1>
          <span class="tk-telemetry-header__count">
            {{ total }} {{ t('telemetry.monitor.list.countSuffix') }}
          </span>
        </div>
        <p class="tk-telemetry-header__subtitle">
          {{ t('telemetry.monitor.list.subtitle') }}
        </p>
      </div>
      <div class="tk-telemetry-header__actions">
        <el-button @click="fetchData">
          {{ t('telemetry.monitor.list.refresh') }}
        </el-button>
        <el-button
          type="primary"
          @click="handleCreate"
        >
          {{ t('telemetry.monitor.list.create') }}
        </el-button>
      </div>
    </div>

    <!-- Summary bar -->
    <div class="tk-summary-bar">
      <span
        v-for="(item, idx) in summaryItems"
        :key="idx"
        class="tk-summary-chip"
      >
        <span
          class="tk-summary-chip__dot"
          :style="{ backgroundColor: item.dot }"
        />
        <span class="tk-summary-chip__label">{{ item.label }}</span>
        <span class="tk-summary-chip__value">{{ item.value }}</span>
      </span>
    </div>

    <!-- Mode tabs -->
    <div class="tk-mode-tabs">
      <div
        v-for="tab in tabItems"
        :key="tab.value"
        class="tk-mode-tabs__item"
        :class="{ 'tk-mode-tabs__item--active': activeTab === tab.value }"
        @click="handleTabChange(tab.value)"
      >
        {{ tab.label }}
      </div>
    </div>

    <!-- Table area -->
    <div class="tk-table-area">
      <DataTable
        table-id="monitor-list"
        :data="tableData"
        :columns="columns"
        :loading="loading"
        :total="total"
        :current="currentPage"
        :page-size="pageSize"
        @page-change="handlePageChange"
      >
        <template #mode="{ row }">
          <span
            class="tk-mode-badge"
            :class="MODE_BADGE_CLASS[(row as MonitorPoint).mode] || 'tk-mode-badge--default'"
          >
            <span class="tk-mode-badge__dot" />
            {{ (row as MonitorPoint).mode ? t(`telemetry.monitor.mode.${(row as MonitorPoint).mode}`) : '-' }}
          </span>
        </template>

        <template #type="{ row }">
          <span
            class="tk-type-badge"
            :class="TYPE_BADGE_CLASS[(row as MonitorPoint).type] || 'tk-type-badge--default'"
          >
            <span class="tk-type-badge__dot" />
            {{ (row as MonitorPoint).type ? t(`telemetry.monitor.type.${(row as MonitorPoint).type}`, (row as MonitorPoint).type) : '-' }}
          </span>
        </template>

        <template #schedule="{ row }">
          <span class="tk-mono">{{ formatSchedule((row as MonitorPoint).schedule) }}</span>
        </template>

        <template #enabled="{ row }">
          <el-switch
            :model-value="(row as MonitorPoint).enabled"
            size="small"
            @change="(val: boolean) => handleToggleEnable(row as MonitorPoint, val)"
          />
        </template>

        <template #action-column>
          <el-table-column
            :label="t('common.app.action')"
            width="200"
            fixed="right"
            align="center"
            :resizable="false"
          >
            <template #default="{ row }">
              <el-button
                link
                type="primary"
                @click="handleDetail(row as MonitorPoint)"
              >
                {{ t('common.app.detail') }}
              </el-button>
              <el-button
                link
                type="primary"
                @click="handleEdit(row as MonitorPoint)"
              >
                {{ t('common.app.edit') }}
              </el-button>
              <el-button
                link
                type="danger"
                @click="handleDelete(row as MonitorPoint)"
              >
                {{ t('common.app.delete') }}
              </el-button>
            </template>
          </el-table-column>
        </template>
      </DataTable>
    </div>

    <!-- Delete confirmation dialog -->
    <ConfirmDialog
      v-model="deleteVisible"
      :title="t('telemetry.monitor.list.deleteTitle')"
      :content="t('telemetry.monitor.list.deleteContent', { name: deleteTarget?.name ?? '', type: deleteTarget?.type ? t(`telemetry.monitor.type.${deleteTarget.type}`, deleteTarget.type) : '' })"
      :loading="deleteLoading"
      type="danger"
      @confirm="confirmDelete"
    />
  </div>
</template>

<style scoped lang="scss">
.tk-telemetry-header {
  display: flex;
  gap: var(--tk-spacing-md);
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: var(--tk-spacing-md);

  &__left {
    min-width: 0;
  }

  &__title-row {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: baseline;
  }

  &__title {
    margin: 0;
    font-size: var(--tk-font-size-xl);
    font-weight: var(--tk-font-weight-semibold);
    line-height: 1;
    color: var(--tk-text-primary);
  }

  &__count {
    font-family: var(--tk-font-mono, monospace);
    font-size: var(--tk-font-size-sm);
    color: var(--tk-text-secondary);
  }

  &__subtitle {
    max-width: 640px;
    margin-top: 6px;
    font-size: var(--tk-font-size-sm);
    line-height: 1.5;
    color: var(--tk-text-secondary);
  }

  &__actions {
    display: flex;
    flex-shrink: 0;
    gap: var(--tk-spacing-sm);
    align-items: center;
  }
}

.tk-summary-bar {
  display: flex;
  flex-wrap: wrap;
  gap: var(--tk-spacing-sm);
  margin-bottom: var(--tk-spacing-md);
}

.tk-summary-chip {
  display: inline-flex;
  gap: var(--tk-spacing-xs);
  align-items: center;
  padding: var(--tk-spacing-xs) var(--tk-spacing-sm);
  font-size: var(--tk-font-size-xs);
  background: var(--tk-bg-color);
  border: 1px solid var(--tk-border-color-light);
  border-radius: var(--tk-border-radius-base);

  &__dot {
    flex-shrink: 0;
    width: 8px;
    height: 8px;
    border-radius: 50%;
  }

  &__label {
    font-weight: var(--tk-font-weight-medium);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  &__value {
    font-family: var(--tk-font-mono, monospace);
    font-weight: var(--tk-font-weight-semibold);
    font-variant-numeric: tabular-nums;
    color: var(--tk-text-primary);
  }
}

.tk-mode-tabs {
  display: flex;
  gap: var(--tk-spacing-xs);
  margin-bottom: var(--tk-spacing-md);
  border-bottom: 1px solid var(--tk-border-color-light);

  &__item {
    padding: var(--tk-spacing-sm) var(--tk-spacing-md);
    font-size: var(--tk-font-size-sm);
    font-weight: var(--tk-font-weight-medium);
    color: var(--tk-text-secondary);
    cursor: pointer;
    border-bottom: 2px solid transparent;
    transition: color var(--tk-transition-fast), border-color var(--tk-transition-fast);

    &:hover {
      color: var(--tk-text-primary);
    }

    &--active {
      color: var(--tk-primary-color);
      border-bottom-color: var(--tk-primary-color);
    }
  }
}

.tk-mode-badge {
  display: inline-flex;
  gap: var(--tk-spacing-xs);
  align-items: center;
  padding: 1px var(--tk-spacing-sm);
  font-family: var(--tk-font-mono, monospace);
  font-size: var(--tk-font-size-xs);
  font-weight: var(--tk-font-weight-medium);
  border: 1px solid transparent;
  border-radius: var(--tk-border-radius-sm);

  &__dot {
    flex-shrink: 0;
    width: 6px;
    height: 6px;
    background: currentcolor;
    border-radius: 50%;
  }

  &--active {
    color: var(--tk-primary-color);
    background: var(--tk-primary-color-light-9);
    border-color: var(--tk-primary-color-light-7);
  }

  &--passive {
    color: var(--tk-success-color);
    background: var(--tk-success-color-light-9);
    border-color: var(--tk-success-color-light-7);
  }

  &--default {
    color: var(--tk-text-secondary);
    background: var(--tk-bg-color-page);
    border-color: var(--tk-border-color);
  }
}

.tk-type-badge {
  display: inline-flex;
  gap: var(--tk-spacing-xs);
  align-items: center;
  padding: 1px var(--tk-spacing-sm);
  font-family: var(--tk-font-mono, monospace);
  font-size: var(--tk-font-size-xs);
  font-weight: var(--tk-font-weight-medium);
  border: 1px solid transparent;
  border-radius: var(--tk-border-radius-sm);

  &__dot {
    flex-shrink: 0;
    width: 6px;
    height: 6px;
    background: currentcolor;
    border-radius: 50%;
  }

  &--icmp {
    color: var(--tk-info-color);
    background: var(--tk-info-color-light-9);
    border-color: var(--tk-info-color-light-7);
  }

  &--tcp {
    color: var(--tk-success-color);
    background: var(--tk-success-color-light-9);
    border-color: var(--tk-success-color-light-7);
  }

  &--http {
    color: var(--tk-primary-color);
    background: var(--tk-primary-color-light-9);
    border-color: var(--tk-primary-color-light-7);
  }

  &--webhook {
    color: var(--tk-warning-color);
    background: var(--tk-warning-color-light-9);
    border-color: var(--tk-warning-color-light-7);
  }

  &--dns {
    color: var(--tk-warning-color);
    background: var(--tk-warning-color-light-9);
    border-color: var(--tk-warning-color-light-7);
  }

  &--udp {
    color: var(--tk-warning-color);
    background: var(--tk-warning-color-light-9);
    border-color: var(--tk-warning-color-light-7);
  }

  &--ssl {
    color: var(--tk-danger-color);
    background: var(--tk-danger-color-light-9);
    border-color: var(--tk-danger-color-light-7);
  }

  &--default {
    color: var(--tk-text-secondary);
    background: var(--tk-bg-color-page);
    border-color: var(--tk-border-color);
  }
}

.tk-mono {
  font-family: var(--tk-font-mono, monospace);
  font-size: var(--tk-font-size-xs);
  font-variant-numeric: tabular-nums;
  color: var(--tk-text-regular);
}
</style>
