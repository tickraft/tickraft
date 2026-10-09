// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { describe, expect, it } from 'vitest'
import { localizeApiError } from './apiErrorMessages'

describe('localizeApiError quota guidance', () => {
  it('maps the exact "quota exceeded" message to guidance copy', () => {
    const out = localizeApiError(40900, 'quota exceeded')
    expect(out).toContain('已达开源版配额上限')
    expect(out).toContain('tickraft.io/editions')
  })

  it('maps every backend ceiling-rejection phrasing to the same guidance', () => {
    const messages = [
      'scheduled task quota exceeded: maximum 20 tasks',
      'prober quota exceeded: maximum 20 active probers',
      'remediation rule quota exceeded: maximum 5 rules',
      'custom field quota exceeded',
    ]
    for (const msg of messages) {
      expect(localizeApiError(40900, msg)).toContain('已达开源版配额上限')
    }
  })

  it('leaves non-quota conflicts on the generic code copy', () => {
    expect(localizeApiError(40900, 'asset key already exists')).toBe('资产密钥已存在')
    expect(localizeApiError(40900, 'some other conflict')).toBe('资源冲突或已存在')
  })
})
