// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Login page
 *
 * Visual source: storyboard/tickraft/pages/auth-login.html
 * Layout is provided by BlankLayout(split): the left brand area and the top-right theme
 * switcher are provided by the core layout; this component only handles the right-side
 * form content.
 *
 * Authentication flow: call login API -> set token -> write user state -> navigate to dashboard.
 */
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import {
  BlankLayout,
  useUserStore,
  setToken,
  setRefreshToken,
  setStorage,
  getStorage,
  removeStorage,
} from '@tickraft/core'
import type { LoginParams, UserInfo } from '@tickraft/core'
import { login as loginApi } from '../../../../api/auth'
import { getProfile } from '../../../../api/system'

/** External link to the editions comparison page */
const EDITIONS_URL = 'https://tickraft.io/editions'

/** Local storage key for remembering the username (only the username is stored; never the password) */
const REMEMBER_KEY = 'tk-remember'

const router = useRouter()
const { t } = useI18n()
const userStore = useUserStore()

const loading = ref(false)
const rememberMe = ref(false)
const errorMsg = ref('')
const showPassword = ref(false)
const errors = reactive<{ username: string; password: string }>({ username: '', password: '' })

const form = reactive<LoginParams>({
  username: '',
  password: '',
})

// Pre-fill the remembered username (if any) when entering the login page
const remembered = getStorage<string>(REMEMBER_KEY)
if (remembered) {
  form.username = remembered
  rememberMe.value = true
}

/** Clear the error message for a specific field */
function clearFieldError(field: 'username' | 'password'): void {
  errors[field] = ''
}

/** Non-empty form validation */
function validate(): boolean {
  errors.username = form.username.trim() ? '' : t('auth.login.usernameRequired')
  errors.password = form.password ? '' : t('auth.login.passwordRequired')
  return !errors.username && !errors.password
}

/** Forgot password: open-source edition has no standalone page, prompt to contact admin */
function handleForgotPassword(): void {
  ElMessage.info(t('auth.login.forgotPasswordHint'))
}

async function handleSubmit(): Promise<void> {
  errorMsg.value = ''
  if (!validate()) return
  loading.value = true
  try {
    const data = await loginApi({ username: form.username.trim(), password: form.password })
    setToken(data.accessToken)
    setRefreshToken(data.refreshToken)

    // Fetch the real user profile so role/id are accurate.
    // Fall back to the entered username if the profile fetch fails (e.g. the
    // token requires MFA verification first).
    let info: UserInfo
    try {
      const profile = await getProfile()
      // Map the backend numeric role (0=viewer, 1=developer, 2=admin) to a string
      const roleMap: Record<number, string> = { 0: 'viewer', 1: 'developer', 2: 'admin' }
      info = {
        id: profile.id,
        username: profile.username || form.username.trim(),
        role: roleMap[profile.role] ?? 'viewer',
        features: {},
      }
    } catch {
      info = {
        id: 0,
        username: form.username.trim(),
        role: 'admin',
        features: {},
      }
    }
    userStore.setUserInfo(info)

    if (rememberMe.value) {
      setStorage(REMEMBER_KEY, form.username.trim())
    } else {
      removeStorage(REMEMBER_KEY)
    }

    // Backend may force a password change on first login
    if (data.mustChangePassword) {
      router.push('/change-password')
    } else {
      router.push('/dashboard/overview')
    }
  } catch (err) {
    errorMsg.value = err instanceof Error ? err.message : t('auth.login.failed')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <BlankLayout layout="split">
    <form
      class="tk-auth-form"
      novalidate
      @submit.prevent="handleSubmit"
    >
      <div class="tk-auth-form__header">
        <h2 class="tk-auth-form__title">
          {{ t('auth.login.welcome') }}
        </h2>
        <p class="tk-auth-form__subtitle">
          {{ t('auth.login.subtitle') }}
        </p>
      </div>

      <!-- Global error prompt -->
      <transition name="tk-auth-fade">
        <div
          v-if="errorMsg"
          class="tk-auth-form__alert"
          role="alert"
        >
          {{ errorMsg }}
        </div>
      </transition>

      <div class="tk-auth-form__field">
        <label
          class="tk-auth-form__label"
          for="tk-username"
        >
          {{ t('auth.login.username') }}
        </label>
        <div class="tk-auth-form__input-wrap">
          <span
            class="tk-auth-form__input-icon"
            aria-hidden="true"
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>
          </span>
          <input
            id="tk-username"
            v-model="form.username"
            class="tk-auth-form__input"
            :class="{ 'tk-auth-form__input--error': errors.username }"
            type="text"
            :placeholder="t('auth.login.usernamePlaceholder')"
            autocomplete="username"
            @input="clearFieldError('username')"
          >
        </div>
        <div class="tk-auth-form__field-error">
          {{ errors.username }}
        </div>
      </div>

      <div class="tk-auth-form__field">
        <label
          class="tk-auth-form__label"
          for="tk-password"
        >
          {{ t('auth.login.password') }}
        </label>
        <div class="tk-auth-form__input-wrap">
          <span
            class="tk-auth-form__input-icon"
            aria-hidden="true"
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
          </span>
          <input
            id="tk-password"
            v-model="form.password"
            class="tk-auth-form__input"
            :class="{ 'tk-auth-form__input--error': errors.password }"
            :type="showPassword ? 'text' : 'password'"
            :placeholder="t('auth.login.passwordPlaceholder')"
            autocomplete="current-password"
            @input="clearFieldError('password')"
          >
          <button
            class="tk-auth-form__toggle-pw"
            type="button"
            :aria-label="t('auth.login.togglePassword')"
            @click="showPassword = !showPassword"
          >
            <svg
              v-if="!showPassword"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
            ><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
            <svg
              v-else
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
            ><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/></svg>
          </button>
        </div>
        <div class="tk-auth-form__field-error">
          {{ errors.password }}
        </div>
      </div>

      <div class="tk-auth-form__row">
        <label class="tk-auth-form__check">
          <input
            v-model="rememberMe"
            type="checkbox"
          >
          <span>{{ t('auth.login.rememberMe') }}</span>
        </label>
        <a
          class="tk-auth-form__link"
          href="javascript:void(0)"
          @click="handleForgotPassword"
        >
          {{ t('auth.login.forgotPassword') }}
        </a>
      </div>

      <button
        type="submit"
        class="tk-auth-form__submit"
        :class="{ 'tk-auth-form__submit--loading': loading }"
        :disabled="loading"
      >
        {{ loading ? t('auth.login.submitting') : t('auth.login.submit') }}
      </button>

      <!-- CE notice: promotes upgrading to the professional edition for multi-user, SSO, RBAC and audit logs -->
      <div
        class="tk-auth-form__ce-notice"
        role="note"
      >
        <span
          class="tk-auth-form__ce-notice-icon"
          aria-hidden="true"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="M9 12l2 2 4-4"/></svg>
        </span>
        <div class="tk-auth-form__ce-notice-body">
          <strong>{{ t('auth.login.ceTitle') }}</strong>{{ t('auth.login.ceDesc') }}<a
            class="tk-auth-form__link"
            :href="EDITIONS_URL"
            target="_blank"
            rel="noopener"
          >{{ t('auth.login.ceEditions') }}</a>。
        </div>
      </div>

      <div class="tk-auth-form__foot">
        {{ t('auth.login.foot') }}<br>
        {{ t('auth.login.footLicense') }}
      </div>
    </form>
  </BlankLayout>
</template>

<style scoped lang="scss">
.tk-auth-form {
  width: 100%;
  max-width: 384px;

  &__header {
    margin-bottom: var(--tk-spacing-xl);
  }

  &__title {
    margin: 0 0 6px;
    font-family: var(--tk-font-display);
    font-size: 26px;
    font-weight: var(--tk-font-weight-bold);
    color: var(--tk-text-primary);
    letter-spacing: -0.02em;
  }

  &__subtitle {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
    color: var(--tk-text-secondary);
  }

  &__alert {
    display: flex;
    align-items: center;
    padding: var(--tk-spacing-sm) var(--tk-spacing-md);
    margin-bottom: var(--tk-spacing-md);
    font-size: var(--tk-font-size-sm);
    color: var(--tk-danger-color-text);
    background-color: var(--tk-danger-color-bg);
    border: 1px solid var(--tk-danger-color-border);
    border-radius: var(--tk-radius-md);
  }

  &__field {
    margin-bottom: 18px;
  }

  &__label {
    display: block;
    margin-bottom: 8px;
    font-family: var(--tk-font-mono);
    font-size: 11px;
    font-weight: var(--tk-font-weight-medium);
    color: var(--tk-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  &__input-wrap {
    position: relative;
    display: flex;
    align-items: center;
  }

  &__input-icon {
    position: absolute;
    left: 12px;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--tk-text-placeholder);
    pointer-events: none;

    svg {
      width: 16px;
      height: 16px;
    }
  }

  &__input {
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

    &::placeholder {
      color: var(--tk-text-placeholder);
    }

    &:hover {
      border-color: var(--tk-border-color-dark);
    }

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

  &__toggle-pw {
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

    svg {
      width: 16px;
      height: 16px;
    }
  }

  &__field-error {
    min-height: 16px;
    margin-top: 6px;
    font-size: 12px;
    line-height: 1.4;
    color: var(--tk-danger-color-text);
  }

  &__row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin: 4px 0 24px;
  }

  &__check {
    display: inline-flex;
    gap: 8px;
    align-items: center;
    font-size: 13px;
    color: var(--tk-text-regular);
    cursor: pointer;
    user-select: none;

    input {
      width: 14px;
      height: 14px;
      accent-color: var(--tk-primary-color);
      cursor: pointer;
    }
  }

  &__link {
    font-size: 13px;
    color: var(--tk-text-link);
    text-decoration: none;

    &:hover {
      color: var(--tk-text-link-hover);
      text-decoration: underline;
      text-underline-offset: 3px;
    }
  }

  &__submit {
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
      transform var(--tk-duration-fast) var(--tk-ease-out);

    &:hover:not(:disabled) {
      box-shadow: var(--tk-glow-primary);
    }

    &:active:not(:disabled) {
      transform: translateY(1px);
    }

    &:disabled {
      cursor: not-allowed;
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
        animation: tk-auth-spin 0.6s linear infinite;
      }
    }
  }

  &__ce-notice {
    display: flex;
    gap: 10px;
    align-items: flex-start;
    padding: 12px 14px;
    margin-top: var(--tk-spacing-lg);
    font-size: 12px;
    line-height: 1.55;
    color: var(--tk-text-regular);
    background: var(--tk-primary-color-bg);
    border: 1px solid var(--tk-primary-color-border);
    border-radius: var(--tk-radius-md);
  }

  &__ce-notice-icon {
    flex-shrink: 0;
    width: 18px;
    height: 18px;
    margin-top: 1px;
    color: var(--tk-primary-color);

    svg {
      width: 18px;
      height: 18px;
    }
  }

  &__ce-notice-body strong {
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
  }

  &__foot {
    margin-top: 36px;
    font-family: var(--tk-font-mono);
    font-size: 11px;
    line-height: 1.7;
    color: var(--tk-text-placeholder);
    text-align: center;
    letter-spacing: 0.04em;
  }
}

.tk-auth-fade-enter-active,
.tk-auth-fade-leave-active {
  transition: opacity var(--tk-duration-fast);
}

.tk-auth-fade-enter-from,
.tk-auth-fade-leave-to {
  opacity: 0;
}

@keyframes tk-auth-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
