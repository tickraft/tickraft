// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * useFormGuard - re-exported from @tickraft/core.
 *
 * The canonical implementation lives in @tickraft/core so that all
 * frontends (including standalone ones without @tickraft/ui) can use it.
 * @tickraft/ui re-exports it for consumers that import from the ui package.
 */
export { useFormGuard } from '@tickraft/core'
export type { UseFormGuardOptions, UseFormGuardReturn } from '@tickraft/core'
