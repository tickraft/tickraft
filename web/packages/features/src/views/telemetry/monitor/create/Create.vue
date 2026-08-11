// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Unified monitor point create page.
 *
 * Replaces the separate prober create and listener webhook pages.
 * Provides a mode selector (Active Probing / Passive Receiving) and a
 * type selector that changes based on the selected mode. The available
 * types are loaded from the backend API (GET /telemetry/probers and
 * GET /telemetry/listeners), which returns only the types supported by
 * the current runtime. The config form is dynamically loaded based on
 * the selected type.
 */
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { ArrowLeft, Check, CopyDocument, InfoFilled } from '@element-plus/icons-vue'
import type { Asset } from '../../../../types/asset'
import type {
  ListenerTypeInfo,
  MonitorCreateParams,
  MonitorMode,
  MonitorType,
  ProberTypeInfo,
} from '../../../../types/telemetry'
import {
  getAssets,
  createMonitor,
  getMonitor,
  updateMonitor,
  getProbers,
  getListeners,
} from '../../../../api/telemetry'

interface IcmpConfig {
  host: string
  count: number
}

interface TcpConfig {
  host: string
  port: number
}

interface HttpConfig {
  method: string
  url: string
  expectCode: number
  headers: string
}

interface WebhookConfig {
  secret: string
  authType: 'hmac' | 'asset-key'
}

const router = useRouter()
const route = useRoute()
const { t } = useI18n()

const formRef = ref<FormInstance>()
const loading = ref(false)
const assetLoading = ref(false)
const assets = ref<Asset[]>([])
const isEdit = computed(() => !!route.params.id)
const editId = computed(() => Number(route.params.id) || 0)

/** Prober and listener types loaded from the backend API */
const proberTypes = ref<ProberTypeInfo[]>([])
const listenerTypes = ref<ListenerTypeInfo[]>([])

const form = reactive<MonitorCreateParams>({
  name: '',
  description: '',
  assetType: 'host',
  mode: 'active',
  type: 'icmp',
  schedule: '60s',
  enabled: true,
  config: {},
})

/** Currently selected asset ID (bound to the el-select). Separate from
 *  form.assetType because multiple assets can share the same assetType,
 *  causing the select to collide. */
const selectedAssetId = ref<number | undefined>(undefined)

/** Active mode configs */
const icmpConfig = reactive<IcmpConfig>({ host: '', count: 4 })
const tcpConfig = reactive<TcpConfig>({ host: '', port: 0 })
const httpConfig = reactive<HttpConfig>({
  method: 'GET',
  url: '',
  expectCode: 200,
  headers: '',
})

/** Passive mode config */
const webhookConfig = reactive<WebhookConfig>({
  secret: '',
  authType: 'hmac',
})

/** Static dot color mapping for known type identifiers */
const TYPE_DOT_COLORS: Record<string, string> = {
  icmp: 'var(--tk-info-color, #909399)',
  tcp: 'var(--tk-success-color, #67c23a)',
  http: 'var(--tk-primary-color, #409eff)',
  webhook: 'var(--tk-warning-color, #e6a23c)',
  dns: 'var(--tk-warning-color, #e6a23c)',
  udp: 'var(--tk-warning-color, #e6a23c)',
  ssl: 'var(--tk-danger-color, #f56c6c)',
}

/** Selected asset — looked up by the selectedAssetId ref */
const selectedAsset = computed<Asset | undefined>(() =>
  assets.value.find((r) => r.id === selectedAssetId.value),
)

/** Asset description */
const assetDesc = computed(() => {
  const res = selectedAsset.value
  if (!res) return ''
  return t('telemetry.monitor.create.assetDesc', {
    type: t(`telemetry.asset.type.${res.assetType}`),
    key: res.assetKey,
    status: t(`common.status.${res.status}`),
  })
})

/** Type options based on selected mode, populated from backend API response */
const typeOptions = computed(() => {
  const types = form.mode === 'active' ? proberTypes.value : listenerTypes.value
  return types.map((tp) => ({
    value: tp.type as MonitorType,
    labelKey: `telemetry.monitor.type.${tp.type}`,
    descKey: `telemetry.monitor.create.typeDesc.${tp.type}`,
    dot: TYPE_DOT_COLORS[tp.type] || 'var(--tk-text-secondary)',
  }))
})

/** Mode options */
const modeOptions = computed(() => [
  { value: 'active' as MonitorMode, labelKey: 'telemetry.monitor.create.modeActive', descKey: 'telemetry.monitor.create.modeActiveDesc' },
  { value: 'passive' as MonitorMode, labelKey: 'telemetry.monitor.create.modePassive', descKey: 'telemetry.monitor.create.modePassiveDesc' },
])

/** Form validation rules */
const rules = computed<FormRules<MonitorCreateParams>>(() => ({
  name: [
    { required: true, message: t('telemetry.monitor.create.nameRequired'), trigger: 'blur' },
  ],
  schedule: [
    { required: true, message: t('telemetry.monitor.create.scheduleRequired'), trigger: 'blur' },
  ],
}))

/** Reset type when mode changes — select the first available type for the new mode */
watch(() => form.mode, (newMode) => {
  const types = newMode === 'active' ? proberTypes.value : listenerTypes.value
  if (types.length > 0 && !types.some((tp) => tp.type === form.type)) {
    form.type = types[0].type as MonitorType
  }
})

/** Build config object based on selected type */
function buildConfig(): Record<string, unknown> {
  switch (form.type) {
    case 'icmp':
      return { host: icmpConfig.host, count: icmpConfig.count }
    case 'tcp':
      return { host: tcpConfig.host, port: tcpConfig.port }
    case 'http':
      return {
        method: httpConfig.method,
        url: httpConfig.url,
        expectCode: httpConfig.expectCode,
        headers: httpConfig.headers || undefined,
      }
    case 'webhook':
      return {
        secret: webhookConfig.secret || undefined,
        authType: webhookConfig.authType,
      }
    default:
      return {}
  }
}

/** Live preview of config JSON */
const configPreview = computed(() => JSON.stringify(buildConfig(), null, 2))

/** Copy JSON to clipboard */
async function handleCopyJson(): Promise<void> {
  try {
    await navigator.clipboard.writeText(configPreview.value)
    ElMessage.success(t('telemetry.monitor.create.copySuccess'))
  } catch {
    ElMessage.error(t('common.app.failed'))
  }
}

/** Asset selection handler — updates form.assetType from the chosen asset */
function handleAssetChange(id: number): void {
  const asset = assets.value.find((r) => r.id === id)
  if (asset) {
    form.assetType = asset.assetType
  }
}

/** Submit the form */
async function handleSubmit(): Promise<void> {
  if (!formRef.value) return
  // Dynamic validation based on type
  if (form.type === 'icmp' && !icmpConfig.host) {
    ElMessage.warning(t('telemetry.monitor.create.hostRequired'))
    return
  }
  if (form.type === 'tcp') {
    if (!tcpConfig.host) {
      ElMessage.warning(t('telemetry.monitor.create.hostRequired'))
      return
    }
    if (tcpConfig.port < 1 || tcpConfig.port > 65535) {
      ElMessage.warning(t('telemetry.monitor.create.portRequired'))
      return
    }
  }
  if (form.type === 'http') {
    if (!httpConfig.url) {
      ElMessage.warning(t('telemetry.monitor.create.urlRequired'))
      return
    }
    if (!/^https?:\/\//.test(httpConfig.url)) {
      ElMessage.warning(t('telemetry.monitor.create.urlPattern'))
      return
    }
  }

  try {
    await formRef.value.validate()
  } catch {
    return
  }

  loading.value = true
  try {
    form.config = buildConfig()
    if (isEdit.value && editId.value) {
      await updateMonitor(editId.value, { ...form })
      ElMessage.success(t('telemetry.monitor.create.updateSuccess'))
    } else {
      await createMonitor({ ...form })
      ElMessage.success(t('telemetry.monitor.create.saveSuccess'))
    }
    router.push('/telemetry/monitor/list')
  } catch {
    // Errors are handled centrally by the interceptor
  } finally {
    loading.value = false
  }
}

function handleCancel(): void {
  router.back()
}

/** Load asset list */
async function fetchAssets(): Promise<void> {
  assetLoading.value = true
  try {
    const res = await getAssets({ page: 1, pageSize: 1000 })
    assets.value = res.items
  } catch {
    assets.value = []
  } finally {
    assetLoading.value = false
  }
}

/** Load supported prober types from backend API */
async function fetchProberTypes(): Promise<void> {
  try {
    proberTypes.value = await getProbers()
  } catch {
    proberTypes.value = []
  }
}

/** Load supported listener types from backend API */
async function fetchListenerTypes(): Promise<void> {
  try {
    listenerTypes.value = await getListeners()
  } catch {
    listenerTypes.value = []
  }
}

/** Load existing monitor for edit mode */
async function fetchMonitor(): Promise<void> {
  if (!isEdit.value || !editId.value) return
  loading.value = true
  try {
    const monitor = await getMonitor(editId.value)
    form.name = monitor.name
    form.description = monitor.description ?? ''
    form.assetType = monitor.assetType
    form.mode = monitor.mode
    form.type = monitor.type
    form.schedule = monitor.schedule
    form.enabled = monitor.enabled
    form.config = monitor.config ?? {}

    // Set selectedAssetId to the first asset matching the monitor's assetType
    const matchedAsset = assets.value.find((a) => a.assetType === monitor.assetType)
    selectedAssetId.value = matchedAsset?.id

    // Parse config into reactive form objects
    const config = monitor.config ?? {}
    if (form.type === 'icmp') {
      icmpConfig.host = (config.host as string) ?? ''
      icmpConfig.count = (config.count as number) ?? 4
    } else if (form.type === 'tcp') {
      tcpConfig.host = (config.host as string) ?? ''
      tcpConfig.port = (config.port as number) ?? 0
    } else if (form.type === 'http') {
      httpConfig.method = (config.method as string) ?? 'GET'
      httpConfig.url = (config.url as string) ?? ''
      httpConfig.expectCode = (config.expectCode as number) ?? 200
      httpConfig.headers = (config.headers as string) ?? ''
    } else if (form.type === 'webhook') {
      webhookConfig.secret = (config.secret as string) ?? ''
      webhookConfig.authType = (config.authType as 'hmac' | 'asset-key') ?? 'hmac'
    }
  } catch {
    ElMessage.error(t('telemetry.monitor.create.notFound'))
    router.push('/telemetry/monitor/list')
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  // Load API metadata and assets in parallel before fetching monitor data
  await Promise.all([fetchAssets(), fetchProberTypes(), fetchListenerTypes()])
  if (isEdit.value) {
    void fetchMonitor()
  } else {
    // Set default type to the first available prober type
    if (proberTypes.value.length > 0) {
      form.type = proberTypes.value[0].type as MonitorType
    }
  }
})
</script>

<template>
  <div class="tk-monitor-create">
    <!-- Page header -->
    <div class="tk-monitor-create__header">
      <div class="tk-monitor-create__title-row">
        <el-button
          circle
          class="tk-monitor-create__back"
          @click="handleCancel"
        >
          <el-icon><ArrowLeft /></el-icon>
        </el-button>
        <div class="tk-monitor-create__title-block">
          <div class="tk-monitor-create__eyebrow">
            {{ t('telemetry.monitor.create.eyebrow') }}
          </div>
          <h1 class="tk-monitor-create__title">
            {{ isEdit ? t('telemetry.monitor.create.editTitle') : t('telemetry.monitor.create.title') }}
          </h1>
        </div>
      </div>
      <div class="tk-monitor-create__actions">
        <el-button @click="handleCancel">
          {{ t('telemetry.monitor.create.cancel') }}
        </el-button>
        <el-button
          type="primary"
          :loading="loading"
          @click="handleSubmit"
        >
          {{ t('telemetry.monitor.create.save') }}
        </el-button>
      </div>
    </div>

    <!-- Body grid -->
    <div class="tk-monitor-create__grid">
      <!-- Main column: form sections -->
      <div class="tk-monitor-create__main">
        <el-form
          ref="formRef"
          :model="form"
          :rules="rules"
          label-position="top"
        >
          <!-- Section 1: Basic Info -->
          <div class="tk-form-section">
            <div class="tk-form-section__header">
              <div class="tk-form-section__title">
                <span class="tk-form-section__index">01</span>
                <span>{{ t('telemetry.monitor.create.sectionBasic') }}</span>
              </div>
              <span class="tk-form-section__hint">{{ t('telemetry.monitor.create.sectionBasicHint') }}</span>
            </div>
            <div class="tk-form-section__body">
              <el-form-item
                :label="t('telemetry.monitor.create.name')"
                prop="name"
              >
                <el-input
                  v-model="form.name"
                  :placeholder="t('telemetry.monitor.create.namePlaceholder')"
                />
              </el-form-item>
              <el-form-item :label="t('telemetry.monitor.create.description')">
                <el-input
                  v-model="form.description"
                  type="textarea"
                  :rows="2"
                  :placeholder="t('telemetry.monitor.create.descriptionPlaceholder')"
                />
              </el-form-item>
              <el-form-item :label="t('telemetry.monitor.create.assetType')">
                <el-select
                  v-model="selectedAssetId"
                  :placeholder="t('telemetry.monitor.create.assetTypePlaceholder')"
                  style="width: 100%"
                  @change="handleAssetChange"
                >
                  <el-option
                    v-for="item in assets"
                    :key="item.id"
                    :label="item.name"
                    :value="item.id"
                  />
                </el-select>
                <div
                  v-if="assetDesc"
                  class="tk-monitor-create__asset-desc"
                >
                  {{ assetDesc }}
                </div>
              </el-form-item>
            </div>
          </div>

          <!-- Section 2: Mode & Type -->
          <div class="tk-form-section">
            <div class="tk-form-section__header">
              <div class="tk-form-section__title">
                <span class="tk-form-section__index">02</span>
                <span>{{ t('telemetry.monitor.create.sectionModeType') }}</span>
              </div>
              <span class="tk-form-section__hint">{{ t('telemetry.monitor.create.sectionModeTypeHint') }}</span>
            </div>
            <div class="tk-form-section__body">
              <!-- Mode selector -->
              <el-form-item :label="t('telemetry.monitor.create.mode')">
                <div class="tk-mode-radio">
                  <div
                    v-for="item in modeOptions"
                    :key="item.value"
                    class="tk-mode-radio__option"
                    :class="{ 'tk-mode-radio__option--active': form.mode === item.value }"
                    @click="form.mode = item.value"
                  >
                    <div class="tk-mode-radio__head">
                      <span class="tk-mode-radio__name">{{ t(item.labelKey) }}</span>
                      <span class="tk-mode-radio__dot" />
                    </div>
                    <div class="tk-mode-radio__desc">
                      {{ t(item.descKey) }}
                    </div>
                  </div>
                </div>
              </el-form-item>

              <!-- Type selector -->
              <el-form-item :label="t('telemetry.monitor.create.type')">
                <div class="tk-type-radio">
                  <div
                    v-for="item in typeOptions"
                    :key="item.value"
                    class="tk-type-radio__option"
                    :class="{ 'tk-type-radio__option--active': form.type === item.value }"
                    @click="form.type = item.value"
                  >
                    <div class="tk-type-radio__head">
                      <span class="tk-type-radio__name">
                        <span
                          class="tk-type-radio__name-dot"
                          :style="{ background: item.dot }"
                        />
                        {{ t(item.labelKey) }}
                      </span>
                      <span class="tk-type-radio__dot" />
                    </div>
                    <div class="tk-type-radio__desc">
                      {{ t(item.descKey) }}
                    </div>
                  </div>
                </div>
              </el-form-item>
            </div>
          </div>

          <!-- Section 3: Configuration -->
          <div class="tk-form-section">
            <div class="tk-form-section__header">
              <div class="tk-form-section__title">
                <span class="tk-form-section__index">03</span>
                <span>{{ t('telemetry.monitor.create.sectionConfig') }}</span>
              </div>
              <span class="tk-form-section__hint">{{ t('telemetry.monitor.create.sectionConfigHint') }}</span>
            </div>
            <div class="tk-form-section__body">
              <!-- ICMP parameters -->
              <template v-if="form.type === 'icmp'">
                <el-form-item :label="t('telemetry.monitor.create.targetHost')">
                  <el-input
                    v-model="icmpConfig.host"
                    :placeholder="t('telemetry.monitor.create.targetHostPlaceholder', { example: '10.0.1.11' })"
                  />
                </el-form-item>
                <el-form-item :label="t('telemetry.monitor.create.pingCount')">
                  <el-input-number
                    v-model="icmpConfig.count"
                    :min="1"
                    :max="10"
                  />
                  <div class="tk-form-help">
                    {{ t('telemetry.monitor.create.pingCountHelp') }}
                  </div>
                </el-form-item>
              </template>

              <!-- TCP parameters -->
              <template v-if="form.type === 'tcp'">
                <el-form-item :label="t('telemetry.monitor.create.targetHost')">
                  <el-input
                    v-model="tcpConfig.host"
                    :placeholder="t('telemetry.monitor.create.targetHostPlaceholder', { example: '10.0.2.21' })"
                  />
                </el-form-item>
                <el-form-item :label="t('telemetry.monitor.create.targetPort')">
                  <el-input-number
                    v-model="tcpConfig.port"
                    :min="1"
                    :max="65535"
                    :placeholder="t('telemetry.monitor.create.targetPortPlaceholder')"
                  />
                  <div class="tk-form-help">
                    {{ t('telemetry.monitor.create.targetPortHelp') }}
                  </div>
                </el-form-item>
              </template>

              <!-- HTTP parameters -->
              <template v-if="form.type === 'http'">
                <el-form-item :label="t('telemetry.monitor.create.requestMethod')">
                  <el-select
                    v-model="httpConfig.method"
                    style="width: 120px"
                  >
                    <el-option label="GET" value="GET" />
                    <el-option label="POST" value="POST" />
                    <el-option label="HEAD" value="HEAD" />
                  </el-select>
                </el-form-item>
                <el-form-item :label="t('telemetry.monitor.create.requestUrl')">
                  <el-input
                    v-model="httpConfig.url"
                    :placeholder="t('telemetry.monitor.create.requestUrlPlaceholder')"
                  />
                </el-form-item>
                <el-form-item :label="t('telemetry.monitor.create.expectCode')">
                  <el-input-number
                    v-model="httpConfig.expectCode"
                    :min="100"
                    :max="599"
                  />
                </el-form-item>
                <el-form-item :label="t('telemetry.monitor.create.requestHeaders')">
                  <el-input
                    v-model="httpConfig.headers"
                    type="textarea"
                    :rows="3"
                    :placeholder="t('telemetry.monitor.create.requestHeadersPlaceholder')"
                  />
                  <div class="tk-form-help">
                    {{ t('telemetry.monitor.create.requestHeadersHelp') }}
                  </div>
                </el-form-item>
              </template>

              <!-- Webhook parameters -->
              <template v-if="form.type === 'webhook'">
                <el-form-item :label="t('telemetry.monitor.create.authMethod')">
                  <el-radio-group v-model="webhookConfig.authType">
                    <el-radio value="hmac">
                      {{ t('telemetry.monitor.create.hmacAuth') }}
                    </el-radio>
                    <el-radio value="asset-key">
                      {{ t('telemetry.monitor.create.assetKeyAuth') }}
                    </el-radio>
                  </el-radio-group>
                  <div class="tk-form-help">
                    {{ webhookConfig.authType === 'hmac' ? t('telemetry.monitor.create.hmacDesc') : t('telemetry.monitor.create.assetKeyDesc') }}
                  </div>
                </el-form-item>
                <el-form-item :label="t('telemetry.monitor.create.webhookSecret')">
                  <el-input
                    v-model="webhookConfig.secret"
                    type="password"
                    show-password
                    :placeholder="t('telemetry.monitor.create.webhookSecretPlaceholder')"
                  />
                  <div class="tk-form-help">
                    {{ t('telemetry.monitor.create.webhookSecretHelp') }}
                  </div>
                </el-form-item>
              </template>

              <!-- Common parameters -->
              <el-form-item :label="t('telemetry.monitor.create.schedule')">
                <el-input
                  v-model="form.schedule"
                  :placeholder="t('telemetry.monitor.create.schedulePlaceholder')"
                />
                <div class="tk-form-help">
                  {{ t('telemetry.monitor.create.scheduleHelp') }}
                </div>
              </el-form-item>
              <el-form-item :label="t('telemetry.monitor.create.enable')">
                <el-switch v-model="form.enabled" />
              </el-form-item>
            </div>
          </div>
        </el-form>

        <!-- Sticky footer -->
        <div class="tk-form-footer">
          <el-button @click="handleCancel">
            {{ t('telemetry.monitor.create.cancel') }}
          </el-button>
          <el-button
            type="primary"
            :loading="loading"
            @click="handleSubmit"
          >
            <el-icon class="tk-form-footer__icon"><Check /></el-icon>
            {{ t('telemetry.monitor.create.save') }}
          </el-button>
        </div>
      </div>

      <!-- Side column: JSON preview + tips -->
      <aside class="tk-monitor-create__side">
        <!-- JSON preview card -->
        <div class="tk-code-block">
          <div class="tk-code-block__bar">
            <span class="tk-code-block__label">{{ t('telemetry.monitor.create.jsonPreview') }}</span>
            <el-button
              link
              type="primary"
              size="small"
              @click="handleCopyJson"
            >
              <el-icon><CopyDocument /></el-icon>
              {{ t('telemetry.monitor.create.copyJson') }}
            </el-button>
          </div>
          <pre class="tk-code-block__code">{{ configPreview }}</pre>
        </div>

        <!-- Tips card -->
        <div class="tk-tips-card">
          <div class="tk-tips-card__title">
            <el-icon :size="14"><InfoFilled /></el-icon>
            <span>{{ t('telemetry.monitor.create.tipsTitle') }}</span>
          </div>
          <ul class="tk-tips-card__list">
            <li>{{ t('telemetry.monitor.create.tipsItem1') }}</li>
            <li>{{ t('telemetry.monitor.create.tipsItem2') }}</li>
            <li>{{ t('telemetry.monitor.create.tipsItem3') }}</li>
            <li>{{ t('telemetry.monitor.create.tipsItem4') }}</li>
          </ul>
        </div>
      </aside>
    </div>
  </div>
</template>

<style scoped lang="scss">
.tk-monitor-create {
  max-width: var(--tk-content-max-width, 1200px);
  padding: var(--tk-spacing-lg, 40px) var(--tk-content-padding-x, 24px) var(--tk-spacing-xl, 96px);
  margin: 0 auto;

  &__header {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-lg, 32px);
    align-items: flex-start;
    justify-content: space-between;
    margin-bottom: var(--tk-spacing-lg, 40px);
  }

  &__title-row {
    display: flex;
    gap: var(--tk-spacing-sm, 16px);
    align-items: center;
  }

  &__back {
    flex-shrink: 0;
  }

  &__title-block {
    min-width: 0;
  }

  &__eyebrow {
    margin-bottom: 4px;
    font-family: var(--tk-font-mono, 'Monaco', monospace);
    font-size: var(--tk-font-size-xs, 12px);
    color: var(--tk-text-secondary, #909399);
    text-transform: uppercase;
    letter-spacing: 0.1em;
  }

  &__title {
    margin: 0;
    font-size: var(--tk-font-size-2xl, 24px);
    font-weight: var(--tk-font-weight-bold, 700);
    line-height: 1.1;
    color: var(--tk-text-primary, #303133);
    letter-spacing: -0.02em;
  }

  &__actions {
    display: flex;
    gap: var(--tk-spacing-sm, 12px);
    align-items: center;
  }

  &__grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 320px;
    gap: var(--tk-spacing-lg, 32px);
    align-items: start;
  }

  &__main {
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-lg, 32px);
    min-width: 0;
  }

  &__side {
    position: sticky;
    top: var(--tk-spacing-md, 24px);
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-md, 24px);
    min-width: 0;
  }

  &__asset-desc {
    margin-top: 4px;
    font-size: var(--tk-font-size-xs, 12px);
    line-height: 1.5;
    color: var(--tk-text-secondary, #909399);
  }
}

/* Section card */
.tk-form-section {
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

  &__title {
    display: flex;
    gap: var(--tk-spacing-sm, 16px);
    align-items: center;
    font-size: var(--tk-font-size-md, 16px);
    font-weight: var(--tk-font-weight-semibold, 600);
    color: var(--tk-text-primary, #303133);
  }

  &__index {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    font-family: var(--tk-font-mono, 'Monaco', monospace);
    font-size: var(--tk-font-size-xs, 12px);
    font-weight: var(--tk-font-weight-bold, 700);
    color: var(--tk-primary-color, #409eff);
    background: var(--tk-primary-color-bg, rgb(64 158 255 / 10%));
    border: 1px solid var(--tk-primary-color-border, rgb(64 158 255 / 20%));
    border-radius: var(--tk-radius-sm, 4px);
  }

  &__hint {
    font-family: var(--tk-font-mono, 'Monaco', monospace);
    font-size: var(--tk-font-size-xs, 12px);
    color: var(--tk-text-secondary, #909399);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  &__body {
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-md, 24px);
    padding: var(--tk-spacing-lg, 40px);
  }
}

.tk-form-help {
  margin-top: var(--tk-spacing-xs, 8px);
  font-size: var(--tk-font-size-xs, 12px);
  line-height: 1.5;
  color: var(--tk-text-secondary, #909399);
}

/* Mode radio */
.tk-mode-radio {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: var(--tk-spacing-sm, 12px);
  width: 100%;
}

.tk-mode-radio__option {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: var(--tk-spacing-md, 16px) var(--tk-spacing-sm, 12px);
  cursor: pointer;
  background: var(--tk-bg-surface, var(--tk-bg-color, #fff));
  border: 1px solid var(--tk-border-color, #e4e7ed);
  border-radius: var(--tk-radius-md, 8px);
  transition: all 0.2s ease;

  &:hover {
    background: var(--tk-bg-surface-hover, #f5f7fa);
    border-color: var(--tk-border-color-dark, #d0d3d9);
  }

  &--active {
    background: var(--tk-primary-color-bg, rgb(64 158 255 / 8%));
    border-color: var(--tk-primary-color, #409eff);
    box-shadow: 0 0 0 2px rgb(64 158 255 / 15%);
  }

  &__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  &__name {
    font-family: var(--tk-font-mono, 'Monaco', monospace);
    font-size: var(--tk-font-size-sm, 14px);
    font-weight: var(--tk-font-weight-semibold, 600);
    color: var(--tk-text-primary, #303133);
    letter-spacing: 0.03em;
  }

  &__dot {
    position: relative;
    width: 14px;
    height: 14px;
    border: 1.5px solid var(--tk-border-color-dark, #d0d3d9);
    border-radius: 50%;
  }

  &--active &__dot {
    border-color: var(--tk-primary-color, #409eff);

    &::after {
      position: absolute;
      top: 2px;
      left: 2px;
      width: 8px;
      height: 8px;
      content: "";
      background: var(--tk-primary-color, #409eff);
      border-radius: 50%;
    }
  }

  &__desc {
    font-size: var(--tk-font-size-xs, 12px);
    line-height: 1.4;
    color: var(--tk-text-secondary, #909399);
  }
}

/* Type radio grid */
.tk-type-radio {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: var(--tk-spacing-sm, 12px);
  width: 100%;
}

.tk-type-radio__option {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: var(--tk-spacing-md, 16px) var(--tk-spacing-sm, 12px);
  cursor: pointer;
  background: var(--tk-bg-surface, var(--tk-bg-color, #fff));
  border: 1px solid var(--tk-border-color, #e4e7ed);
  border-radius: var(--tk-radius-md, 8px);
  transition: all 0.2s ease;

  &:hover {
    background: var(--tk-bg-surface-hover, #f5f7fa);
    border-color: var(--tk-border-color-dark, #d0d3d9);
  }

  &--active {
    background: var(--tk-primary-color-bg, rgb(64 158 255 / 8%));
    border-color: var(--tk-primary-color, #409eff);
    box-shadow: 0 0 0 2px rgb(64 158 255 / 15%);
  }

  &__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  &__name {
    display: inline-flex;
    gap: var(--tk-spacing-sm, 12px);
    align-items: center;
    font-family: var(--tk-font-mono, 'Monaco', monospace);
    font-size: var(--tk-font-size-sm, 14px);
    font-weight: var(--tk-font-weight-semibold, 600);
    color: var(--tk-text-primary, #303133);
    letter-spacing: 0.03em;
  }

  &__name-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
  }

  &__dot {
    position: relative;
    width: 14px;
    height: 14px;
    border: 1.5px solid var(--tk-border-color-dark, #d0d3d9);
    border-radius: 50%;
  }

  &--active &__dot {
    border-color: var(--tk-primary-color, #409eff);

    &::after {
      position: absolute;
      top: 2px;
      left: 2px;
      width: 8px;
      height: 8px;
      content: "";
      background: var(--tk-primary-color, #409eff);
      border-radius: 50%;
    }
  }

  &__desc {
    font-size: var(--tk-font-size-xs, 12px);
    line-height: 1.4;
    color: var(--tk-text-secondary, #909399);
  }
}

/* Code block */
.tk-code-block {
  overflow: hidden;
  background: var(--tk-bg-fill-blank, #fafafa);
  border: 1px solid var(--tk-border-color, #e4e7ed);
  border-radius: var(--tk-radius-md, 8px);

  &__bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px var(--tk-spacing-sm, 16px);
    background: var(--tk-bg-fill-light, #f5f7fa);
    border-bottom: 1px solid var(--tk-border-color-light, #ebeef5);
  }

  &__label {
    font-family: var(--tk-font-mono, 'Monaco', monospace);
    font-size: var(--tk-font-size-xs, 12px);
    color: var(--tk-text-secondary, #909399);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  &__code {
    padding: var(--tk-spacing-md, 16px);
    margin: 0;
    font-family: var(--tk-font-mono, 'Monaco', monospace);
    font-size: var(--tk-font-size-sm, 14px);
    line-height: 1.5;
    color: var(--tk-text-primary, #303133);
    word-break: break-all;
    white-space: pre-wrap;
  }
}

/* Tips card */
.tk-tips-card {
  padding: var(--tk-spacing-md, 24px);
  background: var(--tk-info-color-bg, rgb(64 158 255 / 6%));
  border: 1px solid var(--tk-info-color-border, rgb(64 158 255 / 15%));
  border-radius: var(--tk-radius-md, 8px);

  &__title {
    display: flex;
    gap: 6px;
    align-items: center;
    margin-bottom: var(--tk-spacing-sm, 12px);
    font-size: var(--tk-font-size-sm, 14px);
    font-weight: var(--tk-font-weight-semibold, 600);
    color: var(--tk-info-color-text, #409eff);
  }

  &__list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding-left: 16px;
    margin: 0;
    font-size: var(--tk-font-size-xs, 12px);
    line-height: 1.5;
    color: var(--tk-text-regular, #606266);
  }
}

/* Sticky footer */
.tk-form-footer {
  position: sticky;
  bottom: 0;
  display: flex;
  gap: var(--tk-spacing-sm, 12px);
  align-items: center;
  justify-content: flex-end;
  padding: var(--tk-spacing-md, 20px) var(--tk-spacing-lg, 40px);
  background: var(--tk-bg-surface, var(--tk-bg-color, #fff));
  border: 1px solid var(--tk-border-color, #e4e7ed);
  border-radius: var(--tk-radius-lg, 12px);
  box-shadow: 0 2px 12px rgb(0 0 0 / 8%);

  &__icon {
    margin-right: 4px;
  }
}

/* Responsive */
@media (max-width: 1100px) {
  .tk-monitor-create__grid {
    grid-template-columns: 1fr;
  }

  .tk-monitor-create__side {
    position: static;
  }
}

@media (max-width: 720px) {
  .tk-type-radio {
    grid-template-columns: 1fr;
  }

  .tk-mode-radio {
    grid-template-columns: 1fr;
  }
}
</style>
