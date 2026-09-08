// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Delivery record page (extension-enhanced)
 *
 * Aligns with prototype storyboard/tickraft-x/pages/prism-channel-delivery.html:
 * - Search area: channel filter / status filter / alert title search / time range (with today/7d quick options)
 * - Delivery record table: DataTable + inline actions (detail/retry)
 * - Detail drawer: basic info / request detail / response detail / retry history (split into DeliveryDetailDrawer sub-component)
 * - Retry button: only failed status can be retried, with confirmation
 * - Export CSV button
 *
 * Data logic is extracted to composables.ts; detail drawer is extracted to components/DeliveryDetailDrawer.vue.
 */
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { DataTable } from '@tickraft/core'
import type { ChannelConfig, DeliveryRecord, DeliveryStatus } from '../../../../api/channel'
import { getChannels } from '../../../../api/channel'
import { useDeliveries, getChannelIcon } from '../composables'
import DeliveryDetailDrawer from '../components/DeliveryDetailDrawer.vue'

const { t } = useI18n()
const route = useRoute()

const routeChannelId = computed(() => {
  const id = route.params.id
  return id ? Number(id) : 0
})

const {
  records, total, loading, page, size, retryingId, filters,
  loadDeliveries, handleSearch, handleReset, handlePageChange, handleRetry, handleExport,
} = useDeliveries()

/* ── Channel list (for filter dropdown) ── */
const channels = ref<ChannelConfig[]>([])
const channelOptions = computed(() => [
  { value: 0, label: t('prism.channel.delivery.allChannels') },
  ...channels.value.map((ch) => ({ value: ch.id, label: ch.name })),
])

const statusOptions = computed(() => [
  { value: '', label: t('prism.channel.delivery.all') },
  { value: 'success', label: t('prism.channel.delivery.success') },
  { value: 'failed', label: t('prism.channel.delivery.failed') },
])

/* ── Quick time range ── */
const activeQuickRange = ref('')

function fmtDate(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function setQuickRange(range: 'today' | '7d'): void {
  activeQuickRange.value = range
  const now = new Date()
  filters.endTime = fmtDate(now)
  if (range === 'today') {
    filters.startTime = fmtDate(now)
  } else {
    const from = new Date(now)
    from.setDate(from.getDate() - 6)
    filters.startTime = fmtDate(from)
  }
}

function onDateChange(): void {
  activeQuickRange.value = ''
}

function onChannelChange(): void {
  handleSearch()
}

function onReset(): void {
  activeQuickRange.value = ''
  handleReset()
}

/* ── Detail drawer ── */
const drawerVisible = ref(false)
const drawerRecord = ref<DeliveryRecord | null>(null)

function openDrawer(row: DeliveryRecord): void {
  drawerRecord.value = row
  drawerVisible.value = true
}

async function drawerRetry(): Promise<void> {
  if (!drawerRecord.value) return
  const ok = await handleRetry(drawerRecord.value)
  if (ok) drawerVisible.value = false
}

/* ── Table formatting helpers (used in table slots; drawer formatting is in the sub-component) ── */
function formatTime(iso: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function formatDuration(ms: number): string {
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`
}

function statusTagType(status: DeliveryStatus): 'success' | 'danger' {
  return status === 'success' ? 'success' : 'danger'
}

/* ── Table column definitions ── */
const tableColumns = computed(() => [
  { prop: 'sent_at', label: t('prism.channel.delivery.sentAt'), width: '170px', slot: 'sentAt' },
  { prop: 'channel_name', label: t('prism.channel.delivery.channelName'), width: '140px', slot: 'channelName' },
  { prop: 'channel_type', label: t('prism.channel.delivery.channelType'), width: '120px', slot: 'channelType' },
  { prop: 'alert_title', label: t('prism.channel.delivery.alertTitle'), minWidth: '160px' },
  { prop: 'status', label: t('prism.channel.delivery.status'), width: '100px', slot: 'status' },
  { prop: 'durationMs', label: t('prism.channel.delivery.duration'), width: '100px', slot: 'duration' },
])

/* ── Toolbar ── */
function handleRefresh(): void {
  void loadDeliveries(filters.channelId)
  ElMessage.success(t('prism.channel.list.dataRefreshed'))
}

/* ── Initialization ── */
async function loadChannelsList(): Promise<void> {
  try {
    const res = await getChannels()
    channels.value = res.items ?? []
  } catch (e) {
    ElMessage.error((e as Error).message)
  }
}

onMounted(async () => {
  await loadChannelsList()
  filters.channelId = routeChannelId.value
  setQuickRange('7d')
  handleSearch()
})
</script>

<template>
  <div class="tk-delivery-page tk-page-container">
    <div class="tk-page-header tk-flex-between">
      <h2 class="tk-page-title">
        {{ $t('prism.channel.delivery.title') }}
      </h2>
    </div>

    <!-- Search area -->
    <div
      class="tk-delivery-search"
      role="search"
      aria-label="Delivery record search"
    >
      <div class="tk-delivery-search__row">
        <div class="tk-delivery-search__field">
          <label>{{ $t('prism.channel.delivery.filterChannel') }}</label>
          <el-select
            v-model="filters.channelId"
            @change="onChannelChange"
          >
            <el-option
              v-for="opt in channelOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
        </div>
        <div class="tk-delivery-search__field">
          <label>{{ $t('prism.channel.delivery.filterStatus') }}</label>
          <el-select v-model="filters.status">
            <el-option
              v-for="opt in statusOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
        </div>
        <div class="tk-delivery-search__field">
          <label>{{ $t('prism.channel.delivery.alertTitle') }}</label>
          <el-input
            v-model="filters.alertTitle"
            :placeholder="$t('prism.channel.delivery.alertTitlePlaceholder')"
            @keyup.enter="handleSearch"
          />
        </div>
        <div class="tk-delivery-search__actions">
          <el-button @click="onReset">
            {{ $t('prism.channel.delivery.reset') }}
          </el-button>
          <el-button
            type="primary"
            @click="handleSearch"
          >
            {{ $t('prism.channel.delivery.search') }}
          </el-button>
        </div>
      </div>
      <div class="tk-delivery-search__extra">
        <div class="tk-delivery-search__field">
          <label>{{ $t('prism.channel.delivery.timeRange') }}</label>
          <div class="tk-delivery-date-range">
            <el-date-picker
              v-model="filters.startTime"
              type="date"
              :placeholder="$t('prism.channel.delivery.dateFrom')"
              value-format="YYYY-MM-DD"
              @change="onDateChange"
            />
            <span class="tk-delivery-date-range__sep">-</span>
            <el-date-picker
              v-model="filters.endTime"
              type="date"
              :placeholder="$t('prism.channel.delivery.dateTo')"
              value-format="YYYY-MM-DD"
              @change="onDateChange"
            />
          </div>
        </div>
        <div class="tk-delivery-quick-range">
          <span
            class="tk-delivery-quick-range__btn"
            :class="{ 'tk-delivery-quick-range__btn--active': activeQuickRange === 'today' }"
            @click="setQuickRange('today')"
          >{{ $t('prism.channel.delivery.today') }}</span>
          <span
            class="tk-delivery-quick-range__btn"
            :class="{ 'tk-delivery-quick-range__btn--active': activeQuickRange === '7d' }"
            @click="setQuickRange('7d')"
          >{{ $t('prism.channel.delivery.last7d') }}</span>
        </div>
      </div>
    </div>

    <!-- Toolbar -->
    <div class="tk-toolbar tk-flex-between">
      <el-button @click="handleRefresh">
        {{ $t('prism.channel.delivery.refresh') }}
      </el-button>
      <el-button @click="handleExport">
        {{ $t('prism.channel.delivery.export') }}
      </el-button>
    </div>

    <!-- Delivery list -->
    <DataTable
      :data="records"
      :columns="tableColumns"
      :loading="loading"
      row-key="id"
      :total="total"
      :page="page"
      :size="size"
      :size-options="[10, 20, 50]"
      aria-label="Delivery record list"
      @page-change="handlePageChange"
    >
      <template #sentAt="{ row }">
        <span class="tk-delivery-mono">{{ formatTime((row as DeliveryRecord).sentAt) }}</span>
      </template>
      <template #channelName="{ row }">
        <span class="tk-delivery-channel-name">
          <span class="tk-delivery-channel-icon">{{ getChannelIcon((row as DeliveryRecord).channelType) }}</span>
          {{ (row as DeliveryRecord).channelName }}
        </span>
      </template>
      <template #channelType="{ row }">
        <el-tag size="small">
          {{ $t(`prism.channel.type.${(row as DeliveryRecord).channelType}`) }}
        </el-tag>
      </template>
      <template #status="{ row }">
        <el-tag
          :type="statusTagType((row as DeliveryRecord).status)"
          size="small"
        >
          {{ $t(`prism.channel.delivery.${(row as DeliveryRecord).status}`) }}
        </el-tag>
      </template>
      <template #duration="{ row }">
        {{ formatDuration((row as DeliveryRecord).durationMs) }}
      </template>
      <template #action-column>
        <el-table-column
          :label="$t('prism.channel.delivery.actions')"
          width="140"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              size="small"
              aria-label="View delivery detail"
              @click="openDrawer(row as DeliveryRecord)"
            >
              {{ $t('prism.channel.delivery.detail') }}
            </el-button>
            <el-button
              v-if="(row as DeliveryRecord).status === 'failed'"
              link
              type="danger"
              size="small"
              aria-label="Retry delivery"
              :loading="retryingId === (row as DeliveryRecord).id"
              @click="handleRetry(row as DeliveryRecord)"
            >
              {{ $t('prism.channel.delivery.retry') }}
            </el-button>
          </template>
        </el-table-column>
      </template>
      <template #empty>
        <el-empty :description="$t('prism.channel.delivery.emptyDescription')" />
      </template>
    </DataTable>

    <!-- Detail drawer (sub-component) -->
    <DeliveryDetailDrawer
      v-model:visible="drawerVisible"
      :record="drawerRecord"
      @retry="drawerRetry"
    />
  </div>
</template>

<style scoped lang="scss">
.tk-delivery-search {
  padding: var(--tk-spacing-md);
  margin-bottom: var(--tk-spacing-md);
  background-color: var(--tk-bg-color);
  border: 1px solid var(--tk-border-color-lighter);
  border-radius: var(--tk-border-radius-md);
}

.tk-delivery-search__row {
  display: flex;
  flex-wrap: wrap;
  gap: var(--tk-spacing-md);
  align-items: flex-end;
}

.tk-delivery-search__field {
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-xs);

  label {
    font-size: var(--tk-font-size-sm);
    color: var(--tk-text-secondary);
  }
}

.tk-delivery-search__actions {
  display: flex;
  gap: var(--tk-spacing-sm);
}

.tk-delivery-search__extra {
  display: flex;
  flex-wrap: wrap;
  gap: var(--tk-spacing-md);
  align-items: flex-end;
  margin-top: var(--tk-spacing-md);
}

.tk-delivery-date-range {
  display: flex;
  gap: var(--tk-spacing-xs);
  align-items: center;

  &__sep { color: var(--tk-text-secondary); }
}

.tk-delivery-quick-range {
  display: flex;
  gap: var(--tk-spacing-xs);
  align-items: center;
}

.tk-delivery-quick-range__btn {
  padding: 4px var(--tk-spacing-sm);
  font-size: var(--tk-font-size-xs);
  color: var(--tk-text-regular);
  cursor: pointer;
  user-select: none;
  border: 1px solid var(--tk-border-color-light);
  border-radius: var(--tk-border-radius-base);
  transition: all var(--tk-animation-duration-base) var(--tk-ease-in-out);

  &:hover {
    color: var(--tk-primary-color-text);
    border-color: var(--tk-primary-color);
  }

  &--active {
    color: var(--tk-primary-color-text);
    background-color: var(--tk-primary-color-light-9);
    border-color: var(--tk-primary-color);
  }
}

.tk-delivery-mono {
  font-family: var(--tk-font-family-mono);
  font-size: var(--tk-font-size-xs);
}

.tk-delivery-channel-name {
  display: inline-flex;
  gap: 4px;
  align-items: center;
}

.tk-delivery-channel-icon {
  font-size: 14px;
}
</style>
