// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { DataTable, SearchForm, StatusTag, useTable, formatDate } from '@tickraft/core'
import PrismPageHeader from '../../components/PrismPageHeader.vue'
import { getRemediationRecords } from '../../../../api/prism'
import type { RemediationRecord } from '../../../../api/prism'

const { t } = useI18n()

/** Search form */
const searchModel = reactive<Record<string, unknown>>({
  status: '',
})

/** Lifecycle status options aligned with backend record statuses */
const statusOptions = computed(() => [
  { label: t('prism.remediation.list.statusCompleted'), value: 'completed' },
  { label: t('prism.remediation.list.statusFailed'), value: 'failed' },
  { label: t('prism.remediation.list.statusStarted'), value: 'started' },
  { label: t('prism.remediation.list.statusTriggered'), value: 'triggered' },
  { label: t('prism.remediation.list.statusSkipped'), value: 'skipped' },
])

const searchFields = computed(() => [
  {
    prop: 'status',
    label: t('prism.remediation.list.status'),
    type: 'select' as const,
    placeholder: t('prism.remediation.list.allStatus'),
    span: 8,
    options: statusOptions.value,
  },
])

/** Table columns */
const columns = computed(() => [
  { prop: 'ruleName', label: t('prism.remediation.list.ruleName'), minWidth: 160 },
  { prop: 'trigger', label: t('prism.remediation.list.actionType'), width: 140, slot: 'trigger' },
  { prop: 'assetKey', label: t('prism.remediation.list.asset'), minWidth: 140, slot: 'asset' },
  { prop: 'status', label: t('prism.remediation.list.status'), width: 120, slot: 'status' },
  { prop: 'error', label: t('prism.remediation.list.error'), minWidth: 180, slot: 'error' },
  { prop: 'startedAt', label: t('prism.remediation.list.startedAt'), width: 180, slot: 'startedAt' },
  { prop: 'finishedAt', label: t('prism.remediation.list.finishedAt'), width: 180, slot: 'finishedAt' },
])

const {
  data,
  loading,
  total,
  page,
  pageSize,
  immediateSearch,
  resetSearch,
  changePage,
  changePageSize,
} = useTable<RemediationRecord>({
  defaultPageSize: 15,
  fetchFn: async (params) => {
    const status = (params.status as string) || ''
    return getRemediationRecords({
      page: Number(params.page) || 1,
      pageSize: Number(params.size) || 15,
      ...(status ? { status } : {}),
    })
  },
})

/** Click search: trigger query with current search model */
function handleSearch(values: Record<string, unknown>): void {
  immediateSearch({
    status: (values.status as string) || '',
  })
}

/** Reset search conditions */
function handleReset(): void {
  resetSearch()
}

/** Pagination change handler */
function handlePageChange(payload: { current: number; pageSize: number }): void {
  if (payload.pageSize !== pageSize.value) {
    changePageSize(payload.pageSize)
  } else {
    changePage(payload.current)
  }
}

/** Display label for a record's trigger type */
function triggerLabel(trigger: string): string {
  const key = `prism.remediation.rule.triggerType.${trigger}`
  const label = t(key)
  return label === key ? trigger : label
}

/** Resolve the asset display value: asset key when present, else numeric ID */
function assetLabel(row: RemediationRecord): string {
  return row.assetKey || (row.assetId ? `#${row.assetId}` : '-')
}

onMounted(() => {
  immediateSearch()
})
</script>

<template>
  <div class="tk-prism-remediation-list tk-page-container">
    <PrismPageHeader
      :title="t('prism.remediation.list.title')"
      :subtitle="t('prism.remediation.list.subtitle')"
      :count="total"
      :count-label="t('prism.remediation.list.countLabel')"
    />

    <SearchForm
      v-model="searchModel"
      :fields="searchFields"
      :loading="loading"
      :show-collapse="false"
      @search="handleSearch"
      @reset="handleReset"
    />

    <DataTable
      table-id="remediation-records"
      :data="data"
      :columns="columns"
      :loading="loading"
      :total="total"
      :current="page"
      :page-size="pageSize"
      :page-sizes="[10, 15, 20, 50]"
      row-key="id"
      @page-change="handlePageChange"
    >
      <template #trigger="{ row }">
        {{ triggerLabel((row as RemediationRecord).trigger) }}
      </template>
      <template #asset="{ row }">
        {{ assetLabel(row as RemediationRecord) }}
      </template>
      <template #status="{ row }">
        <StatusTag
          category="log"
          :status="(row as RemediationRecord).status"
          show-icon
        >
          {{ t(`prism.remediation.list.status${(row as RemediationRecord).status.charAt(0).toUpperCase() + (row as RemediationRecord).status.slice(1)}`) }}
        </StatusTag>
      </template>
      <template #error="{ row }">
        <span class="tk-prism-remediation-list__error">
          {{ (row as RemediationRecord).error || '-' }}
        </span>
      </template>
      <template #startedAt="{ row }">
        {{ (row as RemediationRecord).startedAt ? formatDate((row as RemediationRecord).startedAt as string) : '-' }}
      </template>
      <template #finishedAt="{ row }">
        {{ (row as RemediationRecord).finishedAt ? formatDate((row as RemediationRecord).finishedAt as string) : '-' }}
      </template>
    </DataTable>
  </div>
</template>

<style scoped lang="scss">
/* PrismPageHeader provides the page header structure */
.tk-prism-remediation-list__error {
  color: var(--tk-text-secondary);
}
</style>
