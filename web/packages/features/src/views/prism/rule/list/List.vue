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
  getAlertRules,
  getAlertRule,
  updateAlertRule,
  deleteAlertRule,
} from '../../../../api/prism'
import type { AlertRule } from '../../../../api/prism'
import { parseDate } from '../../constants'
import PrismPageHeader from '../../components/PrismPageHeader.vue'

const router = useRouter()
const { t } = useI18n()
const { canDelete } = usePermission()

const deleteVisible = ref(false)
const deleteTarget = ref<AlertRule | null>(null)
const deleting = ref(false)

/** Scene tag type mapping */
function sceneTagType(scene: string): 'success' | 'warning' | 'danger' | 'info' {
  switch (scene) {
    case 'task': return 'info'
    case 'probe': return 'success'
    case 'metric': return 'warning'
    case 'remediation': return 'danger'
    default: return 'info'
  }
}

/** Table column configuration */
const columns = computed(() => [
  { prop: 'name', label: t('prism.rule.list.name'), minWidth: '160' },
  { prop: 'scene', label: t('prism.rule.list.scene'), width: '120', slot: 'scene' },
  { prop: 'expression', label: t('prism.rule.list.expression'), minWidth: '200', showOverflowTooltip: true },
  { prop: 'priority', label: t('prism.rule.list.priority'), width: '100', align: 'center' as const },
  { prop: 'enabled', label: t('prism.rule.list.enabled'), width: '90', slot: 'enabled' },
  { prop: 'createdAt', label: t('prism.rule.list.createdAt'), width: '170', align: 'center' as const, formatter: formatTimeCell },
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
} = useTable<AlertRule>({
  defaultPageSize: 10,
  fetchFn: async (params) => {
    const res = await getAlertRules({
      page: params.page,
      pageSize: params.size as number,
    })
    return { items: res.items, total: res.total }
  },
})

/** Time cell formatter */
function formatTimeCell(_row: AlertRule, _column: unknown, value: unknown): string {
  if (!value) return '-'
  return formatDate(parseDate(String(value)))
}

/** Pagination change handler */
function handlePageChange(payload: { current: number; pageSize: number }): void {
  if (payload.pageSize !== pageSize.value) {
    changePageSize(payload.pageSize)
  } else {
    changePage(payload.current)
  }
}

/** Create new rule */
function handleCreate(): void {
  router.push('/prism/rule/edit')
}

/** Edit rule */
function handleEdit(row: AlertRule): void {
  router.push(`/prism/rule/edit/${row.id}`)
}

/** Toggle rule enabled state.
 *  Backend UpdateAlertRule uses PUT semantics with the full AlertRule struct,
 *  so we fetch the complete rule first, toggle only `enabled`, and send the
 *  full payload to avoid overwriting other fields with zero values.
 */
async function handleToggle(
  row: AlertRule,
  enabled: boolean | string | number,
): Promise<void> {
  const value = Boolean(enabled)
  try {
    const fullRule = await getAlertRule(row.id)
    const updated = await updateAlertRule(row.id, {
      name: fullRule.name,
      description: fullRule.description,
      scene: fullRule.scene,
      expression: fullRule.expression,
      priority: fullRule.priority,
      enabled: value,
    })
    const idx = data.value.findIndex((r) => r.id === row.id)
    if (idx !== -1) data.value[idx] = updated
    ElMessage.success(
      t(value ? 'prism.rule.list.toggleOn' : 'prism.rule.list.toggleOff', { name: row.name }),
    )
  } catch {
    // Refresh on failure to restore the real state
    refresh()
  }
}

/** Click delete */
function handleDeleteClick(row: AlertRule): void {
  deleteTarget.value = row
  deleteVisible.value = true
}

/** Confirm delete */
async function handleDeleteConfirm(): Promise<void> {
  if (!deleteTarget.value) return
  deleting.value = true
  try {
    await deleteAlertRule(deleteTarget.value.id)
    deleteVisible.value = false
    ElMessage.success(t('prism.rule.list.deletedToast'))
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
  ElMessage.success(t('prism.rule.list.refreshed'))
}

/** Validation text required to confirm deletion */
const deleteRequireInput = computed(() => deleteTarget.value?.name ?? '')

onMounted(() => {
  immediateSearch()
})
</script>

<template>
  <div class="tk-prism-rule-list tk-page-container">
    <PrismPageHeader
      :title="t('prism.rule.list.title')"
      :subtitle="t('prism.rule.list.subtitle')"
      :count="total"
      :count-label="t('prism.rule.list.countLabel')"
    >
      <template #actions>
        <el-button
          type="primary"
          @click="handleCreate"
        >
          + {{ t('prism.rule.list.create') }}
        </el-button>
        <el-button @click="handleRefresh">
          {{ t('prism.rule.list.refresh') }}
        </el-button>
      </template>
    </PrismPageHeader>

    <DataTable
      table-id="alert-rules"
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
      <template #scene="{ row }">
        <el-tag
          :type="sceneTagType((row as AlertRule).scene)"
          effect="light"
        >
          {{ t(`prism.scene.${(row as AlertRule).scene}`) }}
        </el-tag>
      </template>

      <template #enabled="{ row }">
        <el-switch
          :model-value="(row as AlertRule).enabled"
          @change="handleToggle(row as AlertRule, $event)"
        />
      </template>

      <template #action-column>
        <el-table-column
          :label="t('prism.rule.list.action')"
          width="150"
          fixed="right"
          align="center"
          :resizable="false"
        >
          <template #default="{ row }">
            <div class="tk-rule-actions" @click.stop>
              <el-button
                link
                type="primary"
                @click="handleEdit(row as AlertRule)"
              >
                {{ t('prism.rule.list.edit') }}
              </el-button>
              <el-button
                v-if="canDelete('alert')"
                link
                type="danger"
                @click="handleDeleteClick(row as AlertRule)"
              >
                {{ t('prism.rule.list.delete') }}
              </el-button>
            </div>
          </template>
        </el-table-column>
      </template>
    </DataTable>

    <!-- Delete confirmation dialog (dangerous operation; requires rule name for secondary confirmation) -->
    <ConfirmDialog
      v-model="deleteVisible"
      :title="t('prism.rule.list.deleteTitle')"
      type="danger"
      :require-input="deleteRequireInput"
      :loading="deleting"
      @confirm="handleDeleteConfirm"
    />
  </div>
</template>

<style scoped lang="scss">
.tk-rule-actions {
  display: flex;
  gap: var(--tk-spacing-2);
  align-items: center;
  white-space: nowrap;
}
</style>
