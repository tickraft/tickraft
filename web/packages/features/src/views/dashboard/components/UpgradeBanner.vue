// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * UpgradeBanner - unified dashboard upgrade banner.
 *
 * Replaces the scattered `<FeatureGuard locked>` placeholders across CE
 * business pages with a single, visually appealing card on the dashboard
 * that promotes upgrading to the professional edition. Pro-only features
 * (SSH/MySQL/Redis executors, DNS/SSL probes, multi-user, audit logs, etc.)
 * are highlighted here rather than shown as locked placeholders inline.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

/**
 * Upgrade banner visibility toggle.
 *
 * The banner is hidden while the professional edition is not yet live.
 * Flip this flag to `true` once the paid edition ships and the pricing page
 * is reachable. Keeping it as a single module constant avoids scattering
 * feature-gating logic across the codebase.
 */
const UPGRADE_BANNER_ENABLED = false

/** External link to the subscription / pricing page */
const PRICING_URL = 'https://tickraft.com/pricing'

const { t } = useI18n()

/** Feature highlight shown in the banner; i18n keys are resolved via computed to stay reactive on language switch */
interface UpgradeFeature {
  icon: string
  labelKey: string
}

const features = computed<UpgradeFeature[]>(() => [
  { icon: 'i-ep-connection', labelKey: 'dashboard.upgrade.featureSshExecutor' },
  { icon: 'i-ep-data-base', labelKey: 'dashboard.upgrade.featureDbExecutor' },
  { icon: 'i-ep-position', labelKey: 'dashboard.upgrade.featureAdvancedProber' },
  { icon: 'i-user', labelKey: 'dashboard.upgrade.featureMultiUser' },
  { icon: 'i-ep-document', labelKey: 'dashboard.upgrade.featureAuditLog' },
  { icon: 'i-ep-bell', labelKey: 'dashboard.upgrade.featureMultiChannel' },
])
</script>

<template>
  <section
    v-if="UPGRADE_BANNER_ENABLED"
    class="tk-upgrade-banner"
    role="region"
    :aria-label="t('dashboard.upgrade.title')"
  >
    <div class="tk-upgrade-banner__glow" />
    <div class="tk-upgrade-banner__content">
      <div class="tk-upgrade-banner__head">
        <div class="tk-upgrade-banner__heading">
          <span class="tk-upgrade-banner__badge">PRO</span>
          <h2 class="tk-upgrade-banner__title">
            {{ t('dashboard.upgrade.title') }}
          </h2>
        </div>
        <p class="tk-upgrade-banner__desc">
          {{ t('dashboard.upgrade.description') }}
        </p>
      </div>

      <ul class="tk-upgrade-banner__features">
        <li
          v-for="item in features"
          :key="item.labelKey"
          class="tk-upgrade-banner__feature"
        >
          <span class="tk-upgrade-banner__feature-icon">
            <i :class="item.icon" />
          </span>
          <span class="tk-upgrade-banner__feature-label">{{ t(item.labelKey) }}</span>
        </li>
      </ul>

      <div class="tk-upgrade-banner__actions">
        <a
          class="tk-upgrade-banner__cta"
          :href="PRICING_URL"
          target="_blank"
          rel="noopener"
        >
          {{ t('dashboard.upgrade.cta') }}
          <i class="i-ep-arrow-right" />
        </a>
      </div>
    </div>
  </section>
</template>

<style scoped lang="scss">
.tk-upgrade-banner {
  position: relative;
  display: block;
  padding: var(--tk-spacing-lg);
  overflow: hidden;
  // Fallback for browsers without color-mix support (#4263eb is --tk-primary-color)
  background:
    linear-gradient(135deg, rgba(66, 99, 235, 0.12) 0%, transparent 60%),
    var(--tk-bg-surface);
  background:
    linear-gradient(135deg, color-mix(in srgb, var(--tk-primary-color) 12%, transparent) 0%, transparent 60%),
    var(--tk-bg-surface);
  // Fallback for browsers without color-mix support
  border: 1px solid rgba(66, 99, 235, 0.3);
  border: 1px solid color-mix(in srgb, var(--tk-primary-color) 30%, var(--tk-border-color));
  border-radius: var(--tk-border-radius-lg);
  isolation: isolate;
}

.tk-upgrade-banner__glow {
  position: absolute;
  top: -60px;
  right: -60px;
  z-index: -1;
  width: 220px;
  height: 220px;
  pointer-events: none;
  // Fallback for browsers without color-mix support
  background: radial-gradient(
    circle,
    rgba(66, 99, 235, 0.22) 0%,
    transparent 70%
  );
  background: radial-gradient(
    circle,
    color-mix(in srgb, var(--tk-primary-color) 22%, transparent) 0%,
    transparent 70%
  );
  border-radius: 50%;
}

.tk-upgrade-banner__content {
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-md);
}

.tk-upgrade-banner__head {
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-xs);
}

.tk-upgrade-banner__heading {
  display: flex;
  flex-wrap: wrap;
  gap: var(--tk-spacing-sm);
  align-items: center;
}

.tk-upgrade-banner__badge {
  display: inline-flex;
  align-items: center;
  padding: 2px 8px;
  font-family: var(--tk-font-family-mono, monospace);
  font-size: var(--tk-font-size-xs);
  font-weight: var(--tk-font-weight-bold);
  color: #fff;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  background: linear-gradient(135deg, var(--tk-primary-color) 0%, var(--tk-primary-color-dark, var(--tk-primary-color)) 100%);
  border-radius: var(--tk-radius-sm);
}

.tk-upgrade-banner__title {
  margin: 0;
  font-family: var(--tk-font-family);
  font-size: var(--tk-font-size-xl);
  font-weight: var(--tk-font-weight-bold);
  line-height: 1.2;
  color: var(--tk-text-primary);
  letter-spacing: -0.02em;
}

.tk-upgrade-banner__desc {
  max-width: 640px;
  margin: 0;
  font-size: var(--tk-font-size-sm);
  line-height: var(--tk-line-height-normal);
  color: var(--tk-text-secondary);
}

.tk-upgrade-banner__features {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: var(--tk-spacing-sm);
  padding: 0;
  margin: 0;
  list-style: none;
}

.tk-upgrade-banner__feature {
  display: flex;
  gap: var(--tk-spacing-sm);
  align-items: center;
  padding: var(--tk-spacing-sm) var(--tk-spacing-md);
  background-color: var(--tk-bg-surface);
  border: 1px solid var(--tk-border-color-lighter);
  border-radius: var(--tk-radius-md);
  transition: border-color var(--tk-transition-fast), background-color var(--tk-transition-fast);

  &:hover {
    // Fallback for browsers without color-mix support
    background-color: rgba(66, 99, 235, 0.05);
    background-color: color-mix(in srgb, var(--tk-primary-color) 5%, var(--tk-bg-surface));
    // Fallback for browsers without color-mix support
    border-color: rgba(66, 99, 235, 0.4);
    border-color: color-mix(in srgb, var(--tk-primary-color) 40%, var(--tk-border-color));
  }
}

.tk-upgrade-banner__feature-icon {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  color: var(--tk-primary-color);
  background-color: var(--tk-primary-color-bg);
  border-radius: var(--tk-radius-sm);

  i { font-size: 15px; }
}

.tk-upgrade-banner__feature-label {
  font-size: var(--tk-font-size-sm);
  font-weight: var(--tk-font-weight-medium);
  line-height: 1.3;
  color: var(--tk-text-primary);
}

.tk-upgrade-banner__actions {
  display: flex;
  gap: var(--tk-spacing-sm);
  align-items: center;
}

.tk-upgrade-banner__cta {
  display: inline-flex;
  gap: var(--tk-spacing-xs);
  align-items: center;
  padding: var(--tk-spacing-sm) var(--tk-spacing-lg);
  font-family: var(--tk-font-family);
  font-size: var(--tk-font-size-sm);
  font-weight: var(--tk-font-weight-semibold);
  color: #fff;
  text-decoration: none;
  cursor: pointer;
  background: var(--tk-gradient-primary, var(--tk-primary-color));
  border: 1px solid var(--tk-primary-color);
  border-radius: var(--tk-radius-md);
  transition: box-shadow var(--tk-transition-fast), transform var(--tk-transition-fast);

  &:hover {
    color: #fff;
    box-shadow: var(--tk-glow-primary, 0 0 0 3px color-mix(in srgb, var(--tk-primary-color) 20%, transparent));
  }

  &:active {
    transform: translateY(1px);
  }

  &:focus-visible {
    outline: 2px solid var(--tk-primary-color);
    outline-offset: 2px;
  }

  i { font-size: 14px; }
}

@media (max-width: 1199px) {
  .tk-upgrade-banner__features {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (max-width: 639px) {
  .tk-upgrade-banner__features {
    grid-template-columns: 1fr;
  }
}

@media (prefers-reduced-motion: reduce) {
  .tk-upgrade-banner__cta,
  .tk-upgrade-banner__feature {
    transition: none;
  }
}
</style>
