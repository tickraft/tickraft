// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Monitor point templates page.
 *
 * Provides pre-configured probe/monitor recipes to quickly create new monitor
 * points. The backend ListTemplates endpoint returns a plain array (not
 * paginated), so pagination and type-chip filtering are handled client-side.
 * The category filter is sent to the backend as a query parameter.
 *
 * Actions:
 * - Apply: creates a new monitor point from a template via POST /templates/:id/apply
 * - Delete: deletes a custom (non-builtin) template
 */
import { ref, reactive, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import { DataTable, SearchForm, formatDate, usePermission } from '@tickraft/core'
import { getTelemetryTemplates, deleteTelemetryTemplate, applyTemplate } from '../../../../api/telemetry'
import type { TelemetryTemplate } from '../../../../types/telemetry'

const router = useRouter()
const { t } = useI18n()
const { canDelete } = usePermission()

const loading = ref(false)
const allTemplates = ref<TelemetryTemplate[]>([])
const currentPage = ref(1)
const pageSize = ref(15)

/** Search form */
const searchModel = reactive<Record<string, unknown>>({
  category: '',
})

const searchFields = computed(() => [
  {
    prop: 'category',
    label: t('telemetry.monitor.templates.category'),
    type: 'select' as const,
    placeholder: t('telemetry.monitor.templates.allCategories'),
    span: 8,
    options: [
      { label: t('telemetry.monitor.templates.categoryProbe'), value: 'probe' },
      { label: t('telemetry.monitor.templates.categoryMonitor'), value: 'monitor' },
    ],
  },
])

/** Type chip filter state */
const activeType = ref('')

/** Type chip definitions with semantic dot colors */
const TYPE_CHIP_COLORS: Record<string, string> = {
  icmp: 'var(--tk-info-color)',
  tcp: 'var(--tk-success-color)',
  http: 'var(--tk-primary-color)',
  webhook: 'var(--tk-warning-color)',
  dns: 'var(--tk-warning-color)',
  ssl: 'var(--tk-danger-color)',
}

const availableTypes = computed(() => {
  const types = new Set<string>()
  for (const item of allTemplates.value) {
    if (item.executorType) {
      types.add(item.executorType)
    }
  }
  return Array.from(types)
})

const typeChipItems = computed(() => {
  const items: { type: string; label: string; count: number; dot: string }[] = []
  for (const type of availableTypes.value) {
    const count = allTemplates.value.filter((item) => item.executorType === type).length
    items.push({
      type,
      label: t(`telemetry.monitor.type.${type}`, type.toUpperCase()),
      count,
      dot: TYPE_CHIP_COLORS[type] || 'var(--tk-text-secondary)',
    })
  }
  return items
})

/** Filtered by type chip (client-side) */
const filteredData = computed(() => {
  if (!activeType.value) return allTemplates.value
  return allTemplates.value.filter((item) => item.executorType === activeType.value)
})

/** Total after type chip filter */
const total = computed(() => filteredData.value.length)

/** Paginated data (client-side) */
const pagedData = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return filteredData.value.slice(start, start + pageSize.value)
})

/** Table columns */
const columns = computed(() => [
  { prop: 'name', label: t('telemetry.monitor.templates.name'), minWidth: 160 },
  { prop: 'category', label: t('telemetry.monitor.templates.category'), width: 120 },
  { prop: 'executorType', label: t('telemetry.monitor.templates.executorType'), width: 120, slot: 'executorType' },
  { prop: 'isBuiltin', label: t('telemetry.monitor.templates.builtin'), width: 100, slot: 'isBuiltin' },
  { prop: 'updatedAt', label: t('telemetry.monitor.templates.updatedAt'), width: 180, slot: 'updatedAt' },
])

/** Fetch all templates from the backend API */
async function fetchData(): Promise<void> {
  loading.value = true
  try {
    const res = await getTelemetryTemplates({
      category: (searchModel.category as string) || undefined,
    })
    // Backend returns a plain array, not paginated
    allTemplates.value = Array.isArray(res) ? res : []
  } catch {
    allTemplates.value = []
  } finally {
    loading.value = false
  }
}

/** Apply a template to create a new monitor point */
async function handleApply(row: TelemetryTemplate): Promise<void> {
  try {
    await ElMessageBox.confirm(
      t('telemetry.monitor.templates.applyConfirm', { name: row.name }),
      t('common.app.confirm'),
      { type: 'info' },
    )
    await applyTemplate(row.id)
    ElMessage.success(t('telemetry.monitor.templates.applySuccess'))
    router.push('/telemetry/monitor/list')
  } catch {
    // user cancelled or apply failed
  }
}

/** Delete a custom template */
async function handleDelete(row: TelemetryTemplate): Promise<void> {
  try {
    await ElMessageBox.confirm(
      t('telemetry.monitor.templates.deleteConfirm', { name: row.name }),
      t('common.app.confirm'),
      { type: 'warning' },
    )
    await deleteTelemetryTemplate(row.id)
    ElMessage.success(t('telemetry.monitor.templates.deleteSuccess'))
    fetchData()
  } catch {
    // user cancelled or delete failed
  }
}

/** Click search: trigger query with current search model */
function handleSearch(values: Record<string, unknown>): void {
  searchModel.category = (values.category as string) || ''
  currentPage.value = 1
  void fetchData()
}

/** Reset search conditions */
function handleReset(): void {
  searchModel.category = ''
  activeType.value = ''
  currentPage.value = 1
  void fetchData()
}

/** Pagination change handler */
function handlePageChange(payload: { current: number; pageSize: number }): void {
  currentPage.value = payload.current
  pageSize.value = payload.pageSize
}

/** Type chip click: filter by type (client-side) */
function handleTypeChipClick(type: string): void {
  activeType.value = activeType.value === type ? '' : type
  currentPage.value = 1
}

/** Handle back to monitor list */
function handleBack(): void {
  router.push('/telemetry/monitor/list')
}

/** Handle create blank */
function handleCreateBlank(): void {
  router.push('/telemetry/monitor/create')
}

onMounted(() => {
  void fetchData()
})
</script>

<template>
  <div class="tk-monitor-templates tk-page-container">
    <!-- Page header -->
    <div class="tk-tpl-header">
      <div class="tk-tpl-header__left">
        <el-button
          class="tk-tpl-header__back"
          circle
          @click="handleBack"
        >
          <el-icon><ArrowLeft /></el-icon>
        </el-button>
        <div class="tk-tpl-header__title-block">
          <div class="tk-tpl-header__eyebrow">
            {{ t('telemetry.monitor.templates.eyebrow') }}
          </div>
          <div class="tk-tpl-header__title-line">
            <h1 class="tk-tpl-header__title">
              {{ t('telemetry.monitor.templates.title') }}
            </h1>
            <span class="tk-tpl-header__count">
              {{ total }} {{ t('telemetry.monitor.templates.countSuffix') }}
            </span>
          </div>
          <p class="tk-tpl-header__subtitle">
            {{ t('telemetry.monitor.templates.subtitle') }}
          </p>
        </div>
      </div>
      <div class="tk-tpl-header__actions">
        <el-button @click="fetchData">
          {{ t('telemetry.monitor.list.refresh') }}
        </el-button>
        <el-button
          type="primary"
          @click="handleCreateBlank"
        >
          {{ t('telemetry.monitor.templates.createBlank') }}
        </el-button>
      </div>
    </div>

    <!-- Type chip filter strip -->
    <div
      v-if="typeChipItems.length > 0"
      class="tk-type-chips"
    >
      <span
        class="tk-type-chip"
        :class="{ 'is-active': !activeType }"
        @click="handleTypeChipClick('')"
      >
        <span
          class="tk-type-chip__dot"
          style="background: var(--tk-text-secondary);"
        />
        <span class="tk-type-chip__label">{{ t('telemetry.monitor.templates.allTypes') }}</span>
        <span class="tk-type-chip__count">{{ allTemplates.length }}</span>
      </span>
      <span
        v-for="chip in typeChipItems"
        :key="chip.type"
        class="tk-type-chip"
        :class="{ 'is-active': activeType === chip.type }"
        @click="handleTypeChipClick(chip.type)"
      >
        <span
          class="tk-type-chip__dot"
          :style="{ background: chip.dot }"
        />
        <span class="tk-type-chip__label">{{ chip.label }}</span>
        <span class="tk-type-chip__count">{{ chip.count }}</span>
      </span>
    </div>

    <SearchForm
      v-model="searchModel"
      :fields="searchFields"
      :loading="loading"
      :show-collapse="false"
      @search="handleSearch"
      @reset="handleReset"
    />

    <DataTable
      table-id="monitor-templates"
      :data="pagedData"
      :columns="columns"
      :loading="loading"
      :total="total"
      :current="currentPage"
      :page-size="pageSize"
      :page-sizes="[10, 15, 20, 50]"
      row-key="id"
      @page-change="handlePageChange"
    >
      <template #executorType="{ row }">
        <span
          class="tk-type-badge"
          :class="`tk-type-badge--${row.executorType}`"
        >
          <span class="tk-type-badge__dot" />
          {{ t(`telemetry.monitor.type.${row.executorType}`, row.executorType?.toUpperCase()) }}
        </span>
      </template>
      <template #isBuiltin="{ row }">
        <el-tag :type="row.isBuiltin ? 'info' : 'success'" size="small">
          {{ row.isBuiltin ? t('telemetry.monitor.templates.builtinYes') : t('telemetry.monitor.templates.builtinNo') }}
        </el-tag>
      </template>
      <template #updatedAt="{ row }">
        {{ formatDate(row.updatedAt) }}
      </template>
      <template #action-column>
        <el-table-column
          :label="t('common.app.action')"
          width="140"
          fixed="right"
          align="center"
          :resizable="false"
        >
          <template #default="{ row }">
            <el-button
              type="primary"
              size="small"
              link
              @click="handleApply(row)"
            >
              {{ t('telemetry.monitor.templates.apply') }}
            </el-button>
            <el-button
              v-if="!row.isBuiltin && canDelete('*')"
              type="danger"
              size="small"
              link
              @click="handleDelete(row)"
            >
              {{ t('common.app.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </template>
    </DataTable>
  </div>
</template>

<style scoped lang="scss">
.tk-tpl-header {
  display: flex;
  flex-wrap: wrap;
  gap: var(--tk-spacing-md);
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: var(--tk-spacing-md);

  &__left {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: flex-start;
    min-width: 0;
  }

  &__back {
    flex-shrink: 0;
  }

  &__title-block {
    min-width: 0;
  }

  &__eyebrow {
    margin-bottom: 4px;
    font-family: var(--tk-font-mono, monospace);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.1em;
  }

  &__title-line {
    display: flex;
    flex-wrap: wrap;
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
    max-width: 680px;
    margin-top: 6px;
    font-size: var(--tk-font-size-sm);
    line-height: 1.5;
    color: var(--tk-text-secondary);
  }

  &__actions {
    display: flex;
    flex-shrink: 0;
    gap: var(--tk-spacing-xs);
    align-items: center;
  }
}

.tk-type-chips {
  display: flex;
  flex-wrap: wrap;
  gap: var(--tk-spacing-sm);
  margin-bottom: var(--tk-spacing-md);
}

.tk-type-chip {
  display: inline-flex;
  gap: var(--tk-spacing-xs);
  align-items: center;
  padding: var(--tk-spacing-xs) var(--tk-spacing-sm);
  font-size: var(--tk-font-size-xs);
  cursor: pointer;
  user-select: none;
  background: var(--tk-bg-color);
  border: 1px solid var(--tk-border-color-light);
  border-radius: var(--tk-border-radius-base);
  transition: border-color var(--tk-transition-fast), background-color var(--tk-transition-fast);

  &:hover {
    border-color: var(--tk-border-color);
  }

  &__dot {
    flex-shrink: 0;
    width: 8px;
    height: 8px;
    border-radius: 50%;
  }

  &__label {
    font-family: var(--tk-font-mono, monospace);
    font-size: 11px;
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  &__count {
    min-width: 14px;
    font-family: var(--tk-font-mono, monospace);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-bold);
    font-variant-numeric: tabular-nums;
    color: var(--tk-text-primary);
    text-align: right;
  }

  &.is-active {
    background: var(--tk-primary-color-light-9);
    border-color: var(--tk-primary-color);

    .tk-type-chip__label,
    .tk-type-chip__count {
      color: var(--tk-primary-color);
    }
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

  &--ssl {
    color: var(--tk-danger-color);
    background: var(--tk-danger-color-light-9);
    border-color: var(--tk-danger-color-light-7);
  }
}
</style>
