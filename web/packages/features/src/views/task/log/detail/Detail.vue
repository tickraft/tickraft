// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ArrowLeft, View, CopyDocument, Download, WarningFilled } from '@element-plus/icons-vue'
import { PageEmpty } from '@tickraft/core'
import { formatDate, formatDuration } from '@tickraft/core'
import type { LogModel } from '../../../../types/task'
import { getLog } from '../../../../api/task'

interface TimelinePhase {
  cls: string
  title: string
  time: string
  delta: string
  desc: string
}

interface OutputLine {
  text: string
  cls: string
}

const router = useRouter()
const route = useRoute()
const { t } = useI18n()

const taskId = Number(route.params.taskId)
const logId = Number(route.params.execId)
const loading = ref(false)
const notFound = ref(false)
const detail = ref<LogModel | null>(null)
const activeTab = ref('detail')

const EXECUTOR_LABELS = computed<Record<string, string>>(() => ({
  http: 'HTTP', tcp: 'TCP', icmp: 'ICMP', local: t('task.task.create.executorLocal'),
  webhook: 'Webhook', ssh: 'SSH', mysql: 'MYSQL', redis: 'REDIS',
}))

const statusCls = computed(() => {
  const s = detail.value?.status ?? 'unknown'
  if (s === 'success') return 'success'
  if (s === 'failed') return 'failed'
  if (s === 'running') return 'running'
  if (s === 'timeout') return 'warning'
  return 'unknown'
})

const hasError = computed(() => {
  if (!detail.value) return false
  return (detail.value.status === 'failed' || detail.value.status === 'timeout') && !!detail.value.error
})

const hasOutput = computed(() => !!detail.value?.output)

const outputLines = computed<OutputLine[]>(() => {
  if (!detail.value?.output) return []
  return detail.value.output.split('\n').map((line) => {
    const lower = line.toLowerCase()
    if (lower.includes('[ok]') || lower.includes('success')) return { text: line, cls: 'ok' }
    if (lower.includes('[error]') || lower.includes('failed') || lower.includes('exception')) return { text: line, cls: 'err' }
    if (lower.includes('[timeout]') || lower.includes('timeout')) return { text: line, cls: 'warn' }
    if (lower.includes('[running]') || lower.includes('running')) return { text: line, cls: 'info' }
    return { text: line, cls: '' }
  })
})

const timelinePhases = computed<TimelinePhase[]>(() => {
  if (!detail.value) return []
  const log = detail.value
  const dur = formatDuration(log.duration ?? 0)
  const finalTitle = log.status === 'success' ? t('task.log.detail.timelineExecSuccess') :
    log.status === 'timeout' ? t('task.log.detail.timelineExecTimeout') :
    log.status === 'failed' ? t('task.log.detail.timelineExecFailed') : t('task.log.detail.timelineExecComplete')
  const finalCls = log.status === 'success' ? 'ok' : log.status === 'running' ? 'info' : 'fail'
  return [
    { cls: 'info', title: t('task.log.detail.timelineDispatch'), time: log.startedAt, delta: '0ms', desc: t('task.log.detail.timelineStartDesc', { executor: log.executorType }) },
    { cls: 'info', title: t('task.log.detail.timelineWorkerPickup'), time: log.startedAt, delta: '~1ms', desc: t('task.log.detail.workerLabel') },
    { cls: finalCls, title: t('task.log.detail.timelineExecComplete'), time: log.finishedAt ?? '', delta: dur, desc: t('task.log.detail.duration') + ': ' + dur },
    { cls: finalCls, title: finalTitle, time: log.finishedAt ?? '', delta: '', desc: hasError.value ? log.error ?? '' : '' },
  ]
})

async function fetchData(): Promise<void> {
  loading.value = true
  try {
    detail.value = await getLog(taskId, logId)
  } catch {
    notFound.value = true
  } finally {
    loading.value = false
  }
}

function handleBack(): void { router.push('/task/log/list') }
function handleViewTask(): void {
  if (detail.value?.taskId) router.push(`/task/detail/${detail.value.taskId}`)
}

function handleCopyOutput(): void {
  if (!detail.value?.output) return
  try {
    navigator.clipboard.writeText(detail.value.output)
    ElMessage.success(t('task.log.detail.copy') + ' ✓')
  } catch {
    ElMessage.warning(t('task.log.detail.copy'))
  }
}

function handleSaveOutput(): void {
  if (!detail.value?.output) return
  const blob = new Blob([detail.value.output], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `log-${logId}-output.txt`
  a.click()
  URL.revokeObjectURL(url)
}

onMounted(() => { void fetchData() })
</script>

<template>
  <div v-loading="loading" class="tk-log-detail tk-page-container">
    <template v-if="notFound">
      <PageEmpty :description="t('task.log.detail.notFound', { id: logId })">
        <el-button type="primary" @click="handleBack">{{ t('task.log.detail.back') }}</el-button>
      </PageEmpty>
    </template>

    <template v-else-if="detail">
      <!-- Header -->
      <div class="tk-detail-header">
        <div class="tk-detail-header__left">
          <button class="tk-detail-header__back" @click="handleBack"><el-icon :size="16"><ArrowLeft /></el-icon></button>
          <div class="tk-detail-header__title-block">
            <div class="tk-detail-header__eyebrow">{{ t('task.log.detail.eyebrow', { id: logId }) }}</div>
            <div class="tk-detail-header__title-row">
              <h1 class="tk-detail-header__title">{{ detail.taskName || '—' }}</h1>
              <span class="tk-status-tag" :class="`tk-status-tag--${statusCls}`"><span class="tk-status-tag__dot" />{{ detail.status }}</span>
              <span class="tk-executor-badge" :class="`tk-executor-badge--${detail.executorType ?? ''}`"><span class="tk-executor-badge__dot" />{{ EXECUTOR_LABELS[detail.executorType ?? ''] || detail.executorType }}</span>
              <span class="tk-detail-header__key">LOG-{{ detail.id }}</span>
            </div>
          </div>
        </div>
        <div class="tk-detail-header__actions">
          <el-button @click="handleBack">{{ t('task.log.detail.backToList') }}</el-button>
          <el-button @click="handleViewTask"><el-icon><View /></el-icon>{{ t('task.log.detail.viewTask') }}</el-button>
        </div>
      </div>

      <!-- Metrics strip: 4 tiles -->
      <div class="tk-metric-strip">
        <div class="tk-metric-tile" :class="`tk-metric-tile--${statusCls}`">
          <div class="tk-metric-tile__label">{{ t('task.log.detail.metricsStatus') }}</div>
          <div class="tk-metric-tile__value">{{ detail.status }}</div>
          <div class="tk-metric-tile__sub">{{ t('task.log.detail.executorType') }}: {{ detail.executorType }}</div>
        </div>
        <div class="tk-metric-tile">
          <div class="tk-metric-tile__label">{{ t('task.log.detail.metricsDuration') }}</div>
          <div class="tk-metric-tile__value">{{ formatDuration(detail.duration ?? 0) }}</div>
          <div class="tk-metric-tile__sub">{{ detail.retryCount }} {{ t('task.log.detail.retryCount') }}</div>
        </div>
        <div class="tk-metric-tile tk-metric-tile--info">
          <div class="tk-metric-tile__label">{{ t('task.log.detail.metricsStarted') }}</div>
          <div class="tk-metric-tile__value tk-metric-tile__value--sm">{{ formatDate(detail.startedAt) }}</div>
          <div class="tk-metric-tile__sub">{{ t('task.log.detail.startedAt') }}</div>
        </div>
        <div class="tk-metric-tile tk-metric-tile--info">
          <div class="tk-metric-tile__label">{{ t('task.log.detail.metricsFinished') }}</div>
          <div class="tk-metric-tile__value tk-metric-tile__value--sm">{{ detail.finishedAt ? formatDate(detail.finishedAt) : '—' }}</div>
          <div class="tk-metric-tile__sub">{{ t('task.log.detail.finishedAt') }}</div>
        </div>
      </div>

      <!-- Error block -->
      <div v-if="hasError" class="tk-error-block">
        <div class="tk-error-block__title"><el-icon><WarningFilled /></el-icon>{{ t('task.log.detail.error') }}</div>
        <div class="tk-error-block__msg">{{ detail.error }}</div>
      </div>

      <!-- Tabs -->
      <div class="tk-detail-card">
        <el-tabs v-model="activeTab" class="tk-detail-tabs">
          <!-- Execution Detail -->
          <el-tab-pane :label="t('task.log.detail.basicInfo')" name="detail">
            <!-- Task info -->
            <div class="tk-sub-section">
              <div class="tk-sub-section__title">{{ t('task.log.detail.taskInfo') }}<span class="tk-sub-section__hint">{{ t('task.log.detail.taskInfoHint') }}</span></div>
              <div class="tk-descriptions">
                <div class="tk-desc-item"><span class="tk-desc-item__label">{{ t('task.log.detail.taskNameLabel') }}</span><span class="tk-desc-item__value">{{ detail.taskName || '—' }}</span></div>
                <div class="tk-desc-item"><span class="tk-desc-item__label">{{ t('task.log.detail.taskIdLabel') }}</span><span class="tk-desc-item__value tk-desc-item__value--mono">{{ detail.taskId }}</span></div>
                <div class="tk-desc-item"><span class="tk-desc-item__label">{{ t('task.log.detail.executorLabel') }}</span><span class="tk-desc-item__value">{{ EXECUTOR_LABELS[detail.executorType ?? ''] || detail.executorType }}</span></div>
                <div class="tk-desc-item"><span class="tk-desc-item__label">{{ t('task.log.detail.workerLabel') }}</span><span class="tk-desc-item__value tk-desc-item__value--placeholder">—</span></div>
              </div>
            </div>
            <!-- Exec metrics -->
            <div class="tk-sub-section">
              <div class="tk-sub-section__title">{{ t('task.log.detail.execMetrics') }}<span class="tk-sub-section__hint">{{ t('task.log.detail.execMetricsHint') }}</span></div>
              <div class="tk-descriptions">
                <div class="tk-desc-item"><span class="tk-desc-item__label">{{ t('task.log.detail.status') }}</span><span class="tk-desc-item__value">{{ detail.status }}</span></div>
                <div class="tk-desc-item"><span class="tk-desc-item__label">{{ t('task.log.detail.duration') }}</span><span class="tk-desc-item__value tk-desc-item__value--mono">{{ formatDuration(detail.duration ?? 0) }}</span></div>
                <div class="tk-desc-item"><span class="tk-desc-item__label">{{ t('task.log.detail.startedAt') }}</span><span class="tk-desc-item__value tk-desc-item__value--mono">{{ formatDate(detail.startedAt) }}</span></div>
                <div class="tk-desc-item"><span class="tk-desc-item__label">{{ t('task.log.detail.finishedAt') }}</span><span class="tk-desc-item__value tk-desc-item__value--mono">{{ detail.finishedAt ? formatDate(detail.finishedAt) : '—' }}</span></div>
                <div class="tk-desc-item"><span class="tk-desc-item__label">{{ t('task.log.detail.statusCode') }}</span><span class="tk-desc-item__value tk-desc-item__value--mono">{{ detail.statusCode }}</span></div>
                <div class="tk-desc-item"><span class="tk-desc-item__label">{{ t('task.log.detail.retryCount') }}</span><span class="tk-desc-item__value tk-desc-item__value--mono">{{ detail.retryCount }}</span></div>
              </div>
            </div>
            <!-- Timeline -->
            <div class="tk-sub-section">
              <div class="tk-sub-section__title">{{ t('task.log.detail.timeline') }}<span class="tk-sub-section__hint">{{ t('task.log.detail.timelineHint') }}</span></div>
              <div class="tk-timeline">
                <div v-for="(phase, idx) in timelinePhases" :key="idx" class="tk-timeline__item">
                  <span class="tk-timeline__dot" :class="`tk-timeline__dot--${phase.cls}`" />
                  <div class="tk-timeline__head">
                    <span class="tk-timeline__title">{{ phase.title }}
                      <span v-if="phase.delta" class="tk-timeline__delta">{{ phase.delta }}</span>
                    </span>
                    <span class="tk-timeline__time">{{ phase.time ? formatDate(phase.time) : '—' }}</span>
                  </div>
                  <div v-if="phase.desc" class="tk-timeline__desc">{{ phase.desc }}</div>
                </div>
              </div>
            </div>
          </el-tab-pane>

          <!-- Output Log -->
          <el-tab-pane :label="t('task.log.detail.output')" name="output">
            <div v-if="hasOutput" class="tk-terminal">
              <div class="tk-terminal__bar">
                <div class="tk-terminal__dots">
                  <span class="tk-terminal__dot tk-terminal__dot--red" />
                  <span class="tk-terminal__dot tk-terminal__dot--yellow" />
                  <span class="tk-terminal__dot tk-terminal__dot--green" />
                </div>
                <span class="tk-terminal__title">EXECUTION OUTPUT · #{{ logId }}</span>
                <div class="tk-terminal__actions">
                  <button class="tk-terminal__btn" @click="handleCopyOutput"><el-icon :size="12"><CopyDocument /></el-icon>{{ t('task.log.detail.copy') }}</button>
                  <button class="tk-terminal__btn" @click="handleSaveOutput"><el-icon :size="12"><Download /></el-icon>{{ t('task.log.detail.save') }}</button>
                </div>
              </div>
              <div class="tk-terminal__body">
                <span v-for="(line, i) in outputLines" :key="i" class="tk-terminal__line" :class="line.cls ? `tk-terminal__${line.cls}` : ''">{{ line.text }}<br></span>
              </div>
            </div>
            <div v-else class="tk-terminal__empty">{{ t('task.log.detail.noOutput') }}</div>
          </el-tab-pane>

          <!-- Environment -->
          <el-tab-pane :label="t('task.log.detail.environment')" name="env">
            <div class="tk-env-placeholder">
              <div class="tk-env-placeholder__icon"><el-icon :size="28"><View /></el-icon></div>
              <div class="tk-env-placeholder__text">{{ t('task.log.detail.environment') }}</div>
              <div class="tk-env-placeholder__desc">—</div>
            </div>
          </el-tab-pane>
        </el-tabs>
      </div>
    </template>
  </div>
</template>

<style scoped lang="scss">
.tk-detail-header {
  display: flex; flex-wrap: wrap; gap: var(--tk-spacing-8); align-items: flex-start; justify-content: space-between; margin-bottom: var(--tk-spacing-8);
  &__left { display: flex; gap: var(--tk-spacing-5); align-items: center; min-width: 0; }
  &__back { display: inline-flex; flex-shrink: 0; align-items: center; justify-content: center; width: 36px; height: 36px; color: var(--tk-text-secondary); cursor: pointer; background: var(--tk-bg-surface); border: 1px solid var(--tk-border-color-base); border-radius: var(--tk-radius-md); transition: all var(--tk-duration-fast) var(--tk-ease-out); &:hover { color: var(--tk-primary-color); background: var(--tk-primary-color-bg); border-color: var(--tk-primary-color-border); } }
  &__title-block { min-width: 0; }
  &__eyebrow { margin-bottom: 4px; font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); color: var(--tk-text-secondary); text-transform: uppercase; letter-spacing: var(--tk-letter-widest); }
  &__title-row { display: flex; flex-wrap: wrap; gap: var(--tk-spacing-4); align-items: center; }
  &__title { margin: 0; font-family: var(--tk-font-display); font-size: var(--tk-font-size-2xl); font-weight: var(--tk-font-weight-bold); line-height: 1.1; color: var(--tk-text-primary); letter-spacing: var(--tk-letter-tight); }
  &__key { padding: 2px var(--tk-spacing-4); font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); color: var(--tk-text-secondary); background: var(--tk-bg-fill); border: 1px solid var(--tk-border-color-light); border-radius: var(--tk-radius-sm); }
  &__actions { display: flex; gap: var(--tk-spacing-3); align-items: center; }
}

.tk-metric-strip { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--tk-spacing-4); margin-bottom: var(--tk-spacing-8); }

.tk-metric-tile {
  position: relative; display: flex; flex-direction: column; gap: 4px; padding: var(--tk-spacing-6) var(--tk-spacing-8); overflow: hidden; background: var(--tk-bg-surface); border: 1px solid var(--tk-border-color-base); border-radius: var(--tk-radius-lg);
  &::before { position: absolute; top: 0; left: 0; width: 3px; height: 100%; content: ""; background-color: var(--tk-primary-color); }
  &--ok::before, &--success::before { background-color: var(--tk-success-color); }
  &--fail::before, &--failed::before { background-color: var(--tk-danger-color); }
  &--info::before, &--running::before { background-color: var(--tk-info-color); }
  &--warn::before, &--warning::before { background-color: var(--tk-warning-color); }
  &__label { font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); color: var(--tk-text-secondary); text-transform: uppercase; letter-spacing: var(--tk-letter-wider); }
  &__value { font-family: var(--tk-font-display); font-size: var(--tk-font-size-2xl); font-weight: var(--tk-font-weight-extrabold); font-variant-numeric: tabular-nums; line-height: 1; color: var(--tk-text-primary); text-transform: capitalize; letter-spacing: var(--tk-letter-tight); &--sm { font-size: var(--tk-font-size-sm); } }
  &__sub { font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); color: var(--tk-text-secondary); }
}

.tk-executor-badge { display: inline-flex; gap: var(--tk-spacing-2); align-items: center; padding: 2px var(--tk-spacing-6); font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); font-weight: var(--tk-font-weight-semibold); text-transform: uppercase; letter-spacing: var(--tk-letter-wide); white-space: nowrap; border: 1px solid transparent; border-radius: var(--tk-radius-sm);
  &__dot { flex-shrink: 0; width: 5px; height: 5px; background-color: currentcolor; border-radius: var(--tk-radius-circle); }
  &--http { color: #2563eb; background-color: rgb(37 99 235 / 10%); border-color: rgb(37 99 235 / 25%); }
  &--tcp { color: #0891b2; background-color: rgb(8 145 178 / 10%); border-color: rgb(8 145 178 / 25%); }
  &--icmp { color: #7c3aed; background-color: rgb(124 58 237 / 10%); border-color: rgb(124 58 237 / 25%); }
  &--local { color: #475569; background-color: rgb(71 85 105 / 10%); border-color: rgb(71 85 105 / 25%); }
  &--ssh { color: #b45309; background-color: rgb(180 83 9 / 10%); border-color: rgb(180 83 9 / 25%); }
  &--mysql { color: #15803d; background-color: rgb(21 128 61 / 10%); border-color: rgb(21 128 61 / 25%); }
  &--redis { color: #dc2626; background-color: rgb(220 38 38 / 10%); border-color: rgb(220 38 38 / 25%); }
  &--webhook { color: #be185d; background-color: rgb(190 24 93 / 10%); border-color: rgb(190 24 93 / 25%); }
}

.tk-status-tag { display: inline-flex; gap: 6px; align-items: center; padding: 2px var(--tk-spacing-4); font-size: var(--tk-font-size-xs); font-weight: var(--tk-font-weight-medium); text-transform: capitalize; border: 1px solid transparent; border-radius: var(--tk-radius-sm);
  &__dot { flex-shrink: 0; width: 7px; height: 7px; border-radius: var(--tk-radius-circle); }
  &--success { color: var(--tk-success-color-text); background: var(--tk-success-color-bg); border-color: var(--tk-success-color-border); .tk-status-tag__dot { background-color: var(--tk-success-color); } }
  &--failed { color: var(--tk-danger-color-text); background: var(--tk-danger-color-bg); border-color: var(--tk-danger-color-border); .tk-status-tag__dot { background-color: var(--tk-danger-color); } }
  &--running { color: var(--tk-info-color-text); background: var(--tk-info-color-bg); border-color: var(--tk-info-color-border); .tk-status-tag__dot { background-color: var(--tk-info-color); animation: tk-blink 1.4s ease-in-out infinite; } }
  &--warning { color: var(--tk-warning-color-text); background: var(--tk-warning-color-bg); border-color: var(--tk-warning-color-border); .tk-status-tag__dot { background-color: var(--tk-warning-color); } }
  &--unknown { color: var(--tk-text-secondary); background: var(--tk-bg-fill); border-color: var(--tk-border-color-base); .tk-status-tag__dot { background-color: var(--tk-text-placeholder); } }
}

@keyframes tk-blink { 0%, 100% { opacity: 1; } 50% { opacity: 0.35; } }

.tk-error-block { display: flex; flex-direction: column; gap: var(--tk-spacing-3); padding: var(--tk-spacing-5) var(--tk-spacing-6); margin-bottom: var(--tk-spacing-8); background: var(--tk-danger-color-bg); border: 1px solid var(--tk-danger-color-border); border-radius: var(--tk-radius-md);
  &__title { display: flex; gap: var(--tk-spacing-3); align-items: center; font-family: var(--tk-font-display); font-size: var(--tk-font-size-sm); font-weight: var(--tk-font-weight-semibold); color: var(--tk-danger-color-text); }
  &__msg { font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); line-height: var(--tk-line-height-snug); color: var(--tk-danger-color-text); word-break: break-all; }
}

.tk-detail-card { overflow: hidden; background: var(--tk-bg-surface); border: 1px solid var(--tk-border-color-base); border-radius: var(--tk-radius-lg); }
.tk-detail-tabs { :deep(.el-tabs__header) { padding: 0 var(--tk-spacing-10); margin: 0; } :deep(.el-tabs__content) { padding: var(--tk-spacing-10); } }

.tk-sub-section { margin-bottom: var(--tk-spacing-8); &:last-child { margin-bottom: 0; }
  &__title { display: flex; gap: var(--tk-spacing-3); align-items: center; margin-bottom: var(--tk-spacing-5); font-family: var(--tk-font-display); font-size: var(--tk-font-size-sm); font-weight: var(--tk-font-weight-semibold); color: var(--tk-text-primary); }
  &__hint { margin-left: auto; font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); color: var(--tk-text-secondary); text-transform: uppercase; letter-spacing: var(--tk-letter-wide); }
}

.tk-descriptions { display: grid; grid-template-columns: repeat(2, 1fr); gap: var(--tk-spacing-6) var(--tk-spacing-12); }

.tk-desc-item { display: flex; flex-direction: column; gap: 4px; padding-bottom: var(--tk-spacing-4); border-bottom: 1px dashed var(--tk-border-color-lighter);
  &__label { font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); color: var(--tk-text-secondary); text-transform: uppercase; letter-spacing: var(--tk-letter-wide); }
  &__value { font-size: var(--tk-font-size-sm); color: var(--tk-text-primary); word-break: break-all; &--mono { font-family: var(--tk-font-mono); font-variant-numeric: tabular-nums; } &--placeholder { font-style: italic; color: var(--tk-text-placeholder); } }
}

.tk-timeline { position: relative; padding-left: var(--tk-spacing-10); &::before { position: absolute; top: 6px; bottom: 6px; left: 11px; width: 2px; content: ''; background: var(--tk-border-color-light); }
  &__item { position: relative; padding: 0 0 var(--tk-spacing-8) var(--tk-spacing-8); &:last-child { padding-bottom: 0; } }

  &__dot { position: absolute; top: 4px; left: -22px; z-index: 1; width: 14px; height: 14px; background-color: var(--tk-text-placeholder); border: 3px solid var(--tk-bg-surface); border-radius: var(--tk-radius-circle); box-shadow: 0 0 0 1px var(--tk-border-color-base);
    &--ok { background-color: var(--tk-success-color); } &--fail { background-color: var(--tk-danger-color); } &--info { background-color: var(--tk-info-color); } &--warn { background-color: var(--tk-warning-color); } }
  &__head { display: flex; flex-wrap: wrap; gap: var(--tk-spacing-4); align-items: center; justify-content: space-between; margin-bottom: 4px; }
  &__title { display: flex; gap: var(--tk-spacing-3); align-items: center; font-size: var(--tk-font-size-sm); font-weight: var(--tk-font-weight-medium); color: var(--tk-text-primary); }
  &__time { font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); font-variant-numeric: tabular-nums; color: var(--tk-text-secondary); }
  &__desc { font-size: var(--tk-font-size-xs); line-height: var(--tk-line-height-snug); color: var(--tk-text-secondary); }
  &__delta { display: inline-flex; align-items: center; padding: 1px 6px; margin-left: var(--tk-spacing-3); font-family: var(--tk-font-mono); font-size: 10px; color: var(--tk-text-secondary); background: var(--tk-bg-fill); border-radius: var(--tk-radius-xs); }
}

.tk-terminal { overflow: hidden;
  background: #0b0e14; border: 1px solid var(--tk-border-color-base); border-radius: var(--tk-radius-md);
  &__bar { display: flex; gap: var(--tk-spacing-4); align-items: center; padding: var(--tk-spacing-3) var(--tk-spacing-5); background: #11151c; border-bottom: 1px solid rgb(255 255 255 / 6%); }
  &__dots { display: inline-flex; gap: 6px; align-items: center; }
  &__dot { width: 10px; height: 10px; border-radius: var(--tk-radius-circle); &--red { background: #ff5f57; } &--yellow { background: #febc2e; } &--green { background: #28c840; } }
  &__title { font-family: var(--tk-font-mono); font-size: var(--tk-font-size-xs); color: #9ca3af; text-transform: uppercase; letter-spacing: var(--tk-letter-wide); }
  &__actions { display: inline-flex; gap: var(--tk-spacing-2); align-items: center; margin-left: auto; }
  &__btn { display: inline-flex; gap: 4px; align-items: center; padding: 2px var(--tk-spacing-3); font-family: var(--tk-font-mono); font-size: 10px; color: #9ca3af; cursor: pointer; background: transparent; border: 1px solid rgb(255 255 255 / 8%); border-radius: var(--tk-radius-xs); transition: all var(--tk-duration-fast) var(--tk-ease-out); &:hover { color: #e5e7eb; background: rgb(255 255 255 / 4%); border-color: rgb(255 255 255 / 18%); } }
  &__body { max-height: 480px; padding: var(--tk-spacing-5) var(--tk-spacing-6); overflow-y: auto; font-family: var(--tk-font-mono); font-size: var(--tk-font-size-sm); line-height: var(--tk-line-height-snug); color: #d1d5db; word-break: break-all; white-space: pre-wrap; }
  &__line { display: inline; }
  &__ok { font-weight: bold; color: #28c840; }
  &__err { font-weight: bold; color: #ff5f57; }
  &__warn { font-weight: bold; color: #febc2e; }
  &__info { color: #58a6ff; }
  &__empty { padding: var(--tk-spacing-12); font-family: var(--tk-font-mono); font-size: var(--tk-font-size-sm); color: var(--tk-text-secondary); text-align: center; background: var(--tk-bg-surface); border: 1px solid var(--tk-border-color-base); border-radius: var(--tk-radius-md); }
}

.tk-env-placeholder { display: flex; flex-direction: column; gap: var(--tk-spacing-4); align-items: center; justify-content: center; padding: var(--tk-spacing-16) var(--tk-spacing-8); text-align: center;
  &__icon { display: flex; align-items: center; justify-content: center; width: 56px; height: 56px; color: var(--tk-text-secondary); background: var(--tk-bg-fill); border-radius: var(--tk-radius-lg); }
  &__text { font-family: var(--tk-font-display); font-size: var(--tk-font-size-md); font-weight: var(--tk-font-weight-semibold); color: var(--tk-text-primary); }
  &__desc { font-size: var(--tk-font-size-sm); color: var(--tk-text-secondary); }
}

@media (max-width: 960px) { .tk-metric-strip { grid-template-columns: repeat(2, 1fr); } .tk-descriptions { grid-template-columns: 1fr; } }
</style>
