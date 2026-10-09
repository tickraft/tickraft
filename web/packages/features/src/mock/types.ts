// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * Mock route definition type
 * Compatible with vite-plugin-mock's MockMethod
 */
export interface MockMethod {
  url: string
  method: string
  response: (options: {
    url: string
    body: Record<string, unknown>
    query: Record<string, string>
    headers: Record<string, string>
  }) => unknown
}
