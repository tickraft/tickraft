// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * SystemStatus settings - status page management.
 *
 * Edits the singleton backend configuration: page title, description,
 * publish toggle, and the component map that groups monitor points into
 * public entries. An empty component map falls back to one component per
 * enabled monitor point (derived on the server).
 */
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Delete, Promotion } from '@element-plus/icons-vue'
import {
  getStatusConfig,
  updateStatusConfig,
} from '../../../../api/status'
import type { StatusConfig } from '../../../../api/status'

const { t } = useI18n()

const loading = ref(false)
const saving = ref(false)

/** Editable component row (point IDs edited as a comma/space separated string) */
interface ComponentRow {
  name: string
  description: string
  points: string
}

const form = reactive({
  title: '',
  description: '',
  enabled: false,
})

const rows = ref<ComponentRow[]>([])

function toRow(c: { name: string, description?: string, pointIds?: number[] }): ComponentRow {
  return {
    name: c.name,
    description: c.description ?? '',
    points: (c.pointIds ?? []).join(', '),
  }
}

/** Parse the points field into point IDs, ignoring non-numeric chunks. */
function parsePoints(raw: string): number[] {
  return raw
    .split(/[,;\s]+/)
    .map((chunk) => Number.parseInt(chunk, 10))
    .filter((n) => Number.isInteger(n) && n > 0)
}

function buildPayload(): StatusConfig {
  const components = rows.value
    .filter((row) => {
      // Unnamed trailing rows are dropped instead of surfacing a backend
      // validation error; the save path keeps the interaction forgiving.
      const isEmpty = row.name.trim() === '' && row.description.trim() === '' && row.points.trim() === ''
      return !isEmpty
    })
    .map((row) => ({
      name: row.name.trim(),
      description: row.description.trim() || undefined,
      // Wire key: components travel nested in the body, where the request
      // layer no longer rewrites keys — emit the backend tag directly.
      point_ids: parsePoints(row.points),
    }))
  return {
    title: form.title,
    description: form.description,
    enabled: form.enabled,
    components,
  } as StatusConfig
}

function addRow(): void {
  rows.value.push({ name: '', description: '', points: '' })
}

function removeRow(index: number): void {
  rows.value.splice(index, 1)
}

function openPublicPage(): void {
  window.open('/status', '_blank', 'noopener')
}

async function load(): Promise<void> {
  loading.value = true
  try {
    const cfg = await getStatusConfig()
    form.title = cfg.title
    form.description = cfg.description ?? ''
    form.enabled = cfg.enabled
    rows.value = (cfg.components ?? []).map(toRow)
  }
  catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('status.settings.loadFailed'))
  }
  finally {
    loading.value = false
  }
}

async function save(): Promise<void> {
  saving.value = true
  try {
    const updated = await updateStatusConfig(buildPayload())
    form.title = updated.title
    form.description = updated.description ?? ''
    form.enabled = updated.enabled
    rows.value = (updated.components ?? []).map(toRow)
    ElMessage.success(t('status.settings.saved'))
  }
  catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('status.settings.saveFailed'))
  }
  finally {
    saving.value = false
  }
}

onMounted(() => {
  void load()
})
</script>

<template>
  <div
    v-loading="loading"
    class="tk-status-settings"
  >
    <el-card class="tk-status-settings__card">
      <template #header>
        <div class="tk-status-settings__header">
          <span>{{ t('status.settings.title') }}</span>
          <el-button
            :icon="Promotion"
            @click="openPublicPage"
          >
            {{ t('status.settings.viewPublic') }}
          </el-button>
        </div>
      </template>

      <el-form label-position="top">
        <el-form-item :label="t('status.settings.fieldTitle')">
          <el-input
            v-model="form.title"
            :placeholder="t('status.settings.fieldTitlePlaceholder')"
            maxlength="255"
          />
        </el-form-item>
        <el-form-item :label="t('status.settings.fieldDescription')">
          <el-input
            v-model="form.description"
            :placeholder="t('status.settings.fieldDescriptionPlaceholder')"
            maxlength="1024"
          />
        </el-form-item>
        <el-form-item :label="t('status.settings.fieldEnabled')">
          <el-switch v-model="form.enabled" />
          <span class="tk-status-settings__hint">{{ t('status.settings.fieldEnabledHelp') }}</span>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card class="tk-status-settings__card">
      <template #header>
        <div class="tk-status-settings__header">
          <span>{{ t('status.settings.components') }}</span>
          <el-button
            type="primary"
            plain
            @click="addRow"
          >
            {{ t('status.settings.addComponent') }}
          </el-button>
        </div>
      </template>

      <p class="tk-status-settings__hint">
        {{ t('status.settings.componentsHelp') }}
      </p>

      <div
        v-for="(row, index) in rows"
        :key="index"
        class="tk-status-settings__row"
      >
        <el-input
          v-model="row.name"
          :placeholder="t('status.settings.componentName')"
          class="tk-status-settings__row-name"
        />
        <el-input
          v-model="row.description"
          :placeholder="t('status.settings.componentDescription')"
          class="tk-status-settings__row-desc"
        />
        <el-input
          v-model="row.points"
          :placeholder="t('status.settings.componentPoints')"
          class="tk-status-settings__row-points"
        />
        <el-button
          type="danger"
          plain
          circle
          :aria-label="t('status.settings.removeComponent')"
          @click="removeRow(index)"
        >
          <el-icon><Delete /></el-icon>
        </el-button>
      </div>

      <el-empty
        v-if="rows.length === 0"
        :description="t('status.settings.noComponents')"
        :image-size="60"
      />

      <div class="tk-status-settings__actions">
        <el-button
          type="primary"
          :loading="saving"
          @click="save"
        >
          {{ t('common.app.save') }}
        </el-button>
      </div>
    </el-card>
  </div>
</template>

<style scoped lang="scss">
.tk-status-settings {
  display: flex;
  flex-direction: column;
  gap: 16px;
  max-width: 860px;

  &__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  &__hint {
    margin: 0 0 12px;
    font-size: var(--tk-font-size-sm);
    color: var(--tk-text-secondary);
  }

  &__row {
    display: flex;
    gap: 8px;
    align-items: center;
    margin-bottom: 8px;
  }

  &__row-name {
    flex: 0 0 200px;
  }

  &__row-desc {
    flex: 1;
  }

  &__row-points {
    flex: 0 0 220px;
  }

  &__actions {
    display: flex;
    justify-content: flex-end;
    margin-top: 16px;
  }
}
</style>
