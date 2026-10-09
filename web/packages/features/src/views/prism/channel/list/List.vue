// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Notification channel list page (extension-enhanced)
 *
 * Aligns with prototype storyboard/tickraft-x/pages/prism-channel-list.html:
 * - Card view (default): grouped by channel type, each card contains icon / name / status / last test / toggle switch
 * - Table view: standard table display
 * - Test receipt: dialog showing response_code / latency / request_id
 * - Enable toggle: with confirmation
 * - Delete: with confirmation (warns that associated rules will be removed)
 *
 * Data logic is extracted to composables.ts so this file can focus on UI rendering.
 */
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { DataTable } from '@tickraft/core'
import type { ChannelConfig } from '../../../../api/channel'
import { useChannels } from '../composables'

const { t } = useI18n()
const router = useRouter()
const {
  channels, loading, testingId, lastTestResult, lastTestChannel,
  groupedChannels, getIcon,
  loadChannels, handleToggle, handleDelete, handleTest,
} = useChannels()

const currentView = ref<'card' | 'table'>('card')
const testReceiptVisible = ref(false)

/* ── Constant mappings ── */
const STATUS_TAG_TYPE: Record<string, 'success' | 'info'> = {
  true: 'success', false: 'info',
}

function formatTime(iso: string): string {
  if (!iso) return t('prism.channel.list.notTested')
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function testResultTag(result: string): 'success' | 'danger' | 'info' {
  if (result === 'success') return 'success'
  if (result === 'failed') return 'danger'
  return 'info'
}

function testResultMark(result: string): string {
  if (result === 'success') return '✓'
  if (result === 'failed') return '✗'
  return '-'
}

const tableColumns = computed(() => [
  { prop: 'name', label: t('prism.channel.list.name'), minWidth: '160px' },
  { prop: 'type', label: t('prism.channel.list.type'), width: '120px', slot: 'type' },
  { prop: 'enabled', label: t('prism.channel.list.status'), width: '100px', slot: 'enabled' },
  { prop: 'last_test_at', label: t('prism.channel.list.lastTest'), width: '170px', slot: 'lastTest' },
  { prop: 'actions', label: t('prism.channel.list.actions'), width: '280px', fixed: 'right' as const, slot: 'actions' },
])

function handleEdit(row: ChannelConfig): void {
  router.push(`/prism/channel/form/${row.id}`)
}
function handleCreate(): void {
  router.push('/prism/channel/form')
}
function handleDelivery(row: ChannelConfig): void {
  router.push(`/prism/channel/delivery/${row.id}`)
}

async function onTest(row: ChannelConfig): Promise<void> {
  await handleTest(row)
  testReceiptVisible.value = true
}

onMounted(() => {
  void loadChannels()
})
</script>

<template>
  <div class="tk-channel-list tk-page-container">
    <div class="tk-toolbar tk-flex-between">
      <div class="tk-channel-view-toggle">
        <span
          class="tk-channel-view-toggle__btn"
          :class="{ 'tk-channel-view-toggle__btn--active': currentView === 'card' }"
          @click="currentView = 'card'"
        >{{ $t('prism.channel.list.cardView') }}</span>
        <span
          class="tk-channel-view-toggle__btn"
          :class="{ 'tk-channel-view-toggle__btn--active': currentView === 'table' }"
          @click="currentView = 'table'"
        >{{ $t('prism.channel.list.tableView') }}</span>
      </div>
      <div class="tk-channel-list__actions">
        <el-button @click="loadChannels">
          {{ $t('prism.channel.list.refresh') }}
        </el-button>
        <el-button
          type="primary"
          @click="handleCreate"
        >
          {{ $t('prism.channel.list.create') }}
          <span class="tk-tier-badge tk-tier-badge--team">Team</span>
        </el-button>
      </div>
    </div>

    <!-- Card view -->
    <div
      v-if="currentView === 'card'"
      v-loading="loading"
      class="tk-channel-card-view"
    >
      <template v-if="groupedChannels.length === 0">
        <el-empty :description="$t('prism.channel.list.emptyText')" />
      </template>
      <div
        v-for="group in groupedChannels"
        :key="group.type"
        class="tk-channel-group"
      >
        <div class="tk-channel-group__title">
          <span class="tk-channel-group__icon">{{ group.icon }}</span>
          <span>{{ $t(`prism.channel.type.${group.type}`) }}</span>
          <code class="tk-channel-group__code">{{ group.type }}</code>
          <span class="tk-channel-group__count">{{ group.items.length }}</span>
        </div>
        <div class="tk-channel-group__grid">
          <div
            v-for="ch in group.items"
            :key="ch.id"
            class="tk-channel-card"
          >
            <div class="tk-channel-card__header">
              <div class="tk-channel-card__name-row">
                <span class="tk-channel-card__icon">{{ getIcon(ch.type) }}</span>
                <span
                  class="tk-channel-card__name"
                  :title="ch.name"
                >{{ ch.name }}</span>
              </div>
              <el-switch
                :model-value="ch.enabled"
                :aria-label="`${ch.name} — ${$t('prism.channel.list.status')}`"
                @change="handleToggle(ch)"
              />
            </div>
            <div class="tk-channel-card__body">
              <div class="tk-channel-card__meta-row">
                <span>{{ $t('prism.channel.list.status') }}:
                  <strong>{{ ch.enabled ? $t('prism.channel.list.enabled') : $t('prism.channel.list.disabled') }}</strong>
                </span>
                <span class="tk-channel-card__last-test">
                  <span :class="`tk-channel-card__last-test-mark--${ch.lastTestResult}`">
                    {{ testResultMark(ch.lastTestResult) }}
                  </span>
                  {{ ch.lastTestAt ? formatTime(ch.lastTestAt) : $t('prism.channel.list.notTested') }}
                </span>
              </div>
            </div>
            <div class="tk-channel-card__actions">
              <el-button
                link
                size="small"
                @click="handleEdit(ch)"
              >
                {{ $t('prism.channel.list.edit') }}
              </el-button>
              <el-button
                link
                size="small"
                type="primary"
                :loading="testingId === ch.id"
                @click="onTest(ch)"
              >
                {{ $t('prism.channel.list.test') }}
              </el-button>
              <el-button
                link
                size="small"
                @click="handleDelivery(ch)"
              >
                {{ $t('prism.channel.list.delivery') }}
              </el-button>
              <el-button
                link
                size="small"
                type="danger"
                @click="handleDelete(ch)"
              >
                {{ $t('prism.channel.list.delete') }}
              </el-button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Table view -->
    <div
      v-else
      class="tk-channel-table-view"
    >
      <DataTable
        :data="channels"
        :loading="loading"
        :columns="tableColumns"
        :aria-label="$t('prism.channel.list.title')"
      >
        <template #type="{ row }">
          <el-tag size="small">
            {{ $t(`prism.channel.type.${(row as ChannelConfig).type}`) }}
          </el-tag>
        </template>
        <template #enabled="{ row }">
          <el-tag
            :type="STATUS_TAG_TYPE[String((row as ChannelConfig).enabled)]"
            size="small"
          >
            {{ (row as ChannelConfig).enabled ? $t('prism.channel.list.enabled') : $t('prism.channel.list.disabled') }}
          </el-tag>
        </template>
        <template #lastTest="{ row }">
          <span
            v-if="(row as ChannelConfig).lastTestAt"
            class="tk-channel-test-cell"
          >
            <span :class="`tk-channel-card__last-test-mark--${(row as ChannelConfig).lastTestResult}`">
              {{ testResultMark((row as ChannelConfig).lastTestResult) }}
            </span>
            {{ formatTime((row as ChannelConfig).lastTestAt) }}
          </span>
          <span v-else>{{ $t('prism.channel.list.notTested') }}</span>
        </template>
        <template #actions="{ row }">
          <el-button
            link
            type="primary"
            size="small"
            @click="handleEdit(row as ChannelConfig)"
          >
            {{ $t('prism.channel.list.edit') }}
          </el-button>
          <el-button
            link
            type="primary"
            size="small"
            :loading="testingId === (row as ChannelConfig).id"
            @click="onTest(row as ChannelConfig)"
          >
            {{ $t('prism.channel.list.test') }}
          </el-button>
          <el-button
            link
            size="small"
            @click="handleDelivery(row as ChannelConfig)"
          >
            {{ $t('prism.channel.list.delivery') }}
          </el-button>
          <el-button
            link
            type="danger"
            size="small"
            @click="handleDelete(row as ChannelConfig)"
          >
            {{ $t('prism.channel.list.delete') }}
          </el-button>
        </template>
      </DataTable>
    </div>

    <!-- Test receipt dialog -->
    <el-dialog
      v-model="testReceiptVisible"
      :title="$t('prism.channel.list.testReceiptTitle')"
      width="480px"
    >
      <div
        v-if="lastTestResult"
        class="tk-test-receipt"
      >
        <div class="tk-test-receipt__header">
          <el-tag
            :type="testResultTag(lastTestResult.status)"
            size="large"
          >
            {{ lastTestResult.status === 'success' ? $t('prism.channel.list.success') : $t('prism.channel.list.failed') }}
          </el-tag>
          <span class="tk-test-receipt__channel">{{ lastTestChannel }}</span>
        </div>
        <div class="tk-test-receipt__detail">
          <div class="tk-test-receipt__row">
            <span class="tk-test-receipt__label">{{ $t('prism.channel.list.latency') }}:</span>
            <span class="tk-test-receipt__value">{{ lastTestResult.latency ?? '-' }}ms</span>
          </div>
          <div class="tk-test-receipt__row">
            <span class="tk-test-receipt__label">{{ $t('prism.channel.delivery.responseCode') }}:</span>
            <span class="tk-test-receipt__value">{{ lastTestResult.responseCode ?? '-' }}</span>
          </div>
          <div class="tk-test-receipt__row">
            <span class="tk-test-receipt__label">Request ID:</span>
            <span class="tk-test-receipt__value tk-test-receipt__mono">{{ lastTestResult.requestId ?? '-' }}</span>
          </div>
          <div
            v-if="lastTestResult.error"
            class="tk-test-receipt__row"
          >
            <span class="tk-test-receipt__label">{{ $t('prism.channel.delivery.error') }}:</span>
            <span class="tk-test-receipt__value tk-test-receipt__error">{{ lastTestResult.error }}</span>
          </div>
        </div>
      </div>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.tk-channel-view-toggle {
  display: inline-flex;
  overflow: hidden;
  border: 1px solid var(--tk-border-color);
  border-radius: var(--tk-border-radius-base);
}

.tk-channel-view-toggle__btn {
  padding: 6px var(--tk-spacing-md);
  font-size: var(--tk-font-size-sm);
  color: var(--tk-text-regular);
  cursor: pointer;
  user-select: none;
  background-color: var(--tk-bg-color);

  &:hover { color: var(--tk-primary-color-text); }

  &--active {
    color: #fff;
    background-color: var(--tk-primary-color);
  }
}

.tk-channel-list__actions {
  display: flex;
  gap: var(--tk-spacing-sm);
  align-items: center;
}

.tk-channel-group {
  margin-bottom: var(--tk-spacing-lg);
}

.tk-channel-group__title {
  display: flex;
  gap: var(--tk-spacing-sm);
  align-items: center;
  padding-bottom: var(--tk-spacing-xs);
  margin-bottom: var(--tk-spacing-md);
  font-size: var(--tk-font-size-base);
  font-weight: var(--tk-font-weight-semibold);
  color: var(--tk-text-primary);
  border-bottom: 1px solid var(--tk-border-color-lighter);
}

.tk-channel-group__icon { font-size: 18px; }

.tk-channel-group__code {
  font-family: var(--tk-font-family-mono);
  font-size: var(--tk-font-size-xs);
  color: var(--tk-text-secondary);
}

.tk-channel-group__count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 22px;
  height: 22px;
  padding: 0 6px;
  font-size: var(--tk-font-size-xs);
  font-weight: var(--tk-font-weight-semibold);
  color: var(--tk-primary-color-text);
  background-color: var(--tk-primary-color-light-9);
  border-radius: var(--tk-border-radius-round);
}

.tk-channel-group__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: var(--tk-spacing-md);
}

.tk-channel-card {
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-sm);
  padding: var(--tk-spacing-md);
  background-color: var(--tk-bg-color);
  border: 1px solid var(--tk-border-color-lighter);
  border-radius: var(--tk-border-radius-md);
  transition: all var(--tk-animation-duration-base) var(--tk-ease-in-out);

  &:hover {
    border-color: var(--tk-primary-color-light-5);
    box-shadow: var(--tk-shadow-md);
  }
}

.tk-channel-card__header {
  display: flex;
  gap: var(--tk-spacing-sm);
  align-items: flex-start;
  justify-content: space-between;
}

.tk-channel-card__name-row {
  display: flex;
  flex: 1;
  gap: var(--tk-spacing-sm);
  align-items: center;
  min-width: 0;
}

.tk-channel-card__icon {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  font-size: 20px;
  background-color: var(--tk-primary-color-light-9);
  border-radius: var(--tk-border-radius-md);
}

.tk-channel-card__name {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: var(--tk-font-size-lg);
  font-weight: var(--tk-font-weight-semibold);
  color: var(--tk-text-primary);
  white-space: nowrap;
}

.tk-channel-card__body {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: var(--tk-font-size-xs);
  color: var(--tk-text-secondary);
}

.tk-channel-card__meta-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.tk-channel-card__last-test {
  display: inline-flex;
  gap: 4px;
  align-items: center;
}

.tk-channel-card__last-test-mark--success { font-weight: var(--tk-font-weight-semibold); color: var(--tk-success-color-text); }
.tk-channel-card__last-test-mark--failed { font-weight: var(--tk-font-weight-semibold); color: var(--tk-danger-color-text); }
.tk-channel-card__last-test-mark--none { color: var(--tk-text-secondary); }

.tk-channel-card__actions {
  display: flex;
  gap: var(--tk-spacing-xs);
  padding-top: var(--tk-spacing-xs);
  border-top: 1px dashed var(--tk-border-color-lighter);
}

.tk-channel-test-cell {
  display: inline-flex;
  gap: 4px;
  align-items: center;
}

/* Test receipt */
.tk-test-receipt__header {
  display: flex;
  gap: var(--tk-spacing-sm);
  align-items: center;
  margin-bottom: var(--tk-spacing-md);
}

.tk-test-receipt__channel {
  font-weight: var(--tk-font-weight-semibold);
  color: var(--tk-text-primary);
}

.tk-test-receipt__detail {
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-sm);
  padding: var(--tk-spacing-md);
  font-size: var(--tk-font-size-sm);
  background-color: var(--tk-gray-3);
  border-radius: var(--tk-border-radius-md);
}

.tk-test-receipt__row {
  display: flex;
  gap: var(--tk-spacing-sm);
}

.tk-test-receipt__label {
  min-width: 100px;
  color: var(--tk-text-secondary);
}

.tk-test-receipt__value {
  color: var(--tk-text-primary);
}

.tk-test-receipt__mono {
  font-family: var(--tk-font-family-mono);
  word-break: break-all;
}

.tk-test-receipt__error {
  color: var(--tk-danger-color-text);
}
</style>
