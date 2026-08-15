// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Asset detail page.
 *
 * Read-only view of a single asset record with basic info, runtime info,
 * and a placeholder section for related monitor points / tasks.
 */
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { StatusTag, usePermission } from '@tickraft/core'
import { getAsset, deleteAsset, parseMetadata } from '../../../api/asset'
import type { Asset, AssetMetadata } from '../../../types/asset'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { canDelete } = usePermission()

const loading = ref(false)
const deleting = ref(false)
const record = ref<Asset | undefined>()

const assetId = computed(() => {
  const raw = route.params.id
  return raw ? Number(Array.isArray(raw) ? raw[0] : raw) : 0
})

/** Parsed metadata for display */
const meta = computed<AssetMetadata>(() => {
  return record.value ? parseMetadata(record.value.metadata) : {}
})

/** Format endpoint with optional port from metadata */
const formattedEndpoint = computed(() => {
  if (!record.value) return '-'
  const endpoint = meta.value.endpoint ?? record.value.assetKey
  return meta.value.port ? `${endpoint}:${meta.value.port}` : endpoint
})

async function loadAsset(): Promise<void> {
  if (!assetId.value) return
  loading.value = true
  try {
    record.value = await getAsset(assetId.value)
  } catch {
    ElMessage.error(t('asset.detail.notFound'))
    router.replace('/asset/list')
  } finally {
    loading.value = false
  }
}

function handleEdit(): void {
  if (assetId.value) router.push(`/asset/edit/${assetId.value}`)
}

async function handleDelete(): Promise<void> {
  if (!record.value) return
  deleting.value = true
  try {
    await deleteAsset(record.value.id)
    ElMessage.success(t('asset.detail.deleteSuccess'))
    router.replace('/asset/list')
  } catch {
    // Errors are handled centrally by the interceptor
  } finally {
    deleting.value = false
  }
}

function handleBack(): void {
  router.push('/asset/list')
}

/** Format ISO timestamp to a readable local string */
function formatTime(iso: string): string {
  if (!iso) return '-'
  const d = new Date(iso.replace(' ', 'T'))
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

onMounted(() => {
  void loadAsset()
})
</script>

<template>
  <div
    v-loading="loading"
    class="tk-page-container"
  >
    <!-- Header -->
    <div class="tk-asset-detail-header">
      <div class="tk-asset-detail-header__left">
        <el-button
          link
          type="primary"
          @click="handleBack"
        >
          <i class="i-ep-arrow-left" />
          {{ t('asset.detail.back') }}
        </el-button>
        <h1 class="tk-asset-detail-header__title">
          {{ record?.name ?? t('asset.detail.title') }}
        </h1>
      </div>
      <div class="tk-asset-detail-header__actions">
        <el-button @click="handleEdit">
          {{ t('asset.detail.edit') }}
        </el-button>
        <el-button
          v-if="canDelete('device')"
          type="danger"
          :loading="deleting"
          @click="handleDelete"
        >
          {{ t('asset.detail.delete') }}
        </el-button>
      </div>
    </div>

    <template v-if="record">
      <!-- Basic info -->
      <div class="tk-asset-detail-card">
        <div class="tk-asset-detail-card__head">
          <h2 class="tk-asset-detail-card__title">
            {{ t('asset.detail.basicInfo') }}
          </h2>
        </div>
        <div class="tk-asset-detail-grid">
          <div class="tk-asset-detail-field">
            <span class="tk-asset-detail-field__label">{{ t('asset.list.name') }}</span>
            <span class="tk-asset-detail-field__value">{{ record.name }}</span>
          </div>
          <div class="tk-asset-detail-field">
            <span class="tk-asset-detail-field__label">{{ t('asset.list.type') }}</span>
            <span class="tk-asset-detail-field__value">
              <el-tag
                size="small"
                effect="light"
                type="info"
              >
                {{ t(`asset.type.${record.assetType}`) }}
              </el-tag>
            </span>
          </div>
          <div class="tk-asset-detail-field">
            <span class="tk-asset-detail-field__label">{{ t('asset.create.assetKey') }}</span>
            <span class="tk-asset-detail-field__value tk-asset-detail-mono">{{ record.assetKey }}</span>
          </div>
          <div class="tk-asset-detail-field">
            <span class="tk-asset-detail-field__label">{{ t('asset.list.endpoint') }}</span>
            <span class="tk-asset-detail-field__value tk-asset-detail-mono">{{ formattedEndpoint }}</span>
          </div>
          <div class="tk-asset-detail-field">
            <span class="tk-asset-detail-field__label">{{ t('asset.list.labels') }}</span>
            <span class="tk-asset-detail-field__value">
              <span class="tk-asset-detail-labels">
                <el-tag
                  v-for="label in meta.labels ?? []"
                  :key="label"
                  size="small"
                  effect="plain"
                >
                  {{ label }}
                </el-tag>
                <span
                  v-if="!meta.labels?.length"
                  class="tk-asset-detail-field__placeholder"
                >-</span>
              </span>
            </span>
          </div>
          <div class="tk-asset-detail-field tk-asset-detail-field--full">
            <span class="tk-asset-detail-field__label">{{ t('asset.create.description') }}</span>
            <span class="tk-asset-detail-field__value">
              {{ meta.description || '-' }}
            </span>
          </div>
        </div>
      </div>

      <!-- Runtime info -->
      <div class="tk-asset-detail-card">
        <div class="tk-asset-detail-card__head">
          <h2 class="tk-asset-detail-card__title">
            {{ t('asset.detail.runtimeInfo') }}
          </h2>
        </div>
        <div class="tk-asset-detail-grid">
          <div class="tk-asset-detail-field">
            <span class="tk-asset-detail-field__label">{{ t('asset.detail.status') }}</span>
            <span class="tk-asset-detail-field__value">
              <StatusTag
                category="asset"
                :status="record.status"
                size="sm"
                show-icon
              />
            </span>
          </div>
          <div class="tk-asset-detail-field">
            <span class="tk-asset-detail-field__label">{{ t('asset.detail.lastSeen') }}</span>
            <span class="tk-asset-detail-field__value tk-asset-detail-mono">{{ formatTime(record.lastActiveAt) }}</span>
          </div>
          <div class="tk-asset-detail-field">
            <span class="tk-asset-detail-field__label">{{ t('asset.detail.createdAt') }}</span>
            <span class="tk-asset-detail-field__value tk-asset-detail-mono">{{ formatTime(record.createdAt) }}</span>
          </div>
          <div class="tk-asset-detail-field">
            <span class="tk-asset-detail-field__label">{{ t('asset.detail.updatedAt') }}</span>
            <span class="tk-asset-detail-field__value tk-asset-detail-mono">{{ formatTime(record.updatedAt) }}</span>
          </div>
        </div>
      </div>

      <!-- Relations -->
      <div class="tk-asset-detail-card">
        <div class="tk-asset-detail-card__head">
          <h2 class="tk-asset-detail-card__title">
            {{ t('asset.detail.relations') }}
          </h2>
        </div>
        <div class="tk-asset-detail-relations">
          <div class="tk-asset-detail-relation">
            <div class="tk-asset-detail-relation__title">
              {{ t('asset.detail.relatedMonitors') }}
            </div>
            <div class="tk-asset-detail-relation__empty">
              {{ t('asset.detail.noRelations') }}
            </div>
          </div>
          <div class="tk-asset-detail-relation">
            <div class="tk-asset-detail-relation__title">
              {{ t('asset.detail.relatedTasks') }}
            </div>
            <div class="tk-asset-detail-relation__empty">
              {{ t('asset.detail.noRelations') }}
            </div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped lang="scss">
.tk-asset-detail-header {
  display: flex;
  flex-wrap: wrap;
  gap: var(--tk-spacing-md);
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--tk-spacing-md);

  &__left {
    display: flex;
    gap: var(--tk-spacing-md);
    align-items: center;
    min-width: 0;
  }

  &__title {
    margin: 0;
    font-size: var(--tk-font-size-xl);
    font-weight: var(--tk-font-weight-semibold);
    line-height: 1;
    color: var(--tk-text-primary);
  }

  &__actions {
    display: flex;
    flex-shrink: 0;
    gap: var(--tk-spacing-sm);
    align-items: center;
  }
}

.tk-asset-detail-card {
  padding: var(--tk-spacing-lg);
  margin-bottom: var(--tk-spacing-md);
  background-color: var(--tk-bg-color);
  border-radius: var(--tk-border-radius-md);
  box-shadow: var(--tk-shadow-sm);

  &__head {
    margin-bottom: var(--tk-spacing-md);
  }

  &__title {
    margin: 0;
    font-size: var(--tk-font-size-base);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
  }
}

.tk-asset-detail-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: var(--tk-spacing-md) var(--tk-spacing-lg);
}

.tk-asset-detail-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;

  &--full {
    grid-column: span 2;
  }

  &__label {
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  &__value {
    font-size: var(--tk-font-size-sm);
    color: var(--tk-text-primary);
    word-break: break-all;
  }

  &__placeholder {
    color: var(--tk-text-placeholder);
  }
}

.tk-asset-detail-mono {
  font-family: var(--tk-font-mono, monospace);
  font-variant-numeric: tabular-nums;
}

.tk-asset-detail-labels {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
}

.tk-asset-detail-relations {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: var(--tk-spacing-md);
}

.tk-asset-detail-relation {
  padding: var(--tk-spacing-md);
  background-color: var(--tk-bg-color-page);
  border: 1px dashed var(--tk-border-color-light);
  border-radius: var(--tk-border-radius-base);

  &__title {
    margin-bottom: var(--tk-spacing-sm);
    font-size: var(--tk-font-size-sm);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
  }

  &__empty {
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-placeholder);
  }
}

@media (max-width: 767px) {
  .tk-asset-detail-grid,
  .tk-asset-detail-relations {
    grid-template-columns: 1fr;
  }

  .tk-asset-detail-field--full {
    grid-column: span 1;
  }
}
</style>
