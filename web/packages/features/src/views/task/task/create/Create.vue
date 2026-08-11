// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import TaskForm from '../components/TaskForm.vue'
import type { TaskFormData, TaskCreateParams } from '../../../../types/task'
import { createTask } from '../../../../api/task'

const router = useRouter()
const { t } = useI18n()
const loading = ref(false)

const initialData = reactive<TaskFormData>({
  name: '',
  description: '',
  executor: 'local',
  schedule: '',
  config: {},
  group: '',
  tags: [],
  enabled: true,
  retryPolicy: 'fixed',
  concurrency: 0,
  scheduleType: 'cron',
  cronExpr: '',
  interval: 60,
})

function buildCreateParams(data: TaskFormData): TaskCreateParams {
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
    const task = await createTask(buildCreateParams(data))
    ElMessage.success(t('task.task.create.saveSuccess'))
    router.push(`/task/detail/${task.id}`)
  } catch {
    // Errors are handled centrally by the interceptor
  } finally {
    loading.value = false
  }
}

function handleCancel(): void {
  router.back()
}
</script>

<template>
  <div class="tk-page-container">
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
          {{ t('task.task.create.breadcrumbHint') }}
        </div>
        <h1 class="tk-task-form__title">
          {{ t('task.task.create.title') }}
        </h1>
      </div>
    </div>

    <TaskForm
      :initial-data="initialData"
      :loading="loading"
      @submit="handleSubmit"
      @cancel="handleCancel"
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
