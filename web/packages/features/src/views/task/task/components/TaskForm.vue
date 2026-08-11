// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { isValidCron, isValidUrl } from '@tickraft/core'
import type { TaskFormData, ExecutorType, ScheduleType, RetryPolicy } from '../../../../types/task'

interface TaskFormProps {
  initialData?: TaskFormData
  isEdit?: boolean
  loading?: boolean
}

interface TaskFormEmits {
  (e: 'submit', data: TaskFormData): void
  (e: 'cancel'): void
  (e: 'change'): void
}

const props = withDefaults(defineProps<TaskFormProps>(), {
  initialData: undefined,
  isEdit: false,
  loading: false,
})

const emit = defineEmits<TaskFormEmits>()
const { t } = useI18n()

const advancedOpen = ref(false)

const form = reactive<TaskFormData>(props.initialData ?? {
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

watch(form, () => emit('change'), { deep: true })

watch(() => props.initialData, (val) => {
  if (val) Object.assign(form, val)
}, { deep: true })

const scheduleTypes = computed<Array<{ value: ScheduleType; label: string; num: string }>>(() => [
  { value: 'interval', label: t('task.task.create.interval'), num: '01' },
  { value: 'cron', label: t('task.task.create.cron'), num: '02' },
  { value: 'event', label: t('task.task.create.event'), num: '03' },
])

const cronPresets = computed(() => [
  { value: '* * * * *', label: t('task.task.create.cronPresetMinute') },
  { value: '*/5 * * * *', label: t('task.task.create.cronPreset5Minute') },
  { value: '0 * * * *', label: t('task.task.create.cronPresetHour') },
  { value: '0 0 * * *', label: t('task.task.create.cronPresetDay') },
  { value: '0 0 * * 0', label: t('task.task.create.cronPresetWeek') },
])

interface ExecutorCard {
  value: ExecutorType
  label: string
  desc: string
}

/** CE-supported executor options (pro executors SSH/MySQL/Redis are filtered out, not shown as locked) */
const executorCards = computed<ExecutorCard[]>(() => [
  { value: 'http', label: t('task.task.create.executorHttp'), desc: t('task.task.create.executorHttpDesc') },
  { value: 'tcp', label: t('task.task.create.executorTcp'), desc: t('task.task.create.executorTcpDesc') },
  { value: 'icmp', label: t('task.task.create.executorIcmp'), desc: t('task.task.create.executorIcmpDesc') },
  { value: 'local', label: t('task.task.create.executorLocal'), desc: t('task.task.create.executorLocalDesc') },
  { value: 'webhook', label: t('task.task.create.executorWebhook'), desc: t('task.task.create.executorWebhookDesc') },
])

const retryPolicies = computed<Array<{ value: RetryPolicy; label: string }>>(() => [
  { value: 'fixed', label: t('task.task.create.retryPolicyFixed') },
  { value: 'exponential', label: t('task.task.create.retryPolicyExponential') },
])

const cronPreviewText = computed(() => {
  const expr = form.cronExpr.trim()
  if (!expr) return t('task.task.create.cronPreview')
  const presets: Record<string, string> = {
    '* * * * *': t('task.task.create.cronPresetMinute'),
    '*/5 * * * *': t('task.task.create.cronPreset5Minute'),
    '0 * * * *': t('task.task.create.cronPresetHour'),
    '0 0 * * *': t('task.task.create.cronPresetDay'),
    '0 0 * * 0': t('task.task.create.cronPresetWeek'),
  }
  if (presets[expr]) return presets[expr]
  return t('task.task.create.cronPreview') + ': ' + expr
})

const previewSchedule = computed(() => {
  switch (form.scheduleType) {
    case 'interval': return `${form.interval}s`
    case 'cron': return form.cronExpr || '-'
    case 'event': return t('task.task.create.event')
    default: return '-'
  }
})

const previewExecutorLabel = computed(() => {
  const card = executorCards.value.find((c) => c.value === form.executor)
  return card?.label ?? form.executor
})

/** Tag input state for the el-select multiple tag input */
const tagInput = ref('')

function handleTagAdd(): void {
  const val = tagInput.value.trim()
  if (val && !form.tags.includes(val)) {
    form.tags.push(val)
  }
  tagInput.value = ''
}

function handleTagRemove(tag: string): void {
  form.tags = form.tags.filter((t) => t !== tag)
}

function setCronPreset(value: string) {
  form.cronExpr = value
}

function toggleAdvanced() {
  advancedOpen.value = !advancedOpen.value
}

function selectExecutor(type: ExecutorType) {
  form.executor = type
}

function buildExecutorConfig(): Record<string, unknown> {
  const cfg = form.config
  switch (form.executor) {
    case 'http':
      return {
        url: cfg.url ?? '',
        method: cfg.method ?? 'GET',
        headers: cfg.headers ?? '',
        timeout: cfg.timeout ?? 10,
      }
    case 'tcp':
      return { host: cfg.host ?? '', port: cfg.port ?? 0, timeout: cfg.timeout ?? 5 }
    case 'icmp':
      return { host: cfg.host ?? '', count: cfg.count ?? 4, timeout: cfg.timeout ?? 3 }
    case 'local':
      return {
        interpreter: cfg.interpreter ?? 'bash',
        source: cfg.source ?? '',
        timeout: cfg.timeout ?? 60,
      }
    case 'webhook':
      return {
        url: cfg.url ?? '',
        method: cfg.method ?? 'POST',
        headers: cfg.headers ?? '',
      }
    default:
      return { ...cfg }
  }
}

/** Compute the schedule string from form UI state */
function computeSchedule(): string {
  switch (form.scheduleType) {
    case 'cron':
      return form.cronExpr.trim()
    case 'interval':
      return `${form.interval}s`
    case 'event':
      return ''
    default:
      return ''
  }
}

function validate(): boolean {
  if (!form.name.trim()) {
    ElMessage.warning(t('task.task.list.name'))
    return false
  }
  if (form.scheduleType === 'cron' && !isValidCron(form.cronExpr)) {
    ElMessage.warning(t('task.task.create.cronExpr'))
    return false
  }
  if (form.scheduleType === 'interval' && (!form.interval || form.interval < 1)) {
    ElMessage.warning(t('task.task.create.intervalLabel'))
    return false
  }
  const cfg = form.config
  if (form.executor === 'http' && !isValidUrl(String(cfg.url ?? ''))) {
    ElMessage.warning(t('task.task.create.httpUrl'))
    return false
  }
  if (form.executor === 'tcp' && !String(cfg.host ?? '').trim()) {
    ElMessage.warning(t('task.task.create.tcpHost'))
    return false
  }
  if (form.executor === 'icmp' && !String(cfg.host ?? '').trim()) {
    ElMessage.warning(t('task.task.create.icmpHost'))
    return false
  }
  if (form.executor === 'local' && !String(cfg.source ?? '').trim()) {
    ElMessage.warning(t('task.task.create.localSource'))
    return false
  }
  if (form.executor === 'webhook' && !isValidUrl(String(cfg.url ?? ''))) {
    ElMessage.warning(t('task.task.create.webhookUrl'))
    return false
  }
  return true
}

function handleSubmit() {
  if (!validate()) return
  const data: TaskFormData = {
    ...form,
    config: buildExecutorConfig(),
    schedule: computeSchedule(),
    tags: [...form.tags],
  }
  emit('submit', data)
}

function handleCancel() {
  emit('cancel')
}
</script>

<template>
  <el-form
    :model="form"
    label-position="top"
    class="tk-task-form"
  >
    <div class="tk-task-form__grid">
      <!-- Main column -->
      <div class="tk-task-form__main">
        <!-- Section: Basic Info (01) -->
        <div class="tk-task-form-section">
          <div class="tk-task-form-section__header">
            <div class="tk-task-form-section__title">
              <span class="tk-task-form-section__index">01</span>
              <span>{{ t('task.task.create.basicInfo') }}</span>
            </div>
            <span class="tk-task-form-section__hint">{{ t('task.task.create.sectionHintRequired') }}</span>
          </div>
          <div class="tk-task-form-section__body">
            <el-form-item :label="t('task.task.list.name')" required>
              <el-input
                v-model="form.name"
                :placeholder="t('task.task.list.namePlaceholder')"
                maxlength="64"
              />
            </el-form-item>
            <el-form-item :label="t('task.task.create.description')">
              <el-input
                v-model="form.description"
                type="textarea"
                :rows="2"
                :placeholder="t('task.task.create.descriptionPlaceholder')"
                maxlength="256"
              />
            </el-form-item>
            <el-form-item :label="t('task.task.create.executorType')" required>
              <div class="tk-executor-grid">
                <button
                  v-for="card in executorCards"
                  :key="card.value"
                  type="button"
                  class="tk-executor-card"
                  :class="{ 'tk-executor-card--active': form.executor === card.value }"
                  @click="selectExecutor(card.value)"
                >
                  <span class="tk-executor-card__label">{{ card.label }}</span>
                  <span class="tk-executor-card__desc">{{ card.desc }}</span>
                  <span v-if="form.executor === card.value" class="tk-executor-card__check">&#10003;</span>
                </button>
              </div>
              <div class="tk-task-form-item__help">{{ t('task.task.create.executorHelp') }}</div>
            </el-form-item>
          </div>
        </div>

        <!-- Section: Schedule Config (02) -->
        <div class="tk-task-form-section">
          <div class="tk-task-form-section__header">
            <div class="tk-task-form-section__title">
              <span class="tk-task-form-section__index">02</span>
              <span>{{ t('task.task.create.scheduleConfig') }}</span>
            </div>
            <span class="tk-task-form-section__hint">{{ t('task.task.create.sectionHintSchedule') }}</span>
          </div>
          <div class="tk-task-form-section__body">
            <el-form-item :label="t('task.task.create.scheduleType')" required>
              <div class="tk-schedule-tabs">
                <button
                  v-for="item in scheduleTypes"
                  :key="item.value"
                  type="button"
                  class="tk-schedule-tab"
                  :class="{ 'tk-schedule-tab--active': form.scheduleType === item.value }"
                  @click="form.scheduleType = item.value"
                >
                  <span class="tk-schedule-tab__num">{{ item.num }}</span>
                  <span>{{ item.label }}</span>
                </button>
              </div>
            </el-form-item>

            <template v-if="form.scheduleType === 'cron'">
              <el-form-item :label="t('task.task.create.cronExpr')" required>
                <el-input
                  v-model="form.cronExpr"
                  :placeholder="t('task.task.create.cronExprPlaceholder')"
                />
                <div class="tk-cron-presets">
                  <span
                    v-for="preset in cronPresets"
                    :key="preset.value"
                    class="tk-cron-preset"
                    @click="setCronPreset(preset.value)"
                  >
                    {{ preset.label }}
                  </span>
                </div>
                <div class="tk-cron-preview">
                  <span class="tk-cron-preview__label">{{ t('task.task.create.cronPreview') }}</span>
                  <span class="tk-cron-preview__value">{{ cronPreviewText }}</span>
                </div>
              </el-form-item>
            </template>

            <template v-else-if="form.scheduleType === 'interval'">
              <el-form-item :label="t('task.task.create.intervalLabel')" required>
                <el-input-number
                  v-model="form.interval"
                  :min="1"
                  :max="86400"
                  :placeholder="t('task.task.create.intervalPlaceholder')"
                />
              </el-form-item>
            </template>

            <template v-else-if="form.scheduleType === 'event'">
              <div class="tk-task-form-item__help">{{ t('task.task.create.eventDescPlaceholder') }}</div>
            </template>
          </div>
        </div>

        <!-- Section: Executor Config (03) -->
        <div class="tk-task-form-section">
          <div class="tk-task-form-section__header">
            <div class="tk-task-form-section__title">
              <span class="tk-task-form-section__index">03</span>
              <span>{{ t('task.task.create.executorConfig') }}</span>
            </div>
            <span class="tk-task-form-section__hint">{{ t('task.task.create.sectionHintExecution') }}</span>
          </div>
          <div class="tk-task-form-section__body">
            <!-- HTTP -->
            <template v-if="form.executor === 'http'">
              <el-row :gutter="16">
                <el-col :span="8">
                  <el-form-item :label="t('task.task.create.httpMethod')">
                    <el-select v-model="form.config.method" style="width: 100%">
                      <el-option label="GET" value="GET" />
                      <el-option label="POST" value="POST" />
                      <el-option label="PUT" value="PUT" />
                      <el-option label="DELETE" value="DELETE" />
                    </el-select>
                  </el-form-item>
                </el-col>
                <el-col :span="16">
                  <el-form-item :label="t('task.task.create.httpUrl')" required>
                    <el-input
                      v-model="form.config.url"
                      :placeholder="t('task.task.create.httpUrlPlaceholder')"
                    />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item :label="t('task.task.create.httpHeaders')">
                <el-input
                  v-model="form.config.headers"
                  type="textarea"
                  :rows="3"
                  placeholder="Content-Type: application/json"
                />
              </el-form-item>
            </template>

            <!-- TCP -->
            <template v-else-if="form.executor === 'tcp'">
              <el-row :gutter="16">
                <el-col :span="12">
                  <el-form-item :label="t('task.task.create.tcpHost')" required>
                    <el-input
                      v-model="form.config.host"
                      :placeholder="t('task.task.create.tcpHostPlaceholder')"
                    />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item :label="t('task.task.create.tcpPort')" required>
                    <el-input-number
                      v-model="form.config.port"
                      :min="1"
                      :max="65535"
                      style="width: 100%"
                    />
                  </el-form-item>
                </el-col>
              </el-row>
            </template>

            <!-- ICMP -->
            <template v-else-if="form.executor === 'icmp'">
              <el-form-item :label="t('task.task.create.icmpHost')" required>
                <el-input
                  v-model="form.config.host"
                  :placeholder="t('task.task.create.tcpHostPlaceholder')"
                />
              </el-form-item>
            </template>

            <!-- Local -->
            <template v-else-if="form.executor === 'local'">
              <el-row :gutter="16">
                <el-col :span="12">
                  <el-form-item :label="t('task.task.create.localInterpreter')">
                    <el-select v-model="form.config.interpreter" style="width: 100%">
                      <el-option label="bash" value="bash" />
                      <el-option label="python" value="python" />
                      <el-option label="node" value="node" />
                    </el-select>
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item :label="t('task.task.create.localSource')" required>
                <el-input
                  v-model="form.config.source"
                  type="textarea"
                  :rows="6"
                  :placeholder="t('task.task.create.localSourcePlaceholder')"
                />
              </el-form-item>
            </template>

            <!-- Webhook -->
            <template v-else-if="form.executor === 'webhook'">
              <el-row :gutter="16">
                <el-col :span="8">
                  <el-form-item :label="t('task.task.create.webhookMethod')">
                    <el-select v-model="form.config.method" style="width: 100%">
                      <el-option label="POST" value="POST" />
                      <el-option label="GET" value="GET" />
                      <el-option label="PUT" value="PUT" />
                    </el-select>
                  </el-form-item>
                </el-col>
                <el-col :span="16">
                  <el-form-item :label="t('task.task.create.webhookUrl')" required>
                    <el-input
                      v-model="form.config.url"
                      :placeholder="t('task.task.create.webhookUrlPlaceholder')"
                    />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item :label="t('task.task.create.webhookHeaders')">
                <el-input
                  v-model="form.config.headers"
                  type="textarea"
                  :rows="3"
                  placeholder="Authorization: Bearer xxx"
                />
              </el-form-item>
            </template>
          </div>
        </div>

        <!-- Section: Advanced Config (04) — collapsible -->
        <div
          class="tk-task-form-section"
          :class="{ 'tk-task-form-section--open': advancedOpen }"
        >
          <div class="tk-task-form-section__header">
            <div class="tk-task-form-section__title">
              <span class="tk-task-form-section__index">04</span>
              <span>{{ t('task.task.create.advancedConfig') }}</span>
            </div>
            <button
              type="button"
              class="tk-task-form-section__toggle"
              @click="toggleAdvanced"
            >
              <span>{{ advancedOpen ? t('task.task.create.advancedCollapse') : t('task.task.create.advancedToggle') }}</span>
            </button>
          </div>
          <div v-show="advancedOpen" class="tk-task-form-section__body">
            <el-row :gutter="16">
              <el-col :span="8">
                <el-form-item :label="t('task.task.create.group')">
                  <el-input
                    v-model="form.group"
                    :placeholder="t('task.task.create.groupPlaceholder')"
                  />
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item :label="t('task.task.create.retryPolicy')">
                  <el-select v-model="form.retryPolicy" style="width: 100%">
                    <el-option
                      v-for="item in retryPolicies"
                      :key="item.value"
                      :label="item.label"
                      :value="item.value"
                    />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item :label="t('task.task.create.concurrency')">
                  <el-input-number
                    v-model="form.concurrency"
                    :min="0"
                    :max="100"
                    style="width: 100%"
                  />
                  <div class="tk-task-form-item__help">{{ t('task.task.create.concurrencyHint') }}</div>
                </el-form-item>
              </el-col>
            </el-row>
            <el-form-item :label="t('task.task.create.tags')">
              <div class="tk-tag-input">
                <div class="tk-tag-input__tags">
                  <el-tag
                    v-for="tag in form.tags"
                    :key="tag"
                    closable
                    size="small"
                    @close="handleTagRemove(tag)"
                  >
                    {{ tag }}
                  </el-tag>
                </div>
                <el-input
                  v-model="tagInput"
                  :placeholder="t('task.task.create.tagsPlaceholder')"
                  size="small"
                  @keyup.enter="handleTagAdd"
                  @blur="handleTagAdd"
                />
              </div>
            </el-form-item>
            <el-form-item :label="t('task.task.create.enabled')">
              <el-switch v-model="form.enabled" />
            </el-form-item>
          </div>
        </div>
      </div>

      <!-- Side column: Live Preview -->
      <div class="tk-task-form__side">
        <div class="tk-preview-card">
          <div class="tk-preview-card__header">
            <div class="tk-preview-card__title">{{ t('task.task.create.livePreview') }}</div>
            <span class="tk-task-form-section__hint">{{ t('task.task.create.livePreviewHint') }}</span>
          </div>
          <div class="tk-preview-card__body">
            <div class="tk-preview-row">
              <span class="tk-preview-row__label">{{ t('task.task.create.previewName') }}</span>
              <span class="tk-preview-row__value" :class="{ 'tk-preview-row__value--empty': !form.name }">
                {{ form.name || t('task.task.create.previewEmpty') }}
              </span>
            </div>
            <div class="tk-preview-row">
              <span class="tk-preview-row__label">{{ t('task.task.create.previewExecutor') }}</span>
              <span class="tk-preview-row__value tk-preview-row__value--mono">{{ previewExecutorLabel }}</span>
            </div>
            <div class="tk-preview-row">
              <span class="tk-preview-row__label">{{ t('task.task.create.previewSchedule') }}</span>
              <span class="tk-preview-row__value tk-preview-row__value--mono">{{ previewSchedule }}</span>
            </div>
            <div class="tk-preview-row">
              <span class="tk-preview-row__label">{{ t('task.task.create.group') }}</span>
              <span class="tk-preview-row__value tk-preview-row__value--mono">{{ form.group || '-' }}</span>
            </div>
            <div class="tk-preview-row">
              <span class="tk-preview-row__label">{{ t('task.task.create.retryPolicy') }}</span>
              <span class="tk-preview-row__value tk-preview-row__value--mono">{{ form.retryPolicy }}</span>
            </div>
            <div class="tk-preview-row">
              <span class="tk-preview-row__label">{{ t('task.task.create.concurrency') }}</span>
              <span class="tk-preview-row__value tk-preview-row__value--mono">{{ form.concurrency === 0 ? t('task.task.create.concurrencyUnlimited') : form.concurrency }}</span>
            </div>
            <div class="tk-preview-row">
              <span class="tk-preview-row__label">{{ t('task.task.create.previewStatus') }}</span>
              <span class="tk-preview-row__value">
                {{ props.isEdit ? (form.enabled ? t('common.app.enabled') : t('common.app.disabled')) : t('task.task.create.previewStatusDefault') }}
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Footer -->
    <div class="tk-task-form__footer">
      <div class="tk-task-form__footer-hint">{{ t('task.task.create.footerHint') }}</div>
      <div class="tk-task-form__footer-actions">
        <el-button @click="handleCancel">
          {{ t('task.task.create.cancel') }}
        </el-button>
        <el-button
          type="primary"
          :loading="loading"
          @click="handleSubmit"
        >
          {{ t('task.task.create.submit') }}
        </el-button>
      </div>
    </div>
  </el-form>
</template>

<style scoped lang="scss">
.tk-task-form {
  &__grid {
    display: grid;
    grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
    gap: var(--tk-spacing-lg);
    align-items: start;
  }

  &__main {
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-lg);
    min-width: 0;
  }

  &__side {
    position: sticky;
    top: var(--tk-spacing-md);
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-lg);
    min-width: 0;
  }

  &__footer {
    display: flex;
    gap: var(--tk-spacing-lg);
    align-items: center;
    justify-content: space-between;
    padding: var(--tk-spacing-md) var(--tk-spacing-lg);
    margin-top: var(--tk-spacing-lg);
    background: var(--tk-bg-color);
    border: 1px solid var(--tk-border-color-lighter);
    border-radius: var(--tk-border-radius-md);
    box-shadow: var(--tk-shadow-sm);
  }

  &__footer-hint {
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
  }

  &__footer-actions {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
  }

  &__item-help {
    margin-top: var(--tk-spacing-xs);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
  }
}

.tk-task-form-section {
  overflow: hidden;
  background: var(--tk-bg-color);
  border: 1px solid var(--tk-border-color-lighter);
  border-radius: var(--tk-border-radius-md);

  &__header {
    display: flex;
    gap: var(--tk-spacing-md);
    align-items: center;
    justify-content: space-between;
    padding: var(--tk-spacing-md) var(--tk-spacing-lg);
    background: var(--tk-fill-color-light);
    border-bottom: 1px solid var(--tk-border-color-lighter);
  }

  &__title {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
    font-size: var(--tk-font-size-base);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
  }

  &__index {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    font-family: var(--tk-font-family-mono, monospace);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-bold);
    color: var(--tk-primary-color);
    background-color: var(--tk-primary-color-light-9);
    border: 1px solid var(--tk-primary-color-light-7);
    border-radius: var(--tk-border-radius-sm);
  }

  &__hint {
    font-family: var(--tk-font-family-mono, monospace);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  &__body {
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-md);
    padding: var(--tk-spacing-lg);
  }

  &__toggle {
    font-family: var(--tk-font-family-mono, monospace);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    cursor: pointer;
    background: transparent;
    border: none;

    &:hover { color: var(--tk-primary-color); }
  }
}

.tk-executor-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--tk-spacing-sm);
  width: 100%;
}

.tk-executor-card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-xs);
  padding: var(--tk-spacing-md) var(--tk-spacing-sm);
  text-align: left;
  cursor: pointer;
  background-color: var(--tk-bg-color);
  border: 1px solid var(--tk-border-color-lighter);
  border-radius: var(--tk-border-radius-md);
  transition: all 0.2s ease;

  &:hover {
    background-color: var(--tk-fill-color-light);
    border-color: var(--tk-border-color);
  }

  &--active {
    background-color: var(--tk-primary-color-light-9);
    border-color: var(--tk-primary-color);
  }

  &__label {
    font-size: var(--tk-font-size-sm);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
  }

  &__desc {
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
  }

  &__check {
    position: absolute;
    top: 4px;
    right: 4px;
    font-size: 14px;
    color: var(--tk-primary-color);
  }
}

.tk-schedule-tabs {
  display: inline-flex;
  padding: 2px;
  background-color: var(--tk-bg-fill);
  border: 1px solid var(--tk-border-color-lighter);
  border-radius: var(--tk-border-radius-md);
}

.tk-schedule-tab {
  display: inline-flex;
  gap: var(--tk-spacing-xs);
  align-items: center;
  height: 28px;
  padding: 0 var(--tk-spacing-md);
  font-size: var(--tk-font-size-xs);
  font-weight: var(--tk-font-weight-medium);
  color: var(--tk-text-secondary);
  cursor: pointer;
  background: transparent;
  border: none;
  border-radius: var(--tk-border-radius-sm);
  transition: all 0.2s ease;

  &:hover { color: var(--tk-text-primary); }

  &--active {
    color: var(--tk-primary-color);
    background-color: var(--tk-bg-color);
  }

  &__num {
    font-family: var(--tk-font-family-mono, monospace);
    font-size: 10px;
    color: var(--tk-text-placeholder);
  }

  &--active &__num { color: var(--tk-primary-color); }
}

.tk-cron-presets {
  display: flex;
  flex-wrap: wrap;
  gap: var(--tk-spacing-xs);
  margin-top: var(--tk-spacing-xs);
}

.tk-cron-preset {
  display: inline-flex;
  align-items: center;
  height: 26px;
  padding: 0 var(--tk-spacing-sm);
  font-size: var(--tk-font-size-xs);
  font-weight: var(--tk-font-weight-medium);
  color: var(--tk-text-regular);
  cursor: pointer;
  background-color: var(--tk-bg-fill);
  border: 1px solid var(--tk-border-color-lighter);
  border-radius: var(--tk-border-radius-round);
  transition: all 0.2s ease;

  &:hover {
    color: var(--tk-primary-color);
    background-color: var(--tk-primary-color-light-9);
    border-color: var(--tk-primary-color-light-7);
  }
}

.tk-cron-preview {
  padding: var(--tk-spacing-sm) var(--tk-spacing-md);
  margin-top: var(--tk-spacing-sm);
  font-size: var(--tk-font-size-sm);
  color: var(--tk-text-regular);
  background-color: var(--tk-fill-color-light);
  border: 1px dashed var(--tk-border-color);
  border-radius: var(--tk-border-radius-md);

  &__label {
    margin-right: var(--tk-spacing-sm);
    font-family: var(--tk-font-family-mono, monospace);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  &__value {
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-primary-color);
  }
}

.tk-task-form-item__help {
  display: flex;
  gap: 4px;
  align-items: center;
  margin-top: 4px;
  font-size: var(--tk-font-size-xs);
  color: var(--tk-text-secondary);
}

.tk-tag-input {
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-xs);
  width: 100%;

  &__tags {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-xs);
  }
}

.tk-preview-card {
  overflow: hidden;
  background: var(--tk-bg-color);
  border: 1px solid var(--tk-border-color-lighter);
  border-radius: var(--tk-border-radius-md);

  &__header {
    display: flex;
    gap: var(--tk-spacing-md);
    align-items: center;
    justify-content: space-between;
    padding: var(--tk-spacing-md) var(--tk-spacing-lg);
    background: var(--tk-fill-color-light);
    border-bottom: 1px solid var(--tk-border-color-lighter);
  }

  &__title {
    font-size: var(--tk-font-size-base);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
  }

  &__body {
    padding: var(--tk-spacing-lg);
  }
}

.tk-preview-row {
  display: flex;
  gap: var(--tk-spacing-md);
  align-items: baseline;
  justify-content: space-between;
  padding: var(--tk-spacing-xs) 0;
  border-bottom: 1px dashed var(--tk-border-color-lighter);

  &:last-child { border-bottom: none; }

  &__label {
    flex-shrink: 0;
    font-family: var(--tk-font-family-mono, monospace);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  &__value {
    max-width: 60%;
    overflow: hidden;
    text-overflow: ellipsis;
    font-size: var(--tk-font-size-sm);
    font-weight: var(--tk-font-weight-medium);
    color: var(--tk-text-primary);
    text-align: right;
    white-space: nowrap;

    &--mono {
      font-family: var(--tk-font-family-mono, monospace);
      font-size: var(--tk-font-size-xs);
      font-weight: var(--tk-font-weight-regular);
    }

    &--empty {
      font-style: italic;
      font-weight: var(--tk-font-weight-regular);
      color: var(--tk-text-placeholder);
    }
  }
}

@media (max-width: 1100px) {
  .tk-task-form__grid {
    grid-template-columns: 1fr;
  }

  .tk-task-form__side {
    position: static;
  }

  .tk-executor-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
