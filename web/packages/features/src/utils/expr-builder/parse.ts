// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

/**
 * Expression → wizard reverse parsing (rule-engine-design §9.2).
 *
 * A restricted recursive-descent parser that accepts exactly the
 * subset the wizard can generate — flat condition rows joined by one
 * combinator, row-level NOT, variable-on-the-left comparisons against
 * literals. Anything else (nested parens, mixed && and ||, arithmetic,
 * builtins) returns null and the editor stays in expert mode.
 */

import type { ExprEnv } from './catalog'
import { findVariable, isMapKind, operatorsForKind } from './catalog'
import type { ConditionRow, WizardModel } from './model'

/** Word operators lex as identifiers and are reinterpreted here. */
const WORD_OPERATORS = new Set(['contains', 'startsWith', 'endsWith', 'matches', 'in'])

const SYMBOLIC_OPERATORS = new Set(['==', '!=', '>', '>=', '<', '<='])

interface Token {
  type: 'string' | 'number' | 'ident' | 'op'
  /** Raw source text (for numbers) or decoded value (for strings). */
  value: string
}

/** Tokenize the expression; null on characters outside the subset. */
function tokenize(src: string): Token[] | null {
  const tokens: Token[] = []
  let i = 0
  while (i < src.length) {
    const ch = src[i]
    if (/\s/.test(ch)) {
      i++
      continue
    }
    if (ch === '"') {
      let value = ''
      i++
      while (i < src.length && src[i] !== '"') {
        if (src[i] === '\\' && i + 1 < src.length) {
          const escaped = src[i + 1]
          value += escaped === 'n' ? '\n' : escaped === 't' ? '\t' : escaped
          i += 2
          continue
        }
        value += src[i]
        i++
      }
      if (i >= src.length) return null
      i++ // closing quote
      tokens.push({ type: 'string', value })
      continue
    }
    if (ch === '-' || (ch >= '0' && ch <= '9')) {
      const start = i
      if (ch === '-') i++
      if (!/[0-9]/.test(src[i] ?? '')) return null
      while (i < src.length && /[0-9]/.test(src[i])) i++
      if (src[i] === '.' && /[0-9]/.test(src[i + 1] ?? '')) {
        i++
        while (i < src.length && /[0-9]/.test(src[i])) i++
      }
      if (Number.isNaN(Number(src.slice(start, i)))) return null
      tokens.push({ type: 'number', value: src.slice(start, i) })
      continue
    }
    if (/[A-Za-z_]/.test(ch)) {
      let value = ''
      while (i < src.length && /[A-Za-z0-9_]/.test(src[i])) {
        value += src[i]
        i++
      }
      // Dotted identifier chains (`asset.tags`) lex as one token.
      while (src[i] === '.' && /[A-Za-z_]/.test(src[i + 1] ?? '')) {
        value += '.'
        i++
        while (i < src.length && /[A-Za-z0-9_]/.test(src[i])) {
          value += src[i]
          i++
        }
      }
      tokens.push({ type: 'ident', value })
      continue
    }
    const two = src.slice(i, i + 2)
    if (['==', '!=', '>=', '<=', '&&', '||'].includes(two)) {
      tokens.push({ type: 'op', value: two })
      i += 2
      continue
    }
    if (['>', '<', '!', '(', ')', '[', ']', ','].includes(ch)) {
      tokens.push({ type: 'op', value: ch })
      i++
      continue
    }
    return null
  }
  return tokens
}

/** Cursor over the token stream. */
class Parser {
  private pos = 0
  /** Combinator evidence: mixing both rejects the parse. */
  private sawAnd = false
  private sawOr = false

  constructor(
    private readonly env: ExprEnv,
    private readonly tokens: Token[],
  ) {}

  parse(): WizardModel | null {
    const rows: ConditionRow[] = []
    const first = this.parseTerm(rows)
    if (!first) return null
    while (this.peek()?.value === '||') {
      this.sawOr = true
      this.pos++
      if (!this.parseTerm(rows)) return null
    }
    if (this.pos !== this.tokens.length) return null
    if (this.sawAnd && this.sawOr) return null
    return { combinator: this.sawOr ? 'or' : 'and', rows }
  }

  private peek(): Token | undefined {
    return this.tokens[this.pos]
  }

  /** and-chain: factor ('&&' factor)* */
  private parseTerm(rows: ConditionRow[]): boolean {
    if (!this.parseFactor(rows)) return false
    while (this.peek()?.value === '&&') {
      this.sawAnd = true
      this.pos++
      if (!this.parseFactor(rows)) return false
    }
    return true
  }

  /** factor: '!' '(' comparison ')' | comparison — bare parens are not
   *  generatable and are rejected. */
  private parseFactor(rows: ConditionRow[]): boolean {
    if (this.peek()?.value === '!') {
      this.pos++
      if (this.peek()?.value !== '(') return false
      this.pos++
      const row = this.parseComparison()
      if (!row) return false
      if (this.peek()?.value !== ')') return false
      this.pos++
      rows.push({ ...row, negate: true })
      return true
    }
    const row = this.parseComparison()
    if (!row) return false
    rows.push(row)
    return true
  }

  /** comparison: varRef op literal, validated against the catalog. */
  private parseComparison(): ConditionRow | null {
    const variable = this.parseVarRef()
    if (!variable) return null

    const opToken = this.peek()
    if (!opToken) return null
    const op = opToken.type === 'ident' && WORD_OPERATORS.has(opToken.value)
      ? opToken.value
      : opToken.type === 'op' && SYMBOLIC_OPERATORS.has(opToken.value)
        ? opToken.value
        : null
    if (!op) return null
    this.pos++

    const def = findVariable(this.env, variable.path)
    if (!def) return null
    const kind = isMapKind(def.kind)
      ? def.kind === 'numberMap'
        ? 'number'
        : 'string'
      : def.kind
    if (!operatorsForKind(kind).includes(op)) return null

    const literal = this.parseLiteral(op === 'in')
    if (!literal) return null
    if (op === 'in') {
      if (!literal.list) return null
      return {
        variable: variable.path,
        key: variable.key,
        operator: op,
        value: literal.value,
        negate: false,
      }
    }
    if (kind === 'number') {
      if (literal.list || literal.isString) return null
      if (Number.isNaN(Number(literal.value))) return null
    } else if (!literal.isString || literal.list) {
      return null
    }
    return {
      variable: variable.path,
      key: variable.key,
      operator: op,
      value: literal.value,
      negate: false,
    }
  }

  /** varRef: IDENT ('[' STRING ']')? */
  private parseVarRef(): { path: string; key: string } | null {
    const head = this.peek()
    if (!head || head.type !== 'ident' || WORD_OPERATORS.has(head.value)) return null
    this.pos++
    const ref = { path: head.value, key: '' }
    if (this.peek()?.value === '[') {
      this.pos++
      const keyToken = this.peek()
      if (!keyToken || keyToken.type !== 'string') return null
      this.pos++
      if (this.peek()?.value !== ']') return null
      this.pos++
      ref.key = keyToken.value
    }
    const def = findVariable(this.env, ref.path)
    if (!def) return null
    if (isMapKind(def.kind) !== Boolean(ref.key)) return null
    return ref
  }

  /** literal: STRING | NUMBER | '[' STRING (',' STRING)* ']' */
  private parseLiteral(allowList: boolean): { value: string; isString: boolean; list: boolean } | null {
    const token = this.peek()
    if (token && token.type === 'string') {
      this.pos++
      return { value: token.value, isString: true, list: false }
    }
    if (token && token.type === 'number') {
      this.pos++
      return { value: token.value, isString: false, list: false }
    }
    if (allowList && token?.value === '[') {
      this.pos++
      const items: string[] = []
      for (;;) {
        const item = this.peek()
        if (!item || item.type !== 'string') return null
        this.pos++
        items.push(item.value)
        if (this.peek()?.value === ',') {
          this.pos++
          continue
        }
        break
      }
      if (this.peek()?.value !== ']') return null
      this.pos++
      return { value: items.join(', '), isString: true, list: true }
    }
    return null
  }
}

/**
 * Parse an expression back into the wizard model. Returns null for
 * anything outside the generatable subset (or on any lexical,
 * grammatical, or catalog mismatch). An empty string parses to null —
 * the editor handles emptiness separately.
 */
export function parseExpression(env: ExprEnv, source: string): WizardModel | null {
  const trimmed = source.trim()
  if (!trimmed) return null
  const tokens = tokenize(trimmed)
  if (!tokens || tokens.length === 0) return null
  return new Parser(env, tokens).parse()
}
