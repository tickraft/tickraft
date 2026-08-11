// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { reactive, ref, computed, watch, onBeforeUnmount } from 'vue'
import type { Ref, ComputedRef } from 'vue'

/** Field validator type (sync or async) */
export type FieldValidator = (value: unknown) => Promise<boolean> | boolean

/** Field dependency change callback type */
export type WatchDependencyCallback<T> = (newValue: unknown, form: T) => void

/** Single field validation rule */
export interface ValidationRule {
  /** Whether required */
  required?: boolean
  /** Numeric lower bound */
  min?: number
  /** Numeric upper bound */
  max?: number
  /** Minimum string length */
  minLength?: number
  /** Maximum string length */
  maxLength?: number
  /** Regex validation */
  pattern?: RegExp
  /** Custom validator */
  validator?: FieldValidator
  /** Error message (overrides the default) */
  message?: string
}

/** Field config collection */
export type FieldConfigs<T> = Partial<Record<keyof T, ValidationRule>>

/** useForm options */
export interface UseFormOptions<T extends Record<string, unknown>> {
  /** Initial values (used for dirty detection and reset) */
  initialValues: T
  /** Submit function */
  submitFn: (values: T) => Promise<unknown>
  /** Field validation rule config */
  fieldConfigs?: FieldConfigs<T>
}

/** useForm return type */
export interface UseFormReturn<T extends Record<string, unknown>> {
  // ── Canonical fields ──
  /** Current form values (reactive) */
  form: T
  /** Field-level error messages (indexed by field name) */
  errors: Ref<Record<string, string>>
  /** Submitting state */
  submitting: Ref<boolean>
  /** Whether there are changes relative to the initial values */
  isDirty: ComputedRef<boolean>
  /** Full validation (iterates fieldConfigs) */
  validate: () => Promise<boolean>
  /** Validate a single field (accepts a validator or uses the fieldConfigs rule) */
  validateField: (field: keyof T, validator?: FieldValidator) => Promise<boolean>
  /** Submit the form (debounced at 500ms to prevent duplicate submits) */
  submit: () => Promise<boolean>
  /** Reset to initial values */
  resetForm: () => void
  /** Get the set of changed field names */
  getDirtyFields: () => Array<keyof T>
  /** Register a field link */
  watchDependencies: (field: keyof T, callback: WatchDependencyCallback<T>) => void
  /** Set a field value */
  setFieldValue: (field: keyof T, value: unknown) => void
  /** Get a field value */
  getFieldValue: (field: keyof T) => unknown
}

/** Default submit debounce delay (ms) */
const DEFAULT_SUBMIT_DEBOUNCE = 500

/**
 * Deep clone (JSON-based; suitable for plain-data form values, not for Date/function/etc.)
 * @param value - value to clone
 * @returns deep-cloned result
 */
function cloneDeep<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

/**
 * Deep equality comparison
 * @param a - value a
 * @param b - value b
 * @returns whether deeply equal
 */
function isEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (typeof a !== typeof b) return false
  if (a === null || b === null) return a === b
  if (typeof a !== 'object') return a === b
  const isArrayA = Array.isArray(a)
  const isArrayB = Array.isArray(b)
  if (isArrayA !== isArrayB) return false
  if (isArrayA && isArrayB) {
    const arrA = a as unknown[]
    const arrB = b as unknown[]
    if (arrA.length !== arrB.length) return false
    return arrA.every((item, index) => isEqual(item, arrB[index]))
  }
  const objA = a as Record<string, unknown>
  const objB = b as Record<string, unknown>
  const keysA = Object.keys(objA)
  const keysB = Object.keys(objB)
  if (keysA.length !== keysB.length) return false
  return keysA.every((key) => isEqual(objA[key], objB[key]))
}

/**
 * Run a single rule validation
 * @param value - field value
 * @param rule - validation rule
 * @returns error message (null when validation passes)
 */
function runRule(value: unknown, rule: ValidationRule): string | null {
  if (
    rule.required &&
    (value === undefined || value === null || value === '' ||
      (Array.isArray(value) && value.length === 0))
  ) {
    return rule.message ?? 'This field is required'
  }
  if (value === undefined || value === null || value === '') {
    return null
  }
  if (rule.min !== undefined && typeof value === 'number' && value < rule.min) {
    return rule.message ?? `Minimum value is ${rule.min}`
  }
  if (rule.max !== undefined && typeof value === 'number' && value > rule.max) {
    return rule.message ?? `Maximum value is ${rule.max}`
  }
  if (rule.minLength !== undefined && typeof value === 'string' && value.length < rule.minLength) {
    return rule.message ?? `At least ${rule.minLength} characters`
  }
  if (rule.maxLength !== undefined && typeof value === 'string' && value.length > rule.maxLength) {
    return rule.message ?? `At most ${rule.maxLength} characters`
  }
  if (rule.pattern && typeof value === 'string' && !rule.pattern.test(value)) {
    return rule.message ?? 'Invalid format'
  }
  return null
}

/**
 * Form state, validation and submit composable.
 *
 * Holds the form value state; provides async field validation, field linking,
 * submit debounce, dirty detection and reset.
 * @param options - options
 * @returns form state and operation methods
 */
export function useForm<T extends Record<string, unknown>>(
  options: UseFormOptions<T>,
): UseFormReturn<T> {
  const { initialValues, submitFn, fieldConfigs } = options

  /** Current form values */
  const form = reactive(cloneDeep(initialValues)) as T
  /** Initial value snapshot (reactive; used for dirty detection and reset) */
  const initial = ref<T>(cloneDeep(initialValues))

  const submitting = ref(false)
  const errors: Ref<Record<string, string>> = ref({})

  /** Whether there are changes relative to the initial values */
  const isDirty: ComputedRef<boolean> = computed(() => !isEqual(form, initial.value))

  /** Submit debounce lock */
  let submitLocked = false
  let submitLockTimer: ReturnType<typeof setTimeout> | null = null

  /**
   * Clear the submit debounce lock
   */
  function clearSubmitLockTimer(): void {
    if (submitLockTimer) {
      clearTimeout(submitLockTimer)
      submitLockTimer = null
    }
  }

  /**
   * Validate a single field
   *
   * When `validator` is provided, it is used; otherwise the rule configured in
   * `fieldConfigs` is used.
   * @param field - field name
   * @param validator - optional custom validator
   * @returns whether validation passes
   */
  async function validateField(
    field: keyof T,
    validator?: FieldValidator,
  ): Promise<boolean> {
    const value = form[field]
    // Prefer the passed-in validator
    if (validator) {
      try {
        const valid = await validator(value)
        if (valid) {
          delete errors.value[field as string]
        } else {
          errors.value[field as string] = 'Validation failed'
        }
        return valid
      } catch {
        errors.value[field as string] = 'Validation error'
        return false
      }
    }
    // Otherwise use the fieldConfigs rule
    const rule = fieldConfigs?.[field]
    if (!rule) {
      delete errors.value[field as string]
      return true
    }
    // The custom validator inside the rule takes precedence
    if (rule.validator) {
      return validateField(field, rule.validator)
    }
    const message = runRule(value, rule)
    if (message) {
      errors.value[field as string] = message
      return false
    }
    delete errors.value[field as string]
    return true
  }

  /**
   * Full validation (iterates all fields in fieldConfigs)
   * @returns whether all pass
   */
  async function validate(): Promise<boolean> {
    if (!fieldConfigs) return true
    const fields = Object.keys(fieldConfigs) as Array<keyof T>
    const results = await Promise.all(fields.map((field) => validateField(field)))
    return results.every(Boolean)
  }

  /**
   * Submit the form (debounced at 500ms to prevent duplicate submits)
   *
   * On success, updates the initial value snapshot so isDirty resets to false.
   * @returns whether the submit succeeded
   */
  async function submit(): Promise<boolean> {
    if (submitLocked) return false
    submitLocked = true
    submitting.value = true
    try {
      await submitFn(form)
      initial.value = cloneDeep(form)
      return true
    } catch {
      return false
    } finally {
      submitting.value = false
      clearSubmitLockTimer()
      submitLockTimer = setTimeout(() => {
        submitLocked = false
        submitLockTimer = null
      }, DEFAULT_SUBMIT_DEBOUNCE)
    }
  }

  /**
   * Reset to initial values
   */
  function resetForm(): void {
    const snapshot = cloneDeep(initial.value)
    const target = form as Record<string, unknown>
    Object.keys(target).forEach((key) => delete target[key])
    Object.assign(target, snapshot)
    errors.value = {}
  }

  /**
   * Get the set of changed field names
   * @returns array of changed field names
   */
  function getDirtyFields(): Array<keyof T> {
    const fields: Array<keyof T> = []
    const target = form as Record<string, unknown>
    const snapshot = initial.value as Record<string, unknown>
    Object.keys(target).forEach((key) => {
      if (!isEqual(target[key], snapshot[key])) {
        fields.push(key as keyof T)
      }
    })
    return fields
  }

  /**
   * Register a field link
   *
   * Watches `field` changes and triggers `callback`.
   * @param field - trigger field
   * @param callback - link callback
   */
  function watchDependencies(
    field: keyof T,
    callback: WatchDependencyCallback<T>,
  ): void {
    watch(
      () => form[field],
      (newValue) => {
        callback(newValue, form)
      },
    )
  }

  /**
   * Set a field value
   * @param field - field name
   * @param value - field value
   */
  function setFieldValue(field: keyof T, value: unknown): void {
    ;(form as Record<string, unknown>)[field as string] = value
  }

  /**
   * Get a field value
   * @param field - field name
   * @returns field value
   */
  function getFieldValue(field: keyof T): unknown {
    return form[field]
  }

  // Clear the debounce lock timer on unmount
  onBeforeUnmount(() => {
    clearSubmitLockTimer()
  })

  return {
    form,
    errors,
    submitting,
    isDirty,
    validate,
    validateField,
    submit,
    resetForm,
    getDirtyFields,
    watchDependencies,
    setFieldValue,
    getFieldValue,
  }
}
