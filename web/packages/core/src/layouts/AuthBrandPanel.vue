// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * AuthBrandPanel - left brand panel of the auth split layout.
 *
 * Renders the mission-control style brand area aligned with the auth-login.html
 * prototype: a three-layer animated background (grid overlay / glow pulse / radar
 * rings), a logo wordmark with an edition badge, an i18n-aware headline with a
 * gradient accent (the accent is forced onto its own line via `display: block` so
 * the gradient never breaks mid-text), a tagline, a feature list with SVG icons,
 * and a minimal copyright footer.
 *
 * Layout uses a top-anchored flow: logo (top) → controlled top spacing for the
 * center block (clamp-based, scales with viewport height) → eyebrow/headline/
 * tagline/features → `margin-top: auto` anchors the copyright foot to the bottom.
 * Padding and headline font-size scale with `clamp()` so the panel adapts fluidly
 * across viewports without abrupt breakpoints.
 *
 * All copy is resolved via `tf()` so it falls back gracefully when locale packs
 * are not yet loaded.
 *
 * Root element is `<aside class="tk-blank-layout__brand">`; the parent BlankLayout
 * controls flex sizing and visibility via scoped styles on this class.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

/** Brand feature icon key (mirrors the auth-login.html prototype icon set). */
type BrandFeatureIcon = 'activity' | 'clock' | 'shield' | 'bolt'

/** Brand feature item rendered in the brand panel. */
interface BrandFeature {
  /** Feature title */
  title: string
  /** Feature description */
  description: string
  /** Icon key; defaults by index when omitted */
  icon?: BrandFeatureIcon
}

interface AuthBrandPanelProps {
  /** Brand wordmark (e.g. "Tickraft"); brand names are not translated */
  brandTitle?: string
  /** Eyebrow label rendered above the headline */
  brandEyebrow?: string
  /** Main headline first part (rendered before the gradient accent) */
  brandHeadline?: string
  /** Accent part of the headline (rendered as gradient text on its own line) */
  brandHeadlineAccent?: string
  /** Tagline rendered below the headline */
  brandTagline?: string
  /** Edition badge text (e.g. "Community Edition") */
  brandEdition?: string
  /** Brand area feature list */
  brandFeatures?: BrandFeature[]
}

const props = defineProps<AuthBrandPanelProps>()

const { t, te, locale } = useI18n()

/** Current year used in the bottom copyright line. */
const year = new Date().getFullYear()

/**
 * i18n translation with fallback.
 *
 * When the i18n key is not yet defined in the locale pack, returns the fallback
 * text for the current language, avoiding raw key strings in the UI.
 */
function tf(key: string, zhFallback: string, enFallback: string): string {
  if (te(key)) return t(key)
  return locale.value === 'en-US' ? enFallback : zhFallback
}

/** Default icon cycle for features that do not declare an explicit icon. */
const DEFAULT_FEATURE_ICONS: BrandFeatureIcon[] = ['activity', 'clock', 'shield', 'bolt']

const eyebrow = computed(() =>
  props.brandEyebrow || tf('auth.login.brand.eyebrow', 'SRE · 运维自动化', 'SRE · Ops Automation'),
)

const headline = computed(() =>
  props.brandHeadline || tf('auth.login.brand.headline', '让每一次异常，', 'Every anomaly,'),
)

const headlineAccent = computed(() =>
  props.brandHeadlineAccent || tf('auth.login.brand.headlineAccent', '都第一时间被看见', 'seen the moment it happens'),
)

const tagline = computed(() =>
  props.brandTagline ||
    tf(
      'auth.login.brand.tagline',
      '开源一体化运维平台 · 调度·监测·告警·自愈，单进程开箱即用',
      'Open-source all-in-one ops platform — scheduling, monitoring, alerting, and self-healing in a single binary',
    ),
)

const edition = computed(() =>
  props.brandEdition || tf('auth.login.brand.edition', 'Community Edition', 'Community Edition'),
)

/** Feature list with icon resolution (i18n-aware defaults aligned with the prototype). */
const features = computed<Required<BrandFeature>[]>(() => {
  if (props.brandFeatures && props.brandFeatures.length) {
    return props.brandFeatures.map((feature, index) => ({
      title: feature.title,
      description: feature.description,
      icon: feature.icon ?? DEFAULT_FEATURE_ICONS[index % DEFAULT_FEATURE_ICONS.length],
    }))
  }
  return [
    {
      icon: 'activity',
      title: tf('auth.login.brand.feature1Title', '全链路状态监测', 'Full-link monitoring'),
      description: tf(
        'auth.login.brand.feature1Desc',
        '主动探测 + Webhook 被动接收，主机与服务异常秒级可见',
        'Active probing + passive Webhook listening, host & service anomalies visible in seconds',
      ),
    },
    {
      icon: 'clock',
      title: tf('auth.login.brand.feature2Title', '自动化任务调度', 'Automated task scheduling'),
      description: tf(
        'auth.login.brand.feature2Desc',
        'Cron/事件/间隔触发，多执行器协同，把重复运维交给机器',
        'Cron/event/interval triggers, multi-executor coordination, hand repetitive ops to the machine',
      ),
    },
    {
      icon: 'shield',
      title: tf('auth.login.brand.feature3Title', '告警自愈闭环', 'Alert-to-remediation loop'),
      description: tf(
        'auth.login.brand.feature3Desc',
        '规则引擎 + 多渠道通知 + 自愈脚本，从发现到处置全自动',
        'Rule engine + multi-channel alerts + self-healing scripts, fully automated from detection to fix',
      ),
    },
    {
      icon: 'bolt',
      title: tf('auth.login.brand.feature4Title', '极简单进程部署', 'Single-binary deployment'),
      description: tf(
        'auth.login.brand.feature4Desc',
        '一个二进制，零外部依赖，3 分钟从下载到上线',
        'One binary, zero external deps, from download to live in 3 minutes',
      ),
    },
  ]
})
</script>

<template>
  <aside class="tk-blank-layout__brand">
    <!-- Background layer 3: radar concentric rings (rotates slowly) -->
    <svg class="tk-blank-layout__brand-radar" viewBox="0 0 760 760" aria-hidden="true">
      <circle cx="380" cy="380" r="90" />
      <circle cx="380" cy="380" r="170" />
      <circle cx="380" cy="380" r="250" />
      <line x1="380" y1="40" x2="380" y2="720" />
      <line x1="40" y1="380" x2="720" y2="380" />
      <circle class="tk-blank-layout__brand-radar-ring" cx="380" cy="380" r="330" />
      <circle cx="380" cy="380" r="4" fill="#818cf8" stroke="none" />
      <circle cx="540" cy="250" r="5" fill="#22d3ee" stroke="none" opacity="0.85" />
      <circle cx="250" cy="500" r="5" fill="#a78bfa" stroke="none" opacity="0.75" />
      <circle cx="520" cy="540" r="4" fill="#34d399" stroke="none" opacity="0.7" />
      <circle cx="220" cy="260" r="3" fill="#fbbf24" stroke="none" opacity="0.7" />
    </svg>

    <!-- Brand top: logo + wordmark + edition badge -->
    <div class="tk-blank-layout__brand-top">
      <span class="tk-blank-layout__brand-logo">T</span>
      <span class="tk-blank-layout__brand-wordmark">{{ brandTitle ?? 'Tickraft' }}</span>
      <span class="tk-blank-layout__brand-edition">{{ edition }}</span>
    </div>

    <!-- Brand center: eyebrow + headline + tagline + features -->
    <div class="tk-blank-layout__brand-center">
      <span class="tk-blank-layout__brand-eyebrow">{{ eyebrow }}</span>
      <h1 class="tk-blank-layout__brand-headline">
        {{ headline }}<em class="tk-blank-layout__brand-headline-accent">{{ headlineAccent }}</em>
      </h1>
      <p class="tk-blank-layout__brand-tagline">
        {{ tagline }}
      </p>
      <ul
        v-if="features.length"
        class="tk-blank-layout__brand-features"
      >
        <li
          v-for="feature in features"
          :key="feature.title"
          class="tk-blank-layout__brand-feature"
        >
          <span class="tk-blank-layout__brand-feature-icon" aria-hidden="true">
            <svg v-if="feature.icon === 'activity'" viewBox="0 0 24 24" fill="none" stroke="currentColor"
              stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
            </svg>
            <svg v-else-if="feature.icon === 'clock'" viewBox="0 0 24 24" fill="none" stroke="currentColor"
              stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="12" cy="12" r="9" />
              <polyline points="12 7 12 12 15 14" />
            </svg>
            <svg v-else-if="feature.icon === 'shield'" viewBox="0 0 24 24" fill="none" stroke="currentColor"
              stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
            </svg>
            <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor"
              stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
            </svg>
          </span>
          <span class="tk-blank-layout__brand-feature-text">
            <span class="tk-blank-layout__brand-feature-title">{{ feature.title }}</span>
            <span class="tk-blank-layout__brand-feature-desc">{{ feature.description }}</span>
          </span>
        </li>
      </ul>
    </div>

    <!-- Brand foot: minimal copyright (no pseudo status indicator) -->
    <div class="tk-blank-layout__brand-foot">
      <span class="tk-blank-layout__brand-copyright">&copy; {{ year }} Tickraft</span>
    </div>
  </aside>
</template>

<style scoped lang="scss">
.tk-blank-layout__brand {
  position: relative;
  display: flex;
  flex-direction: column;

  // Fallback for browsers without clamp() support: use the minimum values
  padding: 36px 40px;
  // Fluid padding scales across viewports without abrupt breakpoints; vertical
  // (top/bottom) uses a smaller range than horizontal to keep content dense.
  padding: clamp(36px, 4vw, 48px) clamp(40px, 5vw, 64px);
  overflow: hidden;
  color: #e2e8f0;
  background-color: #080b14;
  background-image:
    radial-gradient(circle at 22% 24%, rgb(66 99 235 / 22%) 0%, transparent 46%),
    radial-gradient(circle at 78% 82%, rgb(245 158 11 / 6%) 0%, transparent 50%),
    radial-gradient(circle at 50% 50%, rgb(139 92 246 / 10%) 0%, transparent 60%),
    linear-gradient(180deg, #080b14 0%, #0c1018 100%);

  // Background layer 1: subtle grid overlay
  &::before {
    position: absolute;
    inset: 0;
    pointer-events: none;
    content: '';
    background-image:
      linear-gradient(rgb(255 255 255 / 2.5%) 1px, transparent 1px),
      linear-gradient(90deg, rgb(255 255 255 / 2.5%) 1px, transparent 1px);
    background-size: 48px 48px;
  }

  // Background layer 2: center glow pulse
  &::after {
    position: absolute;
    top: 50%;
    left: 50%;
    width: 360px;
    height: 360px;
    pointer-events: none;
    content: '';
    background: radial-gradient(circle, rgb(66 99 235 / 32%) 0%, transparent 70%);
    opacity: 0.55;
    filter: blur(10px);
    transform: translate(-50%, -50%) scale(1);
    animation: tk-brand-pulse 4.5s var(--tk-ease-in-out, cubic-bezier(0.4, 0, 0.2, 1)) infinite;
    will-change: transform, opacity;
  }
}

.tk-blank-layout__brand-radar {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 760px;
  height: 760px;
  pointer-events: none;
  opacity: 0.55;
  transform: translate(-50%, -50%);

  circle {
    fill: none;
    stroke: rgb(66 99 235 / 22%);
    stroke-width: 1;
  }

  line {
    stroke: rgb(255 255 255 / 5%);
    stroke-width: 1;
  }
}

.tk-blank-layout__brand-radar-ring {
  stroke: rgb(129 140 248 / 55%);
  stroke-width: 1.4;
  stroke-dasharray: 4 6;
  transform-origin: 380px 380px;
  transform-box: view-box;
  animation: tk-brand-rotate 60s linear infinite;
  will-change: transform;
}

.tk-blank-layout__brand-top {
  position: relative;
  z-index: 1;
  display: flex;
  gap: 12px;
  align-items: center;
}

.tk-blank-layout__brand-logo {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 38px;
  height: 38px;
  font-family: var(--tk-font-display, 'Sora', sans-serif);
  font-size: 19px;
  font-weight: 800;
  color: #fff;
  background: var(--tk-gradient-brand-accent, linear-gradient(135deg, #4263eb 0%, #8b5cf6 100%));
  border-radius: var(--tk-radius-md, 8px);
  box-shadow: 0 0 18px rgb(66 99 235 / 45%);
}

.tk-blank-layout__brand-wordmark {
  font-family: var(--tk-font-display, 'Sora', sans-serif);
  font-size: 20px;
  font-weight: 700;
  color: #fff;
  letter-spacing: -0.02em;
}

.tk-blank-layout__brand-edition {
  padding: 2px 10px;
  margin-left: 8px;
  font-family: var(--tk-font-mono, 'JetBrains Mono', monospace);
  font-size: 10px;
  color: #94a3b8;
  text-transform: uppercase;
  letter-spacing: 0.10em;
  border: 1px solid rgb(255 255 255 / 18%);
  border-radius: var(--tk-radius-round, 999px);
}

// Center block is top-anchored with a controlled top margin instead of relying
// on `justify-content: space-between` leftover distribution, so the eyebrow's
// upper gap scales predictably with viewport height (no large empty band on
// common laptop heights). `min(480px, 100%)` prevents horizontal overflow on
// narrow brand columns.
.tk-blank-layout__brand-center {
  position: relative;
  z-index: 1;
  // Fallback for browsers without min() support: 480px cap (equivalent when the
  // container is narrower than 480px, width then follows the parent)
  max-width: 480px;
  max-width: min(480px, 100%);
  // Fallback for browsers without clamp() support: use the minimum value
  margin-top: 56px;
  margin-top: clamp(56px, 9vh, 104px);
}

.tk-blank-layout__brand-eyebrow {
  display: inline-flex;
  gap: 8px;
  align-items: center;
  padding: 4px 12px;
  margin-bottom: 28px;
  font-family: var(--tk-font-mono, 'JetBrains Mono', monospace);
  font-size: 11px;
  color: #a5b4fc;
  text-transform: uppercase;
  letter-spacing: 0.10em;
  background: rgb(66 99 235 / 14%);
  border: 1px solid rgb(66 99 235 / 40%);
  border-radius: var(--tk-radius-round, 999px);

  &::before {
    width: 6px;
    height: 6px;
    content: '';
    background: #818cf8;
    border-radius: 50%;
    box-shadow: 0 0 8px #818cf8;
  }
}

.tk-blank-layout__brand-headline {
  margin: 0 0 18px;
  font-family: var(--tk-font-display, 'Sora', sans-serif);

  // Fallback for browsers without clamp() support: mid-range fixed size
  font-size: 38px;
  // Fluid font-size scales between 30px and 46px with viewport width, avoiding
  // the previous 46px→34px jump at 980px.
  font-size: clamp(30px, 3.4vw, 46px);
  font-weight: 800;
  line-height: 1.08;
  color: #fff;
  letter-spacing: -0.025em;
  text-wrap: balance;
}

.tk-blank-layout__brand-headline-accent {
  // Force the gradient accent onto its own line so it is never split mid-text
  // when the headline wraps.
  display: block;
  font-style: normal;
  color: transparent;
  background: linear-gradient(135deg, #7c95f5 0%, #fbbf24 100%);
  background-clip: text;
  -webkit-text-fill-color: transparent;
}

.tk-blank-layout__brand-tagline {
  margin: 0 0 36px;
  font-family: var(--tk-font-mono, 'JetBrains Mono', monospace);
  font-size: 13px;
  color: #94a3b8;
  letter-spacing: 0.02em;
}

.tk-blank-layout__brand-features {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 0;
  margin: 0;
  list-style: none;
}

.tk-blank-layout__brand-feature {
  display: flex;
  gap: 12px;
  align-items: flex-start;
}

.tk-blank-layout__brand-feature-icon {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  color: #a5b4fc;
  background: rgb(66 99 235 / 14%);
  border: 1px solid rgb(66 99 235 / 30%);
  border-radius: var(--tk-radius-md, 8px);

  svg { width: 18px; height: 18px; }
}

.tk-blank-layout__brand-feature-text {
  flex: 1;
  line-height: 1.5;
}

.tk-blank-layout__brand-feature-title {
  display: block;
  font-size: 14px;
  font-weight: 600;
  line-height: 1.3;
  color: #e2e8f0;
}

.tk-blank-layout__brand-feature-desc {
  display: block;
  margin-top: 2px;
  font-size: 12px;
  line-height: 1.55;
  color: #94a3b8;
}

// Foot is anchored to the bottom via `margin-top: auto`; holds only the minimal
// copyright line (no pseudo "all systems operational" status).
.tk-blank-layout__brand-foot {
  position: relative;
  z-index: 1;
  margin-top: auto;
  font-family: var(--tk-font-mono, 'JetBrains Mono', monospace);
  font-size: 11px;
  color: #64748b;
  letter-spacing: 0.04em;
}

.tk-blank-layout__brand-copyright {
  display: inline-block;
}

@keyframes tk-brand-pulse {
  0%, 100% { opacity: 0.55; transform: translate(-50%, -50%) scale(1); }
  50% { opacity: 0.92; transform: translate(-50%, -50%) scale(1.14); }
}

@keyframes tk-brand-rotate {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

// Narrow-viewport downscale for the fixed-size radar SVG only (padding and
// headline font-size are already fluid via clamp() above, so no abrupt jumps).
@media (max-width: 980px) {
  .tk-blank-layout__brand-radar { width: 560px; height: 560px; }
}
</style>
