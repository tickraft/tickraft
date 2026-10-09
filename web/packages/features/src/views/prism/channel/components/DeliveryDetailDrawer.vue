// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Delivery detail drawer component
 *
 * Renders the persisted delivery record: basic info, the replayed alert
 * event payload (request), the channel response (status code + error),
 * and the per-attempt history. Attempt 0 is the original delivery; each
 * manual retry appends an entry.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DeliveryAttempt, DeliveryRecord, DeliveryStatus } from '../../../../api/channel'
import { getChannelIcon } from '../composables'

interface Props {
  record: DeliveryRecord | null
  visible: boolean
}

const props = defineProps<Props>()
const emit = defineEmits<{
  'update:visible': [value: boolean]
  retry: []
}>()

const { t } = useI18n()

/** Pretty-printed alert event persisted with the delivery. */
const requestPayload = computed(() => {
  if (!props.record?.requestPayload) return ''
  try {
    return JSON.stringify(JSON.parse(props.record.requestPayload), null, 2)
  } catch {
    return props.record.requestPayload
  }
})

function close(): void {
  emit('update:visible', false)
}

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

function attemptLabel(item: DeliveryAttempt): string {
  return item.n === 0 ? t('prism.channel.delivery.retryInitial') : t('prism.channel.delivery.retryN', { n: item.n })
}

function attemptClass(item: DeliveryAttempt): string {
  if (item.result === 'success') return 'tk-delivery-drawer__retry-item--success'
  if (item.result === 'failed') return 'tk-delivery-drawer__retry-item--failed'
  return ''
}
</script>

<template>
  <el-drawer
    :model-value="visible"
    size="560px"
    aria-labelledby="delivery-detail-title"
    aria-modal="true"
    @update:model-value="emit('update:visible', $event)"
  >
    <template #header>
      <h3
        id="delivery-detail-title"
        class="tk-delivery-drawer__title"
      >
        {{ $t('prism.channel.delivery.drawerTitle') }}
      </h3>
    </template>
    <div
      v-if="record"
      role="region"
      :aria-label="$t('prism.channel.delivery.drawerTitle')"
    >
      <!-- Basic info -->
      <div class="tk-delivery-drawer__section">
        <div class="tk-delivery-drawer__section-title">
          {{ $t('prism.channel.delivery.sectionBasic') }}
        </div>
        <div class="tk-delivery-drawer__row">
          <span class="tk-delivery-drawer__row-label">{{ $t('prism.channel.delivery.drawerDeliveryId') }}</span>
          <code class="tk-delivery-drawer__code-inline">#{{ record.id }}</code>
        </div>
        <div class="tk-delivery-drawer__row">
          <span class="tk-delivery-drawer__row-label">{{ $t('prism.channel.delivery.drawerChannel') }}</span>
          <span>{{ getChannelIcon(record.channelType) }} {{ record.channelName }} ({{ $t(`prism.channel.type.${record.channelType}`) }})</span>
        </div>
        <div class="tk-delivery-drawer__row">
          <span class="tk-delivery-drawer__row-label">{{ $t('prism.channel.delivery.drawerAlert') }}</span>
          <span>
            {{ record.alertTitle }}
            <code
              v-if="record.eventId"
              class="tk-delivery-drawer__code-inline"
            >{{ record.eventId }}</code>
          </span>
        </div>
        <div class="tk-delivery-drawer__row">
          <span class="tk-delivery-drawer__row-label">{{ $t('prism.channel.delivery.drawerTime') }}</span>
          <span>{{ formatTime(record.sentAt) }}</span>
        </div>
        <div class="tk-delivery-drawer__row">
          <span class="tk-delivery-drawer__row-label">{{ $t('prism.channel.delivery.drawerStatus') }}</span>
          <el-tag
            :type="statusTagType(record.status)"
            size="small"
          >
            {{ $t(`prism.channel.delivery.${record.status}`) }}
          </el-tag>
        </div>
        <div class="tk-delivery-drawer__row">
          <span class="tk-delivery-drawer__row-label">{{ $t('prism.channel.delivery.drawerDuration') }}</span>
          <span>{{ formatDuration(record.durationMs) }}</span>
        </div>
        <div class="tk-delivery-drawer__row">
          <span class="tk-delivery-drawer__row-label">{{ $t('prism.channel.delivery.drawerResponseCode') }}</span>
          <span>{{ record.responseCode || '-' }}</span>
        </div>
      </div>

      <!-- Request detail: the persisted alert event -->
      <div class="tk-delivery-drawer__section">
        <div class="tk-delivery-drawer__section-title">
          {{ $t('prism.channel.delivery.sectionRequest') }}
        </div>
        <pre
          v-if="requestPayload"
          class="tk-delivery-drawer__code-block"
        >{{ requestPayload }}</pre>
        <div
          v-else
          class="tk-delivery-drawer__empty"
        >
          {{ $t('prism.channel.delivery.payloadEmpty') }}
        </div>
      </div>

      <!-- Response detail: channel status code and error -->
      <div class="tk-delivery-drawer__section">
        <div class="tk-delivery-drawer__section-title">
          {{ $t('prism.channel.delivery.sectionResponse') }}
        </div>
        <pre
          v-if="record.status === 'failed'"
          class="tk-delivery-drawer__code-block"
        >HTTP {{ record.responseCode || '-' }}
        {{ record.error || '-' }}</pre>
        <div
          v-else
          class="tk-delivery-drawer__empty"
        >
          {{ $t('prism.channel.delivery.responseOk') }}
        </div>
      </div>

      <!-- Attempt history -->
      <div class="tk-delivery-drawer__section">
        <div class="tk-delivery-drawer__section-title">
          {{ $t('prism.channel.delivery.sectionRetryHistory') }}
        </div>
        <div
          v-if="!record.attempts || record.attempts.length === 0"
          class="tk-delivery-drawer__empty"
        >
          {{ $t('prism.channel.delivery.retryHistoryEmpty') }}
        </div>
        <div
          v-else
          class="tk-delivery-drawer__retry-history"
        >
          <div
            v-for="(item, idx) in record.attempts"
            :key="idx"
            class="tk-delivery-drawer__retry-item"
            :class="attemptClass(item)"
          >
            <div class="tk-delivery-drawer__retry-item-time">
              {{ formatTime(item.time) }}
            </div>
            <div class="tk-delivery-drawer__retry-item-label">
              {{ attemptLabel(item) }}
            </div>
            <div class="tk-delivery-drawer__retry-item-result">
              {{ $t(`prism.channel.delivery.${item.result}`) }} · {{ formatDuration(item.durationMs) }}
            </div>
            <div
              v-if="item.error"
              class="tk-delivery-drawer__retry-item-error"
            >
              {{ item.error }}
            </div>
          </div>
        </div>
      </div>
    </div>
    <template #footer>
      <el-button @click="close">
        {{ $t('common.app.cancel') }}
      </el-button>
      <el-button
        v-if="record?.status === 'failed'"
        type="primary"
        @click="emit('retry')"
      >
        {{ $t('prism.channel.delivery.retry') }}
      </el-button>
    </template>
  </el-drawer>
</template>

<style scoped lang="scss">
.tk-delivery-drawer__section {
  margin-bottom: var(--tk-spacing-md);
}

.tk-delivery-drawer__section-title {
  padding-bottom: var(--tk-spacing-xs);
  margin-bottom: var(--tk-spacing-xs);
  font-size: var(--tk-font-size-sm);
  font-weight: var(--tk-font-weight-semibold);
  color: var(--tk-text-primary);
  border-bottom: 1px solid var(--tk-border-color-lighter);
}

.tk-delivery-drawer__row {
  display: flex;
  gap: var(--tk-spacing-md);
  padding: 4px 0;
  font-size: var(--tk-font-size-sm);
}

.tk-delivery-drawer__row-label {
  flex-shrink: 0;
  width: 90px;
  color: var(--tk-text-secondary);
}

.tk-delivery-drawer__code-inline {
  font-family: var(--tk-font-family-mono);
}

.tk-delivery-drawer__code-block {
  max-height: 200px;
  padding: var(--tk-spacing-sm);
  margin: 0;
  overflow-y: auto;
  font-family: var(--tk-font-family-mono);
  font-size: var(--tk-font-size-xs);
  color: var(--tk-text-regular);
  word-break: break-all;
  white-space: pre-wrap;
  background-color: var(--tk-gray-2);
  border-radius: var(--tk-border-radius-base);
}

.tk-delivery-drawer__empty {
  font-size: var(--tk-font-size-sm);
  font-style: italic;
  color: var(--tk-text-secondary);
}

.tk-delivery-drawer__retry-history {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-sm);
  padding-left: var(--tk-spacing-sm);

  &::before {
    position: absolute;
    top: 6px;
    bottom: 6px;
    left: 5px;
    width: 1px;
    content: '';
    background-color: var(--tk-border-color);
  }
}

.tk-delivery-drawer__retry-item {
  position: relative;
  padding-left: var(--tk-spacing-md);
  font-size: var(--tk-font-size-sm);

  &::before {
    position: absolute;
    top: 6px;
    left: 0;
    width: 11px;
    height: 11px;
    content: '';
    background-color: var(--tk-primary-color);
    border: 2px solid var(--tk-bg-color);
    border-radius: 50%;
  }

  &--failed::before { background-color: var(--tk-danger-color); }
  &--success::before { background-color: var(--tk-success-color); }
}

.tk-delivery-drawer__retry-item-time {
  font-family: var(--tk-font-family-mono);
  font-size: var(--tk-font-size-xs);
  color: var(--tk-text-secondary);
}

.tk-delivery-drawer__retry-item-label {
  font-weight: var(--tk-font-weight-medium);
  color: var(--tk-text-primary);
}

.tk-delivery-drawer__retry-item-result {
  font-size: var(--tk-font-size-xs);
  color: var(--tk-text-regular);
}

.tk-delivery-drawer__retry-item-error {
  font-size: var(--tk-font-size-xs);
  color: var(--tk-danger-color);
  word-break: break-all;
}
</style>
