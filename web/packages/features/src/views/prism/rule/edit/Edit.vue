// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { createAlertRule, getAlertRule, updateAlertRule } from '../../../../api/prism'
import type { AlertRulePayload } from '../../../../api/prism'
import { TEMPLATES } from '../../templates/presets'

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
  isEdit.value ? t('prism.rule.edit.titleEdit') : t('prism.rule.edit.titleCreate'),
)

/** Mode badge label (CREATE / EDIT) */
const modeLabel = computed(() =>
  isEdit.value ? t('prism.rule.edit.modeEdit') : t('prism.rule.edit.modeCreate'),
)

/** Enabled label for preview */
const enabledLabel = computed(() =>
  form.enabled ? t('prism.rule.edit.previewEnabled') : t('prism.rule.edit.previewDisabled'),
)

/** Scene dropdown options */
const sceneOptions = computed(() => [
  { value: 'task', label: t('prism.scene.task') },
  { value: 'probe', label: t('prism.scene.probe') },
  { value: 'metric', label: t('prism.scene.metric') },
  { value: 'remediation', label: t('prism.scene.remediation') },
])

/**
 * Scene-specific expression help text.
 * Each scene exposes a different set of variables to the expr-lang engine;
 * metrics must be accessed via the structured path (e.g. event.metrics["cpu"]),
 * not as bare names (e.g. cpu > 80 would fail to compile).
 */
const sceneExpressionHelp = computed(() => {
  switch (form.scene) {
    case 'task':
      return t('prism.rule.edit.exprHelpTask')
    case 'probe':
      return t('prism.rule.edit.exprHelpProbe')
    case 'metric':
      return t('prism.rule.edit.exprHelpMetric')
    case 'remediation':
      return t('prism.rule.edit.exprHelpRemediation')
    default:
      return t('prism.rule.edit.expressionHelp')
  }
})

/** Scene-specific expression examples for the placeholder */
const expressionPlaceholder = computed(() => {
  switch (form.scene) {
    case 'task':
      return 'event.executor_type == "http" && event.priority > 5'
    case 'probe':
      return 'event.metrics["cpu_usage"] > 90 || event.status == "abnormal"'
    case 'metric':
      return 'event.metrics["cpu_usage"] > 90'
    case 'remediation':
      return 'event.metric_value > 80 && event.severity == "critical"'
    default:
      return t('prism.rule.edit.expressionPlaceholder')
  }
})

/** Form data */
const form = reactive({
  name: '',
  description: '',
  scene: 'task',
  expression: '',
  priority: 0,
  enabled: true,
})

/** Form validation rules */
const rules = computed<FormRules>(() => ({
  name: [
    { required: true, message: t('prism.rule.edit.namePlaceholder'), trigger: 'blur' },
    { max: 255, message: t('prism.rule.edit.nameMaxLen'), trigger: 'blur' },
  ],
  scene: [{ required: true, message: t('prism.rule.edit.scenePlaceholder'), trigger: 'change' }],
  expression: [
    { required: true, message: t('prism.rule.edit.expressionPlaceholder'), trigger: 'blur' },
  ],
}))

/** Validate the form (callback wrapped as a Promise) */
function validateForm(): Promise<boolean> {
  return new Promise((resolve) => {
    formRef.value?.validate((valid: boolean) => resolve(valid))
  })
}

/** Load edit rule data */
async function loadRule(): Promise<void> {
  if (!isEdit.value || !editId.value) return
  loading.value = true
  try {
    const rule = await getAlertRule(editId.value)
    form.name = rule.name
    form.description = rule.description ?? ''
    form.scene = rule.scene
    form.expression = rule.expression
    form.priority = rule.priority ?? 0
    form.enabled = rule.enabled
  } catch {
    ElMessage.warning(t('prism.rule.edit.notFound'))
  } finally {
    loading.value = false
  }
}

/** Build submission payload */
function buildPayload(enabled: boolean): AlertRulePayload {
  return {
    name: form.name.trim(),
    description: form.description.trim() || undefined,
    scene: form.scene,
    expression: form.expression.trim(),
    priority: Number(form.priority) || 0,
    enabled,
  }
}

/** Submit and save */
async function handleSubmit(forceEnable: boolean): Promise<void> {
  const valid = await validateForm()
  if (!valid) {
    ElMessage.warning(t('prism.rule.edit.validateError'))
    return
  }
  saving.value = true
  try {
    const payload = buildPayload(forceEnable ? true : form.enabled)
    if (isEdit.value && editId.value) {
      await updateAlertRule(editId.value, payload)
      ElMessage.success(t('prism.rule.edit.updatedToast'))
    } else {
      await createAlertRule(payload)
      ElMessage.success(t('prism.rule.edit.createdToast'))
    }
    router.push('/prism/rule/list')
  } finally {
    saving.value = false
  }
}

/** Back */
function handleBack(): void {
  router.push('/prism/rule/list')
}

/**
 * Pre-fill the form from a preset alert template identified by the
 * `templateId` query parameter. This supports the "Apply" flow from
 * the prism templates list page.
 */
function applyTemplate(templateId: number): void {
  const tpl = TEMPLATES.find((tp) => tp.id === templateId)
  if (!tpl) return
  form.name = t(tpl.nameKey)
  form.description = t(tpl.descriptionKey)
  // Map monitor type to scene
  const sceneMap: Record<string, string> = {
    host: 'probe',
    network: 'probe',
    web: 'probe',
    data: 'metric',
    cert: 'probe',
  }
  const scene = sceneMap[tpl.monitorType] ?? 'probe'
  form.scene = scene

  // Build a valid expr-lang expression using the correct variable path
  // for the selected scene. Metrics must be accessed via the structured
  // env object (e.g. event.metrics["cpu_usage"]), not as bare names.
  const operatorMap: Record<string, string> = {
    gt: '>',
    lt: '<',
    gte: '>=',
    lte: '<=',
    eq: '==',
  }
  const op = operatorMap[tpl.condition] ?? '>'
  const metricPath = scene === 'remediation'
    ? `event.metric_value`
    : `event.metrics["${tpl.metric}"]`
  form.expression = `${metricPath} ${op} ${tpl.threshold}`
}

onMounted(() => {
  if (isEdit.value) {
    loadRule()
  } else {
    // In create mode, check for a templateId query param (from templates list "Apply")
    const templateId = Number(route.query.templateId)
    if (templateId) {
      applyTemplate(templateId)
    }
  }
})
</script>

<template>
  <div
    v-loading="loading"
    class="tk-prism-rule-edit tk-page-container"
  >
    <!-- Header: title + mode badge + subtitle + back -->
    <header class="tk-prism-rule-edit__header">
      <div class="tk-prism-rule-edit__header-left">
        <span class="tk-prism-rule-edit__mode-badge">
          {{ modeLabel }}
        </span>
        <div class="tk-prism-rule-edit__heading">
          <h1 class="tk-prism-rule-edit__title">
            {{ pageTitle }}
          </h1>
          <p class="tk-prism-rule-edit__subtitle">
            {{ t('prism.rule.edit.subtitle') }}
          </p>
        </div>
      </div>
      <el-button @click="handleBack">
        &larr; {{ t('prism.rule.edit.back') }}
      </el-button>
    </header>

    <!-- Two-column grid: form + preview -->
    <div class="tk-prism-rule-edit__grid">
      <!-- Left: form card -->
      <div class="tk-prism-rule-edit__form-card">
        <el-form
          ref="formRef"
          :model="form"
          :rules="rules"
          label-width="100px"
        >
          <!-- Section 01: Basic Info -->
          <section class="tk-prism-rule-edit__section">
            <h3 class="tk-prism-rule-edit__section-title">
              <span class="tk-prism-rule-edit__section-index">01</span>
              {{ t('prism.rule.edit.sectionBasic') }}
            </h3>
            <el-form-item
              :label="t('prism.rule.edit.name')"
              prop="name"
            >
              <el-input
                v-model="form.name"
                :placeholder="t('prism.rule.edit.namePlaceholder')"
                maxlength="255"
                show-word-limit
              />
            </el-form-item>
            <el-form-item :label="t('prism.rule.edit.description')">
              <el-input
                v-model="form.description"
                type="textarea"
                :rows="2"
                maxlength="1024"
                show-word-limit
                :placeholder="t('prism.rule.edit.descriptionPlaceholder')"
              />
            </el-form-item>
          </section>

          <!-- Section 02: Trigger Condition -->
          <section class="tk-prism-rule-edit__section">
            <h3 class="tk-prism-rule-edit__section-title">
              <span class="tk-prism-rule-edit__section-index">02</span>
              {{ t('prism.rule.edit.sectionTrigger') }}
            </h3>
            <el-form-item
              :label="t('prism.rule.edit.scene')"
              prop="scene"
            >
              <el-select
                v-model="form.scene"
                :placeholder="t('prism.rule.edit.scenePlaceholder')"
                class="tk-prism-rule-edit__block"
              >
                <el-option
                  v-for="opt in sceneOptions"
                  :key="opt.value"
                  :label="opt.label"
                  :value="opt.value"
                />
              </el-select>
            </el-form-item>

            <el-form-item
              :label="t('prism.rule.edit.expression')"
              prop="expression"
            >
              <el-input
                v-model="form.expression"
                type="textarea"
                :rows="5"
                :placeholder="expressionPlaceholder"
                class="tk-prism-rule-edit__expr-input"
              />
              <div class="tk-prism-rule-edit__help">
                {{ sceneExpressionHelp }}
              </div>
            </el-form-item>

            <el-form-item :label="t('prism.rule.edit.priority')">
              <el-input-number
                v-model="form.priority"
                :min="0"
                :step="1"
                class="tk-prism-rule-edit__block"
              />
              <div class="tk-prism-rule-edit__help">
                {{ t('prism.rule.edit.priorityHelp') }}
              </div>
            </el-form-item>
          </section>

          <!-- Section 03: Status -->
          <section class="tk-prism-rule-edit__section">
            <h3 class="tk-prism-rule-edit__section-title">
              <span class="tk-prism-rule-edit__section-index">03</span>
              {{ t('prism.rule.edit.sectionAdvanced') }}
            </h3>
            <el-form-item :label="t('prism.rule.edit.enabled')">
              <el-switch v-model="form.enabled" />
            </el-form-item>
          </section>

          <!-- Footer actions -->
          <div class="tk-prism-rule-edit__footer">
            <el-button @click="handleBack">
              {{ t('prism.rule.edit.cancel') }}
            </el-button>
            <el-button
              type="primary"
              :loading="saving"
              @click="handleSubmit(false)"
            >
              {{ t('prism.rule.edit.save') }}
            </el-button>
            <el-button
              type="success"
              :loading="saving"
              @click="handleSubmit(true)"
            >
              {{ t('prism.rule.edit.saveAndEnable') }}
            </el-button>
          </div>
        </el-form>
      </div>

      <!-- Right: sticky preview panel -->
      <aside class="tk-prism-rule-edit__preview">
        <div class="tk-prism-rule-edit__preview-card">
          <div class="tk-prism-rule-edit__preview-card-header">
            <span class="tk-prism-rule-edit__preview-card-title">
              {{ t('prism.rule.edit.previewTitle') }}
            </span>
          </div>
          <div class="tk-prism-rule-edit__preview-card-body">
            <!-- Expression preview -->
            <div class="tk-prism-rule-edit__expr">
              <span class="tk-prism-rule-edit__expr-text">
                {{ form.expression || '—' }}
              </span>
            </div>

            <!-- Metadata preview -->
            <div class="tk-prism-rule-edit__preview-meta">
              <div class="tk-prism-rule-edit__preview-meta-cell">
                <span class="tk-prism-rule-edit__preview-meta-label">
                  {{ t('prism.rule.edit.scene') }}
                </span>
                <span class="tk-prism-rule-edit__preview-meta-value">
                  {{ t(`prism.scene.${form.scene}`) }}
                </span>
              </div>
              <div class="tk-prism-rule-edit__preview-meta-cell">
                <span class="tk-prism-rule-edit__preview-meta-label">
                  {{ t('prism.rule.edit.priority') }}
                </span>
                <span class="tk-prism-rule-edit__preview-meta-value">
                  {{ form.priority }}
                </span>
              </div>
              <div class="tk-prism-rule-edit__preview-meta-cell">
                <span class="tk-prism-rule-edit__preview-meta-label">
                  {{ t('prism.rule.edit.enabled') }}
                </span>
                <span class="tk-prism-rule-edit__preview-meta-value">
                  {{ enabledLabel }}
                </span>
              </div>
            </div>
          </div>
        </div>
      </aside>
    </div>
  </div>
</template>

<style scoped lang="scss">
.tk-prism-rule-edit {
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

  &__grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 380px;
    gap: var(--tk-spacing-lg);
    align-items: start;
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

  &__block {
    width: 100%;
  }

  &__expr-input {
    :deep(textarea) {
      font-family: var(--tk-font-family-mono);
    }
  }

  &__help {
    margin-top: 4px;
    font-size: var(--tk-font-size-sm);
    line-height: 1.5;
    color: var(--tk-text-secondary);
  }

  &__footer {
    display: flex;
    gap: var(--tk-spacing-sm);
    justify-content: flex-end;
    padding: var(--tk-spacing-lg);
    border-top: 1px solid var(--tk-border-color-light);
  }

  /* Preview panel */
  &__preview {
    position: sticky;
    top: calc(var(--tk-topbar-height, 60px) + var(--tk-spacing-lg));
  }

  &__preview-card {
    overflow: hidden;
    background-color: var(--tk-bg-surface);
    border: 1px solid var(--tk-border-color);
    border-radius: var(--tk-radius-lg);
  }

  &__preview-card-header {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
    justify-content: space-between;
    padding: var(--tk-spacing-md) var(--tk-spacing-lg);
    border-bottom: 1px solid var(--tk-border-color-light);
  }

  &__preview-card-title {
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  &__preview-card-body {
    display: flex;
    flex-direction: column;
    gap: var(--tk-spacing-md);
    padding: var(--tk-spacing-lg);
  }

  &__expr {
    min-height: 60px;
    padding: var(--tk-spacing-md) var(--tk-spacing-lg);
    font-family: var(--tk-font-family-mono);
    word-break: break-all;
    background-color: var(--tk-bg-fill);
    border: 1px solid var(--tk-border-color);
    border-radius: var(--tk-radius-md);
  }

  &__expr-text {
    font-size: var(--tk-font-size-sm);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-primary-color);
  }

  &__preview-meta {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr;
    gap: var(--tk-spacing-md);
  }

  &__preview-meta-cell {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  &__preview-meta-label {
    font-family: var(--tk-font-family-mono);
    font-size: var(--tk-font-size-xs);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  &__preview-meta-value {
    font-size: var(--tk-font-size-sm);
    font-weight: var(--tk-font-weight-medium);
    color: var(--tk-text-primary);
  }

  /* Responsive: collapse to single column on narrow screens */
  @media (max-width: 1100px) {
    &__grid {
      grid-template-columns: 1fr;
    }

    &__preview {
      position: static;
    }
  }
}
</style>
