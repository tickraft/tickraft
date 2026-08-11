// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { isValidUrl } from '@tickraft/core'
import {
  createRemediationRule,
  getRemediationRule,
  updateRemediationRule,
} from '../../../../../api/prism'
import type {
  RemediationExecutorConfig,
  RemediationRulePayload,
} from '../../../../../api/prism'

/** A single header row in the dynamic headers editor */
interface HeaderRow {
  key: string
  value: string
}

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const formRef = ref<FormInstance>()
const loading = ref(false)
const saving = ref(false)

/** Whether in edit mode */
const isEdit = computed(() => !!route.params.id)
/** Edit rule ID */
const editId = computed(() => Number(route.params.id) || 0)

/** Page title */
const pageTitle = computed(() =>
  isEdit.value ? t('prism.remediation.rule.form.titleEdit') : t('prism.remediation.rule.form.titleCreate'),
)

/** Mode badge label (CREATE / EDIT) */
const modeLabel = computed(() =>
  isEdit.value ? t('prism.remediation.rule.form.modeEdit') : t('prism.remediation.rule.form.modeCreate'),
)

/** Form data */
const form = ref({
  name: '',
  description: '',
  triggerEventType: 'alert.firing' as string,
  conditionExpr: '',
  executorType: 'webhook' as string,
  // Executor config fields
  executorUrl: '',
  executorMethod: 'POST' as 'POST' | 'PUT' | 'GET',
  executorTimeout: 10,
  executorBody: '',
  headers: [] as HeaderRow[],
  // Advanced
  cooldown: 300,
  circuitBreakerThreshold: 3,
  enabled: true,
})

/** Trigger event type options (CE supports alert.firing / alert.critical) */
const triggerTypeOptions = computed(() => [
  { value: 'alert.firing', label: t('prism.remediation.rule.triggerType.alert.firing') },
  { value: 'alert.critical', label: t('prism.remediation.rule.triggerType.alert.critical') },
])

/** Executor type options (CE supports webhook / http) */
const executorTypeOptions = computed(() => [
  { value: 'webhook', label: t('prism.remediation.rule.executorType.webhook') },
  { value: 'http', label: t('prism.remediation.rule.executorType.http') },
])

/** HTTP method options */
const methodOptions = computed(() => [
  { value: 'POST', label: 'POST' },
  { value: 'PUT', label: 'PUT' },
  { value: 'GET', label: 'GET' },
])

/** Whether the body field should be shown (only for http executor type) */
const showBody = computed(() => form.value.executorType === 'http')

/** Form validation rules */
const rules = computed<FormRules>(() => ({
  name: [
    { required: true, message: t('prism.remediation.rule.form.namePlaceholder'), trigger: 'blur' },
    { max: 128, message: t('prism.remediation.rule.form.namePlaceholder'), trigger: 'blur' },
  ],
  triggerEventType: [
    { required: true, message: t('prism.remediation.rule.form.triggerEventTypePlaceholder'), trigger: 'change' },
  ],
  executorType: [
    { required: true, message: t('prism.remediation.rule.form.executorTypePlaceholder'), trigger: 'change' },
  ],
  executorUrl: [
    {
      required: true,
      validator: (_rule, value: string, callback) => {
        if (!value) {
          callback(new Error(t('prism.remediation.rule.form.executorUrlPlaceholder')))
        } else if (!isValidUrl(value) || !/^https?:\/\//.test(value)) {
          callback(new Error(t('prism.remediation.rule.form.invalidUrl')))
        } else {
          callback()
        }
      },
      trigger: 'blur',
    },
  ],
  executorTimeout: [
    { required: true, message: t('prism.remediation.rule.form.executorTimeoutPlaceholder'), trigger: 'blur' },
    { type: 'number', min: 1, max: 60, message: t('prism.remediation.rule.form.executorTimeoutPlaceholder'), trigger: 'blur' },
  ],
  cooldown: [
    { required: true, message: t('prism.remediation.rule.form.cooldownPlaceholder'), trigger: 'blur' },
    { type: 'number', min: 1, max: 86400, message: t('prism.remediation.rule.form.cooldownPlaceholder'), trigger: 'blur' },
  ],
  circuitBreakerThreshold: [
    { required: true, message: t('prism.remediation.rule.form.circuitBreakerThresholdPlaceholder'), trigger: 'blur' },
    { type: 'number', min: 1, max: 100, message: t('prism.remediation.rule.form.circuitBreakerThresholdPlaceholder'), trigger: 'blur' },
  ],
}))

/** Parse a timeout duration string (e.g. "10s") into seconds */
function parseTimeoutSeconds(timeoutStr: string): number {
  const match = timeoutStr.match(/^(\d+)s$/)
  if (match) return Number(match[1])
  const parsed = parseInt(timeoutStr, 10)
  return Number.isNaN(parsed) ? 10 : parsed
}

/** Load rule data into the form when editing */
async function loadRule(): Promise<void> {
  if (!isEdit.value || !editId.value) return
  loading.value = true
  try {
    const rule = await getRemediationRule(editId.value)
    form.value.name = rule.name
    form.value.description = rule.description
    form.value.triggerEventType = rule.triggerEventType
    form.value.conditionExpr = rule.conditionExpr
    form.value.executorType = rule.executorType
    form.value.cooldown = rule.cooldown
    form.value.circuitBreakerThreshold = rule.circuitBreakerThreshold
    form.value.enabled = rule.enabled

    // Parse executor config from the JSON config string
    try {
      const cfg = JSON.parse(rule.executorConfig) as Partial<RemediationExecutorConfig>
      form.value.executorUrl = cfg.url ?? ''
      form.value.executorMethod = (cfg.method as 'POST' | 'PUT' | 'GET') ?? 'POST'
      form.value.executorTimeout = parseTimeoutSeconds(cfg.timeout ?? '10s')
      form.value.executorBody = cfg.body ?? ''
      const headerEntries = Object.entries(cfg.headers ?? {})
      form.value.headers = headerEntries.map(([key, value]) => ({ key, value: String(value) }))
    } catch {
      form.value.executorUrl = ''
      form.value.executorMethod = 'POST'
      form.value.executorTimeout = 10
      form.value.executorBody = ''
      form.value.headers = []
    }
  } catch {
    ElMessage.warning(t('prism.remediation.rule.form.notFound'))
    router.push('/prism/remediation/rule/list')
  } finally {
    loading.value = false
  }
}

/** Reset form to default create-mode state */
function resetForm(): void {
  form.value = {
    name: '',
    description: '',
    triggerEventType: 'alert.firing',
    conditionExpr: '',
    executorType: 'webhook',
    executorUrl: '',
    executorMethod: 'POST',
    executorTimeout: 10,
    executorBody: '',
    headers: [],
    cooldown: 300,
    circuitBreakerThreshold: 3,
    enabled: true,
  }
  formRef.value?.resetFields()
}

/** Add a new header row */
function addHeader(): void {
  form.value.headers.push({ key: '', value: '' })
}

/** Remove a header row by index */
function removeHeader(index: number): void {
  form.value.headers.splice(index, 1)
}

/** Build the executor config JSON string from form data */
function buildExecutorConfig(): string {
  const headers: Record<string, string> = {}
  for (const row of form.value.headers) {
    const key = row.key.trim()
    if (key) {
      headers[key] = row.value
    }
  }
  const config: RemediationExecutorConfig = {
    url: form.value.executorUrl.trim(),
    method: form.value.executorMethod,
    timeout: `${form.value.executorTimeout}s`,
    headers,
  }
  if (showBody.value && form.value.executorBody.trim()) {
    config.body = form.value.executorBody.trim()
  }
  return JSON.stringify(config)
}

/** Validate the form (callback wrapped as a Promise) */
function validateForm(): Promise<boolean> {
  return new Promise((resolve) => {
    formRef.value?.validate((valid: boolean) => resolve(valid))
  })
}

/** Build the submission payload */
function buildPayload(): RemediationRulePayload {
  return {
    name: form.value.name.trim(),
    description: form.value.description.trim(),
    triggerEventType: form.value.triggerEventType,
    conditionExpr: form.value.conditionExpr.trim(),
    executorType: form.value.executorType,
    executorConfig: buildExecutorConfig(),
    cooldown: Number(form.value.cooldown),
    circuitBreakerThreshold: Number(form.value.circuitBreakerThreshold),
    enabled: form.value.enabled,
  }
}

/** Submit and save */
async function handleSubmit(): Promise<void> {
  const valid = await validateForm()
  if (!valid) {
    ElMessage.warning(t('prism.remediation.rule.form.validateError'))
    return
  }
  saving.value = true
  try {
    const payload = buildPayload()
    if (isEdit.value && editId.value) {
      await updateRemediationRule(editId.value, payload)
      ElMessage.success(t('prism.remediation.rule.form.updatedToast'))
    } else {
      await createRemediationRule(payload)
      ElMessage.success(t('prism.remediation.rule.form.createdToast'))
    }
    router.push('/prism/remediation/rule/list')
  } finally {
    saving.value = false
  }
}

/** Cancel and go back to list */
function handleBack(): void {
  router.push('/prism/remediation/rule/list')
}

onMounted(() => {
  if (isEdit.value) {
    loadRule()
  } else {
    resetForm()
  }
})
</script>

<template>
  <div
    v-loading="loading"
    class="tk-prism-remediation-rule-edit tk-page-container"
  >
    <!-- Header: title + mode badge + subtitle + back -->
    <header class="tk-prism-remediation-rule-edit__header">
      <div class="tk-prism-remediation-rule-edit__header-left">
        <span class="tk-prism-remediation-rule-edit__mode-badge">
          {{ modeLabel }}
        </span>
        <div class="tk-prism-remediation-rule-edit__heading">
          <h1 class="tk-prism-remediation-rule-edit__title">
            {{ pageTitle }}
          </h1>
          <p class="tk-prism-remediation-rule-edit__subtitle">
            {{ t('prism.remediation.rule.form.subtitle') }}
          </p>
        </div>
      </div>
      <el-button @click="handleBack">
        &larr; {{ t('prism.remediation.rule.form.back') }}
      </el-button>
    </header>

    <!-- Form card -->
    <div class="tk-prism-remediation-rule-edit__form-card">
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="140px"
      >
        <!-- Section 01: Basic Info -->
        <section class="tk-prism-remediation-rule-edit__section">
          <h3 class="tk-prism-remediation-rule-edit__section-title">
            <span class="tk-prism-remediation-rule-edit__section-index">01</span>
            {{ t('prism.remediation.rule.form.sectionBasic') }}
          </h3>
          <el-form-item
            :label="t('prism.remediation.rule.form.name')"
            prop="name"
          >
            <el-input
              v-model="form.name"
              :placeholder="t('prism.remediation.rule.form.namePlaceholder')"
              maxlength="128"
              show-word-limit
            />
          </el-form-item>
          <el-form-item :label="t('prism.remediation.rule.form.description')">
            <el-input
              v-model="form.description"
              type="textarea"
              :rows="2"
              maxlength="500"
              show-word-limit
              :placeholder="t('prism.remediation.rule.form.descriptionPlaceholder')"
            />
          </el-form-item>
        </section>

        <!-- Section 02: Trigger Condition -->
        <section class="tk-prism-remediation-rule-edit__section">
          <h3 class="tk-prism-remediation-rule-edit__section-title">
            <span class="tk-prism-remediation-rule-edit__section-index">02</span>
            {{ t('prism.remediation.rule.form.sectionTrigger') }}
          </h3>
          <el-form-item
            :label="t('prism.remediation.rule.form.triggerEventType')"
            prop="triggerEventType"
          >
            <el-select
              v-model="form.triggerEventType"
              :placeholder="t('prism.remediation.rule.form.triggerEventTypePlaceholder')"
              class="tk-prism-remediation-rule-edit__block"
            >
              <el-option
                v-for="opt in triggerTypeOptions"
                :key="opt.value"
                :label="opt.label"
                :value="opt.value"
              />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('prism.remediation.rule.form.conditionExpr')">
            <el-input
              v-model="form.conditionExpr"
              type="textarea"
              :rows="2"
              :placeholder="t('prism.remediation.rule.form.conditionExprPlaceholder')"
            />
            <div class="tk-prism-remediation-rule-edit__help">
              {{ t('prism.remediation.rule.form.conditionExprHelp') }}
            </div>
          </el-form-item>
        </section>

        <!-- Section 03: Executor Configuration -->
        <section class="tk-prism-remediation-rule-edit__section">
          <h3 class="tk-prism-remediation-rule-edit__section-title">
            <span class="tk-prism-remediation-rule-edit__section-index">03</span>
            {{ t('prism.remediation.rule.form.sectionExecutor') }}
          </h3>
          <el-form-item
            :label="t('prism.remediation.rule.form.executorType')"
            prop="executorType"
          >
            <el-select
              v-model="form.executorType"
              :placeholder="t('prism.remediation.rule.form.executorTypePlaceholder')"
              class="tk-prism-remediation-rule-edit__block"
            >
              <el-option
                v-for="opt in executorTypeOptions"
                :key="opt.value"
                :label="opt.label"
                :value="opt.value"
              />
            </el-select>
          </el-form-item>
          <el-form-item
            :label="t('prism.remediation.rule.form.executorUrl')"
            prop="executorUrl"
          >
            <el-input
              v-model="form.executorUrl"
              :placeholder="t('prism.remediation.rule.form.executorUrlPlaceholder')"
            />
          </el-form-item>
          <div class="tk-prism-remediation-rule-edit__form-grid">
            <el-form-item :label="t('prism.remediation.rule.form.executorMethod')">
              <el-select
                v-model="form.executorMethod"
                class="tk-prism-remediation-rule-edit__block"
              >
                <el-option
                  v-for="opt in methodOptions"
                  :key="opt.value"
                  :label="opt.label"
                  :value="opt.value"
                />
              </el-select>
            </el-form-item>
            <el-form-item
              :label="t('prism.remediation.rule.form.executorTimeout')"
              prop="executorTimeout"
            >
              <el-input-number
                v-model="form.executorTimeout"
                :min="1"
                :max="60"
                :step="1"
                :placeholder="t('prism.remediation.rule.form.executorTimeoutPlaceholder')"
                class="tk-prism-remediation-rule-edit__block"
              />
              <div class="tk-prism-remediation-rule-edit__help">
                {{ t('prism.remediation.rule.form.executorTimeoutHelp') }}
              </div>
            </el-form-item>
          </div>

          <!-- Dynamic headers editor -->
          <el-form-item :label="t('prism.remediation.rule.form.executorHeaders')">
            <div class="tk-prism-remediation-rule-edit__headers">
              <div
                v-for="(header, index) in form.headers"
                :key="index"
                class="tk-prism-remediation-rule-edit__header-row"
              >
                <el-input
                  v-model="header.key"
                  :placeholder="t('prism.remediation.rule.form.executorHeaderKey')"
                  class="tk-prism-remediation-rule-edit__header-key"
                />
                <el-input
                  v-model="header.value"
                  :placeholder="t('prism.remediation.rule.form.executorHeaderValue')"
                  class="tk-prism-remediation-rule-edit__header-value"
                />
                <el-button
                  link
                  type="danger"
                  @click="removeHeader(index)"
                >
                  {{ t('prism.remediation.rule.list.delete') }}
                </el-button>
              </div>
              <el-button
                link
                type="primary"
                @click="addHeader"
              >
                + {{ t('prism.remediation.rule.form.executorAddHeader') }}
              </el-button>
            </div>
          </el-form-item>

          <!-- Request body (only for http executor type) -->
          <el-form-item
            v-if="showBody"
            :label="t('prism.remediation.rule.form.executorBody')"
          >
            <el-input
              v-model="form.executorBody"
              type="textarea"
              :rows="3"
              :placeholder="t('prism.remediation.rule.form.executorBodyPlaceholder')"
            />
            <div class="tk-prism-remediation-rule-edit__help">
              {{ t('prism.remediation.rule.form.executorBodyHelp') }}
            </div>
          </el-form-item>
        </section>

        <!-- Section 04: Advanced -->
        <section class="tk-prism-remediation-rule-edit__section">
          <h3 class="tk-prism-remediation-rule-edit__section-title">
            <span class="tk-prism-remediation-rule-edit__section-index">04</span>
            {{ t('prism.remediation.rule.form.sectionAdvanced') }}
          </h3>
          <div class="tk-prism-remediation-rule-edit__form-grid">
            <el-form-item
              :label="t('prism.remediation.rule.form.cooldown')"
              prop="cooldown"
            >
              <el-input-number
                v-model="form.cooldown"
                :min="1"
                :max="86400"
                :step="1"
                :placeholder="t('prism.remediation.rule.form.cooldownPlaceholder')"
                class="tk-prism-remediation-rule-edit__block"
              />
              <div class="tk-prism-remediation-rule-edit__help">
                {{ t('prism.remediation.rule.form.cooldownHelp') }}
              </div>
            </el-form-item>
            <el-form-item
              :label="t('prism.remediation.rule.form.circuitBreakerThreshold')"
              prop="circuitBreakerThreshold"
            >
              <el-input-number
                v-model="form.circuitBreakerThreshold"
                :min="1"
                :max="100"
                :step="1"
                :placeholder="t('prism.remediation.rule.form.circuitBreakerThresholdPlaceholder')"
                class="tk-prism-remediation-rule-edit__block"
              />
              <div class="tk-prism-remediation-rule-edit__help">
                {{ t('prism.remediation.rule.form.circuitBreakerThresholdHelp') }}
              </div>
            </el-form-item>
          </div>
          <el-form-item :label="t('prism.remediation.rule.form.enabled')">
            <el-switch v-model="form.enabled" />
          </el-form-item>
        </section>

        <!-- Footer actions -->
        <div class="tk-prism-remediation-rule-edit__footer">
          <el-button @click="handleBack">
            {{ t('prism.remediation.rule.form.cancel') }}
          </el-button>
          <el-button
            type="primary"
            :loading="saving"
            @click="handleSubmit"
          >
            {{ t('prism.remediation.rule.form.save') }}
          </el-button>
        </div>
      </el-form>
    </div>
  </div>
</template>

<style scoped lang="scss">
.tk-prism-remediation-rule-edit {
  &__header {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-md);
    align-items: center;
    justify-content: space-between;
    padding-bottom: var(--tk-spacing-lg);
    margin-bottom: var(--tk-spacing-lg);
    border-bottom: 1px solid var(--tk-border-color);
  }

  &__header-left {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-md);
    align-items: center;
  }

  &__mode-badge {
    padding: var(--tk-spacing-xs) var(--tk-spacing-md);
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-primary-color);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    white-space: nowrap;
    background-color: var(--tk-primary-color-bg);
    border: 1px solid var(--tk-primary-color);
    border-radius: var(--tk-radius-sm);
  }

  &__heading {
    min-width: 0;
  }

  &__title {
    margin: 0;
    font-size: var(--tk-font-size-3xl);
    font-weight: var(--tk-font-weight-bold);
    line-height: 1.1;
    color: var(--tk-text-primary);
  }

  &__subtitle {
    margin: var(--tk-spacing-xs) 0 0;
    font-size: var(--tk-font-size-sm);
    line-height: var(--tk-line-height-normal);
    color: var(--tk-text-secondary);
  }

  &__form-card {
    overflow: hidden;
    background-color: var(--tk-bg-surface);
    border: 1px solid var(--tk-border-color);
    border-radius: var(--tk-radius-lg);
  }

  &__section {
    padding: var(--tk-spacing-lg);
    border-bottom: 1px solid var(--tk-border-color-light);

    &:last-of-type {
      border-bottom: none;
    }
  }

  &__section-title {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
    margin: 0 0 var(--tk-spacing-md);
    font-size: var(--tk-font-size-md);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
  }

  &__section-index {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-bold);
    color: var(--tk-primary-color);
    background-color: var(--tk-primary-color-bg);
    border: 1px solid var(--tk-primary-color);
    border-radius: var(--tk-radius-sm);
  }

  &__form-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 0 var(--tk-spacing-md);
  }

  @media (max-width: 768px) {
    &__form-grid {
      grid-template-columns: 1fr;
    }
  }

  &__block {
    width: 100%;
  }

  &__help {
    margin-top: 4px;
    font-size: var(--tk-font-size-sm);
    line-height: 1.5;
    color: var(--tk-text-secondary);
  }

  &__headers {
    width: 100%;
  }

  &__header-row {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
    margin-bottom: var(--tk-spacing-sm);
  }

  &__header-key {
    flex: 0 0 35%;
  }

  &__header-value {
    flex: 1;
  }

  &__footer {
    display: flex;
    gap: var(--tk-spacing-sm);
    justify-content: flex-end;
    padding: var(--tk-spacing-lg);
    border-top: 1px solid var(--tk-border-color-light);
  }
}
</style>
