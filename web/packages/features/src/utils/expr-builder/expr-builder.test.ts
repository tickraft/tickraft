// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

import { describe, expect, it } from 'vitest'
import { emptyModel, generateExpression, parseExpression } from './index'
import type { ConditionRow, WizardModel } from './index'

/** Row builder for brevity in generation tests. */
function row(overrides: Partial<ConditionRow>): ConditionRow {
  return { variable: 'severity', key: '', operator: '==', value: 'critical', negate: false, ...overrides }
}

describe('generateExpression', () => {
  it('renders scalar string comparisons with quoted values', () => {
    const model: WizardModel = { combinator: 'and', rows: [row({})] }
    expect(generateExpression('alert', model)).toEqual({
      ok: true,
      expression: 'severity == "critical"',
    })
  })

  it('renders map-key access and numeric values', () => {
    const model: WizardModel = {
      combinator: 'and',
      rows: [
        row({ variable: 'metrics', key: 'cpu', operator: '>', value: '90' }),
        row({ variable: 'asset.tags', key: 'env', operator: '==', value: 'prod' }),
      ],
    }
    expect(generateExpression('alert', model).expression).toBe(
      'metrics["cpu"] > 90 && asset.tags["env"] == "prod"',
    )
  })

  it('joins rows with the selected combinator and applies row-level NOT', () => {
    const model: WizardModel = {
      combinator: 'or',
      rows: [
        row({ variable: 'metric.value', operator: '>', value: '95' }),
        row({ variable: 'content', negate: true, operator: 'contains', value: 'oom' }),
      ],
    }
    expect(generateExpression('remediation', model).expression).toBe(
      'metric.value > 95 || !(content contains "oom")',
    )
  })

  it('renders the in operator as a string list', () => {
    const model: WizardModel = {
      combinator: 'and',
      rows: [row({ operator: 'in', value: 'critical, warning' })],
    }
    expect(generateExpression('alert', model).expression).toBe(
      'severity in ["critical", "warning"]',
    )
  })

  it('escapes embedded quotes and backslashes in string values', () => {
    const model: WizardModel = {
      combinator: 'and',
      rows: [row({ operator: 'contains', value: 'say "hi" \\ ok' })],
    }
    expect(generateExpression('alert', model).expression).toBe(
      'severity contains "say \\"hi\\" \\\\ ok"',
    )
  })

  it('yields the empty expression for zero rows', () => {
    expect(generateExpression('remediation', emptyModel())).toEqual({ ok: true, expression: '' })
  })

  it('rejects incomplete rows, wrong-env variables, and type mismatches', () => {
    expect(generateExpression('alert', { combinator: 'and', rows: [row({ value: '' })] }).ok).toBe(false)
    // metric.value only exists in the remediation env
    expect(
      generateExpression('alert', { combinator: 'and', rows: [row({ variable: 'metric.value', operator: '>', value: '1' })] }).ok,
    ).toBe(false)
    // string variable with a numeric-only operator
    expect(generateExpression('alert', { combinator: 'and', rows: [row({ operator: '>', value: '1' })] }).ok).toBe(false)
    // number variable with a non-numeric value
    expect(
      generateExpression('execution', {
        combinator: 'and',
        rows: [row({ variable: 'code', operator: '==', value: 'abc' })],
      }).ok,
    ).toBe(false)
    // map variable without a key
    expect(
      generateExpression('alert', { combinator: 'and', rows: [row({ variable: 'metrics', operator: '>', value: '1' })] }).ok,
    ).toBe(false)
  })
})

describe('parseExpression', () => {
  it('round-trips a generated model', () => {
    const model: WizardModel = {
      combinator: 'or',
      rows: [
        row({ variable: 'metrics', key: 'cpu', operator: '>=', value: '90' }),
        row({ variable: 'content', operator: 'contains', value: 'out of memory', negate: true }),
        row({ operator: 'in', value: 'critical, warning' }),
      ],
    }
    const generated = generateExpression('alert', model).expression!
    expect(parseExpression('alert', generated)).toEqual(model)
  })

  it('parses the documented placeholders', () => {
    expect(parseExpression('alert', 'metrics["cpu"] > 90')).toEqual({
      combinator: 'and',
      rows: [row({ variable: 'metrics', key: 'cpu', operator: '>', value: '90' })],
    })
    expect(parseExpression('remediation', 'metric.name == "cpu" && metric.value > 95')).toEqual({
      combinator: 'and',
      rows: [
        row({ variable: 'metric.name', operator: '==', value: 'cpu' }),
        row({ variable: 'metric.value', operator: '>', value: '95' }),
      ],
    })
    expect(parseExpression('execution', 'code == 200 && duration < 500')).toEqual({
      combinator: 'and',
      rows: [
        row({ variable: 'code', operator: '==', value: '200' }),
        row({ variable: 'duration', operator: '<', value: '500' }),
      ],
    })
  })

  it('rejects expressions beyond the wizard subset', () => {
    // mixed combinators
    expect(parseExpression('alert', 'severity == "x" && type == "y" || keyword == "z"')).toBeNull()
    // bare parenthesized groups
    expect(parseExpression('alert', '(severity == "x" || type == "y") && keyword == "z"')).toBeNull()
    // negation of a group
    expect(parseExpression('alert', '!(severity == "x" && type == "y")')).toBeNull()
    // literal on the left
    expect(parseExpression('alert', '"critical" == severity')).toBeNull()
    // unknown variable and unknown domain field
    expect(parseExpression('alert', 'naem == "x"')).toBeNull()
    expect(parseExpression('alert', 'asset.naem == "x"')).toBeNull()
    // arithmetic and builtins
    expect(parseExpression('alert', 'metrics["cpu"] + 10 > 90')).toBeNull()
    expect(parseExpression('alert', 'len(content) > 5')).toBeNull()
    // trailing garbage
    expect(parseExpression('alert', 'severity == "x" extra')).toBeNull()
    // number variable against a string literal
    expect(parseExpression('execution', 'code == "200"')).toBeNull()
  })

  it('returns null for the empty expression', () => {
    expect(parseExpression('alert', '')).toBeNull()
    expect(parseExpression('alert', '   ')).toBeNull()
  })
})
