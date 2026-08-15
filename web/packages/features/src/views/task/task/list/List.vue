// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { SearchForm, DataTable } from '@tickraft/core'
import { formatDate } from '@tickraft/core'
import type { TaskModel } from '../../../../types/task'
import { getTasks, getExecutionStats, triggerTask, deleteTask, pauseTask, resumeTask, copyTask } from '../../../../api/task'

const router = useRouter()
const { t } = useI18n()

const loading = ref(false)
const tableData = ref<TaskModel[]>([])
const total = ref(0)
const currentPage = ref(1)
const pageSize = ref(10)

const summaryStats = reactive({
  success: 0,
  failed: 0,
  running: 0,
})

const searchValues = reactive<Record<string, unknown>>({
  group: '',
  tags: '',
})

const searchFields = computed(() => [
  {
    prop: 'group',
    label: t('task.task.create.group'),
    type: 'input' as const,
    placeholder: t('task.task.create.groupPlaceholder'),
  },
  {
    prop: 'tags',
    label: t('task.task.list.tags'),
    type: 'input' as const,
    placeholder: t('task.task.list.tagsPlaceholder'),
  },
])

const columns = computed(() => [
  { prop: 'id', label: t('task.task.list.id'), width: 80, slot: 'id' },
  { prop: 'name', label: t('task.task.list.name'), minWidth: 180, slot: 'name', align: 'left' as const },
  { prop: 'executor', label: t('task.task.list.executorType'), width: 130, slot: 'executor' },
  { prop: 'schedule', label: t('task.task.list.scheduleExpr'), width: 200, slot: 'schedule' },
  { prop: 'group', label: t('task.task.create.group'), width: 120, slot: 'group' },
  { prop: 'enabled', label: t('task.task.list.enabled'), width: 80, slot: 'enabled' },
  { prop: 'createdAt', label: t('task.task.detail.createdAt'), width: 170, slot: 'createdAt' },
])

const EXECUTOR_LABELS = computed<Record<string, string>>(() => ({
  http: 'HTTP',
  tcp: 'TCP',
  icmp: 'ICMP',
  local: t('task.task.create.executorLocal'),
  webhook: 'Webhook',
}))

const countText = computed(() => `${total.value} ${t('task.task.list.title').toUpperCase().includes('TASK') ? 'TASKS' : t('task.task.list.title')}`)

/** Format the schedule string for display */
function formatSchedule(row: TaskModel): string {
  return row.schedule || '-'
}

async function fetchData() {
  loading.value = true
  try {
    const [result, stats] = await Promise.all([
      getTasks({
        page: currentPage.value,
        pageSize: pageSize.value,
        group: (searchValues.group as string) || undefined,
        tags: (searchValues.tags as string) || undefined,
      }),
      getExecutionStats().catch(() => null),
    ])
    tableData.value = result.items
    total.value = result.total

    // Populate summary from real execution stats (fallback to zeros on error)
    summaryStats.success = stats?.successCount ?? 0
    summaryStats.failed = stats?.failureCount ?? 0
    // ExecutionStats does not expose "running"; approximate as total minus
    // success minus failure when available, else 0.
    if (stats) {
      const running = Math.max(0, stats.totalExecutions - stats.successCount - stats.failureCount)
      summaryStats.running = running
    } else {
      summaryStats.running = 0
    }
  } catch {
    ElMessage.error(t('common.app.loadFailed'))
  } finally {
    loading.value = false
  }
}

function handleSearch(values: Record<string, unknown>) {
  Object.assign(searchValues, values)
  currentPage.value = 1
  void fetchData()
}

function handleReset() {
  searchValues.group = ''
  searchValues.tags = ''
  currentPage.value = 1
  void fetchData()
}

function handlePageChange({ current, pageSize: size }: { current: number; pageSize: number }) {
  currentPage.value = current
  pageSize.value = size
  void fetchData()
}

function handleCreate() {
  router.push('/task/create')
}

function handleDetail(row: TaskModel) {
  router.push(`/task/detail/${row.id}`)
}

function handleEdit(row: TaskModel) {
  router.push(`/task/edit/${row.id}`)
}

async function handleTrigger(row: TaskModel) {
  try {
    await ElMessageBox.confirm(
      t('task.task.list.triggerConfirm', { name: row.name }),
      t('task.task.list.trigger'),
      { type: 'warning', confirmButtonText: t('task.task.list.trigger') },
    )
    await triggerTask(row.id)
    ElMessage.success(t('task.task.list.triggerSuccess', { name: row.name }))
  } catch {
    // user cancelled or trigger failed
  }
}

async function handleDelete(row: TaskModel) {
  try {
    await ElMessageBox.confirm(
      t('task.task.list.deleteConfirm', { name: row.name }),
      t('task.task.list.delete'),
      { type: 'error', confirmButtonText: t('task.task.list.delete') },
    )
    await deleteTask(row.id)
    ElMessage.success(t('task.task.list.deleteSuccess'))
    if (tableData.value.length === 1 && currentPage.value > 1) {
      currentPage.value--
    }
    await fetchData()
  } catch {
    // user cancelled or delete failed
  }
}

async function handleToggleEnabled(row: TaskModel) {
  try {
    if (row.enabled) {
      await pauseTask(row.id)
      row.enabled = false
      ElMessage.success(t('task.task.list.pauseSuccess', { name: row.name }))
    } else {
      await resumeTask(row.id)
      row.enabled = true
      ElMessage.success(t('task.task.list.resumeSuccess', { name: row.name }))
    }
  } catch {
    ElMessage.error(t('common.app.failed'))
  }
}

async function handleCopy(row: TaskModel) {
  try {
    await ElMessageBox.confirm(
      t('task.task.list.copyConfirm', { name: row.name }),
      t('task.task.list.copy'),
      { type: 'info', confirmButtonText: t('task.task.list.copy') },
    )
    const copied = await copyTask(row.id)
    ElMessage.success(t('task.task.list.copySuccess', { name: copied.name }))
    await fetchData()
  } catch {
    // user cancelled or copy failed
  }
}

onMounted(() => {
  void fetchData()
})
</script>

<template>
  <div class="tk-task-list tk-page-container">
    <!-- Page header with title + count badge + subtitle -->
    <div class="tk-task-list__header">
      <div>
        <div class="tk-task-list__title-row">
          <h1 class="tk-task-list__title">
            {{ t('task.task.list.title') }}
          </h1>
          <span class="tk-task-list__count">{{ countText }}</span>
        </div>
        <p class="tk-task-list__subtitle">
          {{ t('task.task.list.subtitle') }}
        </p>
      </div>
      <div class="tk-task-list__actions">
        <el-button @click="fetchData">
          <el-icon><Refresh /></el-icon>
          {{ t('common.app.refresh') }}
        </el-button>
        <el-button
          type="primary"
          @click="handleCreate"
        >
          <el-icon><Plus /></el-icon>
          {{ t('task.task.list.create') }}
        </el-button>
      </div>
    </div>

    <!-- Summary stats strip -->
    <div class="tk-task-list__summary">
      <div class="tk-task-list__summary-item">
        <span class="tk-task-list__summary-dot tk-task-list__summary-dot--ok" />
        <span>{{ t('task.task.list.summarySuccess') }} <strong>{{ summaryStats.success }}</strong></span>
      </div>
      <div class="tk-task-list__summary-item">
        <span class="tk-task-list__summary-dot tk-task-list__summary-dot--fail" />
        <span>{{ t('task.task.list.summaryFailed') }} <strong>{{ summaryStats.failed }}</strong></span>
      </div>
      <div class="tk-task-list__summary-item">
        <span class="tk-task-list__summary-dot tk-task-list__summary-dot--run" />
        <span>{{ t('task.task.list.summaryRunning') }} <strong>{{ summaryStats.running }}</strong></span>
      </div>
    </div>

    <!-- Filter bar -->
    <div class="tk-search-area">
      <SearchForm
        :fields="searchFields"
        :model-value="searchValues"
        :loading="loading"
        :show-collapse="false"
        @search="handleSearch"
        @reset="handleReset"
      />
    </div>

    <!-- Table -->
    <div class="tk-table-area">
      <DataTable
        table-id="task-list"
        :data="tableData"
        :columns="columns"
        :loading="loading"
        :total="total"
        :current="currentPage"
        :page-size="pageSize"
        row-key="id"
        @page-change="handlePageChange"
      >
        <template #id="{ row }">
          <span class="tk-task-list__task-id">#{{ (row as TaskModel).id }}</span>
        </template>

        <template #name="{ row }">
          <el-button
            link
            type="primary"
            @click="handleDetail(row as TaskModel)"
          >
            {{ (row as TaskModel).name }}
          </el-button>
        </template>

        <template #executor="{ row }">
          <span
            class="tk-executor-badge"
            :class="`tk-executor-badge--${(row as TaskModel).executor}`"
          >
            <span class="tk-executor-badge__dot" />
            {{ EXECUTOR_LABELS[(row as TaskModel).executor] || (row as TaskModel).executor }}
          </span>
        </template>

        <template #schedule="{ row }">
          <span class="tk-schedule-cell__expr">{{ formatSchedule(row as TaskModel) }}</span>
        </template>

        <template #group="{ row }">
          <span class="tk-task-list__group">{{ (row as TaskModel).group || '—' }}</span>
        </template>

        <template #enabled="{ row }">
          <el-switch
            :model-value="(row as TaskModel).enabled"
            @change="handleToggleEnabled(row as TaskModel)"
          />
        </template>

        <template #createdAt="{ row }">
          <span class="tk-mono-text">{{ formatDate((row as TaskModel).createdAt) }}</span>
        </template>

        <template #action-column>
          <el-table-column
            :label="t('task.task.list.action')"
            width="130"
            fixed="right"
            align="center"
            :resizable="false"
          >
            <template #default="{ row }">
              <div class="tk-task-list__row-actions">
                <el-button
                  link
                  type="primary"
                  :title="t('task.task.list.edit')"
                  @click="handleEdit(row as TaskModel)"
                >
                  <el-icon><Edit /></el-icon>
                </el-button>
                <el-button
                  link
                  type="success"
                  :title="t('task.task.list.trigger')"
                  @click="handleTrigger(row as TaskModel)"
                >
                  <el-icon><Lightning /></el-icon>
                </el-button>
                <el-dropdown trigger="hover" @command="(cmd: string) => {
                  if (cmd === 'copy') handleCopy(row as TaskModel)
                  else if (cmd === 'delete') handleDelete(row as TaskModel)
                }">
                  <el-button link type="info" class="tk-task-list__more-btn">
                    <el-icon><MoreFilled /></el-icon>
                  </el-button>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item command="copy">
                        <el-icon><CopyDocument /></el-icon>
                        {{ t('task.task.list.copy') }}
                      </el-dropdown-item>
                      <el-dropdown-item command="delete" divided>
                        <el-icon><Delete /></el-icon>
                        {{ t('task.task.list.delete') }}
                      </el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
              </div>
            </template>
          </el-table-column>
        </template>
      </DataTable>
    </div>
  </div>
</template>

<script lang="ts">
import { Refresh, Plus, Edit, Delete, Lightning, CopyDocument, MoreFilled } from '@element-plus/icons-vue'
export default { name: 'TaskList' }
</script>

<style scoped lang="scss">
.tk-task-list {
  &__header {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-lg);
    align-items: flex-start;
    justify-content: space-between;
    margin-bottom: var(--tk-spacing-md);
  }

  &__title-row {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: baseline;
  }

  &__title {
    font-size: var(--tk-font-size-2xl);
    font-weight: var(--tk-font-weight-semibold);
    line-height: 1.1;
    color: var(--tk-text-primary);
  }

  &__count {
    display: inline-flex;
    align-items: center;
    padding: 2px var(--tk-spacing-sm);
    font-family: var(--tk-font-family-mono, monospace);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-primary-color);
    letter-spacing: 0.05em;
    background-color: var(--tk-primary-color-light-9);
    border: 1px solid var(--tk-primary-color-light-7);
    border-radius: var(--tk-border-radius-round);
  }

  &__subtitle {
    max-width: 540px;
    margin-top: var(--tk-spacing-xs);
    font-size: var(--tk-font-size-sm);
    line-height: 1.5;
    color: var(--tk-text-secondary);
  }

  &__actions {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
  }

  &__summary {
    display: flex;
    gap: var(--tk-spacing-lg);
    align-items: center;
    padding: var(--tk-spacing-sm) var(--tk-spacing-md);
    margin-bottom: var(--tk-spacing-md);
    background-color: var(--tk-bg-color);
    border-radius: var(--tk-border-radius-md);
    box-shadow: var(--tk-shadow-sm);
  }

  &__summary-item {
    display: inline-flex;
    gap: var(--tk-spacing-xs);
    align-items: center;
    font-family: var(--tk-font-family-mono, monospace);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);

    strong {
      font-weight: var(--tk-font-weight-semibold);
      color: var(--tk-text-primary);
    }
  }

  &__summary-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;

    &--ok { background-color: var(--tk-success-color); }
    &--fail { background-color: var(--tk-danger-color); }
    &--run { background-color: var(--tk-primary-color); }
  }

  &__task-id {
    font-family: var(--tk-font-family-mono, monospace);
    font-size: var(--tk-font-size-xs);
    font-variant-numeric: tabular-nums;
    color: var(--tk-text-secondary);
  }

  &__row-actions {
    display: flex;
    flex-wrap: nowrap;
    gap: 2px;
    align-items: center;
  }

  &__more-btn {
    padding: 0 4px;
  }

  &__group {
    font-size: var(--tk-font-size-sm);
    color: var(--tk-text-regular);
  }
}

.tk-executor-badge {
  display: inline-flex;
  gap: var(--tk-spacing-xs);
  align-items: center;
  padding: 2px var(--tk-spacing-sm);
  font-family: var(--tk-font-family-mono, monospace);
  font-size: var(--tk-font-size-xs);
  font-weight: var(--tk-font-weight-semibold);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  white-space: nowrap;
  border: 1px solid transparent;
  border-radius: var(--tk-border-radius-sm);

  &__dot {
    flex-shrink: 0;
    width: 5px;
    height: 5px;
    background-color: currentcolor;
    border-radius: 50%;
  }

  &--http { color: #2563eb; background-color: rgb(37 99 235 / 10%); border-color: rgb(37 99 235 / 25%); }
  &--tcp { color: #0891b2; background-color: rgb(8 145 178 / 10%); border-color: rgb(8 145 178 / 25%); }
  &--icmp { color: #7c3aed; background-color: rgb(124 58 237 / 10%); border-color: rgb(124 58 237 / 25%); }
  &--local { color: #475569; background-color: rgb(71 85 105 / 10%); border-color: rgb(71 85 105 / 25%); }
  &--ssh { color: #b45309; background-color: rgb(180 83 9 / 10%); border-color: rgb(180 83 9 / 25%); }
  &--mysql { color: #15803d; background-color: rgb(21 128 61 / 10%); border-color: rgb(21 128 61 / 25%); }
  &--redis { color: #dc2626; background-color: rgb(220 38 38 / 10%); border-color: rgb(220 38 38 / 25%); }
  &--webhook { color: #be185d; background-color: rgb(190 24 93 / 10%); border-color: rgb(190 24 93 / 25%); }
}

.tk-schedule-cell {
  &__expr {
    display: inline-block;
    max-width: 180px;
    padding: 1px var(--tk-spacing-xs);
    overflow: hidden;
    text-overflow: ellipsis;
    font-family: var(--tk-font-family-mono, monospace);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-primary);
    white-space: nowrap;
    background-color: var(--tk-fill-color-light);
    border-radius: var(--tk-border-radius-sm);
  }
}

.tk-mono-text {
  font-family: var(--tk-font-family-mono, monospace);
  font-size: var(--tk-font-size-xs);
  color: var(--tk-text-secondary);
}
</style>
