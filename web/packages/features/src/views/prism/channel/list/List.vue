// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { DataTable, ConfirmDialog, useTable, formatDate, usePermission } from '@tickraft/core'
import {
  getChannels,
  deleteChannel,
  testChannel,
} from '../../../../api/prism'
import type { NotificationChannel } from '../../../../api/prism'
import PrismPageHeader from '../../components/PrismPageHeader.vue'
import ChannelForm from '../components/ChannelForm.vue'

const { t } = useI18n()
const { canDelete } = usePermission()

/** Drawer state */
const formVisible = ref(false)
/** Channel being edited (null = create mode) */
const editingChannel = ref<NotificationChannel | null>(null)

/** Delete confirmation dialog state */
const deleteVisible = ref(false)
const deleteTarget = ref<NotificationChannel | null>(null)
const deleting = ref(false)

/** Testing state (tracks which channel is being tested) */
const testingId = ref<number | null>(null)

/** Table column configuration */
const columns = computed(() => [
  { prop: 'name', label: t('prism.channel.list.name'), minWidth: '180' },
  { prop: 'type', label: t('prism.channel.list.type'), width: '120', slot: 'type' },
  { prop: 'enabled', label: t('prism.channel.list.enabled'), width: '100', slot: 'enabled' },
  { prop: 'lastUsedAt', label: t('prism.channel.list.lastUsedAt'), width: '180', slot: 'lastUsedAt' },
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
} = useTable<NotificationChannel>({
  defaultPageSize: 10,
  fetchFn: async (params) => {
    const res = await getChannels({
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

/** Open create form */
function handleCreate(): void {
  editingChannel.value = null
  formVisible.value = true
}

/** Open edit form */
function handleEdit(row: NotificationChannel): void {
  editingChannel.value = row
  formVisible.value = true
}

/** Called after a channel is created or updated successfully */
function handleSaved(): void {
  refresh()
}

/** Click delete */
function handleDeleteClick(row: NotificationChannel): void {
  deleteTarget.value = row
  deleteVisible.value = true
}

/** Confirm delete */
async function handleDeleteConfirm(): Promise<void> {
  if (!deleteTarget.value) return
  deleting.value = true
  try {
    await deleteChannel(deleteTarget.value.id)
    deleteVisible.value = false
    ElMessage.success(t('prism.channel.delete.deletedToast'))
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

/** Send a test notification */
async function handleTest(row: NotificationChannel): Promise<void> {
  testingId.value = row.id
  ElMessage.info(t('prism.channel.test.sending'))
  try {
    await testChannel(row.id)
    ElMessage.success(t('prism.channel.test.success'))
    refresh()
  } catch (err) {
    const errorMsg = err instanceof Error ? err.message : String(err)
    ElMessage.error(t('prism.channel.test.failure', { error: errorMsg }))
  } finally {
    testingId.value = null
  }
}

/** Refresh */
function handleRefresh(): void {
  refresh()
  ElMessage.success(t('prism.channel.list.refreshed'))
}

/** Validation text required to confirm deletion */
const deleteRequireInput = computed(() => deleteTarget.value?.name ?? '')

/** Format last used time (null means never used) */
function formatLastUsed(row: NotificationChannel): string {
  if (!row.lastUsedAt) return t('prism.channel.list.neverUsed')
  return formatDate(row.lastUsedAt)
}

onMounted(() => {
  immediateSearch()
})
</script>

<template>
  <div class="tk-prism-channel-list tk-page-container">
    <PrismPageHeader
      :title="t('prism.channel.list.title')"
      :subtitle="t('prism.channel.list.subtitle')"
      :count="total"
      :count-label="t('prism.channel.list.countLabel')"
    >
      <template #actions>
        <el-button
          type="primary"
          @click="handleCreate"
        >
          + {{ t('prism.channel.list.create') }}
        </el-button>
        <el-button @click="handleRefresh">
          {{ t('prism.channel.list.refresh') }}
        </el-button>
      </template>
    </PrismPageHeader>

    <DataTable
      table-id="alert-channels"
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
      <!-- Type column: badge showing channel type -->
      <template #type="{ row }">
        <el-tag
          :type="(row as NotificationChannel).type === 'email' ? 'warning' : 'primary'"
          effect="light"
        >
          {{ t(`prism.channel.type.${(row as NotificationChannel).type}`) }}
        </el-tag>
      </template>

      <!-- Status column: enabled/disabled badge -->
      <template #enabled="{ row }">
        <el-tag
          :type="(row as NotificationChannel).enabled ? 'success' : 'info'"
          effect="light"
        >
          {{ (row as NotificationChannel).enabled
            ? t('prism.channel.list.statusEnabled')
            : t('prism.channel.list.statusDisabled') }}
        </el-tag>
      </template>

      <!-- Last used column: formatted date or "never used" -->
      <template #lastUsedAt="{ row }">
        {{ formatLastUsed(row as NotificationChannel) }}
      </template>

      <!-- Action column -->
      <template #action-column>
        <el-table-column
          :label="t('prism.channel.list.action')"
          width="180"
          fixed="right"
          align="center"
          :resizable="false"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="handleEdit(row as NotificationChannel)"
            >
              {{ t('prism.channel.list.edit') }}
            </el-button>
            <el-button
              link
              type="success"
              :loading="testingId === (row as NotificationChannel).id"
              @click="handleTest(row as NotificationChannel)"
            >
              {{ t('prism.channel.list.test') }}
            </el-button>
            <el-button
              v-if="canDelete('*')"
              link
              type="danger"
              @click="handleDeleteClick(row as NotificationChannel)"
            >
              {{ t('prism.channel.list.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </template>
    </DataTable>

    <!-- Delete confirmation dialog (dangerous operation; requires channel name for secondary confirmation) -->
    <ConfirmDialog
      v-model="deleteVisible"
      :title="t('prism.channel.delete.title')"
      type="danger"
      :require-input="deleteRequireInput"
      :loading="deleting"
      @confirm="handleDeleteConfirm"
    >
      {{ t('prism.channel.delete.content', { name: deleteTarget?.name ?? '' }) }}
    </ConfirmDialog>

    <!-- Channel create/edit drawer -->
    <ChannelForm
      v-model="formVisible"
      :channel="editingChannel"
      @saved="handleSaved"
    />
  </div>
</template>

<style scoped lang="scss">
/* PrismPageHeader provides the page header structure */
</style>
