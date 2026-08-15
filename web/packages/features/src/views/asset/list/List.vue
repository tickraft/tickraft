// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Asset list page.
 *
 * Lightweight asset registry list with status summary cards (inspired by the
 * tickraft-x storyboard), keyword search, type / status filters, and standard
 * CRUD actions. Backed by /api/v1/assets via the api/asset.ts layer.
 */
import { ref, reactive, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { SearchForm, DataTable, ConfirmDialog, StatusTag, usePermission } from '@tickraft/core'
import type { Asset, AssetStatus, AssetMetadata, AssetListQuery } from '../../../types/asset'
import {
  ASSET_TYPES,
  ASSET_STATUSES,
  getAssets,
  deleteAsset,
  parseMetadata,
} from '../../../api/asset'
import { getGlobalStats } from '../../../api/system'

const router = useRouter()
const { t } = useI18n()
const { canDelete } = usePermission()

const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)
const tableData = ref<Asset[]>([])

const deleteVisible = ref(false)
const deleteLoading = ref(false)
const deleteTarget = ref<Asset | null>(null)

/**
 * Unfiltered status counts for the summary cards, sourced from the
 * /system/stats asset_status_counts aggregate so the cards always reflect the
 * whole dataset regardless of the current table filters.
 */
const summaryCounts = reactive<Record<AssetStatus | 'total', number>>({
  total: 0,
  normal: 0,
  abnormal: 0,
  offline: 0,
  unknown: 0,
})

interface SearchValues {
  keyword: string
  type: string
  status: string
}

const searchValues = reactive<SearchValues>({
  keyword: '',
  type: '',
  status: '',
})

const searchFields = computed(() => [
  {
    prop: 'keyword',
    label: t('asset.list.searchKeyword'),
    type: 'input' as const,
    placeholder: t('asset.list.searchKeywordPlaceholder'),
  },
  {
    prop: 'type',
    label: t('asset.list.type'),
    type: 'select' as const,
    placeholder: t('asset.list.typePlaceholder'),
    options: ASSET_TYPES.map((value) => ({ label: t(`asset.type.${value}`), value })),
  },
  {
    prop: 'status',
    label: t('asset.list.status'),
    type: 'select' as const,
    placeholder: t('asset.list.statusPlaceholder'),
    options: ASSET_STATUSES.map((value) => ({ label: t(`asset.status.${value}`), value })),
  },
])

const summaryCards = computed(() => [
  { key: '', label: t('asset.summary.total'), value: summaryCounts.total, tone: 'primary' as const, active: searchValues.status === '' },
  { key: 'normal', label: t('asset.status.normal'), value: summaryCounts.normal, tone: 'success' as const, active: searchValues.status === 'normal' },
  { key: 'abnormal', label: t('asset.status.abnormal'), value: summaryCounts.abnormal, tone: 'warning' as const, active: searchValues.status === 'abnormal' },
  { key: 'offline', label: t('asset.status.offline'), value: summaryCounts.offline, tone: 'danger' as const, active: searchValues.status === 'offline' },
  { key: 'unknown', label: t('asset.status.unknown'), value: summaryCounts.unknown, tone: 'info' as const, active: searchValues.status === 'unknown' },
])

const columns = computed(() => [
  { prop: 'name', label: t('asset.list.name'), minWidth: 180, slot: 'name', align: 'left' as const },
  { prop: 'assetType', label: t('asset.list.type'), minWidth: 120, slot: 'type' },
  { prop: 'assetKey', label: t('asset.list.endpoint'), minWidth: 200, slot: 'endpoint', align: 'left' as const },
  { prop: 'status', label: t('asset.list.status'), minWidth: 110, slot: 'status' },
  { prop: 'labels', label: t('asset.list.labels'), minWidth: 200, slot: 'labels', align: 'left' as const },
  { prop: 'lastActiveAt', label: t('asset.list.lastSeen'), minWidth: 170, slot: 'lastSeen' },
])

/**
 * Fetch the current page from the backend with server-side keyword / type /
 * status filtering, and refresh the unfiltered summary counts from
 * /system/stats.
 */
async function fetchData(): Promise<void> {
  loading.value = true
  try {
    const res = await getAssets({
      page: currentPage.value,
      pageSize: pageSize.value,
      keyword: searchValues.keyword.trim() || undefined,
      assetType: (searchValues.type || undefined) as AssetListQuery['assetType'],
      status: (searchValues.status || undefined) as AssetListQuery['status'],
    })
    tableData.value = res.items || []
    total.value = res.total || 0

    await refreshSummaryCounts()
  } catch {
    tableData.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

/** Refresh the summary card counts from the system stats aggregate. */
async function refreshSummaryCounts(): Promise<void> {
  try {
    const stats = await getGlobalStats()
    summaryCounts.total = stats.totalDevices || 0
    summaryCounts.normal = stats.assetStatusCounts?.normal || 0
    summaryCounts.abnormal = stats.assetStatusCounts?.abnormal || 0
    summaryCounts.offline = stats.assetStatusCounts?.offline || 0
    summaryCounts.unknown = stats.assetStatusCounts?.unknown || 0
  } catch {
    // Summary cards degrade to zeros; the table remains functional.
  }
}

function handleSearch(values: Record<string, unknown>): void {
  searchValues.keyword = (values.keyword as string) ?? ''
  searchValues.type = (values.type as string) ?? ''
  searchValues.status = (values.status as string) ?? ''
  currentPage.value = 1
  void fetchData()
}

function handleReset(): void {
  searchValues.keyword = ''
  searchValues.type = ''
  searchValues.status = ''
  currentPage.value = 1
  void fetchData()
}

function handlePageChange({ current, pageSize: size }: { current: number; pageSize: number }): void {
  currentPage.value = current
  pageSize.value = size
  void fetchData()
}

/** Click a summary card to filter by status (toggle off if already active) */
function handleSummaryClick(status: string): void {
  searchValues.status = searchValues.status === status ? '' : status
  currentPage.value = 1
  void fetchData()
}

function handleCreate(): void {
  router.push('/asset/create')
}

function handleRefresh(): void {
  void fetchData()
}

function handleDetail(row: Asset): void {
  router.push(`/asset/detail/${row.id}`)
}

function handleEdit(row: Asset): void {
  router.push(`/asset/edit/${row.id}`)
}

function handleDelete(row: Asset): void {
  deleteTarget.value = row
  deleteVisible.value = true
}

async function confirmDelete(): Promise<void> {
  if (!deleteTarget.value) return
  deleteLoading.value = true
  try {
    await deleteAsset(deleteTarget.value.id)
    ElMessage.success(t('asset.list.deleteSuccess'))
    deleteVisible.value = false
    deleteTarget.value = null
    await fetchData()
  } catch {
    // Errors are handled centrally by the interceptor
  } finally {
    deleteLoading.value = false
  }
}

/** Extract typed metadata from the JSON-encoded metadata field */
function meta(asset: Asset): AssetMetadata {
  return parseMetadata(asset.metadata)
}

/** Format endpoint with optional port from metadata */
function formatEndpoint(asset: Asset): string {
  const m = meta(asset)
  const endpoint = m.endpoint ?? asset.assetKey
  return m.port ? `${endpoint}:${m.port}` : endpoint
}

/** Format ISO timestamp to a readable local string */
function formatTime(iso: string): string {
  if (!iso) return '-'
  const d = new Date(iso.replace(' ', 'T'))
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

onMounted(() => {
  void fetchData()
})
</script>

<template>
  <div class="tk-page-container">
    <!-- Page header -->
    <div class="tk-asset-header">
      <div class="tk-asset-header__left">
        <div class="tk-asset-header__title-row">
          <h1 class="tk-asset-header__title">
            {{ t('asset.list.title') }}
          </h1>
          <span class="tk-asset-header__count">
            {{ total }} {{ t('asset.list.countSuffix') }}
          </span>
        </div>
        <p class="tk-asset-header__subtitle">
          {{ t('asset.list.subtitle') }}
        </p>
      </div>
      <div class="tk-asset-header__actions">
        <el-button @click="handleRefresh">
          {{ t('asset.list.refresh') }}
        </el-button>
        <el-button
          type="primary"
          @click="handleCreate"
        >
          {{ t('asset.list.create') }}
        </el-button>
      </div>
    </div>

    <!-- Summary cards -->
    <div class="tk-asset-summary">
      <button
        v-for="card in summaryCards"
        :key="card.key || 'total'"
        type="button"
        class="tk-asset-summary__card"
        :class="[
          `tk-asset-summary__card--${card.tone}`,
          { 'tk-asset-summary__card--active': card.active },
        ]"
        @click="handleSummaryClick(card.key)"
      >
        <span class="tk-asset-summary__label">{{ card.label }}</span>
        <span class="tk-asset-summary__value">{{ card.value }}</span>
      </button>
    </div>

    <!-- Search area -->
    <div class="tk-search-area">
      <SearchForm
        :fields="searchFields"
        :model-value="searchValues"
        :loading="loading"
        @search="handleSearch"
        @reset="handleReset"
      />
    </div>

    <!-- Table area -->
    <div class="tk-table-area">
      <DataTable
        table-id="asset-list"
        :data="tableData"
        :columns="columns"
        :loading="loading"
        :total="total"
        :current="currentPage"
        :page-size="pageSize"
        @page-change="handlePageChange"
      >
        <template #name="{ row }">
          <el-button
            link
            type="primary"
            class="tk-asset-name-link"
            @click="handleDetail(row as Asset)"
          >
            {{ (row as Asset).name }}
          </el-button>
        </template>

        <template #type="{ row }">
          <el-tag
            size="small"
            effect="light"
            type="info"
          >
            {{ t(`asset.type.${(row as Asset).assetType}`) }}
          </el-tag>
        </template>

        <template #endpoint="{ row }">
          <span class="tk-asset-mono">{{ formatEndpoint(row as Asset) }}</span>
        </template>

        <template #status="{ row }">
          <StatusTag
            category="asset"
            :status="(row as Asset).status"
            size="sm"
            show-icon
          />
        </template>

        <template #labels="{ row }">
          <span class="tk-asset-labels">
            <el-tag
              v-for="label in meta(row as Asset).labels ?? []"
              :key="label"
              size="small"
              effect="plain"
              class="tk-asset-label"
            >
              {{ label }}
            </el-tag>
            <span
              v-if="!meta(row as Asset).labels?.length"
              class="tk-asset-labels-empty"
            >-</span>
          </span>
        </template>

        <template #lastSeen="{ row }">
          <span class="tk-asset-mono">{{ formatTime((row as Asset).lastActiveAt) }}</span>
        </template>

        <template #action-column>
          <el-table-column
            :label="t('asset.list.operation')"
            width="180"
            fixed="right"
            align="center"
            :resizable="false"
          >
            <template #default="{ row }">
              <el-button
                link
                type="primary"
                @click="handleDetail(row as Asset)"
              >
                {{ t('common.app.detail') }}
              </el-button>
              <el-button
                link
                type="primary"
                @click="handleEdit(row as Asset)"
              >
                {{ t('common.app.edit') }}
              </el-button>
              <el-button
                v-if="canDelete('device')"
                link
                type="danger"
                @click="handleDelete(row as Asset)"
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
      :title="t('asset.list.deleteTitle')"
      :content="t('asset.list.deleteContent', { name: deleteTarget?.name ?? '' })"
      :loading="deleteLoading"
      type="danger"
      @confirm="confirmDelete"
    />
  </div>
</template>

<style scoped lang="scss">
.tk-asset-header {
  display: flex;
  flex-wrap: wrap;
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

.tk-asset-summary {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: var(--tk-spacing-sm);
  margin-bottom: var(--tk-spacing-md);

  &__card {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-xs);
    padding: var(--tk-spacing-md) var(--tk-spacing-lg);
    overflow: hidden;
    text-align: left;
    cursor: pointer;
    background-color: var(--tk-bg-surface);
    border: 1px solid var(--tk-border-color-base);
    border-radius: var(--tk-border-radius-base);
    transition: border-color var(--tk-duration-fast) var(--tk-ease-out),
      transform var(--tk-duration-fast) var(--tk-ease-out);

    &:hover {
      border-color: var(--tk-border-color-dark);
      transform: translateY(-1px);
    }

    &::before {
      position: absolute;
      top: 0;
      left: 0;
      width: 3px;
      height: 100%;
      content: '';
      background-color: var(--tk-text-secondary);
    }

    &--primary::before { background-color: var(--tk-primary-color); }
    &--success::before { background-color: var(--tk-success-color); }
    &--warning::before { background-color: var(--tk-warning-color); }
    &--danger::before { background-color: var(--tk-danger-color); }
    &--info::before { background-color: var(--tk-info-color); }

    &--active {
      border-color: var(--tk-primary-color);
      box-shadow: 0 0 0 1px var(--tk-primary-color-border);
    }
  }

  &__label {
    font-family: var(--tk-font-mono, monospace);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-medium);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  &__value {
    font-family: var(--tk-font-display, var(--tk-font-mono, monospace));
    font-size: var(--tk-font-size-xl);
    font-weight: var(--tk-font-weight-bold);
    font-variant-numeric: tabular-nums;
    line-height: 1;
    color: var(--tk-text-primary);
  }
}

.tk-asset-mono {
  font-family: var(--tk-font-mono, monospace);
  font-size: var(--tk-font-size-xs);
  font-variant-numeric: tabular-nums;
  color: var(--tk-text-regular);
}

.tk-asset-name-link {
  padding: 0;
  font-weight: var(--tk-font-weight-medium);
}

.tk-asset-labels {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
}

.tk-asset-label {
  font-size: var(--tk-font-size-xs);
}

.tk-asset-labels-empty {
  font-size: var(--tk-font-size-sm);
  color: var(--tk-text-placeholder);
}

@media (max-width: 1023px) {
  .tk-asset-summary {
    grid-template-columns: repeat(3, 1fr);
  }
}

@media (max-width: 639px) {
  .tk-asset-summary {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
