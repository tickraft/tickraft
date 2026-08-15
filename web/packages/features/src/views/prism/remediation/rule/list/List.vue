// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { DataTable, ConfirmDialog, useTable, formatDate, usePermission } from '@tickraft/core'
import {
  getRemediationRules,
  deleteRemediationRule,
} from '../../../../../api/prism'
import type { RemediationRule } from '../../../../../api/prism'
import PrismPageHeader from '../../../components/PrismPageHeader.vue'

const router = useRouter()
const { t } = useI18n()
const { canDelete } = usePermission()

/** Delete confirmation dialog state */
const deleteVisible = ref(false)
const deleteTarget = ref<RemediationRule | null>(null)
const deleting = ref(false)

/** Table column configuration */
const columns = computed(() => [
  { prop: 'name', label: t('prism.remediation.rule.list.name'), minWidth: '200' },
  { prop: 'triggerEventType', label: t('prism.remediation.rule.list.triggerEventType'), width: '160', slot: 'triggerEventType' },
  { prop: 'executorType', label: t('prism.remediation.rule.list.executorType'), width: '140', slot: 'executorType' },
  { prop: 'enabled', label: t('prism.remediation.rule.list.enabled'), width: '100', slot: 'enabled' },
  { prop: 'lastRunAt', label: t('prism.remediation.rule.list.lastRunAt'), width: '180', slot: 'lastRunAt' },
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
} = useTable<RemediationRule>({
  defaultPageSize: 10,
  fetchFn: async (params) => {
    const res = await getRemediationRules({
      page: params.page,
      pageSize: params.size as number,
    })
    return { items: res.items, total: res.total }
  },
})

/** Pagination change handler */
function handlePageChange(payload: { current: number; pageSize: number }): void {
  if (payload.pageSize !== pageSize.value) {
    changePageSize(payload.pageSize)
  } else {
    changePage(payload.current)
  }
}

/** Navigate to create page */
function handleCreate(): void {
  router.push('/prism/remediation/rule/edit')
}

/** Navigate to edit page */
function handleEdit(row: RemediationRule): void {
  router.push(`/prism/remediation/rule/edit/${row.id}`)
}

/** Click delete */
function handleDeleteClick(row: RemediationRule): void {
  deleteTarget.value = row
  deleteVisible.value = true
}

/** Confirm delete */
async function handleDeleteConfirm(): Promise<void> {
  if (!deleteTarget.value) return
  deleting.value = true
  try {
    await deleteRemediationRule(deleteTarget.value.id)
    deleteVisible.value = false
    ElMessage.success(t('prism.remediation.rule.delete.deletedToast'))
    // After deletion, if only one item remains on the current page and it is not the first page, go back one page
    if (data.value.length === 1 && page.value > 1) {
      changePage(page.value - 1)
    } else {
      refresh()
    }
  } finally {
    deleting.value = false
  }
}

/** Refresh */
function handleRefresh(): void {
  refresh()
  ElMessage.success(t('prism.remediation.rule.list.refreshed'))
}

/** Validation text required to confirm deletion */
const deleteRequireInput = computed(() => deleteTarget.value?.name ?? '')

/** Format last run time (null means never run) */
function formatLastRun(row: RemediationRule): string {
  if (!row.lastRunAt) return t('prism.remediation.rule.list.neverRun')
  return formatDate(row.lastRunAt)
}

onMounted(() => {
  immediateSearch()
})
</script>

<template>
  <div class="tk-prism-remediation-rule-list tk-page-container">
    <PrismPageHeader
      :title="t('prism.remediation.rule.list.title')"
      :subtitle="t('prism.remediation.rule.list.subtitle')"
      :count="total"
      :count-label="t('prism.remediation.rule.list.countLabel')"
    >
      <template #actions>
        <el-button
          type="primary"
          @click="handleCreate"
        >
          + {{ t('prism.remediation.rule.list.create') }}
        </el-button>
        <el-button @click="handleRefresh">
          {{ t('prism.remediation.rule.list.refresh') }}
        </el-button>
      </template>
    </PrismPageHeader>

    <DataTable
      table-id="remediation-rules"
      :data="data"
      :columns="columns"
      :loading="loading"
      :total="total"
      :current="page"
      :page-size="pageSize"
      :page-sizes="[10, 20, 50]"
      row-key="id"
      @page-change="handlePageChange"
    >
      <!-- Trigger event type column -->
      <template #triggerEventType="{ row }">
        <el-tag
          type="warning"
          effect="light"
        >
          {{ t(`prism.remediation.rule.triggerType.${(row as RemediationRule).triggerEventType}`) }}
        </el-tag>
      </template>

      <!-- Executor type column -->
      <template #executorType="{ row }">
        <el-tag
          type="primary"
          effect="light"
        >
          {{ t(`prism.remediation.rule.executorType.${(row as RemediationRule).executorType}`) }}
        </el-tag>
      </template>

      <!-- Status column: enabled/disabled badge -->
      <template #enabled="{ row }">
        <el-tag
          :type="(row as RemediationRule).enabled ? 'success' : 'info'"
          effect="light"
        >
          {{ (row as RemediationRule).enabled
            ? t('prism.remediation.rule.list.statusEnabled')
            : t('prism.remediation.rule.list.statusDisabled') }}
        </el-tag>
      </template>

      <!-- Last run column: formatted date or "never run" -->
      <template #lastRunAt="{ row }">
        {{ formatLastRun(row as RemediationRule) }}
      </template>

      <!-- Action column -->
      <template #action-column>
        <el-table-column
          :label="t('prism.remediation.rule.list.action')"
          width="140"
          fixed="right"
          align="center"
          :resizable="false"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="handleEdit(row as RemediationRule)"
            >
              {{ t('prism.remediation.rule.list.edit') }}
            </el-button>
            <el-button
              v-if="canDelete('*')"
              link
              type="danger"
              @click="handleDeleteClick(row as RemediationRule)"
            >
              {{ t('prism.remediation.rule.list.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </template>
    </DataTable>

    <!-- Delete confirmation dialog (dangerous operation; requires rule name for secondary confirmation) -->
    <ConfirmDialog
      v-model="deleteVisible"
      :title="t('prism.remediation.rule.delete.title')"
      type="danger"
      :require-input="deleteRequireInput"
      :loading="deleting"
      @confirm="handleDeleteConfirm"
    >
      {{ t('prism.remediation.rule.delete.content', { name: deleteTarget?.name ?? '' }) }}
    </ConfirmDialog>
  </div>
</template>

<style scoped lang="scss">
/* PrismPageHeader provides the page header structure */
</style>
