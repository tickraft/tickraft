// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { onMounted, watch } from 'vue'
import { ElConfigProvider } from 'element-plus'
import { useAppStore } from './stores/app'
import { elementPlusLocale, loadElementPlusLocale } from './i18n/element-plus'

const appStore = useAppStore()

// Reload Element Plus locale pack whenever the active locale changes so that
// built-in components (pagination, date pickers, etc.) stay in sync with the
// application locale without a full page reload.
watch(
  () => appStore.locale,
  (next) => {
    void loadElementPlusLocale(next)
  },
)

// Initialize Element Plus locale for the persisted/default locale on mount.
onMounted(() => {
  void loadElementPlusLocale(appStore.locale)
})
</script>

<template>
  <el-config-provider :locale="elementPlusLocale ?? undefined" class="tk-app">
    <router-view />
  </el-config-provider>
</template>
