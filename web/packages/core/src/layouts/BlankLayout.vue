// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * BlankLayout - blank layout component for auth pages.
 *
 * Used for full-screen pages like login. Aligned with the auth-login.html prototype:
 * - split: left brand panel (60%, dark mission-control style) + right form panel (40%)
 * - center: centered card (brand panel hidden)
 *
 * The brand panel is rendered by the AuthBrandPanel sub-component, which owns the
 * three-layer animated background (grid / glow pulse / radar rings) and the i18n-aware
 * brand copy. Brand props are passed through to AuthBrandPanel.
 *
 * A theme toggle icon button is built into the top-right corner; theme state is managed
 * by useAppStore and persisted. Extension injects form content via the default slot.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '../stores/app'
import AuthBrandPanel from './AuthBrandPanel.vue'
import IMoon from '~icons/ep/moon'
import ISunny from '~icons/ep/sunny'

/** Brand feature icon key (forwarded to AuthBrandPanel). */
type BrandFeatureIcon = 'activity' | 'clock' | 'shield' | 'bolt'

/** Brand feature item (forwarded to AuthBrandPanel). */
interface BrandFeature {
  /** Feature title */
  title: string
  /** Feature description */
  description: string
  /** Icon key; defaults by index when omitted */
  icon?: BrandFeatureIcon
}

interface BlankLayoutProps {
  /** Layout mode: split = left-right columns (brand area + form area), center = centered card */
  layout?: 'split' | 'center'
  /** Brand wordmark (e.g. "Tickraft"); brand names are not translated */
  brandTitle?: string
  /** Eyebrow label rendered above the headline */
  brandEyebrow?: string
  /** Main headline first part (rendered before the gradient accent) */
  brandHeadline?: string
  /** Accent part of the headline (rendered as gradient text) */
  brandHeadlineAccent?: string
  /** Tagline rendered below the headline */
  brandTagline?: string
  /** Edition badge text (e.g. "Community Edition") */
  brandEdition?: string
  /** Brand area feature list (only shown in split layout) */
  brandFeatures?: BrandFeature[]
  /** Whether to show the footer copyright bar */
  showFooter?: boolean
}

const props = withDefaults(defineProps<BlankLayoutProps>(), {
  layout: 'split',
  brandTitle: 'Tickraft',
  brandEyebrow: '',
  brandHeadline: '',
  brandHeadlineAccent: '',
  brandTagline: '',
  brandEdition: '',
  brandFeatures: () => [],
  showFooter: false,
})

const { t, te, locale } = useI18n()
const appStore = useAppStore()

const isSplit = computed(() => props.layout === 'split')
const isDark = computed(() => appStore.theme === 'dark')
const year = new Date().getFullYear()

/**
 * i18n translation with fallback.
 *
 * When the i18n key is not yet defined in the locale pack, returns the fallback text
 * for the current language, avoiding raw key strings in the UI.
 */
function tf(key: string, zhFallback: string, enFallback: string): string {
  if (te(key)) return t(key)
  return locale.value === 'en-US' ? enFallback : zhFallback
}

/** Theme toggle button tooltip (hints the target mode to switch to) */
const themeTooltip = computed(() =>
  isDark.value
    ? tf('system.settings.lightTheme', '切换至浅色模式', 'Switch to light mode')
    : tf('system.settings.darkTheme', '切换至深色模式', 'Switch to dark mode'),
)

/** Toggle theme */
function toggleTheme() {
  appStore.setTheme(isDark.value ? 'light' : 'dark')
}
</script>

<template>
  <div
    class="tk-blank-layout"
    :class="`tk-blank-layout--${props.layout}`"
  >
    <!-- Left brand panel (split layout only; hidden on mobile). Flex sizing and
         visibility are controlled below via scoped styles on .tk-blank-layout__brand;
         the AuthBrandPanel root <aside> receives this class so parent scoping applies. -->
    <AuthBrandPanel
      v-if="isSplit"
      :brand-title="brandTitle"
      :brand-eyebrow="brandEyebrow"
      :brand-headline="brandHeadline"
      :brand-headline-accent="brandHeadlineAccent"
      :brand-tagline="brandTagline"
      :brand-edition="brandEdition"
      :brand-features="brandFeatures"
    />

    <!-- Right form area -->
    <main class="tk-blank-layout__main">
      <el-tooltip
        :content="themeTooltip"
        placement="bottom"
      >
        <button
          class="tk-blank-layout__theme-toggle"
          type="button"
          :aria-label="themeTooltip"
          @click="toggleTheme"
        >
          <el-icon>
            <IMoon v-if="!isDark" />
            <ISunny v-else />
          </el-icon>
        </button>
      </el-tooltip>

      <div class="tk-blank-layout__card">
        <slot />
      </div>
    </main>

    <footer
      v-if="showFooter"
      class="tk-blank-layout__footer"
    >
      <slot name="footer">
        <p>&copy; {{ year }} Tickraft</p>
      </slot>
    </footer>
  </div>
</template>

<style scoped lang="scss">
.tk-blank-layout {
  display: flex;
  min-height: 100vh;
  background-color: var(--tk-bg-color-page);

  // ===== split layout: 60/40 brand/form split (aligned with prototype) =====
  &--split {
    // AuthBrandPanel root is <aside class="tk-blank-layout__brand">; in Vue 3 the child
    // root receives the parent scope attribute, so these rules apply correctly.
    .tk-blank-layout__brand { flex: 0 0 60%; }
    .tk-blank-layout__main { flex: 0 0 40%; }
  }

  // ===== center layout: centered card, brand panel hidden =====
  &--center {
    flex-direction: column;
    align-items: center;
    justify-content: center;

    .tk-blank-layout__brand { display: none; }
    .tk-blank-layout__main { width: 100%; }
  }

  // ===== Right form area =====
  &__main {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 48px 32px;
    background-color: var(--tk-bg-color-page);
  }

  // Theme toggle icon button (top-right absolute)
  &__theme-toggle {
    position: absolute;
    top: 24px;
    right: 24px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    padding: 0;
    color: var(--tk-text-secondary);
    cursor: pointer;
    background-color: transparent;
    border: none;
    border-radius: var(--tk-radius-md, 8px);
    transition: background-color var(--tk-transition-fast, 0.15s cubic-bezier(0.4, 0, 0.2, 1)),
      color var(--tk-transition-fast, 0.15s cubic-bezier(0.4, 0, 0.2, 1));

    &:hover {
      color: var(--tk-text-primary);
      background-color: var(--tk-bg-hover);
    }

    .el-icon { font-size: 18px; }
  }

  &__card {
    width: 100%;
    max-width: 384px;
  }

  &__footer {
    padding: var(--tk-spacing-md);
    font-size: var(--tk-font-size-sm);
    color: var(--tk-text-secondary);
    text-align: center;
  }

  // ===== Responsive =====
  @media (max-width: 980px) {
    &--split {
      .tk-blank-layout__brand { flex: 0 0 52%; }
      .tk-blank-layout__main { flex: 0 0 48%; }
    }
  }

  @media (max-width: 767px) {
    &--split { flex-direction: column; }

    .tk-blank-layout__brand { display: none; }

    &__main {
      flex: 1;
      padding: 32px 20px;
    }

    &__card { max-width: 100%; }
  }
}
</style>
