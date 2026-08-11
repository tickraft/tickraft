// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script lang="ts">
/**
 * SearchForm type exports (re-exported from a standalone .ts file for cross-project type visibility)
 */
export type { SearchFormField, SearchFormProps } from './search-form-types'
</script>

<script setup lang="ts">
/**
 * SearchForm - generic search form component.
 * Built on el-form + el-row/el-col, supports dynamic field config, responsive grid
 * layout, expand/collapse, field cascade, and reset confirmation. Visuals align with
 * the .tk-search-area prototype spec.
 */
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown, ArrowUp } from '@element-plus/icons-vue'
import type { SearchFormProps, FieldType, SelectOption } from './search-form-types'

interface SearchFormEmits {
  /** Triggered when the search button is clicked */
  (e: 'search', values: Record<string, unknown>): void
  /** Triggered when the form is reset */
  (e: 'reset'): void
  /** Triggered when any field value changes */
  (e: 'field-change', field: string, value: unknown, form: Record<string, unknown>): void
  /** Field cascade: emitted when dependent fields are cleared */
  (e: 'cascade', field: string, affectedFields: string[]): void
}

const props = withDefaults(defineProps<SearchFormProps>(), {
  fields: () => [],
  modelValue: () => ({}),
  loading: false,
  showCollapse: true,
  collapseThreshold: 3,
  resetConfirm: false,
})

const emit = defineEmits<SearchFormEmits>()

/** Normalized field: unified field name and type */
interface NormalizedField {
  name: string
  label: string
  type: FieldType
  options?: SelectOption[]
  placeholder?: string
  defaultValue?: unknown
  clearable: boolean
  span: number
  visible: boolean
  dependencies?: string[]
}

/** Clamp span to the 1-24 range */
function clampSpan(span: number): number {
  if (span < 1) return 1
  if (span > 24) return 24
  return span
}

/** Return an empty default value based on the control type */
function emptyValueByType(type: FieldType): unknown {
  if (type === 'daterange' || type === 'cascader') return []
  if (type === 'number') return undefined
  return ''
}

const normalizedFields = computed<NormalizedField[]>(() =>
  props.fields
    .map((field) => ({
      name: field.prop,
      label: field.label,
      type: field.type,
      options: field.options,
      placeholder: field.placeholder,
      defaultValue: field.defaultValue,
      clearable: field.clearable ?? true,
      span: clampSpan(field.span ?? 6),
      visible: field.visible ?? true,
      dependencies: field.dependencies,
    }))
    .filter((field) => field.visible && field.name),
)

const formValues = reactive<Record<string, unknown>>({ ...props.modelValue })
const initialValues = ref<Record<string, unknown>>({})
let initialized = false

/** Fill in default values for missing fields and record the initial snapshot on first init */
function ensureDefaults() {
  for (const field of normalizedFields.value) {
    if (formValues[field.name] === undefined) {
      formValues[field.name] = field.defaultValue ?? emptyValueByType(field.type)
    }
  }
  if (!initialized) {
    initialValues.value = { ...formValues }
    initialized = true
  }
}

watch(normalizedFields, ensureDefaults, { immediate: true })

// Sync with external modelValue changes and refresh the initial snapshot (supports parent-driven reset)
watch(
  () => props.modelValue,
  (val) => {
    Object.assign(formValues, val)
    for (const field of normalizedFields.value) {
      if (formValues[field.name] === undefined) {
        formValues[field.name] = field.defaultValue ?? emptyValueByType(field.type)
      }
    }
    initialValues.value = { ...formValues }
  },
  { deep: true },
)

/** Collapsed state */
const isCollapsed = ref(true)

/** Number of fields beyond the collapse threshold */
const hiddenFieldCount = computed(() => {
  if (!props.showCollapse) return 0
  return Math.max(0, normalizedFields.value.length - props.collapseThreshold)
})

/** Whether to show the collapse toggle button */
const showCollapseToggle = computed(
  () => props.showCollapse && hiddenFieldCount.value > 0,
)

/** Whether the field at the given index is visible (v-show is used to preserve DOM state) */
function isFieldVisible(index: number): boolean {
  if (!showCollapseToggle.value) return true
  return index < props.collapseThreshold || !isCollapsed.value
}

function toggleCollapse() {
  isCollapsed.value = !isCollapsed.value
}

/** Whether the form has been modified (used for reset confirmation) */
const isDirty = computed(
  () => JSON.stringify(initialValues.value) !== JSON.stringify(formValues),
)

/** Field value update and cascade handling */
function handleFieldChange(field: NormalizedField, value: unknown) {
  formValues[field.name] = value
  emit('field-change', field.name, value, { ...formValues })

  // Cascade: clear fields that depend on the current field
  const affectedFields: string[] = []
  for (const dependant of normalizedFields.value) {
    if (dependant.dependencies?.includes(field.name)) {
      formValues[dependant.name] = dependant.defaultValue ?? emptyValueByType(dependant.type)
      affectedFields.push(dependant.name)
    }
  }
  if (affectedFields.length > 0) {
    emit('cascade', field.name, affectedFields)
  }
}

/**
 * Typed field value accessors.
 *
 * Vue template expressions cannot safely use TS `as` assertions with union
 * types (the `|` is parsed as a deprecated filter). These helpers centralize
 * the type narrowing for `:model-value` bindings.
 */
function getCascaderValue(field: NormalizedField): Array<string | number> {
  const value = formValues[field.name]
  return Array.isArray(value) ? (value as Array<string | number>) : []
}

function getNumberValue(field: NormalizedField): number | undefined {
  const value = formValues[field.name]
  return typeof value === 'number' ? value : undefined
}

function handleSearch() {
  emit('search', { ...formValues })
}

async function handleReset() {
  if (props.resetConfirm && isDirty.value) {
    try {
      await ElMessageBox.confirm('Reset search conditions?', 'Confirm', {
        type: 'warning',
        confirmButtonText: 'Confirm',
        cancelButtonText: 'Cancel',
      })
    } catch {
      // User cancelled, keep current state
      return
    }
  }
  for (const field of normalizedFields.value) {
    formValues[field.name] = initialValues.value[field.name] ?? emptyValueByType(field.type)
  }
  emit('reset')
  ElMessage.success('Search conditions reset')
}
</script>

<template>
  <div
    class="tk-search-form"
    role="search"
  >
    <el-form
      :model="formValues"
      label-position="top"
    >
      <el-row :gutter="16">
        <el-col
          v-for="(field, index) in normalizedFields"
          v-show="isFieldVisible(index)"
          :key="field.name"
          class="tk-search-form__col"
          :xs="24"
          :sm="12"
          :md="8"
          :lg="field.span"
        >
          <el-form-item :label="field.label">
            <el-input
              v-if="field.type === 'input'"
              :model-value="formValues[field.name] as string"
              :placeholder="field.placeholder || field.label"
              :clearable="field.clearable"
              @update:model-value="(value: unknown) => handleFieldChange(field, value)"
            />
            <el-select
              v-else-if="field.type === 'select'"
              :model-value="formValues[field.name]"
              :placeholder="field.placeholder || field.label"
              :clearable="field.clearable"
              style="width: 100%"
              @update:model-value="(value: unknown) => handleFieldChange(field, value)"
            >
              <el-option
                v-for="option in field.options"
                :key="String(option.value)"
                :label="option.label"
                :value="option.value"
              />
            </el-select>
            <el-date-picker
              v-else-if="field.type === 'date'"
              :model-value="formValues[field.name] as string"
              type="date"
              value-format="YYYY-MM-DD"
              :placeholder="field.placeholder || 'Select date'"
              :clearable="field.clearable"
              style="width: 100%"
              @update:model-value="(value: unknown) => handleFieldChange(field, value as string)"
            />
            <el-date-picker
              v-else-if="field.type === 'daterange'"
              :model-value="formValues[field.name] as string[]"
              type="daterange"
              value-format="YYYY-MM-DD"
              range-separator="-"
              start-placeholder="Start date"
              end-placeholder="End date"
              :clearable="field.clearable"
              style="width: 100%"
              @update:model-value="(value: unknown) => handleFieldChange(field, value as string[])"
            />
            <el-cascader
              v-else-if="field.type === 'cascader'"
              :model-value="getCascaderValue(field)"
              :options="field.options || []"
              :placeholder="field.placeholder || field.label"
              :clearable="field.clearable"
              style="width: 100%"
              @update:model-value="(value: unknown) => handleFieldChange(field, value)"
            />
            <el-input-number
              v-else
              :model-value="getNumberValue(field)"
              :placeholder="field.placeholder || field.label"
              controls-position="right"
              style="width: 100%"
              @update:model-value="(value: unknown) => handleFieldChange(field, value)"
            />
          </el-form-item>
        </el-col>

        <!-- Action button group, fixed to the right of the last row -->
        <el-col
          class="tk-search-form__col tk-search-form__actions-col"
          :xs="24"
          :sm="12"
          :md="8"
          :lg="6"
        >
          <el-form-item>
            <div class="tk-search-form__actions">
              <el-button
                v-if="showCollapseToggle"
                link
                type="primary"
                class="tk-search-form__collapse-btn"
                :aria-expanded="!isCollapsed"
                :aria-label="isCollapsed ? 'Show more' : 'Collapse'"
                @click="toggleCollapse"
              >
                <el-icon class="tk-search-form__collapse-icon">
                  <ArrowDown v-if="isCollapsed" />
                  <ArrowUp v-else />
                </el-icon>
                <span>{{ isCollapsed ? 'Show more' : 'Collapse' }}</span>
              </el-button>
              <el-button
                :loading="loading"
                type="primary"
                @click="handleSearch"
              >
                {{ $t('common.app.search') }}
              </el-button>
              <el-button @click="handleReset">
                {{ $t('common.app.reset') }}
              </el-button>
            </div>
          </el-form-item>
        </el-col>
      </el-row>
    </el-form>
  </div>
</template>

<style scoped lang="scss">
.tk-search-form {
  padding: var(--tk-spacing-md);
  margin-bottom: var(--tk-spacing-md);
  background-color: var(--tk-bg-surface);
  border: var(--tk-border-default);
  border-radius: var(--tk-radius-lg);

  &__col {
    margin-bottom: var(--tk-spacing-sm);
  }

  &__actions-col {
    margin-left: auto;
  }

  &__actions {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
    justify-content: flex-end;
    width: 100%;
  }

  &__collapse-btn {
    margin-right: auto;
  }

  &__collapse-icon {
    margin-right: 4px;
  }

  // Aligned with prototype: smaller label size, regular color
  :deep(.el-form-item) {
    margin-bottom: 0;
  }

  :deep(.el-form-item__label) {
    padding-bottom: var(--tk-spacing-xs);
    font-size: var(--tk-font-size-sm);
    line-height: 1.5;
    color: var(--tk-text-regular);
  }
}
</style>
