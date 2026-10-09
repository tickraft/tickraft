// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { Directive, DirectiveBinding } from 'vue'
import { useUserStore } from '../stores/user'

/**
 * Feature flag directive v-feature.
 * Controls element visibility based on the user's feature flags.
 *
 * Usage:
 * <div v-feature="'ssh_executor'">SSH executor</div>
 * <div v-feature="['ssh_executor', 'mysql_executor']">Multiple feature flags</div>
 */
export const vFeature: Directive = {
  mounted(el: HTMLElement, binding: DirectiveBinding<string | string[]>) {
    const userStore = useUserStore()
    const features = binding.value

    if (!features) return

    const featureList = Array.isArray(features) ? features : [features]
    const hasFeature = featureList.some((f) => userStore.features[f])

    if (!hasFeature) {
      el.parentNode?.removeChild(el)
    }
  },
}
