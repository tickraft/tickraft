// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { WarningFilled, Search, Refresh, Plus, Key, Check, Close, CopyDocument } from '@element-plus/icons-vue'
import { DataTable, ConfirmDialog } from '@tickraft/core'
import { getApiKeys, createApiKey, revokeApiKey, isApiKeyActive } from '../../../../api/auth'
import type { ApiKey, ApiKeyCreateParams, ApiKeyCreateResult } from '../../../../api/auth'

const { t } = useI18n()

const loading = ref(false)
const tableData = ref<ApiKey[]>([])
const currentPage = ref(1)
const pageSize = ref(10)
const searchQuery = ref('')

/** Filtered data by search query */
const filteredData = computed(() => {
  if (!searchQuery.value) return tableData.value
  const q = searchQuery.value.toLowerCase()
  return tableData.value.filter((item) => item.name.toLowerCase().includes(q))
})

const total = computed(() => filteredData.value.length)
const pageData = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return filteredData.value.slice(start, start + pageSize.value)
})

/** Summary strip counts */
const summary = computed(() => {
  const totalKeys = tableData.value.length
  const activeKeys = tableData.value.filter((k) => isApiKeyActive(k)).length
  const revokedKeys = tableData.value.filter((k) => !isApiKeyActive(k)).length
  return { totalKeys, activeKeys, revokedKeys }
})

/** Range text for footer */
const rangeText = computed(() => {
  if (total.value === 0) return ''
  const start = (currentPage.value - 1) * pageSize.value + 1
  const end = Math.min(start + pageSize.value - 1, total.value)
  return t('system.apiKeys.showingRange', { start, end, total: total.value })
})

const columns = computed(() => [
  { prop: 'name', label: t('system.apiKeys.name'), minWidth: 200, slot: 'name', align: 'left' as const },
  { prop: 'keyPrefix', label: t('system.apiKeys.prefix'), minWidth: 160, slot: 'prefix', align: 'left' as const },
  { prop: 'createdAt', label: t('system.apiKeys.createdAt'), minWidth: 170, align: 'center' as const },
  { prop: 'expiredAt', label: t('system.apiKeys.expiresAt'), minWidth: 170, slot: 'expiresAt' },
  { prop: 'status', label: t('system.apiKeys.status'), width: 100, slot: 'status' },
])

// Create dialog
const createVisible = ref(false)
const createLoading = ref(false)
const createForm = reactive({
  name: '',
  expiresInDays: 30,
})

const expireOptions = computed(() => [
  { label: t('system.apiKeys.expire30Days'), value: 30 },
  { label: t('system.apiKeys.expire90Days'), value: 90 },
  { label: t('system.apiKeys.expire365Days'), value: 365 },
  { label: t('system.apiKeys.expireNever'), value: 0 },
])

// Raw key is shown only once on creation
const rawKeyVisible = ref(false)
const rawKeyResult = ref<ApiKeyCreateResult | null>(null)
const copyLoading = ref(false)

// Revoke confirmation
const revokeVisible = ref(false)
const revokeLoading = ref(false)
const selectedKey = ref<ApiKey | null>(null)

async function loadData(): Promise<void> {
  loading.value = true
  try {
    // Fetch a large page so client-side search/pagination can operate on the
    // full dataset (the backend ListAPIKeys only supports page/size filtering).
    const res = await getApiKeys({ page: 1, pageSize: 1000 })
    tableData.value = res.items
    currentPage.value = 1
    searchQuery.value = ''
  } catch {
    ElMessage.error(t('system.apiKeys.loadFailed'))
  } finally {
    loading.value = false
  }
}

function handleCreate(): void {
  createForm.name = ''
  createForm.expiresInDays = 30
  createVisible.value = true
}

async function handleCreateConfirm(): Promise<void> {
  const name = createForm.name.trim()
  if (!name) {
    ElMessage.warning(t('system.apiKeys.nameRequired'))
    return
  }
  createLoading.value = true
  try {
    const params: ApiKeyCreateParams = {
      name,
      expiredAt: computeExpiredAt(createForm.expiresInDays),
    }
    const result = await createApiKey(params)
    createVisible.value = false
    rawKeyResult.value = result
    rawKeyVisible.value = true
    await loadData()
  } catch {
    ElMessage.error(t('system.apiKeys.createFailed'))
  } finally {
    createLoading.value = false
  }
}

async function handleCopyRawKey(): Promise<void> {
  if (!rawKeyResult.value) return
  copyLoading.value = true
  try {
    await navigator.clipboard.writeText(rawKeyResult.value.rawKey)
    ElMessage.success(t('system.apiKeys.copied'))
  } catch {
    ElMessage.error(t('system.apiKeys.copyFailed'))
  } finally {
    copyLoading.value = false
  }
}

function handleRevoke(row: ApiKey): void {
  selectedKey.value = row
  revokeVisible.value = true
}

async function handleRevokeConfirm(): Promise<void> {
  if (!selectedKey.value) return
  revokeLoading.value = true
  try {
    await revokeApiKey(selectedKey.value.id)
    revokeVisible.value = false
    ElMessage.success(t('system.apiKeys.revokeSuccess'))
    await loadData()
  } catch {
    ElMessage.error(t('system.apiKeys.revokeFailed'))
  } finally {
    revokeLoading.value = false
  }
}

function handlePageChange(payload: { current: number; pageSize: number }): void {
  currentPage.value = payload.current
  pageSize.value = payload.pageSize
}

/** Key prefix mask: tk_abc1 -> tk_abc1**** */
function maskPrefix(prefix: string): string {
  return prefix ? `${prefix}****` : ''
}

/** Milliseconds per day, used to compute the expiry timestamp. */
const MS_PER_DAY = 24 * 60 * 60 * 1000

/** Convert a number of expiry days to an RFC3339 timestamp (undefined = never). */
function computeExpiredAt(days: number): string | undefined {
  if (!days || days <= 0) return undefined
  return new Date(Date.now() + days * MS_PER_DAY).toISOString()
}

/** Map a key's numeric status to the i18n label key ('active' | 'revoked'). */
function statusLabelKey(key: ApiKey): 'active' | 'revoked' {
  return isApiKeyActive(key) ? 'active' : 'revoked'
}

onMounted(() => {
  void loadData()
})
</script>

<template>
  <div class="tk-apikey tk-page-container">
    <!-- Summary strip -->
    <div class="tk-apikey__summary">
      <div class="tk-apikey-stat tk-apikey-stat--total">
        <div class="tk-apikey-stat__icon">
          <el-icon><Key /></el-icon>
        </div>
        <div class="tk-apikey-stat__body">
          <span class="tk-apikey-stat__label">{{ t('system.apiKeys.totalKeys') }}</span>
          <span class="tk-apikey-stat__value">{{ summary.totalKeys }}</span>
        </div>
      </div>
      <div class="tk-apikey-stat tk-apikey-stat--active">
        <div class="tk-apikey-stat__icon">
          <el-icon><Check /></el-icon>
        </div>
        <div class="tk-apikey-stat__body">
          <span class="tk-apikey-stat__label">{{ t('system.apiKeys.activeKeys') }}</span>
          <span class="tk-apikey-stat__value">{{ summary.activeKeys }}</span>
        </div>
      </div>
      <div class="tk-apikey-stat tk-apikey-stat--revoked">
        <div class="tk-apikey-stat__icon">
          <el-icon><Close /></el-icon>
        </div>
        <div class="tk-apikey-stat__body">
          <span class="tk-apikey-stat__label">{{ t('system.apiKeys.revokedKeys') }}</span>
          <span class="tk-apikey-stat__value">{{ summary.revokedKeys }}</span>
        </div>
      </div>
    </div>

    <!-- Toolbar -->
    <div class="tk-apikey__toolbar">
      <div class="tk-apikey__toolbar-title">
        <h2 class="tk-apikey__toolbar-heading">
          {{ t('system.apiKeys.title') }}
        </h2>
        <span class="tk-apikey__toolbar-sub">
          {{ t('system.apiKeys.listCount', { count: summary.totalKeys }) }}
        </span>
      </div>
      <el-input
        v-model="searchQuery"
        :placeholder="t('system.apiKeys.searchPlaceholder')"
        :prefix-icon="Search"
        class="tk-apikey__search"
        clearable
        @input="currentPage = 1"
      />
      <el-button
        :icon="Refresh"
        @click="loadData"
      >
        {{ t('common.app.refresh') }}
      </el-button>
      <el-button
        type="primary"
        :icon="Plus"
        @click="handleCreate"
      >
        {{ t('system.apiKeys.create') }}
      </el-button>
    </div>

    <!-- Table card -->
    <div class="tk-apikey__table-card">
      <DataTable
        table-id="api-keys-list"
        :data="pageData"
        :columns="columns"
        :loading="loading"
        :total="total"
        :current="currentPage"
        :page-size="pageSize"
        @page-change="handlePageChange"
      >
        <template #name="{ row }">
          <div class="tk-key-name">
            <span class="tk-key-name__title">{{ row.name }}</span>
            <span class="tk-key-name__id">{{ t('system.apiKeys.keyId', { id: row.id }) }}</span>
          </div>
        </template>
        <template #prefix="{ row }">
          <span class="tk-key-cell">
            <span
              class="tk-key-cell__dot"
              :class="isApiKeyActive(row as ApiKey) ? 'tk-key-cell__dot--active' : 'tk-key-cell__dot--revoked'"
            />
            <code class="tk-key-cell__text">{{ maskPrefix((row as ApiKey).keyPrefix) }}</code>
          </span>
        </template>
        <template #expiresAt="{ row }">
          <span
            v-if="(row as ApiKey).expiredAt"
            class="tk-time-cell"
          >{{ (row as ApiKey).expiredAt }}</span>
          <span
            v-else
            class="tk-time-cell tk-time-cell--muted"
          >
            {{ t('system.apiKeys.neverExpires') }}
          </span>
        </template>
        <template #status="{ row }">
          <el-tag
            :type="isApiKeyActive(row as ApiKey) ? 'success' : 'info'"
            size="small"
          >
            {{ t(`system.apiKeys.statusLabel.${statusLabelKey(row as ApiKey)}`) }}
          </el-tag>
        </template>
        <template #action-column>
          <el-table-column
            :label="t('common.app.action')"
            width="100"
            fixed="right"
            align="center"
            :resizable="false"
          >
            <template #default="{ row }">
              <el-button
                v-if="isApiKeyActive(row as ApiKey)"
                link
                type="danger"
                size="small"
                @click="handleRevoke(row as ApiKey)"
              >
                {{ t('system.apiKeys.revoke') }}
              </el-button>
              <span
                v-else
                class="tk-time-cell tk-time-cell--muted"
              >—</span>
            </template>
          </el-table-column>
        </template>
      </DataTable>
      <div
        v-if="rangeText"
        class="tk-apikey__footer"
      >
        <span class="tk-apikey__footer-hint">{{ rangeText }}</span>
      </div>
    </div>

    <!-- Create API Key dialog -->
    <el-dialog
      v-model="createVisible"
      :title="t('system.apiKeys.createTitle')"
      width="480px"
    >
      <el-form
        :model="createForm"
        label-position="top"
      >
        <el-form-item
          :label="t('system.apiKeys.name')"
          required
        >
          <el-input
            v-model="createForm.name"
            :placeholder="t('system.apiKeys.namePlaceholder')"
            maxlength="64"
          />
        </el-form-item>
        <el-form-item :label="t('system.apiKeys.expire')">
          <el-radio-group
            v-model="createForm.expiresInDays"
            class="tk-apikey__expire-group"
          >
            <el-radio
              v-for="item in expireOptions"
              :key="item.value"
              :value="item.value"
            >
              {{ item.label }}
            </el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">
          {{ t('common.app.cancel') }}
        </el-button>
        <el-button
          type="primary"
          :loading="createLoading"
          @click="handleCreateConfirm"
        >
          {{ t('system.apiKeys.confirmCreate') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- Raw key one-time display dialog -->
    <el-dialog
      v-model="rawKeyVisible"
      :title="t('system.apiKeys.createSuccess')"
      width="560px"
      :close-on-click-modal="false"
    >
      <div class="tk-rawkey-alert">
        <el-icon class="tk-rawkey-alert__icon">
          <WarningFilled />
        </el-icon>
        <div class="tk-rawkey-alert__body">
          <span class="tk-rawkey-alert__strong">{{ t('system.apiKeys.rawKeyWarning') }}</span>
        </div>
      </div>
      <div class="tk-rawkey-meta">
        <div class="tk-rawkey-meta__item">
          <span class="tk-rawkey-meta__label">{{ t('system.apiKeys.rawKeyNameLabel') }}</span>
          <span class="tk-rawkey-meta__value">{{ rawKeyResult?.name }}</span>
        </div>
      </div>
      <div class="tk-rawkey-block">
        {{ rawKeyResult?.rawKey }}
        <button
          type="button"
          class="tk-rawkey-block__copy"
          :loading="copyLoading"
          @click="handleCopyRawKey"
        >
          <el-icon><CopyDocument /></el-icon>
          <span>{{ t('system.apiKeys.copyKey') }}</span>
        </button>
      </div>
      <template #footer>
        <el-button
          type="primary"
          @click="rawKeyVisible = false"
        >
          {{ t('system.apiKeys.savedClose') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- Revoke confirmation (danger + requireInput double confirmation) -->
    <ConfirmDialog
      v-model="revokeVisible"
      type="danger"
      :title="t('system.apiKeys.revokeTitle')"
      :require-input="selectedKey?.name ?? ''"
      :confirm-text="t('system.apiKeys.confirmRevoke')"
      :loading="revokeLoading"
      @confirm="handleRevokeConfirm"
    >
      <p class="tk-apikey__revoke-desc">
        {{ t('system.apiKeys.revokeConfirmDesc', { name: selectedKey?.name }) }}
      </p>
      <p class="tk-apikey__revoke-tip">
        {{ t('system.apiKeys.revokeConfirmTip') }}
      </p>
    </ConfirmDialog>
  </div>
</template>

<style scoped lang="scss">
.tk-apikey {
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-xl);

  // ---- Summary strip ----
  &__summary {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: var(--tk-spacing-md);
  }

  // ---- Toolbar ----
  &__toolbar {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-sm);
    align-items: center;
  }

  &__toolbar-title {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: baseline;
    margin-right: auto;
  }

  &__toolbar-heading {
    margin: 0;
    font-size: var(--tk-font-size-xl);
    font-weight: var(--tk-font-weight-bold);
    color: var(--tk-text-primary);
    letter-spacing: -0.02em;
  }

  &__toolbar-sub {
    font-size: var(--tk-font-size-sm);
    color: var(--tk-text-secondary);
  }

  &__search {
    width: 240px;
  }

  // ---- Table card ----
  &__table-card {
    overflow: hidden;
    background-color: var(--tk-bg-surface);
    border: var(--tk-border-default);
    border-radius: var(--tk-border-radius-lg);
  }

  &__footer {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    padding: var(--tk-spacing-md) var(--tk-spacing-lg);
    background-color: var(--tk-bg-hover);
    border-top: 1px solid var(--tk-border-color-light);
  }

  &__footer-hint {
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
    letter-spacing: 0.01em;
  }

  &__expire-group {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-sm);
  }

  &__revoke-desc {
    margin: 0 0 var(--tk-spacing-sm);
    line-height: var(--tk-line-height-normal);
    color: var(--tk-text-regular);
  }

  &__revoke-tip {
    margin: 0;
    font-size: var(--tk-font-size-sm);
    line-height: var(--tk-line-height-normal);
    color: var(--tk-danger-color-text);
  }
}

// ---- Summary stat card ----
.tk-apikey-stat {
  position: relative;
  display: flex;
  gap: var(--tk-spacing-md);
  align-items: center;
  padding: var(--tk-spacing-lg) var(--tk-spacing-xl);
  overflow: hidden;
  background-color: var(--tk-bg-surface);
  border: var(--tk-border-default);
  border-radius: var(--tk-border-radius-lg);

  &::before {
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    width: 3px;
    content: "";
  }

  &--total::before {
    background-color: var(--tk-primary-color);
  }

  &--active::before {
    background-color: var(--tk-success-color);
  }

  &--revoked::before {
    background-color: var(--tk-text-secondary);
  }

  &__icon {
    display: inline-flex;
    flex-shrink: 0;
    align-items: center;
    justify-content: center;
    width: 40px;
    height: 40px;
    background-color: var(--tk-bg-hover);
    border-radius: var(--tk-border-radius-base);
  }

  &--total .tk-apikey-stat__icon {
    color: var(--tk-primary-color);
  }

  &--active .tk-apikey-stat__icon {
    color: var(--tk-success-color);
  }

  &--revoked .tk-apikey-stat__icon {
    color: var(--tk-text-secondary);
  }

  &__body {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  &__label {
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  &__value {
    font-size: var(--tk-font-size-3xl);
    font-weight: var(--tk-font-weight-bold);
    font-variant-numeric: tabular-nums;
    line-height: 1;
    color: var(--tk-text-primary);
    letter-spacing: -0.02em;
  }
}

// ---- Key name cell ----
.tk-key-name {
  display: flex;
  flex-direction: column;
  gap: 2px;

  &__title {
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
  }

  &__id {
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
    opacity: 0.7;
  }
}

// ---- Key prefix cell ----
.tk-key-cell {
  display: inline-flex;
  gap: var(--tk-spacing-xs);
  align-items: center;

  &__dot {
    flex-shrink: 0;
    width: 6px;
    height: 6px;
    border-radius: var(--tk-border-radius-circle);

    &--active {
      background-color: var(--tk-success-color);
    }

    &--revoked {
      background-color: var(--tk-text-secondary);
    }
  }

  &__text {
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-sm);
    color: var(--tk-text-secondary);
  }
}

// ---- Time cell ----
.tk-time-cell {
  font-family: var(--tk-font-family-mono);
  font-size: var(--tk-font-size-xs);
  color: var(--tk-text-secondary);

  &--muted {
    color: var(--tk-text-secondary);
    opacity: 0.6;
  }
}

// ---- Raw key alert ----
.tk-rawkey-alert {
  display: flex;
  gap: var(--tk-spacing-sm);
  align-items: flex-start;
  padding: var(--tk-spacing-md) var(--tk-spacing-lg);
  margin-bottom: var(--tk-spacing-md);
  background-color: var(--tk-warning-color-bg);
  border: 1px solid var(--tk-warning-color-border);
  border-left: 3px solid var(--tk-warning-color);
  border-radius: var(--tk-border-radius-base);

  &__icon {
    flex-shrink: 0;
    margin-top: 1px;
    font-size: 18px;
    color: var(--tk-warning-color);
  }

  &__body {
    font-size: var(--tk-font-size-sm);
    line-height: var(--tk-line-height-normal);
    color: var(--tk-text-regular);
  }

  &__strong {
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
  }
}

// ---- Raw key meta ----
.tk-rawkey-meta {
  display: flex;
  gap: var(--tk-spacing-lg);
  padding: var(--tk-spacing-sm) 0 var(--tk-spacing-md);
  font-size: var(--tk-font-size-sm);

  &__item {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  &__label {
    font-family: var(--tk-font-family-mono);
    font-size: 10px;
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  &__value {
    font-weight: var(--tk-font-weight-medium);
    color: var(--tk-text-primary);
  }
}

// ---- Raw key block ----
.tk-rawkey-block {
  position: relative;
  padding: var(--tk-spacing-md) var(--tk-spacing-lg);
  padding-right: 64px;
  font-family: var(--tk-font-family-mono);
  font-size: var(--tk-font-size-base);
  line-height: 1.6;
  color: var(--tk-text-primary);
  word-break: break-all;
  background-color: var(--tk-gray-3);
  border: var(--tk-border-default);
  border-radius: var(--tk-border-radius-base);

  &__copy {
    position: absolute;
    top: var(--tk-spacing-sm);
    right: var(--tk-spacing-sm);
    display: inline-flex;
    gap: var(--tk-spacing-xs);
    align-items: center;
    padding: var(--tk-spacing-xs) var(--tk-spacing-sm);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-on-primary);
    cursor: pointer;
    background-color: var(--tk-primary-color);
    border: none;
    border-radius: var(--tk-border-radius-sm);
    transition: background-color var(--tk-transition-fast);

    &:hover {
      background-color: var(--tk-primary-color-hover);
    }
  }
}

// ---- Responsive ----
@media (max-width: 768px) {
  .tk-apikey__summary {
    grid-template-columns: 1fr;
  }

  .tk-apikey__search {
    width: 100%;
  }
}
</style>
