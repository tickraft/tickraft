// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { describe, expect, it } from 'vitest'
import { camelizeKeys, snakeizeKeys, snakeizeTopLevel } from './naming'

describe('camelizeKeys', () => {
  it('deeply converts backend snake keys', () => {
    expect(camelizeKeys({ data: { items: [{ asset_key: 'k', created_at: 't' }] } })).toEqual({
      data: { items: [{ assetKey: 'k', createdAt: 't' }] },
    })
  })

  it('leaves user-authored header and metadata subtrees verbatim', () => {
    const input = {
      config: { headers: { 'X-IT-Source': 'v', Authorization: 'b' } },
      metadata: { 'X-Key': 'value', owner: 'ops' },
    }
    expect(camelizeKeys(input)).toEqual(input)
  })

  it('still converts sibling keys around a verbatim subtree', () => {
    expect(
      camelizeKeys({ config: { expect_status: 200, headers: { 'X-A': '1' } } }),
    ).toEqual({ config: { expectStatus: 200, headers: { 'X-A': '1' } } })
  })

  it('passes primitives, null and raw values through', () => {
    expect(camelizeKeys(null)).toBeNull()
    expect(camelizeKeys(7)).toBe(7)
    expect(camelizeKeys(new Date(0))).toBeInstanceOf(Date)
  })
})

describe('snakeizeKeys', () => {
  it('deeply converts keys (query params contract)', () => {
    expect(snakeizeKeys({ page: 1, assetType: 'host' })).toEqual({ page: 1, asset_type: 'host' })
  })
})

describe('snakeizeTopLevel', () => {
  it('converts only first-level body keys', () => {
    expect(
      snakeizeTopLevel({ executorType: 'local', config: { command: 'echo', someCamel: 1 } }),
    ).toEqual({ executor_type: 'local', config: { command: 'echo', someCamel: 1 } })
  })

  it('round-trips user header keys untouched', () => {
    const body = { config: { headers: { 'X-IT-Source': 'tickraft' } } }
    expect(snakeizeTopLevel(body)).toEqual(body)
  })

  it('converts each element of a top-level array body', () => {
    expect(snakeizeTopLevel([{ assetKey: 'a' }, { assetKey: 'b' }])).toEqual([
      { asset_key: 'a' },
      { asset_key: 'b' },
    ])
  })

  it('passes primitives and null through', () => {
    expect(snakeizeTopLevel(null)).toBeNull()
    expect(snakeizeTopLevel('text')).toBe('text')
  })
})
