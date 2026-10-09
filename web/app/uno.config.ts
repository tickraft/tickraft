// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { defineConfig, presetWind3, presetAttributify } from 'unocss'

export default defineConfig({
  presets: [presetWind3(), presetAttributify()],
  shortcuts: {
    'tk-flex-center': 'flex items-center justify-center',
    'tk-flex-between': 'flex items-center justify-between',
  },
})
