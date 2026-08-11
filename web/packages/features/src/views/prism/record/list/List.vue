// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { DataTable, StatusTag, useTable, formatDate } from '@tickraft/core'
import type { AlertStatus } from '@tickraft/core'
import {
  getAlertRecords,
  acknowledgeAlertRecord,
  resolveAlertRecord,
} from '../../../../api/prism'
import type { AlertRecord, AlertSeverity } from '../../../../api/prism'
import { SEVERITY_TAG_TYPE, parseDate } from '../../constants'
import PrismPageHeader from '../../components/PrismPageHeader.vue'

const router = useRouter()
const { t } = useI18n()

/** Track which record is being acknowledged/resolved (for button loading state) */
const actionLoadingId = ref<number | null>(null)

/** Safely resolve the el-tag type for a severity string */
function severityTagType(severity: string | undefined): 'danger' | 'warning' | 'info' {
  if (severity === 'critical' || severity === 'warning' || severity === 'info') {
    return SEVERITY_TAG_TYPE[severity as AlertSeverity]
  }
  return 'info'
}

/** Safely resolve the i18n label for a severity string */
function severityLabel(severity: string | undefined): string {
  if (!severity) return '-'
  return t(`prism.severity.${severity}`)
}

/** Table column configuration */
const columns = computed(() => [
  { prop: 'ruleName', label: t('prism.record.list.ruleName'), minWidth: '160' },
  { prop: 'severity', label: t('prism.record.list.severity'), width: '100', slot: 'severity' },
  { prop: 'value', label: t('prism.record.list.value'), width: '100', slot: 'value' },
  { prop: 'status', label: t('prism.record.list.status'), width: '130', slot: 'status', showOverflowTooltip: false },
  { prop: 'message', label: t('prism.record.list.message'), minWidth: '200', showOverflowTooltip: true },
  { prop: 'firedAt', label: t('prism.record.list.firedAt'), width: '170', align: 'center' as const, formatter: formatTimeCell },
  { prop: 'acknowledgedAt', label: t('prism.record.list.acknowledgedAt'), width: '170', slot: 'acknowledgedAt' },
  { prop: 'resolvedAt', label: t('prism.record.list.resolvedAt'), width: '170', slot: 'resolvedAt' },
])

const {
  data,
  loading,
  total,
  page,
  pageSize,
  immediateSearch,
  changePage,
  changePageSize,
  refresh,
} = useTable<AlertRecord>({
  defaultPageSize: 15,
  fetchFn: async (params) => {
    const res = await getAlertRecords({
      page: params.page,
      pageSize: params.size as number,
    })
    return { items: res.items, total: res.total }
  },
})

/** Count of firing alerts in current page (for summary chip) */
const firingCount = computed(() =>
  data.value.filter((r) => r.status === 'firing').length,
)

/** Row class for firing state red highlight */
function rowClassName({ row }: { row: AlertRecord; rowIndex: number }): string {
  if (row.status !== 'firing') return ''
  return row.severity === 'critical'
    ? 'tk-prism-record-row tk-prism-record-row--firing tk-prism-record-row--firing-critical'
    : 'tk-prism-record-row tk-prism-record-row--firing'
}

/** Time cell formatter */
function formatTimeCell(_row: AlertRecord, _column: unknown, value: unknown): string {
  if (!value) return '-'
  return formatDate(parseDate(String(value)))
}

/** Format a nullable time string for slot rendering */
function formatNullableTime(value: string | null | undefined): string {
  if (!value) return '-'
  return formatDate(parseDate(value))
}

/** Pagination change handler */
function handlePageChange(payload: { current: number; pageSize: number }): void {
  if (payload.pageSize !== pageSize.value) {
    changePageSize(payload.pageSize)
  } else {
    changePage(payload.current)
  }
}

/** Navigate to detail */
function handleDetail(row: AlertRecord): void {
  router.push(`/prism/record/detail/${row.id}`)
}

/** Acknowledge a firing alert record */
async function handleAcknowledge(row: AlertRecord): Promise<void> {
  actionLoadingId.value = row.id
  try {
    const updated = await acknowledgeAlertRecord(row.id)
    const idx = data.value.findIndex((r) => r.id === row.id)
    if (idx !== -1) data.value[idx] = updated
    ElMessage.success(t('prism.record.list.acknowledgedToast'))
  } catch {
    ElMessage.error(t('prism.record.list.actionFailed'))
  } finally {
    actionLoadingId.value = null
  }
}

/** Resolve an alert record */
async function handleResolve(row: AlertRecord): Promise<void> {
  actionLoadingId.value = row.id
  try {
    const updated = await resolveAlertRecord(row.id)
    const idx = data.value.findIndex((r) => r.id === row.id)
    if (idx !== -1) data.value[idx] = updated
    ElMessage.success(t('prism.record.list.resolvedToast'))
  } catch {
    ElMessage.error(t('prism.record.list.actionFailed'))
  } finally {
    actionLoadingId.value = null
  }
}

/** Refresh */
function handleRefresh(): void {
  refresh()
  ElMessage.success(t('prism.record.list.refreshed'))
}

/** Export (placeholder) */
function handleExport(): void {
  ElMessage.info(t('prism.record.list.exportTip'))
}

onMounted(() => {
  immediateSearch()
})
</script>

<template>
  <div class="tk-prism-record-list tk-page-container">
    <PrismPageHeader
      :title="t('prism.record.list.title')"
      :subtitle="t('prism.record.list.subtitle')"
      :count="total"
      :count-label="t('prism.record.list.countLabel')"
    >
      <template #actions>
        <el-button @click="handleRefresh">
          {{ t('prism.record.list.refresh') }}
        </el-button>
        <el-button @click="handleExport">
          {{ t('prism.record.list.export') }}
        </el-button>
      </template>

      <template #chips>
        <span
          v-if="firingCount > 0"
          class="tk-prism-record-list__summary-chip"
        >
          <span class="tk-prism-record-list__summary-dot" />
          <span class="tk-prism-record-list__summary-label">
            {{ t('prism.record.list.summaryFiring') }}
          </span>
          <strong class="tk-prism-record-list__summary-num">{{ firingCount }}</strong>
        </span>
      </template>
    </PrismPageHeader>

    <DataTable
      table-id="alert-records"
      :data="data"
      :columns="columns"
      :loading="loading"
      :total="total"
      :current="page"
      :page-size="pageSize"
      :page-sizes="[10, 15, 20, 50]"
      row-key="id"
      :row-class-name="rowClassName"
      @page-change="handlePageChange"
    >
      <template #severity="{ row }">
        <el-tag
          v-if="(row as AlertRecord).severity"
          :type="severityTagType((row as AlertRecord).severity)"
          effect="light"
        >
          {{ severityLabel((row as AlertRecord).severity) }}
        </el-tag>
        <span v-else>-</span>
      </template>

      <template #value="{ row }">
        <span :class="{ 'tk-prism-record-list__value--firing': (row as AlertRecord).status === 'firing' }">
          {{ (row as AlertRecord).value }}
        </span>
      </template>

      <template #status="{ row }">
        <StatusTag
          category="alert"
          :status="(row as AlertRecord).status as AlertStatus"
          show-icon
        />
      </template>

      <template #acknowledgedAt="{ row }">
        {{ formatNullableTime((row as AlertRecord).acknowledgedAt) }}
      </template>

      <template #resolvedAt="{ row }">
        {{ formatNullableTime((row as AlertRecord).resolvedAt) }}
      </template>

      <template #action-column>
        <el-table-column
          :label="t('prism.record.list.action')"
          width="220"
          fixed="right"
          align="center"
          :resizable="false"
        >
          <template #default="{ row }">
            <div class="tk-record-actions" @click.stop>
              <el-button
                v-if="(row as AlertRecord).status === 'firing'"
                link
                type="warning"
                :loading="actionLoadingId === (row as AlertRecord).id"
                @click="handleAcknowledge(row as AlertRecord)"
              >
                {{ t('prism.record.list.acknowledge') }}
              </el-button>
              <el-button
                v-if="(row as AlertRecord).status === 'firing' || (row as AlertRecord).status === 'acknowledged'"
                link
                type="success"
                :loading="actionLoadingId === (row as AlertRecord).id"
                @click="handleResolve(row as AlertRecord)"
              >
                {{ t('prism.record.list.resolve') }}
              </el-button>
              <el-button
                link
                type="primary"
                @click="handleDetail(row as AlertRecord)"
              >
                {{ t('prism.record.list.detail') }}
              </el-button>
            </div>
          </template>
        </el-table-column>
      </template>
    </DataTable>
  </div>
</template>

<style scoped lang="scss">
.tk-record-actions {
  display: flex;
  gap: var(--tk-spacing-2);
  align-items: center;
  white-space: nowrap;
}

.tk-prism-record-list {
  &__summary-chip {
    display: inline-flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
    height: 32px;
    padding: 0 var(--tk-spacing-md);
    background-color: var(--tk-danger-color-bg);
    border: 1px solid var(--tk-danger-color-border);
    border-radius: var(--tk-radius-md);
  }

  &__summary-dot {
    flex-shrink: 0;
    width: 8px;
    height: 8px;
    background-color: var(--tk-danger-color);
    border-radius: var(--tk-radius-circle);
  }

  &__summary-label {
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-danger-color-text);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  &__summary-num {
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-base);
    font-weight: var(--tk-font-weight-bold);
    font-variant-numeric: tabular-nums;
    color: var(--tk-danger-color-text);
  }

  &__value--firing {
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-danger-color-text);
  }
}

/* Firing row red highlight (el-table rows are not scoped) */
:deep(.tk-prism-record-row--firing) {
  td {
    background-color: var(--tk-danger-color-bg) !important;
  }

  td:first-child {
    border-left: 3px solid var(--tk-danger-color);
  }
}

:deep(.tk-prism-record-row--firing-critical) {
  td {
    background-color: var(--tk-danger-color-bg-strong, rgb(245 108 108 / 12%)) !important;
  }

  td:first-child {
    border-left-color: var(--tk-danger-color);
  }
}
</style>
