// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * FeatureGuard - feature flag guard component (generic feature gating shell).
 *
 * Combines the backend feature flag list to implement button-level and block-level
 * display control.
 * - When locked is true, renders locked state: grayscale filter + semi-transparent
 *   overlay + lock icon + neutral hint
 * - badge is injected via prop by extension ({ variant, text }), null on core side
 *   means no badge rendered
 * - When locked is not explicitly passed, it is auto-determined from hasFeature
 *
 * The locked overlay carries the upgrade-guidance trio (open-core strategy
 * §6.2-2): the capability name (optional `capability` prop, an
 * already-localized display string), the owning tier name (the extension's
 * badge text), and a one-hop link to the editions comparison page. No
 * engineering column or feature-flag names appear in user-facing copy.
 */
import { computed } from 'vue'
import { Lock } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { usePermission } from '../composables/usePermission'

/** One-hop destination shared by all in-product upgrade touchpoints */
const EDITIONS_URL = 'https://tickraft.io/editions'

/** Badge data injected by extension via prop; null on core side means no badge rendered */
interface FeatureGuardBadge {
  /** Badge style variant identifier; extension styles build `tk-tier-badge--${variant}` from this */
  variant: string
  /** Badge display text */
  text: string
}

interface FeatureGuardProps {
  /** Feature flag identifier */
  feature: string
  /** Tier badge injected by extension; null on core side means no badge rendered */
  badge?: FeatureGuardBadge | null
  /** Locked state; when not passed, auto-determined as the negation of hasFeature(feature) */
  locked?: boolean
  /** Already-localized capability name shown in the locked overlay hint; omit for the generic copy */
  capability?: string
}

interface FeatureGuardEmits {
  (e: 'upgrade'): void
}

const props = withDefaults(defineProps<FeatureGuardProps>(), {
  badge: null,
  locked: undefined,
  capability: '',
})

const emit = defineEmits<FeatureGuardEmits>()

const { hasFeature } = usePermission()
const { t } = useI18n()

/** Whether in locked state: explicit prop takes priority, otherwise auto-determined from feature flag */
const isLocked = computed(() => {
  if (props.locked !== undefined) return props.locked
  return !hasFeature.value(props.feature)
})

/** Badge style class (extension style extensions render color variants based on this) */
const badgeClass = computed(() =>
  props.badge ? `tk-tier-badge--${props.badge.variant}` : '',
)

/** Overlay hint names the capability when provided, else the generic copy */
const lockText = computed(() =>
  props.capability
    ? t('common.feature.lockedHint', { capability: props.capability })
    : t('common.feature.upgradeToUnlock'),
)

function handleUpgrade() {
  emit('upgrade')
}
</script>

<template>
  <div class="tk-feature-guard">
    <span
      v-if="badge"
      class="tk-tier-badge"
      :class="badgeClass"
    >{{ badge.text }}</span>
    <div class="tk-feature-guard__inner">
      <div
        class="tk-feature-guard__content"
        :class="{ 'tk-feature-locked': isLocked }"
      >
        <slot />
      </div>
      <div
        v-if="isLocked"
        class="tk-feature-guard__overlay"
        role="button"
        tabindex="0"
        :aria-label="lockText"
        @click="handleUpgrade"
        @keydown.enter.prevent="handleUpgrade"
        @keydown.space.prevent="handleUpgrade"
      >
        <el-icon class="tk-feature-guard__lock-icon">
          <Lock />
        </el-icon>
        <span class="tk-feature-guard__lock-text">{{ lockText }}</span>
        <a
          class="tk-feature-guard__link"
          :href="EDITIONS_URL"
          target="_blank"
          rel="noopener"
          @click.stop
        >{{ t('common.feature.viewEditions') }}</a>
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.tk-feature-guard {
  position: relative;
  display: inline-block;
}

.tk-feature-guard__inner {
  position: relative;
}

.tk-feature-guard__content {
  transition: filter var(--tk-transition-base),
    opacity var(--tk-transition-base);
}

/* Locked state: grayscale filter + reduced opacity */
.tk-feature-locked {
  pointer-events: none;
  user-select: none;
  opacity: 0.55;
  filter: grayscale(0.8);
}

/* Locked overlay: covers content, click triggers upgrade */
.tk-feature-guard__overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-sm);
  align-items: center;
  justify-content: center;
  cursor: pointer;

  /* Use the themed mask token — a dark scrim in both light and dark themes,
     so the light overlay text below stays readable either way */
  // Fallback for browsers without custom-property support
  background: rgb(10 14 26 / 55%);
  background: var(--tk-bg-mask);
  border-radius: inherit;
  transition: background var(--tk-transition-base);
}

.tk-feature-guard__overlay:hover {
  // Slightly darker scrim on hover (mix 8% black into the mask color)
  background: color-mix(in srgb, var(--tk-bg-mask) 92%, black);
}

.tk-feature-guard__lock-icon {
  font-size: 28px;

  // White in both themes — it sits on the dark mask scrim above
  color: var(--tk-text-on-primary);
}

.tk-feature-guard__lock-text {
  font-size: var(--tk-font-size-sm);
  font-weight: var(--tk-font-weight-semibold);
  color: var(--tk-text-on-primary);
  letter-spacing: 0.02em;
}

/* One-hop editions link inside the locked overlay */
.tk-feature-guard__link {
  padding: 2px 10px;
  font-size: var(--tk-font-size-xs);
  font-weight: var(--tk-font-weight-medium);
  color: var(--tk-text-on-primary);
  text-decoration: none;
  background: rgb(255 255 255 / 14%);
  border-radius: var(--tk-radius-sm);

  &:hover {
    color: var(--tk-text-on-primary);
    text-decoration: underline;
    text-underline-offset: 3px;
    background: rgb(255 255 255 / 22%);
  }

  &:focus-visible {
    outline: 2px solid var(--tk-text-on-primary);
    outline-offset: 2px;
  }
}

/* ===== Tier badge shell (color variants defined by extension style extensions) ===== */
.tk-tier-badge {
  position: absolute;
  top: var(--tk-spacing-xs);
  right: var(--tk-spacing-xs);
  z-index: 1;
  display: inline-flex;
  align-items: center;
  padding: 1px 6px;
  font-size: 10px;
  font-weight: var(--tk-font-weight-medium);
  line-height: 1.4;

  // White in both themes: tier badges sit on saturated accent/primary chips
  color: var(--tk-text-on-accent);
  text-transform: uppercase;
  letter-spacing: 0.3px;
  border-radius: var(--tk-radius-sm);
  box-shadow: var(--tk-shadow-float);
}

/* Accessibility: respect reduced-motion preference, disable filter and opacity transitions */
@media (prefers-reduced-motion: reduce) {
  .tk-feature-guard__content,
  .tk-feature-guard__overlay {
    transition: none;
  }
}
</style>
