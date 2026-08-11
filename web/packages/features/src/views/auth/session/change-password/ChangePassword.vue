// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Change password page
 *
 * Visual source: storyboard/tickraft/pages/auth-change-password.html
 * Layout is provided by BlankLayout(center); this component renders the card content
 * (header + form + strength indicator + rules checklist).
 *
 * Strength algorithm (aligned with prototype):
 *   length>=8 +1 | upper+lower +1 | number+special +1 | length>=12+all +1 | cap 4
 *   length<8 caps at 1.
 *
 * After a successful submission, log out and clear local credentials, then guide
 * the user to log in again with the new password.
 */
import { reactive, ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { BlankLayout, useUserStore } from '@tickraft/core'
import { changePassword as changePasswordApi, logout as logoutApi } from '../../../../api/auth'

const REDIRECT_DELAY = 1500
const PASSWORD_MIN_LENGTH = 8
const PASSWORD_MAX_LENGTH = 64

type RuleKey = 'length' | 'upper' | 'lower' | 'number' | 'special' | 'diff'

const router = useRouter()
const { t } = useI18n()
const userStore = useUserStore()

const loading = ref(false)
const showOldPassword = ref(false)
const showNewPassword = ref(false)
const showConfirmPassword = ref(false)

const form = reactive({
  oldPassword: '',
  newPassword: '',
  confirmPassword: '',
})

/** Password rule validation results (aligned with prototype computeRules) */
const rules = computed<Record<RuleKey, boolean>>(() => ({
  length: form.newPassword.length >= 8,
  upper: /[A-Z]/.test(form.newPassword),
  lower: /[a-z]/.test(form.newPassword),
  number: /[0-9]/.test(form.newPassword),
  special: /[^A-Za-z0-9]/.test(form.newPassword),
  diff: form.newPassword.length > 0 && form.newPassword !== form.oldPassword,
}))

/** Strength level 0-4 (aligned with prototype computeStrength) */
const strengthLevel = computed<number>(() => {
  const pw = form.newPassword
  if (!pw) return 0
  const r = rules.value
  let score = 0
  if (r.length) score++
  if (r.upper && r.lower) score++
  if (r.number && r.special) score++
  if (pw.length >= 12 && r.upper && r.lower && r.number && r.special) score++
  if (score > 4) score = 4
  if (pw.length < 8) score = Math.min(score, 1)
  return score
})

const strengthLabel = computed<string>(() => {
  switch (strengthLevel.value) {
    case 1: return t('auth.changePassword.strengthWeak')
    case 2: return t('auth.changePassword.strengthMedium')
    case 3: return t('auth.changePassword.strengthStrong')
    case 4: return t('auth.changePassword.strengthVeryStrong')
    default: return '—'
  }
})

/** Rule list with i18n labels for template rendering */
const ruleItems = computed<{ key: RuleKey; label: string }[]>(() => [
  { key: 'length', label: t('auth.changePassword.ruleLength') },
  { key: 'upper', label: t('auth.changePassword.ruleUpper') },
  { key: 'lower', label: t('auth.changePassword.ruleLower') },
  { key: 'number', label: t('auth.changePassword.ruleNumber') },
  { key: 'special', label: t('auth.changePassword.ruleSpecial') },
  { key: 'diff', label: t('auth.changePassword.ruleDiff') },
])

/** Real-time confirm password mismatch error */
const confirmError = computed<string>(() => {
  if (form.confirmPassword && form.newPassword !== form.confirmPassword) {
    return t('auth.changePassword.passwordMismatch')
  }
  return ''
})

/** Whether all conditions are met for submission */
const canSubmit = computed<boolean>(() => {
  const r = rules.value
  return r.length && r.upper && r.lower && r.number && r.special && r.diff
    && form.oldPassword.length > 0
    && form.confirmPassword.length > 0
    && form.newPassword === form.confirmPassword
})

/** Navigate back to the login page */
function handleBack(): void {
  router.push('/login')
}

async function handleSubmit(): Promise<void> {
  if (!canSubmit.value || loading.value) return
  if (form.newPassword.length < PASSWORD_MIN_LENGTH || form.newPassword.length > PASSWORD_MAX_LENGTH) return
  loading.value = true
  try {
    await changePasswordApi({
      oldPassword: form.oldPassword,
      newPassword: form.newPassword,
    })
    ElMessage.success(t('auth.changePassword.success'))
    try { await logoutApi() } catch { /* ignore logout errors */ }
    userStore.clearUser()
    setTimeout(() => router.replace('/login'), REDIRECT_DELAY)
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('auth.changePassword.failed'))
    loading.value = false
  }
}
</script>

<template>
  <BlankLayout layout="center">
    <div class="tk-cp__bg" aria-hidden="true" />
    <a
      class="tk-cp__back"
      href="javascript:void(0)"
      @click="handleBack"
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="19" y1="12" x2="5" y2="12"/><polyline points="12 19 5 12 12 5"/></svg>
      {{ t('auth.changePassword.backToLogin') }}
    </a>

    <div class="tk-cp__card">
      <header class="tk-cp__header">
        <div class="tk-cp__icon">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg>
        </div>
        <h1 class="tk-cp__title">{{ t('auth.changePassword.title') }}</h1>
        <p class="tk-cp__subtitle">{{ t('auth.changePassword.subtitle') }}</p>
      </header>

      <form class="tk-cp__form" novalidate @submit.prevent="handleSubmit">
        <div class="tk-cp__field">
          <label class="tk-cp__label" for="tk-cp-current">{{ t('auth.changePassword.oldPassword') }}</label>
          <div class="tk-cp__input-wrap">
            <span class="tk-cp__input-icon" aria-hidden="true">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
            </span>
            <input
              id="tk-cp-current"
              v-model="form.oldPassword"
              class="tk-cp__input"
              :type="showOldPassword ? 'text' : 'password'"
              :placeholder="t('auth.changePassword.oldPasswordPlaceholder')"
              autocomplete="current-password"
            >
            <button class="tk-cp__toggle-pw" type="button" :aria-label="t('auth.login.togglePassword')" @click="showOldPassword = !showOldPassword">
              <svg v-if="!showOldPassword" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
              <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/></svg>
            </button>
          </div>
          <div class="tk-cp__field-error" />
        </div>

        <div class="tk-cp__field">
          <label class="tk-cp__label" for="tk-cp-new">{{ t('auth.changePassword.newPassword') }}</label>
          <div class="tk-cp__input-wrap">
            <span class="tk-cp__input-icon" aria-hidden="true">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
            </span>
            <input
              id="tk-cp-new"
              v-model="form.newPassword"
              class="tk-cp__input"
              :type="showNewPassword ? 'text' : 'password'"
              :placeholder="t('auth.changePassword.newPasswordPlaceholder')"
              autocomplete="new-password"
            >
            <button class="tk-cp__toggle-pw" type="button" :aria-label="t('auth.login.togglePassword')" @click="showNewPassword = !showNewPassword">
              <svg v-if="!showNewPassword" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
              <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/></svg>
            </button>
          </div>
          <div class="tk-cp__field-error" />
        </div>

        <!-- Strength meter (hidden when new password is empty) -->
        <div v-if="form.newPassword" class="tk-cp__strength">
          <div class="tk-cp__strength-bar">
            <div
              v-for="i in 4"
              :key="i"
              class="tk-cp__strength-seg"
              :class="i <= strengthLevel ? `tk-cp__strength-seg--${strengthLevel}` : ''"
            />
          </div>
          <div class="tk-cp__strength-meta">
            <span>{{ t('auth.changePassword.strengthTitle') }}</span>
            <span class="tk-cp__strength-label" :class="strengthLevel ? `tk-cp__strength-label--${strengthLevel}` : ''">{{ strengthLabel }}</span>
          </div>
        </div>

        <!-- Rules checklist -->
        <div class="tk-cp__rules">
          <div
            v-for="rule in ruleItems"
            :key="rule.key"
            class="tk-cp__rule"
            :class="{ 'tk-cp__rule--ok': rules[rule.key] }"
          >
            <span class="tk-cp__rule-icon">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
            </span>
            <span>{{ rule.label }}</span>
          </div>
        </div>

        <div class="tk-cp__field">
          <label class="tk-cp__label" for="tk-cp-confirm">{{ t('auth.changePassword.confirmPassword') }}</label>
          <div class="tk-cp__input-wrap">
            <span class="tk-cp__input-icon" aria-hidden="true">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
            </span>
            <input
              id="tk-cp-confirm"
              v-model="form.confirmPassword"
              class="tk-cp__input"
              :class="{ 'tk-cp__input--error': confirmError }"
              :type="showConfirmPassword ? 'text' : 'password'"
              :placeholder="t('auth.changePassword.confirmPasswordPlaceholder')"
              autocomplete="new-password"
            >
            <button class="tk-cp__toggle-pw" type="button" :aria-label="t('auth.login.togglePassword')" @click="showConfirmPassword = !showConfirmPassword">
              <svg v-if="!showConfirmPassword" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
              <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/></svg>
            </button>
          </div>
          <div class="tk-cp__field-error">{{ confirmError }}</div>
        </div>

        <button
          type="submit"
          class="tk-cp__submit"
          :class="{ 'tk-cp__submit--loading': loading }"
          :disabled="!canSubmit || loading"
        >
          {{ loading ? t('auth.changePassword.submitting') : t('auth.changePassword.submit') }}
        </button>
      </form>

      <div class="tk-cp__foot">
        {{ t('auth.changePassword.footHelp') }}<a href="javascript:void(0)" @click="handleBack">{{ t('auth.changePassword.backToLogin') }}</a>{{ t('auth.changePassword.footOrContact') }}
      </div>
    </div>
  </BlankLayout>
</template>

<style scoped lang="scss">
.tk-cp__bg {
  position: fixed;
  inset: 0;
  z-index: 0;
  pointer-events: none;

  &::before {
    position: absolute;
    top: -240px;
    left: 50%;
    width: 900px;
    height: 900px;
    content: '';
    background: radial-gradient(circle, rgb(59 91 246 / 10%) 0%, transparent 65%);
    transform: translateX(-50%);
  }

  &::after {
    position: absolute;
    right: -200px;
    bottom: -200px;
    width: 600px;
    height: 600px;
    content: '';
    background: radial-gradient(circle, rgb(139 92 246 / 8%) 0%, transparent 70%);
  }
}

.tk-cp__back {
  position: fixed;
  top: 24px;
  left: 24px;
  z-index: 10;
  display: inline-flex;
  gap: 6px;
  align-items: center;
  padding: 6px 10px;
  font-size: 13px;
  color: var(--tk-text-secondary);
  text-decoration: none;
  border-radius: var(--tk-radius-md);
  transition: color var(--tk-duration-fast), background var(--tk-duration-fast);

  &:hover {
    color: var(--tk-text-primary);
    background: var(--tk-bg-surface-hover);
  }

  svg { width: 14px; height: 14px; }
}

.tk-cp__card {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 440px;
  padding: 36px;
  background-color: var(--tk-bg-surface);
  border: 1px solid var(--tk-border-color-base);
  border-radius: var(--tk-radius-xl);
}

.tk-cp__header {
  display: flex;
  flex-direction: column;
  align-items: center;
  margin-bottom: 28px;
  text-align: center;
}

.tk-cp__icon {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 56px;
  height: 56px;
  margin-bottom: 16px;
  color: var(--tk-primary-color);
  background: var(--tk-primary-color-bg);
  border: 1px solid var(--tk-primary-color-border);
  border-radius: var(--tk-radius-lg);

  svg { width: 26px; height: 26px; }

  &::after {
    position: absolute;
    inset: -6px;
    z-index: -1;
    content: '';
    background: radial-gradient(circle, rgb(59 91 246 / 20%) 0%, transparent 70%);
    border-radius: var(--tk-radius-xl);
    filter: blur(6px);
  }
}

.tk-cp__title {
  margin: 0 0 6px;
  font-family: var(--tk-font-display);
  font-size: 22px;
  font-weight: var(--tk-font-weight-bold);
  color: var(--tk-text-primary);
  letter-spacing: -0.02em;
}

.tk-cp__subtitle {
  margin: 0;
  font-size: 13px;
  line-height: 1.5;
  color: var(--tk-text-secondary);
}

.tk-cp__field { margin-bottom: 16px; }

.tk-cp__label {
  display: block;
  margin-bottom: 8px;
  font-family: var(--tk-font-mono);
  font-size: 11px;
  font-weight: var(--tk-font-weight-medium);
  color: var(--tk-text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.tk-cp__input-wrap {
  position: relative;
  display: flex;
  align-items: center;
}

.tk-cp__input-icon {
  position: absolute;
  left: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--tk-text-placeholder);
  pointer-events: none;

  svg { width: 16px; height: 16px; }
}

.tk-cp__input {
  width: 100%;
  height: 42px;
  padding: 0 42px 0 40px;
  font-family: var(--tk-font-body);
  font-size: 14px;
  color: var(--tk-text-primary);
  outline: none;
  background-color: var(--tk-bg-surface);
  border: 1px solid var(--tk-border-color-base);
  border-radius: var(--tk-radius-md);
  transition: border-color var(--tk-duration-fast) var(--tk-ease-out),
    box-shadow var(--tk-duration-fast) var(--tk-ease-out);

  &::placeholder { color: var(--tk-text-placeholder); }
  &:hover { border-color: var(--tk-border-color-dark); }

  &:focus {
    border-color: var(--tk-primary-color);
    box-shadow: var(--tk-shadow-focus);
  }

  &--error {
    border-color: var(--tk-danger-color);

    &:focus {
      border-color: var(--tk-danger-color);
      box-shadow: var(--tk-shadow-danger-focus);
    }
  }
}

.tk-cp__toggle-pw {
  position: absolute;
  right: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  color: var(--tk-text-secondary);
  cursor: pointer;
  background: transparent;
  border: none;
  border-radius: var(--tk-radius-sm);

  &:hover {
    color: var(--tk-text-primary);
    background: var(--tk-bg-surface-hover);
  }

  svg { width: 16px; height: 16px; }
}

.tk-cp__field-error {
  min-height: 16px;
  margin-top: 6px;
  font-size: 12px;
  line-height: 1.4;
  color: var(--tk-danger-color-text);
}

// Strength meter
.tk-cp__strength {
  margin: -4px 0 18px;

  &-bar {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 6px;
    margin-bottom: 6px;
  }

  &-seg {
    height: 5px;
    background: var(--tk-bg-fill);
    border: 1px solid var(--tk-border-color-light);
    border-radius: var(--tk-radius-round);
    transition: background var(--tk-duration-fast) var(--tk-ease-out),
      border-color var(--tk-duration-fast) var(--tk-ease-out);
  }

  &-seg--1 { background: var(--tk-danger-color); border-color: var(--tk-danger-color); }
  &-seg--2 { background: var(--tk-warning-color); border-color: var(--tk-warning-color); }
  &-seg--3 { background: var(--tk-info-color); border-color: var(--tk-info-color); }
  &-seg--4 { background: var(--tk-success-color); border-color: var(--tk-success-color); }

  &-meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-family: var(--tk-font-mono);
    font-size: 10px;
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  &-label { font-weight: var(--tk-font-weight-semibold); }
  &-label--1 { color: var(--tk-danger-color); }
  &-label--2 { color: var(--tk-warning-color-text); }
  &-label--3 { color: var(--tk-info-color-text); }
  &-label--4 { color: var(--tk-success-color-text); }
}

// Rules checklist
.tk-cp__rules {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 6px 14px;
  padding: 12px 14px;
  margin: 0 0 22px;
  background: var(--tk-bg-fill-light);
  border: 1px solid var(--tk-border-color-light);
  border-radius: var(--tk-radius-md);
}

.tk-cp__rule {
  display: flex;
  gap: 8px;
  align-items: center;
  font-size: 12px;
  color: var(--tk-text-secondary);
  transition: color var(--tk-duration-fast) var(--tk-ease-out);

  &--ok {
    color: var(--tk-success-color-text);

    .tk-cp__rule-icon {
      color: #fff;
      background: var(--tk-success-color);
      border-color: var(--tk-success-color);
    }
  }
}

.tk-cp__rule-icon {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  color: var(--tk-text-placeholder);
  background: var(--tk-bg-fill);
  border: 1px solid var(--tk-border-color-base);
  border-radius: 50%;
  transition: background var(--tk-duration-fast), border-color var(--tk-duration-fast), color var(--tk-duration-fast);

  svg { width: 10px; height: 10px; stroke-width: 3; }
}

// Submit button
.tk-cp__submit {
  display: inline-flex;
  gap: 8px;
  align-items: center;
  justify-content: center;
  width: 100%;
  height: 44px;
  font-family: var(--tk-font-body);
  font-size: 14px;
  font-weight: var(--tk-font-weight-semibold);
  color: #fff;
  cursor: pointer;
  background: var(--tk-gradient-primary);
  border: 1px solid var(--tk-primary-color);
  border-radius: var(--tk-radius-md);
  transition: box-shadow var(--tk-duration-fast) var(--tk-ease-out),
    transform var(--tk-duration-fast) var(--tk-ease-out),
    opacity var(--tk-duration-fast) var(--tk-ease-out);

  &:hover:not(:disabled) { box-shadow: var(--tk-glow-primary); }
  &:active:not(:disabled) { transform: translateY(1px); }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.55;
    filter: grayscale(0.2);
  }

  &--loading {
    position: relative;
    color: transparent;
    pointer-events: none;

    &::after {
      position: absolute;
      width: 16px;
      height: 16px;
      content: '';
      border: 2px solid #fff;
      border-top-color: transparent;
      border-radius: 50%;
      animation: tk-cp-spin 0.6s linear infinite;
    }
  }
}

.tk-cp__foot {
  margin-top: 22px;
  font-family: var(--tk-font-mono);
  font-size: 11px;
  color: var(--tk-text-placeholder);
  text-align: center;
  letter-spacing: 0.04em;

  a {
    margin: 0 4px;
    color: var(--tk-text-link);
    text-decoration: none;

    &:hover {
      text-decoration: underline;
      text-underline-offset: 3px;
    }
  }
}

@keyframes tk-cp-spin {
  to { transform: rotate(360deg); }
}

@media (max-width: 480px) {
  .tk-cp__card { padding: 28px 22px; }
  .tk-cp__rules { grid-template-columns: 1fr; }
}
</style>
