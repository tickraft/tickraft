// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * StatusTag - status tag component.
 *
 * Provides a unified semantic rendering for asset / alert / task / log status.
 * Automatically matches the semantic tone, icon, animation and label based on
 * category + status.
 *
 * Capabilities:
 * - Four status categories: asset(normal/abnormal/offline/unknown),
 *   alert(firing/resolved), task(pending/running/success/failed/timeout),
 *   log(success/failed/timeout/running)
 * - Three sizes: sm(20px) / md(24px) / lg(28px)
 * - Leading icon: controlled by showIcon; running spins, firing pulses
 * - Dot mode: when dot is true, only a colored dot is rendered; the label is
 *   exposed via title/aria-label
 * - Three effects: light tinted background / dark solid / plain outlined
 * - Label priority: label prop > i18n(common.status.{status}) > built-in fallback
 *
 * Color, radius and font weight all derive from design token CSS variables;
 * hard-coded color values are prohibited.
 */
import { computed, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  SuccessFilled,
  WarningFilled,
  CircleCloseFilled,
  InfoFilled,
  Loading,
} from '@element-plus/icons-vue'
import type { StatusCategory, StatusSize } from '../types/global'

/** Semantic tone, drives color mapping */
type StatusTone = 'success' | 'warning' | 'danger' | 'info' | 'primary'

/** Tag visual effect */
type StatusEffect = 'light' | 'dark' | 'plain'

/** Single status entry: tone + icon + spin + pulse */
interface StatusConfig {
  tone: StatusTone
  icon: Component
  spin: boolean
  pulse: boolean
}

interface StatusTagProps {
  /** Business module category, determines the status enum semantics */
  category?: StatusCategory
  /** Status value; tone/icon/animation are matched automatically based on category */
  status: string
  /** Size: sm 20px / md 24px / lg 28px */
  size?: StatusSize
  /** Whether to show the leading icon */
  showIcon?: boolean
  /** Dot mode: render only a colored dot; the label is exposed via title/aria-label */
  dot?: boolean
  /** Visual effect: light tinted background / dark solid / plain outlined */
  effect?: StatusEffect
  /** Custom label; when omitted, i18n(common.status.{status}) is used */
  label?: string
}

const props = withDefaults(defineProps<StatusTagProps>(), {
  category: 'asset',
  size: 'md',
  showIcon: false,
  dot: false,
  effect: 'light',
  label: undefined,
})

const { t, te } = useI18n()

/** Status config registry per category: tone drives color, icon/spin/pulse drive icon behavior */
const STATUS_REGISTRY: Record<StatusCategory, Record<string, StatusConfig>> = {
  asset: {
    normal: { tone: 'success', icon: SuccessFilled, spin: false, pulse: false },
    abnormal: { tone: 'warning', icon: WarningFilled, spin: false, pulse: false },
    offline: { tone: 'danger', icon: CircleCloseFilled, spin: false, pulse: false },
    unknown: { tone: 'info', icon: InfoFilled, spin: false, pulse: false },
  },
  alert: {
    firing: { tone: 'danger', icon: WarningFilled, spin: false, pulse: true },
    resolved: { tone: 'success', icon: SuccessFilled, spin: false, pulse: false },
  },
  task: {
    pending: { tone: 'info', icon: InfoFilled, spin: false, pulse: false },
    running: { tone: 'primary', icon: Loading, spin: true, pulse: false },
    success: { tone: 'success', icon: SuccessFilled, spin: false, pulse: false },
    failed: { tone: 'danger', icon: CircleCloseFilled, spin: false, pulse: false },
    timeout: { tone: 'warning', icon: WarningFilled, spin: false, pulse: false },
  },
  log: {
    success: { tone: 'success', icon: SuccessFilled, spin: false, pulse: false },
    failed: { tone: 'danger', icon: CircleCloseFilled, spin: false, pulse: false },
    timeout: { tone: 'warning', icon: WarningFilled, spin: false, pulse: false },
    running: { tone: 'primary', icon: Loading, spin: true, pulse: false },
  },
}

/** Fallback config for unknown status values */
const DEFAULT_CONFIG: StatusConfig = {
  tone: 'info',
  icon: InfoFilled,
  spin: false,
  pulse: false,
}

/** tone → design token CSS variable name mapping */
const COLOR_VARS: Record<
  StatusTone,
  { color: string; colorText: string; bg: string; border: string }
> = {
  success: {
    color: '--tk-success-color',
    colorText: '--tk-success-color-text',
    bg: '--tk-success-color-bg',
    border: '--tk-success-color-border',
  },
  warning: {
    color: '--tk-warning-color',
    colorText: '--tk-warning-color-text',
    bg: '--tk-warning-color-bg',
    border: '--tk-warning-color-border',
  },
  danger: {
    color: '--tk-danger-color',
    colorText: '--tk-danger-color-text',
    bg: '--tk-danger-color-bg',
    border: '--tk-danger-color-border',
  },
  info: {
    color: '--tk-info-color',
    // info tone equals the primary tone; reuse the primary dark text variant for WCAG AA
    colorText: '--tk-text-link',
    bg: '--tk-info-color-bg',
    border: '--tk-info-color-border',
  },
  primary: {
    color: '--tk-primary-color',
    colorText: '--tk-text-link',
    bg: '--tk-primary-color-bg',
    border: '--tk-primary-color-border',
  },
}

/** Fallback labels used when the i18n key is missing */
const FALLBACK_LABELS: Record<string, string> = {
  normal: 'Normal',
  abnormal: 'Abnormal',
  offline: 'Offline',
  unknown: 'Unknown',
  firing: 'Firing',
  resolved: 'Resolved',
  pending: 'Pending',
  running: 'Running',
  success: 'Success',
  failed: 'Failed',
  timeout: 'Timeout',
}

/** Cross-category status lookup order: when the specified category has no match, fall back in this order */
const CATEGORY_FALLBACK: StatusCategory[] = ['asset', 'alert', 'task', 'log']

const config = computed<StatusConfig>(() => {
  const categoryMap = STATUS_REGISTRY[props.category]
  if (categoryMap && categoryMap[props.status]) return categoryMap[props.status]
  // Cross-category fallback: look up the same status value in other categories
  for (const category of CATEGORY_FALLBACK) {
    const found = STATUS_REGISTRY[category][props.status]
    if (found) return found
  }
  return DEFAULT_CONFIG
})

const displayLabel = computed<string>(() => {
  if (props.label !== undefined && props.label !== '') return props.label
  const key = `common.status.${props.status}`
  if (te(key)) return t(key)
  return FALLBACK_LABELS[props.status] ?? props.status
})

const isFiring = computed(() => config.value.pulse)
const isRunning = computed(() => config.value.spin)

/** Inline semantic color CSS custom properties, decoupling tone from effect */
const cssVars = computed(() => {
  const vars = COLOR_VARS[config.value.tone]
  return {
    // Light variant: used for dark solid effect background/border, dot fill and pulse animation
    '--tk-status-color': `var(${vars.color})`,
    // Dark text variant: used for light/plain effect text to satisfy WCAG AA contrast
    '--tk-status-color-text': `var(${vars.colorText})`,
    '--tk-status-bg': `var(${vars.bg})`,
    '--tk-status-border': `var(${vars.border})`,
  } as Record<string, string>
})
</script>

<template>
  <!-- Dot mode: render only a colored dot; the label is exposed via title/aria-label for accessibility -->
  <span
    v-if="dot"
    class="tk-status-tag tk-status-tag--dot"
    :class="[
      `tk-status-tag--${size}`,
      { 'tk-status-tag--pulse': isFiring },
    ]"
    :style="cssVars"
    :title="displayLabel"
    :aria-label="displayLabel"
    role="status"
  />
  <!-- Tag mode: icon + label + default slot -->
  <span
    v-else
    class="tk-status-tag"
    :class="[
      `tk-status-tag--${size}`,
      `tk-status-tag--${effect}`,
      { 'tk-status-tag--pulse': isFiring },
    ]"
    :style="cssVars"
    role="status"
  >
    <span
      v-if="showIcon"
      class="tk-status-tag__icon"
      :class="{ 'tk-status-tag__icon--spin': isRunning }"
    >
      <component :is="config.icon" />
    </span>
    <span class="tk-status-tag__text">
      <slot>{{ displayLabel }}</slot>
    </span>
  </span>
</template>

<style scoped lang="scss">
.tk-status-tag {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  font-weight: var(--tk-font-weight-medium);
  line-height: 1;
  vertical-align: middle;

  /* Semantic colors are injected via inline CSS custom properties; effect only changes how they are consumed */

  /* Default text color uses the dark variant (light/plain effects meet WCAG AA on light backgrounds) */
  color: var(--tk-status-color-text);
  white-space: nowrap;
  user-select: none;
  border: 1px solid transparent;
  border-radius: var(--tk-border-radius-sm);

  /* —— Three sizes —— */
  &--sm {
    height: 20px;
    padding: 0 6px;
    font-size: var(--tk-font-size-xs);
  }

  &--md {
    height: 24px;
    padding: 0 8px;
    font-size: var(--tk-font-size-xs);
  }

  &--lg {
    height: 28px;
    padding: 0 10px;
    font-size: var(--tk-font-size-sm);
  }

  /* —— Three effects —— */
  &--light {
    background-color: var(--tk-status-bg);
    border-color: var(--tk-status-border);
  }

  &--dark {
    color: var(--tk-text-on-primary);
    background-color: var(--tk-status-color);
    border-color: var(--tk-status-color);
  }

  &--plain {
    background-color: transparent;
    border-color: var(--tk-border-color-base);
  }

  /* —— Dot mode —— */
  &--dot {
    width: 8px;
    height: 8px;
    padding: 0;
    font-size: 0;
    line-height: 0;
    background-color: var(--tk-status-color);
    border: none;
    border-radius: 50%;
    // Fallback for browsers without color-mix support: solid ring in the status color
    box-shadow: 0 0 0 2px var(--tk-status-color);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--tk-status-color) 22%, transparent);

    &.tk-status-tag--sm {
      width: 6px;
      height: 6px;
    }

    &.tk-status-tag--lg {
      width: 10px;
      height: 10px;
    }
  }

  /* —— Leading icon: span wraps the svg, pierces scoped to control size —— */
  &__icon {
    display: inline-flex;
    flex-shrink: 0;
    align-items: center;
    justify-content: center;
    width: 1em;
    height: 1em;

    :deep(svg) {
      width: 100%;
      height: 100%;
      fill: currentcolor;
    }

    &--spin {
      animation: tk-status-tag-spin 1s linear infinite;
    }
  }

  &__text {
    line-height: 1;
  }

  /* —— firing pulse animation: box-shadow 0 → 6px → 0 loop —— */
  &--pulse {
    animation: tk-status-tag-pulse 2s infinite;
  }
}

@keyframes tk-status-tag-spin {
  to {
    transform: rotate(360deg);
  }
}

@keyframes tk-status-tag-pulse {
  0%,
  100% {
    // Fallback for browsers without color-mix support: solid pulse ring in the status color
    box-shadow: 0 0 0 0 var(--tk-status-color);
    box-shadow: 0 0 0 0 color-mix(in srgb, var(--tk-status-color) 45%, transparent);
  }

  50% {
    // Fallback for browsers without color-mix support: solid pulse ring in the status color
    box-shadow: 0 0 0 6px var(--tk-status-color);
    box-shadow: 0 0 0 6px color-mix(in srgb, var(--tk-status-color) 45%, transparent);
  }
}

/* Accessibility: respect the user's reduced-motion preference, disable all animations */
@media (prefers-reduced-motion: reduce) {
  .tk-status-tag--pulse,
  .tk-status-tag__icon--spin {
    animation: none;
  }
}
</style>
