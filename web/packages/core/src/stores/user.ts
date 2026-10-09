// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { clearAuth, getToken } from '../utils/request'
import { setStorage, getStorage, removeStorage } from '../utils/storage'
import type { FeatureFlags, UserInfo } from '../types/global'

/** User info storage key */
const USER_INFO_KEY = 'tk-user-info'

/**
 * User state store.
 *
 * Holds only user state and state-change primitives; the authentication flow
 * (calling the login/logout API, setting the token) is orchestrated by the
 * features layer to avoid the core reverse-depending on the features auth API.
 */
export const useUserStore = defineStore('user', () => {
  /** User info */
  const userInfo = ref<UserInfo | null>(getStorage<UserInfo>(USER_INFO_KEY))

  /** Feature flag list */
  const features = ref<FeatureFlags>(userInfo.value?.features ?? {})

  /** User role */
  const role = computed(() => userInfo.value?.role ?? '')

  /** Username */
  const username = computed(() => userInfo.value?.username ?? '')

  /** Whether the user is logged in */
  const isLoggedIn = computed(() => !!getToken())

  /**
   * Set the user info.
   *
   * Called by the features layer after completing the login API call and
   * setting the token; writes the user info to state and persists it.
   */
  function setUserInfo(info: UserInfo): void {
    userInfo.value = info
    features.value = info.features
    setStorage(USER_INFO_KEY, info)
  }

  /**
   * Clear the user state.
   *
   * Clears the auth info and the locally persisted user info, resetting the
   * state to logged out. Does not call the logout API; the API call is the
   * responsibility of the features layer.
   */
  function clearUser(): void {
    clearAuth()
    removeStorage(USER_INFO_KEY)
    userInfo.value = null
    features.value = {}
  }

  /**
   * Update the feature flags.
   */
  function updateFeatures(newFeatures: FeatureFlags): void {
    features.value = newFeatures
    if (userInfo.value) {
      userInfo.value.features = newFeatures
      setStorage(USER_INFO_KEY, userInfo.value)
    }
  }

  return {
    userInfo,
    features,
    role,
    username,
    isLoggedIn,
    setUserInfo,
    clearUser,
    updateFeatures,
  }
})
