// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * LogoMark - tickraft brand logo mark.
 *
 * A rounded square with a white pulse / heartbeat line and a small signal
 * dot, conveying the "signal observatory" monitoring theme. Renders as an
 * inline SVG so it scales crisply at any size and inherits the brand
 * gradient token, staying consistent across light/dark themes.
 */
interface Props {
  /** Square edge length in pixels. */
  size?: number | string
}

withDefaults(defineProps<Props>(), {
  size: 32,
})
</script>

<template>
  <svg
    class="tk-logo-mark"
    :width="size"
    :height="size"
    viewBox="0 0 32 32"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    role="img"
    aria-label="tickraft"
  >
    <!-- Rounded square background with brand gradient -->
    <defs>
      <linearGradient
        id="tk-logo-grad"
        x1="0"
        y1="0"
        x2="32"
        y2="32"
        gradientUnits="userSpaceOnUse"
      >
        <stop
          offset="0"
          stop-color="var(--tk-primary-color)"
        />
        <stop
          offset="1"
          stop-color="var(--tk-primary-color-dark-2, var(--tk-primary-color))"
        />
      </linearGradient>
    </defs>
    <rect
      x="1"
      y="1"
      width="30"
      height="30"
      rx="8"
      fill="url(#tk-logo-grad)"
    />

    <!-- Pulse / heartbeat line -->
    <path
      d="M5 16.5 H11.5 L13.5 11 L16.5 21 L19 14.5 L20.8 16.5 H27"
      stroke="#fff"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
      fill="none"
      opacity="0.96"
    />

    <!-- Signal dot accent (top-right) -->
    <circle
      cx="24"
      cy="9"
      r="2.2"
      fill="#fff"
      opacity="0.9"
    />
  </svg>
</template>

<style scoped>
.tk-logo-mark {
  display: block;
  flex-shrink: 0;
}
</style>
