// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Setting, Sunny } from '@element-plus/icons-vue'
import { useAppStore, getStorage, setStorage, availableLocales, useEventListener } from '@tickraft/core'
import type { ThemeMode, LocaleType } from '@tickraft/core'
import { getSettings, updateSettings } from '../../../../api/system'
import type { SystemSettings } from '../../../../api/system'

const { t } = useI18n()
const appStore = useAppStore()

/** Theme preference storage key (follows tk- prefix convention) */
const THEME_PREF_KEY = 'tk-theme-pref'
/** Density preference storage key */
const DENSITY_KEY = 'tk-density'

type ThemePreference = ThemeMode | 'auto'
type DensityMode = 'compact' | 'comfortable' | 'spacious'

const basicLoading = ref(false)
const basicDirty = ref(false)

const basicForm = reactive({
  defaultLang: 'zh-Hans' as LocaleType,
  logLevel: 'info',
  retentionDays: 30,
})

/** Basic configuration snapshot (used for reset) */
let basicSnapshot: SystemSettings | null = null

const themePreference = ref<ThemePreference>('auto')
const mediaQueryRef = ref<MediaQueryList | null>(null)
const density = ref<DensityMode>('comfortable')

/** Locale options come from the i18n registry (core zh-Hans/en-US + extension registerLocale extension) */
const localeOptions = computed<Array<{ label: string; value: LocaleType }>>(() =>
  availableLocales.value.map((item) => ({
    label: item.englishName ? `${item.label} (${item.englishName})` : item.label,
    value: item.code as LocaleType,
  })),
)

const logLevelOptions = [
  { label: 'debug', value: 'debug' },
  { label: 'info', value: 'info' },
  { label: 'warn', value: 'warn' },
  { label: 'error', value: 'error' },
]

const themeOptions = computed<Array<{ label: string; value: ThemePreference }>>(() => [
  { label: t('system.settings.lightTheme'), value: 'light' },
  { label: t('system.settings.darkTheme'), value: 'dark' },
  { label: t('system.settings.autoTheme'), value: 'auto' },
])

const densityOptions = computed<Array<{ label: string; value: DensityMode }>>(() => [
  { label: t('system.settings.densityCompact'), value: 'compact' },
  { label: t('system.settings.densityComfortable'), value: 'comfortable' },
  { label: t('system.settings.densitySpacious'), value: 'spacious' },
])

/** Read system theme preference */
function resolveSystemTheme(): ThemeMode {
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

/** Apply theme preference to global store */
function applyThemePreference(pref: ThemePreference): void {
  appStore.setTheme(pref === 'auto' ? resolveSystemTheme() : pref)
}

function handleThemeChange(value: ThemePreference): void {
  themePreference.value = value
  setStorage(THEME_PREF_KEY, value)
  applyThemePreference(value)
  ElMessage.success(t(`system.settings.themeChanged.${value}`))
}

function handleDensityChange(value: DensityMode): void {
  density.value = value
  setStorage(DENSITY_KEY, value)
  document.documentElement.setAttribute('data-density', value)
  ElMessage.info(t(`system.settings.densityChanged.${value}`))
}

function handleSidebarChange(value: boolean | string | number): void {
  const collapsed = Boolean(value)
  if (collapsed !== appStore.sidebar.collapsed) {
    appStore.toggleSidebar()
  }
  ElMessage.info(
    t(collapsed ? 'system.settings.sidebarCollapsedOn' : 'system.settings.sidebarCollapsedOff'),
  )
}

/** System theme change callback (follows system in auto mode) */
function handleMediaChange(): void {
  if (themePreference.value === 'auto') {
    appStore.setTheme(resolveSystemTheme())
  }
}

/** Mark basic form as dirty */
function markDirty(): void {
  basicDirty.value = true
}

async function loadBasic(): Promise<void> {
  basicLoading.value = true
  try {
    const data = await getSettings()
    basicSnapshot = data
    basicForm.defaultLang = data.defaultLang as LocaleType
    basicForm.logLevel = data.logLevel
    basicForm.retentionDays = data.retentionDays
    basicDirty.value = false
  } finally {
    basicLoading.value = false
  }
}

async function handleSaveBasic(): Promise<void> {
  if (basicForm.retentionDays < 7 || basicForm.retentionDays > 365) {
    ElMessage.warning(t('system.settings.retentionRangeError'))
    return
  }
  basicLoading.value = true
  try {
    const data = await updateSettings({
      defaultLang: basicForm.defaultLang,
      logLevel: basicForm.logLevel,
      retentionDays: basicForm.retentionDays,
    })
    basicSnapshot = data
    basicDirty.value = false
    ElMessage.success(t('system.settings.saveSuccess'))
  } finally {
    basicLoading.value = false
  }
}

function handleResetBasic(): void {
  if (basicSnapshot) {
    basicForm.defaultLang = basicSnapshot.defaultLang as LocaleType
    basicForm.logLevel = basicSnapshot.logLevel
    basicForm.retentionDays = basicSnapshot.retentionDays
  }
  basicDirty.value = false
  ElMessage.info(t('system.settings.resetSuccess'))
}

onMounted(() => {
  void loadBasic()
  const pref = getStorage<ThemePreference>(THEME_PREF_KEY)
  if (pref === 'light' || pref === 'dark' || pref === 'auto') {
    themePreference.value = pref
  }
  const savedDensity = getStorage<DensityMode>(DENSITY_KEY)
  if (savedDensity === 'compact' || savedDensity === 'comfortable' || savedDensity === 'spacious') {
    density.value = savedDensity
  }
  document.documentElement.setAttribute('data-density', density.value)
  mediaQueryRef.value = window.matchMedia('(prefers-color-scheme: dark)')
})

// useEventListener handles attach-on-mount and detach-on-unmount automatically,
// re-binding when the ref target is assigned.
useEventListener(mediaQueryRef, 'change', handleMediaChange)
</script>

<template>
  <div class="tk-settings tk-page-container">
    <!-- Page header -->
    <div class="tk-settings__page-head">
      <h2 class="tk-settings__page-title">
        {{ t('system.settings.title') }}
      </h2>
      <p class="tk-settings__page-desc">
        {{ t('system.settings.subtitle') }}
      </p>
    </div>

    <!-- Vertically stacked section cards (flattened layout — no sub-navigation) -->
    <div class="tk-settings__sections">
      <!-- Section 1: Basic configuration -->
      <section class="tk-settings__card">
        <header class="tk-settings__card-head">
          <div class="tk-settings__card-icon">
            <el-icon><Setting /></el-icon>
          </div>
          <div class="tk-settings__card-heading">
            <div class="tk-settings__card-title">
              {{ t('system.settings.basic') }}
            </div>
            <div class="tk-settings__card-desc">
              {{ t('system.settings.basicDesc') }}
            </div>
          </div>
        </header>

        <div class="tk-settings__card-body">
          <el-form
            v-loading="basicLoading"
            :model="basicForm"
            label-position="top"
            class="tk-settings__form"
          >
            <div class="tk-settings__row">
              <div class="tk-settings__row-label">
                <div class="tk-settings__row-title">
                  {{ t('system.settings.defaultLocale') }}
                </div>
                <div class="tk-settings__row-help">
                  {{ t('system.settings.defaultLocaleHelp') }}
                </div>
              </div>
              <div class="tk-settings__row-control">
                <el-select
                  v-model="basicForm.defaultLang"
                  class="tk-settings__select"
                  @change="markDirty"
                >
                  <el-option
                    v-for="item in localeOptions"
                    :key="item.value"
                    :label="item.label"
                    :value="item.value"
                  />
                </el-select>
              </div>
            </div>

            <div class="tk-settings__row">
              <div class="tk-settings__row-label">
                <div class="tk-settings__row-title">
                  {{ t('system.settings.logLevel') }}
                </div>
                <div class="tk-settings__row-help">
                  {{ t('system.settings.logLevelHelp') }}
                </div>
              </div>
              <div class="tk-settings__row-control">
                <el-select
                  v-model="basicForm.logLevel"
                  class="tk-settings__select"
                  @change="markDirty"
                >
                  <el-option
                    v-for="item in logLevelOptions"
                    :key="item.value"
                    :label="item.label"
                    :value="item.value"
                  />
                </el-select>
              </div>
            </div>

            <div class="tk-settings__row">
              <div class="tk-settings__row-label">
                <div class="tk-settings__row-title">
                  {{ t('system.settings.retentionDays') }}
                </div>
                <div class="tk-settings__row-help">
                  {{ t('system.settings.retentionHelp') }}
                </div>
              </div>
              <div class="tk-settings__row-control">
                <el-input-number
                  v-model="basicForm.retentionDays"
                  :min="7"
                  :max="365"
                  @change="markDirty"
                />
                <span class="tk-settings__suffix">DAYS · 7–365</span>
              </div>
            </div>
          </el-form>

          <div class="tk-settings__actions">
            <span
              class="tk-settings__save-hint"
              :class="{ 'tk-settings__save-hint--dirty': basicDirty }"
            >
              {{ basicDirty ? '● ' + t('system.settings.hasUnsavedChanges') : t('system.settings.noChanges') }}
            </span>
            <el-button @click="handleResetBasic">
              {{ t('common.app.reset') }}
            </el-button>
            <el-button
              type="primary"
              :loading="basicLoading"
              @click="handleSaveBasic"
            >
              {{ t('system.settings.save') }}
            </el-button>
          </div>
        </div>
      </section>

      <!-- Section 2: Appearance -->
      <section class="tk-settings__card">
        <header class="tk-settings__card-head">
          <div class="tk-settings__card-icon">
            <el-icon><Sunny /></el-icon>
          </div>
          <div class="tk-settings__card-heading">
            <div class="tk-settings__card-title">
              {{ t('system.settings.appearance') }}
            </div>
            <div class="tk-settings__card-desc">
              {{ t('system.settings.appearanceDesc') }}
            </div>
          </div>
        </header>

        <div class="tk-settings__card-body">
          <div class="tk-settings__form">
            <div class="tk-settings__row">
              <div class="tk-settings__row-label">
                <div class="tk-settings__row-title">
                  {{ t('system.settings.theme') }}
                </div>
                <div class="tk-settings__row-help">
                  {{ t('system.settings.themeHelp') }}
                </div>
              </div>
              <div class="tk-settings__row-control">
                <el-radio-group
                  v-model="themePreference"
                  @update:model-value="handleThemeChange"
                >
                  <el-radio-button
                    v-for="item in themeOptions"
                    :key="item.value"
                    :value="item.value"
                  >
                    {{ item.label }}
                  </el-radio-button>
                </el-radio-group>
              </div>
            </div>

            <div class="tk-settings__row">
              <div class="tk-settings__row-label">
                <div class="tk-settings__row-title">
                  {{ t('system.settings.density') }}
                </div>
                <div class="tk-settings__row-help">
                  {{ t('system.settings.densityHelp') }}
                </div>
              </div>
              <div class="tk-settings__row-control">
                <div class="tk-segmented">
                  <button
                    v-for="item in densityOptions"
                    :key="item.value"
                    type="button"
                    class="tk-segmented__item"
                    :class="{ 'tk-segmented__item--active': density === item.value }"
                    @click="handleDensityChange(item.value)"
                  >
                    {{ item.label }}
                  </button>
                </div>
              </div>
            </div>

            <div class="tk-settings__row">
              <div class="tk-settings__row-label">
                <div class="tk-settings__row-title">
                  {{ t('system.settings.sidebarCollapsed') }}
                </div>
                <div class="tk-settings__row-help">
                  {{ t('system.settings.sidebarHelp') }}
                </div>
              </div>
              <div class="tk-settings__row-control">
                <el-switch
                  :model-value="appStore.sidebar.collapsed"
                  @update:model-value="handleSidebarChange"
                />
              </div>
            </div>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped lang="scss">
.tk-settings {
  &__page-head {
    margin-bottom: var(--tk-spacing-lg);
  }

  &__page-title {
    margin: 0;
    font-size: var(--tk-font-size-2xl);
    font-weight: var(--tk-font-weight-bold);
    color: var(--tk-text-primary);
    letter-spacing: -0.02em;
  }

  &__page-desc {
    margin: var(--tk-spacing-xs) 0 0;
    font-size: var(--tk-font-size-sm);
    color: var(--tk-text-secondary);
  }

  // ---- Sections container (single-column stack, max-width for readability) ----
  &__sections {
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-lg);
    max-width: 820px;
  }

  // ---- Section card ----
  &__card {
    overflow: hidden;
    background-color: var(--tk-bg-surface);
    border: var(--tk-border-default);
    border-radius: var(--tk-border-radius-lg);
  }

  &__card-head {
    display: flex;
    gap: var(--tk-spacing-md);
    align-items: center;
    padding: var(--tk-spacing-lg) var(--tk-spacing-xl);
    background-color: var(--tk-bg-surface-hover);
    border-bottom: 1px solid var(--tk-border-color-light);
  }

  &__card-icon {
    display: flex;
    flex-shrink: 0;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    font-size: 18px;
    color: var(--tk-primary-color);
    background-color: var(--tk-primary-color-bg);
    border-radius: var(--tk-border-radius-base);
  }

  &__card-heading {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  &__card-title {
    font-size: var(--tk-font-size-lg);
    font-weight: var(--tk-font-weight-bold);
    color: var(--tk-text-primary);
    letter-spacing: -0.01em;
  }

  &__card-desc {
    font-size: var(--tk-font-size-sm);
    color: var(--tk-text-secondary);
  }

  &__card-body {
    padding: var(--tk-spacing-xl);
  }

  // ---- Form grid ----
  &__form {
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-xl);
  }

  &__row {
    display: grid;
    grid-template-columns: 200px 1fr;
    gap: var(--tk-spacing-xl);
    align-items: start;
  }

  &__row-label {
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-xs);
    padding-top: var(--tk-spacing-xs);
  }

  &__row-title {
    font-size: var(--tk-font-size-base);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
  }

  &__row-help {
    font-size: var(--tk-font-size-xs);
    line-height: var(--tk-line-height-normal);
    color: var(--tk-text-secondary);
  }

  &__row-control {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
  }

  &__select {
    width: 100%;
    max-width: 420px;
  }

  &__suffix {
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
    letter-spacing: 0.01em;
    white-space: nowrap;
  }

  // ---- Actions ----
  &__actions {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
    justify-content: flex-end;
    padding-top: var(--tk-spacing-lg);
    margin-top: var(--tk-spacing-xl);
    border-top: 1px solid var(--tk-border-color-light);
  }

  &__save-hint {
    margin-right: auto;
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    color: var(--tk-text-secondary);
    letter-spacing: 0.01em;

    &--dirty {
      color: var(--tk-warning-color);
    }
  }
}

// ---- Segmented control ----
.tk-segmented {
  display: inline-flex;
  gap: var(--tk-spacing-xs);
  padding: var(--tk-spacing-xs);
  background-color: var(--tk-bg-hover);
  border: var(--tk-border-default);
  border-radius: var(--tk-border-radius-base);

  &__item {
    padding: var(--tk-spacing-xs) var(--tk-spacing-lg);
    font-size: var(--tk-font-size-sm);
    font-weight: var(--tk-font-weight-medium);
    color: var(--tk-text-secondary);
    cursor: pointer;
    user-select: none;
    background: transparent;
    border: none;
    border-radius: var(--tk-border-radius-sm);
    transition: all var(--tk-transition-fast);

    &:hover {
      color: var(--tk-text-primary);
    }

    &--active {
      color: var(--tk-primary-color);
      background-color: var(--tk-bg-surface);
    }
  }
}

// ---- Responsive ----
@media (max-width: 768px) {
  .tk-settings__row {
    grid-template-columns: 1fr;
    gap: var(--tk-spacing-sm);
  }

  .tk-settings__row-label {
    padding-top: 0;
  }
}
</style>
