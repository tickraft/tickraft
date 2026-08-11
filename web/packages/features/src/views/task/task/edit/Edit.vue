// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import { PageEmpty, useFormGuard } from '@tickraft/core'
import TaskForm from '../components/TaskForm.vue'
import type { TaskFormData, TaskUpdateParams, TaskModel, ScheduleType } from '../../../../types/task'
import { getTask, updateTask } from '../../../../api/task'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()

const loading = ref(false)
const pageLoading = ref(false)
const notFound = ref(false)

/** Tracks unsaved form changes for the route-leave guard */
const isDirty = ref(false)

useFormGuard({
  isDirty,
  message: t('common.unsavedChanges') || 'You have unsaved changes. Leave anyway?',
})
const initialData = ref<TaskFormData | undefined>(undefined)

const taskId = Number(route.params.id)

/**
 * Parse a Go time.Duration string (e.g. "30s", "5m", "1h30m") into seconds.
 * Returns 60 as default when parsing fails.
 */
function parseDurationToSeconds(schedule: string): number {
  if (!schedule) return 60
  // Match combined duration like "1h30m", "2h", "5m", "30s"
  const combined = schedule.match(/^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$/)
  if (combined) {
    const h = parseInt(combined[1] || '0', 10)
    const m = parseInt(combined[2] || '0', 10)
    const s = parseInt(combined[3] || '0', 10)
    if (h || m || s) return h * 3600 + m * 60 + s
  }
  return 60
}

/**
 * Detect schedule type from the backend schedule string.
 * - Empty string -> event (never fires on timer)
 * - Parseable as Go duration -> interval
 * - Everything else -> cron expression
 */
function detectScheduleType(schedule: string): { type: ScheduleType; cronExpr: string; interval: number } {
  if (!schedule) {
    return { type: 'event', cronExpr: '', interval: 60 }
  }
  // Check if it's a Go duration string (e.g. "30s", "5m", "1h30m")
  const durationMatch = schedule.match(/^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$/)
  if (durationMatch && (durationMatch[1] || durationMatch[2] || durationMatch[3])) {
    return { type: 'interval', cronExpr: '', interval: parseDurationToSeconds(schedule) }
  }
  return { type: 'cron', cronExpr: schedule, interval: 60 }
}

function buildFormData(task: TaskModel): TaskFormData {
  const { type, cronExpr, interval } = detectScheduleType(task.schedule)
  return {
    name: task.name,
    description: task.description ?? '',
    executor: task.executor as TaskFormData['executor'],
    schedule: task.schedule,
    config: task.config ?? {},
    group: task.group ?? '',
    tags: task.tags ?? [],
    enabled: task.enabled,
    retryPolicy: task.retryPolicy ?? 'fixed',
    concurrency: task.concurrency ?? 0,
    scheduleType: type,
    cronExpr,
    interval,
  }
}

function buildUpdateParams(data: TaskFormData): TaskUpdateParams {
  return {
    name: data.name,
    description: data.description || undefined,
    executor: data.executor,
    schedule: data.schedule,
    enabled: data.enabled,
    config: data.config,
    group: data.group || undefined,
    tags: data.tags.length > 0 ? data.tags : undefined,
    retryPolicy: data.retryPolicy || undefined,
    concurrency: data.concurrency || undefined,
  }
}

async function handleSubmit(data: TaskFormData): Promise<void> {
  loading.value = true
  try {
    await updateTask(taskId, buildUpdateParams(data))
    isDirty.value = false
    ElMessage.success(t('task.task.create.updateSuccess'))
    router.push(`/task/detail/${taskId}`)
  } catch {
    // Errors are handled centrally by the interceptor
  } finally {
    loading.value = false
  }
}

function handleCancel(): void {
  router.back()
}

async function loadTask(): Promise<void> {
  pageLoading.value = true
  try {
    const task = await getTask(taskId)
    initialData.value = buildFormData(task)
  } catch {
    notFound.value = true
  } finally {
    pageLoading.value = false
  }
}

onMounted(() => {
  void loadTask()
})
</script>

<template>
  <div
    v-loading="pageLoading"
    class="tk-page-container"
  >
    <!-- Header: back button + breadcrumb + title -->
    <div class="tk-task-form__header">
      <button
        class="tk-task-form__back"
        :title="t('task.task.create.backToList')"
        @click="handleCancel"
      >
        <el-icon :size="16">
          <ArrowLeft />
        </el-icon>
      </button>
      <div class="tk-task-form__title-block">
        <div class="tk-task-form__breadcrumb">
          {{ t('task.task.create.breadcrumbHintEdit', { id: taskId }) }}
        </div>
        <h1 class="tk-task-form__title">
          {{ t('task.task.create.editTitle') }}
        </h1>
      </div>
    </div>

    <template v-if="notFound">
      <PageEmpty :description="t('task.task.create.notFound', { id: taskId })">
        <el-button
          type="primary"
          @click="router.push('/task/list')"
        >
          {{ t('task.task.create.backToList') }}
        </el-button>
      </PageEmpty>
    </template>
    <TaskForm
      v-else-if="initialData"
      :initial-data="initialData"
      :is-edit="true"
      :loading="loading"
      @submit="handleSubmit"
      @cancel="handleCancel"
      @change="isDirty = true"
    />
  </div>
</template>

<style scoped lang="scss">
.tk-task-form {
  &__header {
    display: flex;
    gap: var(--tk-spacing-5);
    align-items: center;
    margin-bottom: var(--tk-spacing-8);
  }

  &__back {
    display: inline-flex;
    flex-shrink: 0;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    color: var(--tk-text-secondary);
    cursor: pointer;
    background: var(--tk-bg-surface);
    border: 1px solid var(--tk-border-color-base);
    border-radius: var(--tk-radius-md);
    transition: all var(--tk-duration-fast) var(--tk-ease-out);

    &:hover {
      color: var(--tk-primary-color);
      background: var(--tk-primary-color-bg);
      border-color: var(--tk-primary-color-border);
    }
  }

  &__title-block {
    min-width: 0;
  }

  &__breadcrumb {
    margin-bottom: 2px;
    font-family: var(--tk-font-mono);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: var(--tk-letter-widest);
  }

  &__title {
    margin: 0;
    font-family: var(--tk-font-display);
    font-size: var(--tk-font-size-2xl);
    font-weight: var(--tk-font-weight-bold);
    line-height: 1.1;
    color: var(--tk-text-primary);
    letter-spacing: var(--tk-letter-tight);
  }
}
</style>
