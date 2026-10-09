// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * Unified backend response format (aligned with pkg/api/internal/response.go)
 *
 * NOTE: The axios response interceptor unwraps the envelope and returns only
 * the inner `data` payload (with keys camelize to camelCase). `ApiResponse`
 * is used for typing the raw envelope in interceptor logic only.
 */
export interface ApiResponse<T = unknown> {
  /** Status code, 0=success, non-zero=error code */
  code: number
  /** Message description */
  message: string
  /** Business data */
  data: T
}

/**
 * Paginated response format.
 *
 * Backend sends `{ items, total, page, size }`; the interceptor camelizes
 * keys (size is already camelCase) so frontend receives the same shape.
 */
export interface PageData<T = unknown> {
  /** Data list */
  items: T[]
  /** Total record count */
  total: number
  /** Current page number */
  page: number
  /** Page size */
  size: number
}

/**
 * Pagination request params.
 *
 * Keys are sent as-is by the request interceptor (snake_case conversion
 * leaves single-word keys unchanged).
 */
export interface PageParams {
  /** Page number, default 1 */
  page?: number
  /** Page size, default 20 */
  size?: number
}

/**
 * Generic error codes (4 digits, prefix matches HTTP status)
 */
export declare namespace ErrorCode {
  const SUCCESS = 0
  const BAD_REQUEST = 40000
  const MISSING_PARAM = 40001
  const INVALID_FORMAT = 40002
  const UNAUTHORIZED = 40100
  const TOKEN_EXPIRED = 40101
  const FORBIDDEN = 40300
  const NOT_FOUND = 40400
  const METHOD_NOT_ALLOWED = 40500
  const TOO_MANY_REQUESTS = 42900
  const INTERNAL_ERROR = 50000
}

/**
 * Business error code segments (5 digits, first 2 digits are module number)
 * 10xxx = Task
 * 11xxx = Device (resource)
 * 12xxx = Alert
 * 20xxx = Tenant
 * 21xxx = Billing
 */

/**
 * Login request params
 */
export interface LoginParams {
  username: string
  password: string
}

/**
 * Login response data.
 *
 * Backend JSON tags are snake_case (`access_token`, `refresh_token`, etc.);
 * the axios response interceptor automatically camelizes keys so frontend
 * code receives camelCase field names.
 */
export interface LoginData {
  accessToken: string
  refreshToken: string
  /** Token expiry timestamp (RFC 3339); omitted by the backend when not applicable */
  expiresAt?: string
  /** When true, the user must change their password before continuing */
  mustChangePassword?: boolean
  /** When true, MFA verification is required before the session is established */
  mfaRequired?: boolean
  /** Short-lived ticket for MFA verification (present only when mfaRequired is true) */
  mfaTicket?: string
}
