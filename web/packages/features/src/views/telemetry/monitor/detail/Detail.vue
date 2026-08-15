// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Unified monitor point detail page.
 *
 * Shows different details based on the monitor mode:
 * - Active mode: shows prober config, last status, schedule
 * - Passive mode: shows webhook endpoint, auth config
 *
 * Replaces the separate prober detail and listener webhook pages.
 */
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Edit, Delete, InfoFilled, Document, CopyDocument, VideoPause, Refresh } from '@element-plus/icons-vue'
import { ConfirmDialog, PageEmpty, DataTable, usePermission } from '@tickraft/core'
import type { MonitorHistoryEntry, MonitorLog, MonitorPoint, MonitorStatus } from '../../../../types/telemetry'
import { getMonitor, deleteMonitor, getMonitorStatus, probeMonitor, getMonitorHistory, getMonitorLogs } from '../../../../api/telemetry'
import { formatDate } from '@tickraft/core'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { canDelete } = usePermission()

const loading = ref(false)
const detail = ref<MonitorPoint | null>(null)
const statusInfo = ref<MonitorStatus | null>(null)
const probing = ref(false)

const deleteVisible = ref(false)
const deleteLoading = ref(false)

/** Monitor history data */
const historyData = ref<MonitorHistoryEntry[]>([])
const historyLoading = ref(false)

/** Monitor logs data */
const logsData = ref<MonitorLog[]>([])
const logsLoading = ref(false)

/** Mode badge class mapping */
const MODE_BADGE_CLASS: Record<string, string> = {
  active: 'tk-mode-badge--active',
  passive: 'tk-mode-badge--passive',
}

/** Type badge class mapping */
const TYPE_BADGE_CLASS: Record<string, string> = {
  icmp: 'tk-type-badge--icmp',
  tcp: 'tk-type-badge--tcp',
  http: 'tk-type-badge--http',
  webhook: 'tk-type-badge--webhook',
  dns: 'tk-type-badge--dns',
  udp: 'tk-type-badge--udp',
  ssl: 'tk-type-badge--ssl',
}

function getMonitorId(): number {
  return Number(route.params.id) || 0
}

async function fetchDetail(): Promise<void> {
  loading.value = true
  try {
    const id = getMonitorId()
    const [res, statusRes] = await Promise.all([
      getMonitor(id),
      getMonitorStatus(id).catch(() => null),
    ])
    detail.value = res
    statusInfo.value = statusRes
    // Load history and logs after the monitor is available
    void fetchHistory()
    void fetchLogs()
  } catch {
    ElMessage.error(t('telemetry.monitor.detail.notFound', { id: getMonitorId() }))
    router.push('/telemetry/monitor/list')
  } finally {
    loading.value = false
  }
}

/** Parse config JSON for display */
const configParsed = computed<Record<string, unknown>>(() => {
  if (!detail.value?.config) return {}
  return detail.value.config
})

/** Whether this is an active monitor */
const isActive = computed(() => detail.value?.mode === 'active')

/** Status tag type */
function statusTagType(status: string): 'success' | 'warning' | 'danger' | 'info' {
  if (status === 'active') return 'success'
  if (status === 'inactive') return 'info'
  if (status === 'error') return 'danger'
  return 'info'
}

/** Status label */
function statusLabel(status: string): string {
  if (status === 'active') return t('telemetry.monitor.detail.statusActive')
  if (status === 'inactive') return t('telemetry.monitor.detail.statusInactive')
  if (status === 'error') return t('common.status.failed')
  return t('common.status.unknown')
}

/** History table columns */
const historyColumns = computed(() => [
  { prop: 'timestamp', label: t('telemetry.monitor.detail.historyTimestamp'), width: 200, slot: 'timestamp' },
  { prop: 'status', label: t('telemetry.monitor.detail.historyStatus'), minWidth: 120, slot: 'status' },
  { prop: 'value', label: t('telemetry.monitor.detail.historyValue'), minWidth: 200, slot: 'value' },
])

/** Logs table columns */
const logColumns = computed(() => [
  { prop: 'timestamp', label: t('telemetry.monitor.detail.logTimestamp'), width: 200, slot: 'logTimestamp' },
  { prop: 'level', label: t('telemetry.monitor.detail.logLevel'), width: 100, slot: 'logLevel' },
  { prop: 'message', label: t('telemetry.monitor.detail.logMessage'), minWidth: 300, slot: 'logMessage' },
])

/** Log level tag type */
function logLevelType(level: string): 'success' | 'warning' | 'danger' | 'info' {
  const normalized = level.toLowerCase()
  if (normalized === 'error' || normalized === 'fatal') return 'danger'
  if (normalized === 'warn' || normalized === 'warning') return 'warning'
  if (normalized === 'info') return 'info'
  if (normalized === 'debug' || normalized === 'trace') return 'info'
  return 'success'
}

/** Trigger an immediate probe */
async function handleProbe(): Promise<void> {
  if (!detail.value) return
  probing.value = true
  try {
    const result = await probeMonitor(detail.value.id)
    statusInfo.value = result
    ElMessage.success(t('telemetry.monitor.detail.probeSuccess'))
  } catch {
    // Errors are handled centrally by the interceptor
  } finally {
    probing.value = false
  }
}

/** Copy config JSON to clipboard */
async function handleCopyJson(): Promise<void> {
  if (!detail.value) return
  try {
    await navigator.clipboard.writeText(JSON.stringify(detail.value, null, 2))
    ElMessage.success(t('telemetry.monitor.detail.copySuccess'))
  } catch {
    ElMessage.error(t('common.app.failed'))
  }
}

function handleBack(): void {
  router.push('/telemetry/monitor/list')
}

function handleEdit(): void {
  if (!detail.value) return
  router.push(`/telemetry/monitor/edit/${detail.value.id}`)
}

function handleDelete(): void {
  deleteVisible.value = true
}

async function confirmDelete(): Promise<void> {
  deleteLoading.value = true
  try {
    await deleteMonitor(getMonitorId())
    ElMessage.success(t('telemetry.monitor.detail.deleteSuccess'))
    deleteVisible.value = false
    router.push('/telemetry/monitor/list')
  } catch {
    // Errors are handled centrally by the interceptor
  } finally {
    deleteLoading.value = false
  }
}

/** Fetch monitor history data (first page) */
async function fetchHistory(): Promise<void> {
  if (!detail.value) return
  historyLoading.value = true
  try {
    const res = await getMonitorHistory(detail.value.id, { page: 1, pageSize: 50 })
    historyData.value = Array.isArray(res?.items) ? res.items : []
  } catch {
    historyData.value = []
  } finally {
    historyLoading.value = false
  }
}

/** Fetch monitor logs data (first page) */
async function fetchLogs(): Promise<void> {
  if (!detail.value) return
  logsLoading.value = true
  try {
    const res = await getMonitorLogs(detail.value.id, { page: 1, pageSize: 50 })
    logsData.value = Array.isArray(res?.items) ? res.items : []
  } catch {
    logsData.value = []
  } finally {
    logsLoading.value = false
  }
}

/** Get config field display value */
function configField(key: string): string {
  const val = configParsed.value[key]
  if (val === undefined || val === null) return '-'
  return String(val)
}

onMounted(() => {
  void fetchDetail()
})
</script>

<template>
  <div
    v-loading="loading"
    class="tk-monitor-detail"
  >
    <template v-if="detail">
      <!-- Header -->
      <div class="tk-detail-header">
        <div class="tk-detail-header__title-row">
          <el-button circle class="tk-detail-header__back" @click="handleBack">
            <el-icon><ArrowLeft /></el-icon>
          </el-button>
          <div class="tk-detail-header__title-block">
            <div class="tk-detail-header__eyebrow">
              {{ t('telemetry.monitor.detail.eyebrowId', { id: detail.id }) }}
            </div>
            <div class="tk-detail-header__title-row2">
              <h1 class="tk-detail-header__title">{{ detail.name }}</h1>
              <span
                class="tk-mode-badge"
                :class="MODE_BADGE_CLASS[detail.mode] ?? 'tk-mode-badge--default'"
              >
                <span class="tk-mode-badge__dot" />
                {{ t(`telemetry.monitor.mode.${detail.mode}`) }}
              </span>
              <span
                class="tk-type-badge"
                :class="TYPE_BADGE_CLASS[detail.type] ?? 'tk-type-badge--default'"
              >
                <span class="tk-type-badge__dot" />
                {{ t(`telemetry.monitor.type.${detail.type}`, detail.type) }}
              </span>
              <el-tag :type="detail.enabled ? 'success' : 'info'" size="small">
                {{ detail.enabled ? t('common.app.enabled') : t('common.app.disabled') }}
              </el-tag>
            </div>
          </div>
        </div>
        <div class="tk-detail-header__actions">
          <el-button
            v-if="isActive"
            :loading="probing"
            @click="handleProbe"
          >
            <el-icon><VideoPause /></el-icon>
            {{ t('telemetry.monitor.detail.probeNow') }}
          </el-button>
          <el-button @click="handleEdit">
            <el-icon><Edit /></el-icon>
            {{ t('common.app.edit') }}
          </el-button>
          <el-button v-if="canDelete('device')" type="danger" @click="handleDelete">
            <el-icon><Delete /></el-icon>
            {{ t('common.app.delete') }}
          </el-button>
        </div>
      </div>

      <!-- Stat strip -->
      <div class="tk-stat-strip">
        <div class="tk-stat-tile">
          <span class="tk-stat-tile__accent" style="background: var(--tk-primary-color, #409eff)" />
          <span class="tk-stat-tile__label">{{ t('telemetry.monitor.detail.statMode') }}</span>
          <span class="tk-stat-tile__value tk-stat-tile__value--sm">{{ t(`telemetry.monitor.mode.${detail.mode}`) }}</span>
          <span class="tk-stat-tile__sub">{{ t('telemetry.monitor.detail.statModeSub') }}</span>
        </div>
        <div class="tk-stat-tile">
          <span class="tk-stat-tile__accent" style="background: var(--tk-success-color, #67c23a)" />
          <span class="tk-stat-tile__label">{{ t('telemetry.monitor.detail.statStatus') }}</span>
          <span class="tk-stat-tile__value tk-stat-tile__value--sm">
            <el-tag :type="statusTagType(statusInfo?.status ?? 'inactive')" size="small">
              {{ statusLabel(statusInfo?.status ?? 'inactive') }}
            </el-tag>
          </span>
          <span class="tk-stat-tile__sub">{{ t('telemetry.monitor.detail.statStatusSub') }}</span>
        </div>
        <div class="tk-stat-tile">
          <span class="tk-stat-tile__accent" style="background: var(--tk-warning-color, #e6a23c)" />
          <span class="tk-stat-tile__label">{{ t('telemetry.monitor.detail.statSchedule') }}</span>
          <span class="tk-stat-tile__value tk-stat-tile__value--sm">{{ detail.schedule }}</span>
          <span class="tk-stat-tile__sub">{{ t('telemetry.monitor.detail.statScheduleSub') }}</span>
        </div>
        <div class="tk-stat-tile">
          <span class="tk-stat-tile__accent" style="background: var(--tk-info-color, #909399)" />
          <span class="tk-stat-tile__label">{{ t('telemetry.monitor.detail.statUpdatedAt') }}</span>
          <span class="tk-stat-tile__value tk-stat-tile__value--sm">{{ detail.updatedAt }}</span>
          <span class="tk-stat-tile__sub">{{ t('telemetry.monitor.detail.statUpdatedAtSub') }}</span>
        </div>
      </div>

      <!-- Basic info card -->
      <div class="tk-detail-card">
        <div class="tk-detail-card__header">
          <div class="tk-detail-card__title">
            <el-icon><InfoFilled /></el-icon>
            <span>{{ t('telemetry.monitor.detail.basicInfo') }}</span>
          </div>
          <span class="tk-detail-card__hint">{{ t('telemetry.monitor.detail.basicInfoHint') }}</span>
        </div>
        <div class="tk-detail-card__body">
          <div class="tk-descriptions">
            <div class="tk-desc-item">
              <span class="tk-desc-item__label">{{ t('telemetry.monitor.detail.monitorId') }}</span>
              <span class="tk-desc-item__value tk-desc-item__value--mono">{{ detail.id }}</span>
            </div>
            <div class="tk-desc-item">
              <span class="tk-desc-item__label">{{ t('telemetry.monitor.detail.name') }}</span>
              <span class="tk-desc-item__value">{{ detail.name }}</span>
            </div>
            <div class="tk-desc-item">
              <span class="tk-desc-item__label">{{ t('telemetry.monitor.detail.mode') }}</span>
              <span
                class="tk-mode-badge"
                :class="MODE_BADGE_CLASS[detail.mode] ?? 'tk-mode-badge--default'"
              >
                <span class="tk-mode-badge__dot" />
                {{ t(`telemetry.monitor.mode.${detail.mode}`) }}
              </span>
            </div>
            <div class="tk-desc-item">
              <span class="tk-desc-item__label">{{ t('telemetry.monitor.detail.type') }}</span>
              <span
                class="tk-type-badge"
                :class="TYPE_BADGE_CLASS[detail.type] ?? 'tk-type-badge--default'"
              >
                <span class="tk-type-badge__dot" />
                {{ t(`telemetry.monitor.type.${detail.type}`, detail.type) }}
              </span>
            </div>
            <div class="tk-desc-item">
              <span class="tk-desc-item__label">{{ t('telemetry.monitor.detail.schedule') }}</span>
              <span class="tk-desc-item__value tk-desc-item__value--mono">{{ detail.schedule }}</span>
            </div>
            <div class="tk-desc-item">
              <span class="tk-desc-item__label">{{ t('telemetry.monitor.detail.enableStatus') }}</span>
              <el-tag :type="detail.enabled ? 'success' : 'info'" size="small">
                {{ detail.enabled ? t('common.app.enabled') : t('common.app.disabled') }}
              </el-tag>
            </div>
            <div v-if="detail.description" class="tk-desc-item">
              <span class="tk-desc-item__label">{{ t('telemetry.monitor.detail.description') }}</span>
              <span class="tk-desc-item__value">{{ detail.description }}</span>
            </div>
            <div class="tk-desc-item">
              <span class="tk-desc-item__label">{{ t('telemetry.monitor.detail.assetType') }}</span>
              <span class="tk-desc-item__value">{{ detail.assetType }}</span>
            </div>
          </div>

          <!-- Config details -->
          <div class="tk-detail-card__extra">
            <div class="tk-desc-item">
              <span class="tk-desc-item__label">{{ t('telemetry.monitor.detail.config') }}</span>
              <pre class="tk-json-view">{{ JSON.stringify(configParsed, null, 2) }}</pre>
            </div>
          </div>

          <!-- Type-specific config fields -->
          <div v-if="Object.keys(configParsed).length > 0" class="tk-detail-card__extra">
            <div class="tk-config-fields">
              <!-- Active: ICMP config -->
              <template v-if="detail.type === 'icmp'">
                <div class="tk-config-field">
                  <span class="tk-config-field__label">{{ t('telemetry.monitor.create.targetHost') }}</span>
                  <span class="tk-config-field__value">{{ configField('host') }}</span>
                </div>
                <div class="tk-config-field">
                  <span class="tk-config-field__label">{{ t('telemetry.monitor.create.pingCount') }}</span>
                  <span class="tk-config-field__value">{{ configField('count') }}</span>
                </div>
              </template>

              <!-- Active: TCP config -->
              <template v-if="detail.type === 'tcp'">
                <div class="tk-config-field">
                  <span class="tk-config-field__label">{{ t('telemetry.monitor.create.targetHost') }}</span>
                  <span class="tk-config-field__value">{{ configField('host') }}</span>
                </div>
                <div class="tk-config-field">
                  <span class="tk-config-field__label">{{ t('telemetry.monitor.create.targetPort') }}</span>
                  <span class="tk-config-field__value">{{ configField('port') }}</span>
                </div>
              </template>

              <!-- Active: HTTP config -->
              <template v-if="detail.type === 'http'">
                <div class="tk-config-field">
                  <span class="tk-config-field__label">{{ t('telemetry.monitor.create.requestMethod') }}</span>
                  <span class="tk-config-field__value">{{ configField('method') }}</span>
                </div>
                <div class="tk-config-field">
                  <span class="tk-config-field__label">{{ t('telemetry.monitor.create.requestUrl') }}</span>
                  <span class="tk-config-field__value">{{ configField('url') }}</span>
                </div>
                <div class="tk-config-field">
                  <span class="tk-config-field__label">{{ t('telemetry.monitor.create.expectCode') }}</span>
                  <span class="tk-config-field__value">{{ configField('expectCode') }}</span>
                </div>
              </template>

              <!-- Passive: Webhook config -->
              <template v-if="detail.type === 'webhook'">
                <div class="tk-config-field">
                  <span class="tk-config-field__label">{{ t('telemetry.monitor.create.authMethod') }}</span>
                  <span class="tk-config-field__value">{{ configField('authType') }}</span>
                </div>
              </template>
            </div>
          </div>
        </div>
      </div>

      <!-- Raw config card -->
      <div class="tk-detail-card">
        <div class="tk-detail-card__header">
          <div class="tk-detail-card__title">
            <el-icon><Document /></el-icon>
            <span>{{ t('telemetry.monitor.detail.rawConfig') }}</span>
          </div>
          <el-button link type="primary" size="small" @click="handleCopyJson">
            <el-icon><CopyDocument /></el-icon>
            {{ t('telemetry.monitor.detail.copyJson') }}
          </el-button>
        </div>
        <div class="tk-detail-card__body">
          <pre class="tk-json-view">{{ JSON.stringify(detail, null, 2) }}</pre>
        </div>
      </div>

      <!-- History card -->
      <div class="tk-detail-card">
        <div class="tk-detail-card__header">
          <div class="tk-detail-card__title">
            <el-icon><InfoFilled /></el-icon>
            <span>{{ t('telemetry.monitor.detail.history') }}</span>
          </div>
          <el-button
            link
            type="primary"
            size="small"
            :loading="historyLoading"
            @click="fetchHistory"
          >
            <el-icon><Refresh /></el-icon>
            {{ t('telemetry.monitor.list.refresh') }}
          </el-button>
        </div>
        <div class="tk-detail-card__body">
          <DataTable
            :data="historyData"
            :columns="historyColumns"
            :loading="historyLoading"
            :pagination="false"
            density="compact"
          >
            <template #timestamp="{ row }">
              <span class="tk-mono-text">{{ formatDate(row.timestamp) }}</span>
            </template>
            <template #status="{ row }">
              <el-tag :type="row.status === 'success' ? 'success' : row.status === 'error' ? 'danger' : 'info'" size="small">
                {{ row.status }}
              </el-tag>
            </template>
            <template #value="{ row }">
              <span class="tk-mono-text">{{ JSON.stringify(row.value) }}</span>
            </template>
          </DataTable>
          <PageEmpty
            v-if="!historyLoading && historyData.length === 0"
            :description="t('telemetry.monitor.detail.historyEmpty')"
          />
        </div>
      </div>

      <!-- Logs card -->
      <div class="tk-detail-card">
        <div class="tk-detail-card__header">
          <div class="tk-detail-card__title">
            <el-icon><Document /></el-icon>
            <span>{{ t('telemetry.monitor.detail.logs') }}</span>
          </div>
          <el-button
            link
            type="primary"
            size="small"
            :loading="logsLoading"
            @click="fetchLogs"
          >
            <el-icon><Refresh /></el-icon>
            {{ t('telemetry.monitor.list.refresh') }}
          </el-button>
        </div>
        <div class="tk-detail-card__body">
          <DataTable
            :data="logsData"
            :columns="logColumns"
            :loading="logsLoading"
            :pagination="false"
            density="compact"
          >
            <template #logTimestamp="{ row }">
              <span class="tk-mono-text">{{ formatDate(row.timestamp) }}</span>
            </template>
            <template #logLevel="{ row }">
              <el-tag :type="logLevelType(row.level)" size="small">
                {{ row.level }}
              </el-tag>
            </template>
            <template #logMessage="{ row }">
              <span>{{ row.message }}</span>
            </template>
          </DataTable>
          <PageEmpty
            v-if="!logsLoading && logsData.length === 0"
            :description="t('telemetry.monitor.detail.logsEmpty')"
          />
        </div>
      </div>
    </template>

    <PageEmpty
      v-else-if="!loading"
      :description="t('telemetry.monitor.detail.notFound', { id: getMonitorId() })"
    />

    <!-- Delete confirmation dialog -->
    <ConfirmDialog
      v-model="deleteVisible"
      :title="t('telemetry.monitor.detail.deleteTitle')"
      :content="t('telemetry.monitor.detail.deleteContent', { name: detail?.name ?? '', type: detail ? t(`telemetry.monitor.type.${detail.type}`, detail.type) : '' })"
      :loading="deleteLoading"
      type="danger"
      @confirm="confirmDelete"
    />
  </div>
</template>

<style scoped lang="scss">
.tk-monitor-detail {
  max-width: var(--tk-content-max-width, 1200px);
  padding: var(--tk-spacing-lg, 40px) var(--tk-content-padding-x, 24px) var(--tk-spacing-xl, 96px);
  margin: 0 auto;
}

/* Header */
.tk-detail-header {
  display: flex;
  flex-wrap: wrap;
  gap: var(--tk-spacing-lg, 32px);
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: var(--tk-spacing-lg, 32px);

  &__title-row { display: flex; gap: var(--tk-spacing-md, 20px); align-items: center; min-width: 0; }
  &__back { flex-shrink: 0; }
  &__title-block { min-width: 0; }

  &__eyebrow {
    margin-bottom: 4px;
    font-family: var(--tk-font-mono, 'Monaco', monospace);
    font-size: var(--tk-font-size-xs, 12px);
    color: var(--tk-text-secondary, #909399);
    text-transform: uppercase;
    letter-spacing: 0.1em;
  }
  &__title-row2 { display: flex; flex-wrap: wrap; gap: var(--tk-spacing-sm, 16px); align-items: center; }

  &__title {
    margin: 0;
    font-size: var(--tk-font-size-2xl, 24px);
    font-weight: var(--tk-font-weight-bold, 700);
    line-height: 1.1;
    color: var(--tk-text-primary, #303133);
    letter-spacing: -0.02em;
  }
  &__actions { display: flex; gap: var(--tk-spacing-sm, 12px); align-items: center; }
}

/* Stat strip */
.tk-stat-strip {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--tk-spacing-sm, 16px);
  margin-bottom: var(--tk-spacing-lg, 32px);
}

.tk-stat-tile {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: var(--tk-spacing-md, 20px) var(--tk-spacing-lg, 24px);
  overflow: hidden;
  background: var(--tk-bg-surface, var(--tk-bg-color, #fff));
  border: 1px solid var(--tk-border-color, #e4e7ed);
  border-radius: var(--tk-radius-lg, 12px);

  &__accent {
    position: absolute;
    top: -12px;
    right: -12px;
    width: 56px;
    height: 56px;
    pointer-events: none;
    border-radius: 50%;
    opacity: 0.08;
  }

  &__label {
    font-family: var(--tk-font-mono, 'Monaco', monospace);
    font-size: var(--tk-font-size-xs, 12px);
    color: var(--tk-text-secondary, #909399);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  &__value {
    font-size: var(--tk-font-size-xl, 20px);
    font-weight: var(--tk-font-weight-bold, 700);
    font-variant-numeric: tabular-nums;
    line-height: 1.1;
    color: var(--tk-text-primary, #303133);

    &--sm { font-size: var(--tk-font-size-sm, 14px); }
  }

  &__sub { font-size: var(--tk-font-size-xs, 12px); color: var(--tk-text-secondary, #909399); }
}

/* Detail card */
.tk-detail-card {
  margin-bottom: var(--tk-spacing-md, 24px);
  overflow: hidden;
  background: var(--tk-bg-surface, var(--tk-bg-color, #fff));
  border: 1px solid var(--tk-border-color, #e4e7ed);
  border-radius: var(--tk-radius-lg, 12px);

  &__header {
    display: flex;
    gap: var(--tk-spacing-md, 24px);
    align-items: center;
    justify-content: space-between;
    padding: var(--tk-spacing-md, 20px) var(--tk-spacing-lg, 40px);
    background: var(--tk-bg-fill-light, #f5f7fa);
    border-bottom: 1px solid var(--tk-border-color-light, #ebeef5);
  }
  &__title { display: flex; gap: var(--tk-spacing-sm, 16px); align-items: center; font-size: var(--tk-font-size-md, 16px); font-weight: var(--tk-font-weight-semibold, 600); color: var(--tk-text-primary, #303133); }
  &__hint { font-family: var(--tk-font-mono, 'Monaco', monospace); font-size: var(--tk-font-size-xs, 12px); color: var(--tk-text-secondary, #909399); text-transform: uppercase; letter-spacing: 0.05em; }
  &__body { padding: var(--tk-spacing-lg, 40px); }
  &__extra { margin-top: var(--tk-spacing-lg, 32px); }
}

/* Descriptions */
.tk-descriptions { display: grid; grid-template-columns: repeat(2, 1fr); gap: var(--tk-spacing-md, 24px) var(--tk-spacing-xl, 48px); }

.tk-desc-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding-bottom: var(--tk-spacing-sm, 16px);
  border-bottom: 1px dashed var(--tk-border-color-lighter, #f0f0f0);

  &__label { font-family: var(--tk-font-mono, 'Monaco', monospace); font-size: var(--tk-font-size-xs, 12px); color: var(--tk-text-secondary, #909399); text-transform: uppercase; letter-spacing: 0.05em; }

  &__value { font-size: var(--tk-font-size-sm, 14px); color: var(--tk-text-primary, #303133); word-break: break-all;
    &--mono { font-family: var(--tk-font-mono, 'Monaco', monospace); font-variant-numeric: tabular-nums; }
  }
}

/* Config fields */
.tk-config-fields {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: var(--tk-spacing-md, 24px);
}

.tk-config-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: var(--tk-spacing-sm, 12px) var(--tk-spacing-md, 16px);
  background: var(--tk-bg-fill-blank, #fafafa);
  border-radius: var(--tk-radius-md, 8px);

  &__label { font-size: var(--tk-font-size-xs, 12px); color: var(--tk-text-secondary, #909399); }
  &__value { font-family: var(--tk-font-mono, 'Monaco', monospace); font-size: var(--tk-font-size-sm, 14px); color: var(--tk-text-primary, #303133); }
}

/* Mode badge */
.tk-mode-badge {
  display: inline-flex;
  gap: var(--tk-spacing-xs, 4px);
  align-items: center;
  padding: 1px var(--tk-spacing-sm, 16px);
  font-family: var(--tk-font-mono, 'Monaco', monospace);
  font-size: var(--tk-font-size-xs, 12px);
  font-weight: var(--tk-font-weight-medium, 500);
  border: 1px solid transparent;
  border-radius: var(--tk-radius-sm, 4px);

  &__dot { flex-shrink: 0; width: 6px; height: 6px; background: currentcolor; border-radius: 50%; }

  &--active { color: var(--tk-primary-color); background: var(--tk-primary-color-light-9); border-color: var(--tk-primary-color-light-7); }
  &--passive { color: var(--tk-success-color); background: var(--tk-success-color-light-9); border-color: var(--tk-success-color-light-7); }
  &--default { color: var(--tk-text-secondary, #909399); background: var(--tk-bg-fill, #f5f7fa); border-color: var(--tk-border-color, #e4e7ed); }
}

/* Type badge */
.tk-type-badge {
  display: inline-flex;
  gap: var(--tk-spacing-xs, 4px);
  align-items: center;
  padding: 1px var(--tk-spacing-sm, 16px);
  font-family: var(--tk-font-mono, 'Monaco', monospace);
  font-size: var(--tk-font-size-xs, 12px);
  font-weight: var(--tk-font-weight-medium, 500);
  border: 1px solid transparent;
  border-radius: var(--tk-radius-sm, 4px);

  &__dot { flex-shrink: 0; width: 6px; height: 6px; background: currentcolor; border-radius: 50%; }

  &--icmp { color: var(--tk-info-color); background: var(--tk-info-color-light-9); border-color: var(--tk-info-color-light-7); }
  &--tcp { color: var(--tk-success-color); background: var(--tk-success-color-light-9); border-color: var(--tk-success-color-light-7); }
  &--http { color: var(--tk-primary-color); background: var(--tk-primary-color-light-9); border-color: var(--tk-primary-color-light-7); }
  &--webhook { color: var(--tk-warning-color); background: var(--tk-warning-color-light-9); border-color: var(--tk-warning-color-light-7); }
  &--dns { color: var(--tk-warning-color); background: var(--tk-warning-color-light-9); border-color: var(--tk-warning-color-light-7); }
  &--ssl { color: var(--tk-danger-color); background: var(--tk-danger-color-light-9); border-color: var(--tk-danger-color-light-7); }
  &--default { color: var(--tk-text-secondary, #909399); background: var(--tk-bg-fill, #f5f7fa); border-color: var(--tk-border-color, #e4e7ed); }
}

/* JSON view */
.tk-json-view {
  padding: var(--tk-spacing-md, 20px);
  margin: 0;
  font-family: var(--tk-font-mono, 'Monaco', monospace);
  font-size: var(--tk-font-size-sm, 14px);
  line-height: 1.5;
  color: var(--tk-text-primary, #303133);
  word-break: break-all;
  white-space: pre-wrap;
  background: var(--tk-bg-fill-blank, #fafafa);
  border: 1px solid var(--tk-border-color, #e4e7ed);
  border-radius: var(--tk-radius-md, 8px);
}

/* Monospace text for table cells */
.tk-mono-text {
  font-family: var(--tk-font-mono, 'Monaco', monospace);
  font-size: var(--tk-font-size-xs, 12px);
  font-variant-numeric: tabular-nums;
  color: var(--tk-text-regular, #606266);
}

/* Responsive */
@media (max-width: 960px) {
  .tk-stat-strip { grid-template-columns: repeat(2, 1fr); }
  .tk-descriptions { grid-template-columns: 1fr; }
  .tk-config-fields { grid-template-columns: 1fr; }
}
</style>
