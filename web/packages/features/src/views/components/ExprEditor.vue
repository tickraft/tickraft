// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

<script setup lang="ts">
/**
 * Dual-mode expression editor.
 *
 * The expression string is the single source of truth: the wizard
 * regenerates it on every row change, and the expert textarea edits it
 * directly. Switching wizard → expert compiles the rows into the
 * textarea; switching back reverse-parses, and expressions beyond the
 * wizard subset keep the editor in expert mode (saving is not
 * blocked). Expert input is validated against POST /expr/validate on
 * blur and before submit; a structurally valid wizard never calls the
 * backend.
 */
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { validateExpression } from '../../api/prism'
import {
  CATALOG,
  OPERATOR_LABEL_KEYS,
  emptyModel,
  findVariable,
  generateExpression,
  isMapKind,
  operatorsForKind,
  parseExpression,
} from '../../utils/expr-builder'
import type { ConditionRow, ExprEnv, WizardModel } from '../../utils/expr-builder'

const props = withDefaults(
  defineProps<{
    /** Evaluation environment contract (variable catalog + validate env). */
    env: ExprEnv
    /** The expression string (v-model). */
    modelValue: string
    /** Whether an empty expression is rejected. */
    required?: boolean
    /** Expert-mode textarea placeholder. */
    placeholder?: string
    /** Expert-mode textarea height in rows. */
    rows?: number
  }>(),
  { required: false, placeholder: '', rows: 3 },
)

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const { t } = useI18n()

const mode = ref<'wizard' | 'expert'>('wizard')
const model = ref<WizardModel>(emptyModel())
const expertText = ref('')
const errorMessage = ref('')
const validating = ref(false)
const showCatalog = ref(false)
/** Value last emitted by this editor — guards against echo loops. */
const lastEmitted = ref<string | null>(null)
/** Suppresses the model watcher while applying an external value. */
let syncing = false

const variables = computed(() => CATALOG[props.env])

/** Whether a row's variable needs the map-key input. */
function isMapRow(row: ConditionRow): boolean {
  const def = findVariable(props.env, row.variable)
  return def !== undefined && isMapKind(def.kind)
}

/** Operator dropdown options for a row, filtered by variable type. */
function operatorOptions(row: ConditionRow): Array<{ value: string; label: string }> {
  const def = findVariable(props.env, row.variable)
  const kind = def ? def.kind : 'string'
  return operatorsForKind(kind).map((op) => ({
    value: op,
    label: t(OPERATOR_LABEL_KEYS[op]),
  }))
}

/** Whether every wizard row is structurally complete. */
const rowsComplete = computed(() => generateExpression(props.env, model.value).ok)

function emitValue(value: string): void {
  lastEmitted.value = value
  emit('update:modelValue', value)
}

/** Regenerate the expression from the wizard model. */
function regenerate(): void {
  const result = generateExpression(props.env, model.value)
  if (result.ok) {
    errorMessage.value = ''
    emitValue(result.expression ?? '')
  } else {
    // Incomplete rows surface as empty value; validate() blocks submit.
    emitValue('')
  }
}

watch(
  model,
  () => {
    if (syncing || mode.value !== 'wizard') return
    regenerate()
  },
  { deep: true },
)

/** Adopt an externally set value (rule load, template prefill). */
function applyExternal(value: string, initial: boolean): void {
  if (value === lastEmitted.value) return
  syncing = true
  const parsed = value ? parseExpression(props.env, value) : emptyModel()
  if (parsed) model.value = parsed
  expertText.value = value
  if (initial && value && !parsed) {
 // Hand-written rules reopen in expert mode.
    mode.value = 'expert'
  }
  void nextTick(() => {
    syncing = false
  })
}

watch(
  () => props.modelValue,
  (value) => applyExternal(value, false),
)

onMounted(() => applyExternal(props.modelValue, true))

function handleModeChange(target: string | number | boolean | undefined): void {
  if (target === 'expert') {
    expertText.value = props.modelValue
    mode.value = 'expert'
    return
  }
  const parsed = parseExpression(props.env, props.modelValue)
  if (!parsed) {
    ElMessage.warning(t('prism.expr.beyondWizard'))
    return // radio snaps back to expert
  }
  syncing = true
  model.value = parsed
  void nextTick(() => {
    syncing = false
  })
  errorMessage.value = ''
  mode.value = 'wizard'
}

function addRow(): void {
  const first = variables.value[0]
  const firstOp = operatorsForKind(first ? first.kind : 'string')[0]
  model.value.rows.push({
    variable: first ? first.path : '',
    key: '',
    operator: firstOp,
    value: '',
    negate: false,
  })
}

function removeRow(index: number): void {
  model.value.rows.splice(index, 1)
}

/** Variable switch resets the operator (type may have changed). */
function handleVariableChange(row: ConditionRow): void {
  const def = findVariable(props.env, row.variable)
  const ops = operatorsForKind(def ? def.kind : 'string')
  if (!ops.includes(row.operator)) {
    row.operator = ops[0]
  }
}

function valuePlaceholder(row: ConditionRow): string {
  if (row.operator === 'in') return t('prism.expr.valueInPlaceholder')
  return t('prism.expr.valuePlaceholder')
}

function handleExpertInput(value: string): void {
  errorMessage.value = ''
  emitValue(value)
}

async function handleExpertBlur(): Promise<void> {
  await validate()
}

/**
 * Validate before submit. Wizard rows are checked structurally (no
 * backend call); expert text always hits the validate endpoint.
 * Exposed for the parent forms' submit handlers.
 */
async function validate(): Promise<boolean> {
  const value = props.modelValue.trim()
  if (!value) {
    errorMessage.value = props.required ? t('prism.expr.required') : ''
    return !props.required
  }
  if (mode.value === 'wizard') {
    if (!rowsComplete.value) {
      errorMessage.value = t('prism.expr.invalidRows')
      return false
    }
    errorMessage.value = ''
    return true
  }
  validating.value = true
  try {
    await validateExpression({ env: props.env, expression: value })
    errorMessage.value = ''
    return true
  } catch (err) {
    errorMessage.value = err instanceof Error ? err.message : String(err)
    return false
  } finally {
    validating.value = false
  }
}

defineExpose({ validate })
</script>

<template>
  <div class="tk-expr-editor">
    <div class="tk-expr-editor__toolbar">
      <el-radio-group
        :model-value="mode"
        size="small"
        @change="handleModeChange"
      >
        <el-radio-button value="wizard">
          {{ t('prism.expr.wizard') }}
        </el-radio-button>
        <el-radio-button value="expert">
          {{ t('prism.expr.expert') }}
        </el-radio-button>
      </el-radio-group>
      <span class="tk-expr-editor__hint">
        {{ mode === 'wizard' ? t('prism.expr.wizardHint') : t('prism.expr.expertHint') }}
      </span>
      <el-button
        link
        type="primary"
        size="small"
        class="tk-expr-editor__catalog-toggle"
        @click="showCatalog = !showCatalog"
      >
        {{ t('prism.expr.catalogToggle') }}
      </el-button>
    </div>

    <!-- Wizard mode: condition rows -->
    <template v-if="mode === 'wizard'">
      <div
        v-if="model.rows.length > 1"
        class="tk-expr-editor__combinator"
      >
        <el-radio-group
          v-model="model.combinator"
          size="small"
        >
          <el-radio-button value="and">
            {{ t('prism.expr.matchAll') }}
          </el-radio-button>
          <el-radio-button value="or">
            {{ t('prism.expr.matchAny') }}
          </el-radio-button>
        </el-radio-group>
      </div>

      <div
        v-for="(row, index) in model.rows"
        :key="index"
        class="tk-expr-editor__row"
      >
        <el-checkbox
          v-model="row.negate"
          size="small"
        >
          {{ t('prism.expr.negate') }}
        </el-checkbox>
        <el-select
          v-model="row.variable"
          :placeholder="t('prism.expr.variable')"
          class="tk-expr-editor__variable"
          @change="handleVariableChange(row)"
        >
          <el-option
            v-for="v in variables"
            :key="v.path"
            :value="v.path"
            :label="v.path"
          >
            <el-tooltip
              :content="t(v.descKey)"
              placement="top"
              :show-after="400"
            >
              <span>{{ v.path }}</span>
            </el-tooltip>
          </el-option>
        </el-select>
        <el-input
          v-if="isMapRow(row)"
          v-model="row.key"
          :placeholder="t('prism.expr.mapKey')"
          class="tk-expr-editor__key"
        />
        <el-select
          v-model="row.operator"
          class="tk-expr-editor__operator"
        >
          <el-option
            v-for="op in operatorOptions(row)"
            :key="op.value"
            :value="op.value"
            :label="op.label"
          />
        </el-select>
        <el-input
          v-model="row.value"
          :placeholder="valuePlaceholder(row)"
          class="tk-expr-editor__value"
        />
        <el-button
          link
          type="danger"
          @click="removeRow(index)"
        >
          {{ t('prism.expr.remove') }}
        </el-button>
      </div>

      <div class="tk-expr-editor__actions">
        <el-button
          link
          type="primary"
          @click="addRow"
        >
          + {{ t('prism.expr.addCondition') }}
        </el-button>
      </div>

      <div
        v-if="modelValue"
        class="tk-expr-editor__preview"
      >
        {{ modelValue }}
      </div>
      <div
        v-else
        class="tk-expr-editor__empty-hint"
      >
        {{ t('prism.expr.emptyHint') }}
      </div>
    </template>

    <!-- Expert mode: raw textarea -->
    <el-input
      v-else
      :model-value="expertText"
      type="textarea"
      :rows="rows"
      :placeholder="placeholder"
      class="tk-expr-editor__textarea"
      @input="handleExpertInput"
      @blur="handleExpertBlur"
    />

    <!-- Inline validation feedback -->
    <div
      v-if="errorMessage"
      class="tk-expr-editor__error"
    >
      {{ t('prism.expr.validateFailed') }} {{ errorMessage }}
    </div>
    <div
      v-else-if="validating"
      class="tk-expr-editor__validating"
    >
      {{ t('prism.expr.validating') }}
    </div>

    <!-- Variable catalog cheat sheet -->
    <div
      v-if="showCatalog"
      class="tk-expr-editor__catalog"
    >
      <div
        v-for="v in variables"
        :key="v.path"
        class="tk-expr-editor__catalog-row"
      >
        <code>{{ v.path }}</code>
        <span>{{ t(v.descKey) }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.tk-expr-editor {
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-sm, 8px);
  width: 100%;

  &__toolbar {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-sm, 8px);
    align-items: center;
  }

  &__hint {
    font-size: var(--tk-font-size-xs, 12px);
    color: var(--tk-text-secondary, #909399);
  }

  &__catalog-toggle {
    margin-left: auto;
  }

  &__row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-xs, 4px);
    align-items: center;
  }

  &__variable {
    width: 160px;
  }

  &__key {
    width: 120px;
  }

  &__operator {
    width: 120px;
  }

  &__value {
    flex: 1;
    min-width: 140px;
  }

  &__preview,
  &__textarea :deep(textarea) {
    font-family: var(--tk-font-family-mono, monospace);
  }

  &__preview {
    padding: var(--tk-spacing-xs, 4px) var(--tk-spacing-sm, 8px);
    font-size: var(--tk-font-size-sm, 13px);
    word-break: break-all;
    background-color: var(--tk-bg-fill, #f5f7fa);
    border: 1px solid var(--tk-border-color, #dcdfe6);
    border-radius: var(--tk-radius-sm, 4px);
  }

  &__empty-hint {
    font-size: var(--tk-font-size-xs, 12px);
    color: var(--tk-text-secondary, #909399);
  }

  &__error {
    font-size: var(--tk-font-size-xs, 12px);
    line-height: 1.5;
    color: var(--tk-danger-color, #f56c6c);
  }

  &__validating {
    font-size: var(--tk-font-size-xs, 12px);
    color: var(--tk-text-secondary, #909399);
  }

  &__catalog {
    padding: var(--tk-spacing-sm, 8px);
    background-color: var(--tk-bg-fill, #f5f7fa);
    border: 1px solid var(--tk-border-color, #dcdfe6);
    border-radius: var(--tk-radius-sm, 4px);
  }

  &__catalog-row {
    display: flex;
    gap: var(--tk-spacing-sm, 8px);
    font-size: var(--tk-font-size-xs, 12px);
    line-height: 1.8;

    code {
      min-width: 120px;
      font-family: var(--tk-font-family-mono, monospace);
      color: var(--tk-primary-color, #409eff);
    }

    span {
      color: var(--tk-text-secondary, #909399);
    }
  }
}
</style>
