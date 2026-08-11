// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Refresh, InfoFilled, Monitor, CopyDocument } from '@element-plus/icons-vue'
import { getRuntimeInfo, getSettings } from '../../../../api/system'
import type { RuntimeInfo, SystemSettings } from '../../../../api/system'

const { t } = useI18n()

const loading = ref(false)
const refreshing = ref(false)
const runtimeInfo = ref<RuntimeInfo | null>(null)
const settings = ref<SystemSettings | null>(null)

/** Stat strip cards derived from runtime info */
const statCards = computed<Array<{ label: string; value: string; sub: string; accent: string }>>(() => {
  if (!runtimeInfo.value) return []
  const info = runtimeInfo.value
  return [
    {
      label: t('system.info.statVersion'),
      value: info.version || '-',
      sub: '',
      accent: 'primary',
    },
    {
      label: t('system.info.statUptime'),
      value: info.uptime || '-',
      sub: info.startTime ? t('system.info.subSinceDate', { date: info.startTime.slice(0, 10) }) : '',
      accent: 'success',
    },
    {
      label: t('system.info.statBuildTags'),
      value: info.buildTags || '-',
      sub: 'BUILD',
      accent: 'warning',
    },
    {
      label: t('system.info.statStartTime'),
      value: info.startTime || '-',
      sub: t('system.info.subLaunch'),
      accent: 'info',
    },
  ]
})

/** Build metadata list items */
const metaItems = computed<Array<{ label: string; value: string; copyable: boolean }>>(() => {
  if (!runtimeInfo.value) return []
  const info = runtimeInfo.value
  return [
    { label: t('system.info.version'), value: info.version || '-', copyable: true },
    { label: t('system.info.buildTags'), value: info.buildTags || '-', copyable: false },
    { label: t('system.info.startTime'), value: info.startTime || '-', copyable: false },
    { label: t('system.info.uptime'), value: info.uptime || '-', copyable: false },
  ]
})

async function loadInfo(): Promise<void> {
  loading.value = true
  try {
    const [info, cfg] = await Promise.all([
      getRuntimeInfo(),
      getSettings(),
    ])
    runtimeInfo.value = info
    settings.value = cfg
  } catch {
    ElMessage.error(t('system.info.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function handleRefresh(): Promise<void> {
  refreshing.value = true
  try {
    const [info, cfg] = await Promise.all([
      getRuntimeInfo(),
      getSettings(),
    ])
    runtimeInfo.value = info
    settings.value = cfg
    ElMessage.success(t('system.info.refreshed'))
  } catch {
    ElMessage.error(t('system.info.loadFailed'))
  } finally {
    refreshing.value = false
  }
}

async function handleCopy(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success(t('system.info.copySuccess'))
  } catch {
    ElMessage.error(t('system.info.copyFailed'))
  }
}

onMounted(() => {
  void loadInfo()
})
</script>

<template>
  <div class="tk-sysinfo tk-page-container">
    <!-- Header -->
    <div class="tk-sysinfo__header">
      <div class="tk-sysinfo__title-block">
        <h2 class="tk-sysinfo__title">
          {{ t('system.info.title') }}
        </h2>
        <p class="tk-sysinfo__subtitle">
          {{ t('system.info.subtitle') }}
        </p>
      </div>
      <div class="tk-sysinfo__header-actions">
        <span class="tk-sysinfo__edition">
          <span class="tk-sysinfo__edition-dot" />
          <span>{{ t('system.info.communityEdition') }}</span>
        </span>
        <el-button
          :icon="Refresh"
          :loading="refreshing"
          @click="handleRefresh"
        >
          {{ t('common.app.refresh') }}
        </el-button>
      </div>
    </div>

    <div v-loading="loading" class="tk-sysinfo__body">
      <!-- Stat strip -->
      <div
        v-if="statCards.length"
        class="tk-sysinfo__stats"
      >
        <div
          v-for="card in statCards"
          :key="card.label"
          class="tk-stat-card"
          :class="`tk-stat-card--${card.accent}`"
        >
          <div class="tk-stat-card__body">
            <span class="tk-stat-card__label">{{ card.label }}</span>
            <span class="tk-stat-card__value" :title="card.value">{{ card.value }}</span>
            <span
              v-if="card.sub"
              class="tk-stat-card__sub"
            >{{ card.sub }}</span>
          </div>
        </div>
      </div>

      <!-- Build & runtime grid -->
      <div class="tk-sysinfo__grid">
        <!-- Build metadata -->
        <div class="tk-meta-card">
          <div class="tk-meta-card__header">
            <div class="tk-meta-card__title">
              <el-icon class="tk-meta-card__title-icon">
                <InfoFilled />
              </el-icon>
              <span>{{ t('system.info.buildMetadata') }}</span>
            </div>
            <span class="tk-meta-card__hint">{{ t('system.info.buildMetadataHint') }}</span>
          </div>
          <div class="tk-meta-card__body">
            <div
              v-if="metaItems.length"
              class="tk-meta-list"
            >
              <div
                v-for="item in metaItems"
                :key="item.label"
                class="tk-meta-item"
              >
                <span class="tk-meta-item__label">{{ item.label }}</span>
                <span class="tk-meta-item__value">
                  {{ item.value }}
                  <button
                    v-if="item.copyable && item.value !== '-'"
                    type="button"
                    class="tk-meta-item__copy"
                    :title="t('system.info.copySuccess')"
                    @click="handleCopy(item.value)"
                  >
                    <el-icon><CopyDocument /></el-icon>
                  </button>
                </span>
              </div>
            </div>
          </div>
        </div>

        <!-- Config overview -->
        <div class="tk-meta-card">
          <div class="tk-meta-card__header">
            <div class="tk-meta-card__title">
              <el-icon class="tk-meta-card__title-icon">
                <Monitor />
              </el-icon>
              <span>{{ t('system.info.configOverview') }}</span>
            </div>
            <span class="tk-meta-card__hint">{{ t('system.info.configOverviewHint') }}</span>
          </div>
          <div class="tk-meta-card__body">
            <div
              v-if="settings"
              class="tk-meta-list"
            >
              <div class="tk-meta-item">
                <span class="tk-meta-item__label">{{ t('system.info.siteName') }}</span>
                <span class="tk-meta-item__value">{{ settings.siteName }}</span>
              </div>
              <div class="tk-meta-item">
                <span class="tk-meta-item__label">{{ t('system.info.defaultLocale') }}</span>
                <span class="tk-meta-item__value">{{ settings.defaultLang }}</span>
              </div>
              <div class="tk-meta-item">
                <span class="tk-meta-item__label">{{ t('system.info.logLevel') }}</span>
                <span class="tk-meta-item__value">{{ settings.logLevel }}</span>
              </div>
              <div class="tk-meta-item">
                <span class="tk-meta-item__label">{{ t('system.info.retentionDays') }}</span>
                <span class="tk-meta-item__value">{{ settings.retentionDays }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.tk-sysinfo {
  &__header {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-lg);
    align-items: flex-start;
    justify-content: space-between;
    margin-bottom: var(--tk-spacing-xl);
  }

  &__title-block {
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-xs);
  }

  &__title {
    margin: 0;
    font-size: var(--tk-font-size-2xl);
    font-weight: var(--tk-font-weight-bold);
    line-height: 1.1;
    color: var(--tk-text-primary);
    letter-spacing: -0.02em;
  }

  &__subtitle {
    max-width: 560px;
    font-size: var(--tk-font-size-sm);
    line-height: var(--tk-line-height-normal);
    color: var(--tk-text-secondary);
  }

  &__header-actions {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
  }

  &__edition {
    display: inline-flex;
    gap: var(--tk-spacing-xs);
    align-items: center;
    padding: var(--tk-spacing-xs) var(--tk-spacing-sm);
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-primary-color);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    background-color: var(--tk-primary-color-bg);
    border: 1px solid var(--tk-primary-color-border);
    border-radius: var(--tk-border-radius-sm);
  }

  &__edition-dot {
    width: 6px;
    height: 6px;
    background-color: var(--tk-primary-color);
    border-radius: var(--tk-border-radius-circle);
  }

  &__body {
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-xl);
    min-height: 120px;
  }

  // ---- Stat strip ----
  &__stats {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: var(--tk-spacing-md);
  }

  // ---- Build & runtime grid ----
  &__grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--tk-spacing-lg);
    align-items: start;
  }
}

// ---- Stat card ----
.tk-stat-card {
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

  &--primary::before {
    background-color: var(--tk-primary-color);
  }

  &--success::before {
    background-color: var(--tk-success-color);
  }

  &--warning::before {
    background-color: var(--tk-warning-color);
  }

  &--info::before {
    background-color: var(--tk-info-color);
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
    min-width: 0;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xl);
    font-weight: var(--tk-font-weight-bold);
    font-variant-numeric: tabular-nums;
    line-height: 1.2;
    color: var(--tk-text-primary);
    letter-spacing: -0.01em;
    white-space: nowrap;
  }

  &__sub {
    margin-top: 2px;
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
    letter-spacing: 0.01em;
  }
}

// ---- Meta card ----
.tk-meta-card {
  overflow: hidden;
  background-color: var(--tk-bg-surface);
  border: var(--tk-border-default);
  border-radius: var(--tk-border-radius-lg);

  &__header {
    display: flex;
    gap: var(--tk-spacing-md);
    align-items: center;
    justify-content: space-between;
    padding: var(--tk-spacing-md) var(--tk-spacing-lg);
    background-color: var(--tk-bg-hover);
    border-bottom: 1px solid var(--tk-border-color-light);
  }

  &__title {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
    font-size: var(--tk-font-size-base);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
    letter-spacing: -0.02em;
  }

  &__title-icon {
    font-size: 16px;
    color: var(--tk-text-secondary);
  }

  &__hint {
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.01em;
  }

  &__body {
    padding: var(--tk-spacing-lg);
  }
}

// ---- Meta list ----
.tk-meta-list {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--tk-spacing-md) var(--tk-spacing-xl);
}

.tk-meta-item {
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-xs);
  padding-bottom: var(--tk-spacing-md);
  border-bottom: 1px solid var(--tk-border-color-lighter);

  &:nth-last-child(-n+2) {
    padding-bottom: 0;
    border-bottom: none;
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
    display: inline-flex;
    gap: var(--tk-spacing-xs);
    align-items: center;
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-sm);
    line-height: 1.3;
    color: var(--tk-text-primary);
    word-break: break-all;
  }

  &__copy {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    color: var(--tk-text-secondary);
    cursor: pointer;
    background-color: var(--tk-bg-input);
    border: var(--tk-border-default);
    border-radius: var(--tk-border-radius-sm);
    transition: border-color var(--tk-transition-fast),
      color var(--tk-transition-fast);

    &:hover {
      color: var(--tk-primary-color);
      border-color: var(--tk-primary-color);
    }
  }
}

// ---- Responsive ----
@media (max-width: 960px) {
  .tk-sysinfo__stats {
    grid-template-columns: repeat(2, 1fr);
  }

  .tk-sysinfo__grid {
    grid-template-columns: 1fr;
  }

  .tk-meta-list {
    grid-template-columns: 1fr;
  }

  .tk-meta-item:nth-last-child(1) {
    padding-bottom: 0;
    border-bottom: none;
  }
}

@media (max-width: 600px) {
  .tk-sysinfo__stats {
    grid-template-columns: 1fr;
  }
}
</style>
