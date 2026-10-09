// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { getLocale } from '../i18n'

/**
 * Backend error messages are English. The UI runs in zh-Hans by default, so
 * raw messages surface as English strings inside a Chinese interface. This
 * module maps known errors to zh-Hans copy: exact message matches first
 * (most specific), then common message patterns, then the envelope's error
 * code as a generic fallback. Unknown messages under a known code get the
 * code-level copy; anything else passes through unchanged. Long term the
 * backend should produce localized messages per request language; this is
 * the interim client-side mapping.
 */

/** zh-Hans copy per application error code (the stable API contract). */
const codeMessages: Record<number, string> = {
  40000: '请求参数无效',
  40001: '缺少必填参数',
  40002: '参数格式错误',
  40003: '原密码不正确',
  40100: '未登录或凭证无效',
  40101: '登录已过期，请重新登录',
  40102: '缺少资产密钥',
  40300: '没有执行此操作的权限',
  40301: '资产密钥无效',
  40400: '资源不存在',
  40500: '请求方法不被支持',
  40900: '资源冲突或已存在',
  42900: '请求过于频繁，请稍后再试',
  50000: '服务器内部错误',
}

/**
 * Guidance shown for count-ceiling rejections (409). Mentions both honest
 * ways forward: the open-source build is recompilable with adjusted quotas,
 * or a professional edition raises the ceilings (open-core strategy §6.2-1).
 */
const QUOTA_GUIDANCE
  = '已达开源版配额上限。可重新编译源码调整配额，或了解专业版扩容：tickraft.io/editions'

/** Matches backend ceiling rejections like "scheduled task quota exceeded: maximum 20 tasks". */
const quotaPattern = /quota exceeded/

/** zh-Hans copy for exact backend messages that appear frequently in the UI. */
const messageOverrides: Record<string, string> = {
  'auth: unauthorized': '用户名或密码错误',
  'auth: invalid argument': '请求参数无效',
  'auth: forbidden': '没有执行此操作的权限',
  'auth: conflict': '资源冲突或已存在',
  unauthorized: '未登录或凭证无效',
  forbidden: '没有执行此操作的权限',
  'not found': '资源不存在',
  conflict: '资源冲突或已存在',
  'invalid argument': '请求参数无效',
  'internal error': '服务器内部错误',
  'username and password are required': '请输入用户名和密码',
  'old_password and new_password are required': '请填写原密码和新密码',
  'old password mismatch': '原密码不正确',
  'refresh_token is required': '缺少刷新令牌',
  'admin role required': '需要管理员权限',
  'invalid request body': '请求体格式错误',
  'invalid request parameters': '请求参数无效',
  'invalid id parameter': 'ID 参数无效',
  'invalid page': '分页参数无效',
  'invalid size': '每页数量参数无效',
  'invalid cursor': '分页游标无效',
  'name is required': '请填写名称',
  'asset not found': '资产不存在',
  'asset key already exists': '资产密钥已存在',
  'template not found': '模板不存在',
  'template name already exists': '模板名称已存在',
  'quota exceeded': QUOTA_GUIDANCE,
  'asset type not allowed in this edition': '该资产类型在当前版本不可用',
  'missing authorization header': '缺少认证信息',
  'missing token': '缺少令牌',
  'invalid token': '令牌无效',
  'rate limit exceeded': '请求过于频繁，请稍后再试',
}

/** Matches backend validation messages like "type is required". */
const requiredPattern = /^(.+?) is required$/

/** Matches backend validation messages like "invalid asset_type". */
const invalidPattern = /^invalid (.+)$/

/** Formats a snake_case backend field name for display. */
function formatField(field: string): string {
  return field.replace(/_/g, ' ')
}

/**
 * Returns zh-Hans copy for a backend error, or the original message when no
 * mapping applies. Only called for zh-Hans users; English users keep the
 * backend's English messages.
 */
export function localizeApiError(code: number | undefined, message: string): string {
  const exact = messageOverrides[message]
  if (exact) return exact

  if (quotaPattern.test(message)) return QUOTA_GUIDANCE

  const required = message.match(requiredPattern)
  if (required) return `请填写${formatField(required[1])}`

  const invalid = message.match(invalidPattern)
  if (invalid) return `参数无效：${formatField(invalid[1])}`

  if (code !== undefined && codeMessages[code]) return codeMessages[code]

  return message
}

/**
 * Reports whether the current UI locale is Chinese, in which case backend
 * errors should be localized.
 */
export function shouldLocalizeErrors(): boolean {
  const locale = getLocale()
  return locale.startsWith('zh')
}
