// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import {
  StatusTag,
  ConfirmDialog,
  formatDate,
} from '@tickraft/core'
import type { AlertStatus } from '@tickraft/core'
import {
  getAlertRecord,
  resolveAlertRecord,
  acknowledgeAlertRecord,
  getAlertRule,
} from '../../../../api/prism'
import type { AlertRecord, AlertRule, AlertSeverity } from '../../../../api/prism'
import { SEVERITY_TAG_TYPE, parseDate } from '../../constants'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const loading = ref(true)
const record = ref<AlertRecord | null>(null)
const rule = ref<AlertRule | null>(null)
const resolveVisible = ref(false)
const acknowledgeVisible = ref(false)
const resolving = ref(false)
const acknowledging = ref(false)

/** Whether in firing state (controls visibility of action buttons) */
const isFiring = computed(() => record.value?.status === 'firing')
/** Whether in acknowledged state */
const isAcknowledged = computed(() => record.value?.status === 'acknowledged')

/** Safely resolve the el-tag type for the record severity */
const severityTagType = computed<'danger' | 'warning' | 'info'>(() => {
  const sev = record.value?.severity
  if (sev === 'critical' || sev === 'warning' || sev === 'info') {
    return SEVERITY_TAG_TYPE[sev as AlertSeverity]
  }
  return 'info'
})

/** Safely resolve the i18n label for the record severity */
const severityText = computed(() => {
  const sev = record.value?.severity
  if (!sev) return '-'
  return t(`prism.severity.${sev}`)
})

/** Scene tag type for the associated rule */
function sceneTagType(scene: string): 'success' | 'warning' | 'danger' | 'info' {
  switch (scene) {
    case 'task': return 'info'
    case 'probe': return 'success'
    case 'metric': return 'warning'
    case 'remediation': return 'danger'
    default: return 'info'
  }
}

/** Format a nullable time string */
function formatTime(value: string | null | undefined): string {
  if (!value) return '-'
  return formatDate(parseDate(value))
}

/** Load alert record and associated rule */
async function loadData(): Promise<void> {
  const id = Number(route.params.id)
  if (!id) {
    ElMessage.error(t('prism.record.detail.missingId'))
    loading.value = false
    return
  }
  loading.value = true
  try {
    const data = await getAlertRecord(id)
    record.value = data
    // Fetch associated rule details (fall back to empty on failure; the detail section shows a fallback prompt)
    try {
      rule.value = await getAlertRule(data.ruleId)
    } catch {
      rule.value = null
    }
  } catch {
    ElMessage.error(t('prism.record.detail.notFound'))
  } finally {
    loading.value = false
  }
}

/** Return to the list */
function handleBack(): void {
  router.push('/prism/record/list')
}

/** Click acknowledge */
function handleAcknowledgeClick(): void {
  acknowledgeVisible.value = true
}

/** Confirm acknowledge */
async function handleAcknowledgeConfirm(): Promise<void> {
  if (!record.value) return
  acknowledging.value = true
  try {
    const updated = await acknowledgeAlertRecord(record.value.id)
    record.value = updated
    acknowledgeVisible.value = false
    ElMessage.success(t('prism.record.detail.acknowledgedToast'))
  } catch {
    ElMessage.error(t('prism.record.detail.notFound'))
  } finally {
    acknowledging.value = false
  }
}

/** Click mark as resolved */
function handleResolveClick(): void {
  resolveVisible.value = true
}

/** Confirm marking as resolved */
async function handleResolveConfirm(): Promise<void> {
  if (!record.value) return
  resolving.value = true
  try {
    const updated = await resolveAlertRecord(record.value.id)
    record.value = updated
    resolveVisible.value = false
    ElMessage.success(t('prism.record.detail.resolvedToast'))
  } catch {
    ElMessage.error(t('prism.record.detail.notFound'))
  } finally {
    resolving.value = false
  }
}

onMounted(() => {
  loadData()
})
</script>

<template>
  <div
    v-loading="loading"
    class="tk-prism-record-detail tk-page-container"
  >
    <!-- Detail header: id + severity + status + actions -->
    <div
      v-if="record"
      class="tk-prism-record-detail__header"
    >
      <div class="tk-prism-record-detail__header-left">
        <span class="tk-prism-record-detail__header-id">
          <span class="tk-prism-record-detail__header-id-hash">#</span>{{ record.id }}
        </span>
        <span class="tk-prism-record-detail__header-sep" />
        <el-tag
          v-if="record.severity"
          :type="severityTagType"
          effect="light"
        >
          {{ severityText }}
        </el-tag>
        <StatusTag
          category="alert"
          :status="record.status as AlertStatus"
          show-icon
        />
      </div>
      <div class="tk-prism-record-detail__header-actions">
        <el-button @click="handleBack">
          &larr; {{ t('prism.record.detail.back') }}
        </el-button>
        <el-button
          v-if="isFiring"
          type="warning"
          @click="handleAcknowledgeClick"
        >
          {{ t('prism.record.detail.acknowledgeBtn') }}
        </el-button>
        <el-button
          v-if="isFiring || isAcknowledged"
          type="success"
          @click="handleResolveClick"
        >
          {{ t('prism.record.detail.resolveBtn') }}
        </el-button>
      </div>
    </div>

    <!-- Stat strip: alert message + key metrics -->
    <div
      v-if="record"
      class="tk-prism-record-detail__stat-strip"
    >
      <h2 class="tk-prism-record-detail__stat-message">
        {{ record.message }}
      </h2>
      <div class="tk-prism-record-detail__stat-grid">
        <div class="tk-prism-record-detail__stat-cell">
          <span class="tk-prism-record-detail__stat-label">
            {{ t('prism.record.detail.statValue') }}
          </span>
          <span
            class="tk-prism-record-detail__stat-value"
            :class="{ 'tk-prism-record-detail__stat-value--danger': isFiring }"
          >
            {{ record.value }}
          </span>
        </div>
        <div class="tk-prism-record-detail__stat-cell">
          <span class="tk-prism-record-detail__stat-label">
            {{ t('prism.record.detail.statSeverity') }}
          </span>
          <el-tag
            v-if="record.severity"
            :type="severityTagType"
            effect="light"
            size="small"
          >
            {{ severityText }}
          </el-tag>
          <span v-else>-</span>
        </div>
        <div class="tk-prism-record-detail__stat-cell">
          <span class="tk-prism-record-detail__stat-label">
            {{ t('prism.record.detail.statStatus') }}
          </span>
          <StatusTag
            category="alert"
            :status="record.status as AlertStatus"
            show-icon
          />
        </div>
        <div class="tk-prism-record-detail__stat-cell">
          <span class="tk-prism-record-detail__stat-label">
            {{ t('prism.record.detail.statFiredAt') }}
          </span>
          <span class="tk-prism-record-detail__stat-value">
            {{ formatTime(record.firedAt) }}
          </span>
        </div>
      </div>
    </div>

    <template v-if="record">
      <!-- Basic information -->
      <section class="tk-prism-record-detail__section">
        <h3 class="tk-prism-record-detail__section-title">
          {{ t('prism.record.detail.basicInfo') }}
        </h3>
        <el-descriptions
          :column="2"
          border
        >
          <el-descriptions-item :label="t('prism.record.detail.ruleName')">
            {{ record.ruleName }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('prism.record.detail.severity')">
            <el-tag
              v-if="record.severity"
              :type="severityTagType"
              effect="light"
            >
              {{ severityText }}
            </el-tag>
            <span v-else>-</span>
          </el-descriptions-item>
          <el-descriptions-item :label="t('prism.record.detail.value')">
            <span :class="{ 'tk-prism-record-detail__value--firing': isFiring }">
              {{ record.value }}
            </span>
          </el-descriptions-item>
          <el-descriptions-item :label="t('prism.record.detail.status')">
            <StatusTag
              category="alert"
              :status="record.status as AlertStatus"
              show-icon
            />
          </el-descriptions-item>
          <el-descriptions-item :label="t('prism.record.detail.firedAt')">
            {{ formatTime(record.firedAt) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('prism.record.detail.acknowledgedAt')">
            {{ formatTime(record.acknowledgedAt) }}
          </el-descriptions-item>
          <el-descriptions-item
            :label="t('prism.record.detail.message')"
            :span="2"
          >
            {{ record.message }}
          </el-descriptions-item>
          <el-descriptions-item
            :label="t('prism.record.detail.resolvedAt')"
            :span="2"
          >
            {{ formatTime(record.resolvedAt) }}
          </el-descriptions-item>
        </el-descriptions>
      </section>

      <!-- Associated rule details -->
      <section class="tk-prism-record-detail__section">
        <h3 class="tk-prism-record-detail__section-title">
          {{ t('prism.record.detail.ruleInfo') }}
        </h3>
        <el-descriptions
          v-if="rule"
          :column="2"
          border
        >
          <el-descriptions-item :label="t('prism.record.detail.ruleName')">
            {{ rule.name }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('prism.record.detail.scene')">
            <el-tag
              :type="sceneTagType(rule.scene)"
              effect="light"
              size="small"
            >
              {{ t(`prism.scene.${rule.scene}`) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('prism.record.detail.priority')">
            {{ rule.priority ?? 0 }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('prism.record.detail.enabled')">
            <StatusTag
              v-if="rule.enabled"
              category="alert"
              status="resolved"
            />
            <el-tag
              v-else
              type="info"
              effect="plain"
            >
              {{ t('prism.record.detail.disabled') }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item
            :label="t('prism.record.detail.expression')"
            :span="2"
          >
            <code class="tk-prism-record-detail__expr-code">
              {{ rule.expression }}
            </code>
          </el-descriptions-item>
          <el-descriptions-item
            v-if="rule.description"
            :label="t('prism.record.detail.description')"
            :span="2"
          >
            {{ rule.description }}
          </el-descriptions-item>
        </el-descriptions>
        <el-empty
          v-else
          :description="t('prism.record.detail.ruleNotFound')"
        />
      </section>

      <!-- Event timeline -->
      <section class="tk-prism-record-detail__section">
        <h3 class="tk-prism-record-detail__section-title">
          {{ t('prism.record.detail.timeline') }}
        </h3>
        <div class="tk-prism-record-detail__card">
          <el-timeline>
            <el-timeline-item
              type="danger"
              :timestamp="formatTime(record.firedAt)"
            >
              <strong>{{ t('prism.record.detail.fired') }}</strong>
              <span> — {{ record.message }}</span>
            </el-timeline-item>
            <el-timeline-item
              v-if="record.acknowledgedAt"
              type="warning"
              :timestamp="formatTime(record.acknowledgedAt)"
            >
              {{ t('prism.record.detail.acknowledgeEvent') }}
            </el-timeline-item>
            <el-timeline-item
              v-if="record.status === 'resolved' && record.resolvedAt"
              type="success"
              :timestamp="formatTime(record.resolvedAt)"
            >
              {{ t('prism.record.detail.resolveEvent') }}
            </el-timeline-item>
          </el-timeline>
        </div>
      </section>
    </template>

    <!-- Acknowledge confirmation dialog -->
    <ConfirmDialog
      v-model="acknowledgeVisible"
      :title="t('prism.record.detail.acknowledgeTitle')"
      :content="t('prism.record.detail.acknowledgeContent')"
      :loading="acknowledging"
      @confirm="handleAcknowledgeConfirm"
    />

    <!-- Mark as resolved confirmation dialog -->
    <ConfirmDialog
      v-model="resolveVisible"
      :title="t('prism.record.detail.resolveTitle')"
      :content="t('prism.record.detail.resolveContent')"
      :loading="resolving"
      @confirm="handleResolveConfirm"
    />
  </div>
</template>

<style scoped lang="scss">
.tk-prism-record-detail {
  &__header {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-md);
    align-items: center;
    justify-content: space-between;
    padding-bottom: var(--tk-spacing-lg);
    margin-bottom: var(--tk-spacing-lg);
    border-bottom: 1px solid var(--tk-border-color);
  }

  &__header-left {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-md);
    align-items: center;
  }

  &__header-id {
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-lg);
    font-weight: var(--tk-font-weight-bold);
    font-variant-numeric: tabular-nums;
    color: var(--tk-text-primary);
  }

  &__header-id-hash {
    margin-right: 2px;
    color: var(--tk-text-placeholder);
  }

  &__header-sep {
    width: 1px;
    height: 22px;
    background-color: var(--tk-border-color);
  }

  &__header-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-sm);
  }

  &__stat-strip {
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-md);
    padding: var(--tk-spacing-lg);
    margin-bottom: var(--tk-spacing-lg);
    background-color: var(--tk-bg-surface);
    border: 1px solid var(--tk-border-color);
    border-radius: var(--tk-radius-lg);
  }

  &__stat-message {
    margin: 0;
    font-size: var(--tk-font-size-xl);
    font-weight: var(--tk-font-weight-bold);
    line-height: 1.3;
    color: var(--tk-text-primary);
  }

  &__stat-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: var(--tk-spacing-lg);
  }

  &__stat-cell {
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-xs);
  }

  &__stat-label {
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  &__stat-value {
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-md);
    font-weight: var(--tk-font-weight-bold);
    font-variant-numeric: tabular-nums;
    color: var(--tk-text-primary);
  }

  &__stat-value--danger {
    color: var(--tk-danger-color-text);
  }

  &__section {
    margin-bottom: var(--tk-spacing-md);
  }

  &__section-title {
    padding-left: var(--tk-spacing-sm);
    margin: 0 0 var(--tk-spacing-sm);
    font-size: var(--tk-font-size-lg);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
    border-left: 3px solid var(--tk-primary-color);
  }

  &__value--firing {
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-danger-color-text);
  }

  &__expr-code {
    padding: var(--tk-spacing-xs) var(--tk-spacing-sm);
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-sm);
    word-break: break-all;
    background-color: var(--tk-bg-fill);
    border-radius: var(--tk-radius-sm);
  }

  &__card {
    padding: var(--tk-spacing-md);
    background-color: var(--tk-bg-surface);
    border-radius: var(--tk-radius-md);
  }
}
</style>
