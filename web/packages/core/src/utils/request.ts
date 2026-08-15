// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import axios from 'axios'
import type { AxiosInstance, AxiosRequestConfig, InternalAxiosRequestConfig, AxiosResponse } from 'axios'
import {
  getStorage,
  getSessionStorage,
  removeStorage,
  setStorage,
  setSessionStorage,
} from './storage'
import { camelizeKeys, snakeizeKeys } from './naming'

/** Token storage key */
const TOKEN_KEY = 'tk-token'
const REFRESH_TOKEN_KEY = 'tk-refresh-token'

/** Whether a token refresh is in progress */
let isRefreshing = false
/** Request queue waiting during token refresh */
let pendingRequests: Array<{
  resolve: (token: string) => void
  reject: (error: Error) => void
}> = []

/**
 * Create Axios instance
 */
const service: AxiosInstance = axios.create({
  baseURL: '/api/v1',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

/**
 * Generate request tracing ID
 */
function generateRequestId(): string {
  return `${Date.now()}-${Math.random().toString(36).substring(2, 9)}`
}

/**
 * Get stored token
 */
export function getToken(): string {
  return getStorage<string>(TOKEN_KEY) || ''
}

/**
 * Set token
 */
export function setToken(token: string): void {
  setStorage(TOKEN_KEY, token)
}

/**
 * Get refresh token. Stored in session storage so the long-lived
 * credential is cleared when the tab closes instead of persisting in
 * local storage where XSS could exfiltrate it.
 */
export function getRefreshToken(): string {
  return getSessionStorage<string>(REFRESH_TOKEN_KEY) || ''
}

/**
 * Set refresh token
 */
export function setRefreshToken(token: string): void {
  setSessionStorage(REFRESH_TOKEN_KEY, token)
}

/**
 * Clear auth credentials
 */
export function clearAuth(): void {
  removeStorage(TOKEN_KEY)
  removeStorage(REFRESH_TOKEN_KEY)
}

/**
 * Refresh token
 */
async function refreshToken(): Promise<string> {
  const refreshTokenValue = getRefreshToken()
  if (!refreshTokenValue) {
    throw new Error('No refresh token available')
  }

  // Send snake_case explicitly: this call bypasses the service instance
  // (to avoid the 401 interceptor loop), so the naming interceptor does
  // not apply here. The backend expects `refresh_token`.
  const { data } = await axios.post('/api/v1/auth/refresh', {
    refresh_token: refreshTokenValue,
  })

  if (data.code === 0) {
    const payload = camelizeKeys<{ accessToken: string; refreshToken: string }>(data.data)
    const newToken = payload.accessToken
    const newRefreshToken = payload.refreshToken
    setToken(newToken)
    setRefreshToken(newRefreshToken)
    return newToken
  }

  throw new Error('Token refresh failed')
}

/**
 * Request interceptor
 */
service.interceptors.request.use(
  (config: InternalAxiosRequestConfig) => {
    const token = getToken()
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }

    // Inject tracing header
    config.headers['X-Tickraft-Request-Id'] = generateRequestId()

    // Inject locale header
    const locale = getStorage<string>('tk-locale') || 'zh-Hans'
    config.headers['X-Tickraft-Locale'] = locale

    // Convert request body keys to snake_case for backend
    if (config.data && typeof config.data === 'object' && !(config.data instanceof FormData)) {
      config.data = snakeizeKeys(config.data)
    }

    // Convert query param keys to snake_case for backend
    if (config.params && typeof config.params === 'object') {
      config.params = snakeizeKeys(config.params)
    }

    return config
  },
  (error) => Promise.reject(error),
)

/**
 * Redirect to the login page, clearing auth state.
 * Uses a guard flag to prevent multiple concurrent redirects.
 */
let isRedirecting = false
function redirectToLogin(): void {
  if (isRedirecting) return
  isRedirecting = true
  clearAuth()
  removeStorage('tk-user-info')
  // Clear pending requests so they don't hang forever
  pendingRequests = []
  isRefreshing = false
  // Use href for a hard redirect to ensure a clean state reset
  window.location.href = '/login'
}

/**
 * Response interceptor
 */
service.interceptors.response.use(
  (response: AxiosResponse) => {
    const { data } = response

    // code=0 means success, return data directly (camelize keys)
    if (data.code === 0) {
      return camelizeKeys(data.data)
    }

    // Token expired, attempt refresh
    if (data.code === 40101) {
      if (!isRefreshing) {
        isRefreshing = true
        refreshToken()
          .then((newToken) => {
            isRefreshing = false
            // Retry queued requests with the new token
            const queue = pendingRequests
            pendingRequests = []
            queue.forEach((cb) => cb.resolve(newToken))
          })
          .catch((err) => {
            isRefreshing = false
            // Reject all queued requests so they don't hang forever
            const queue = pendingRequests
            pendingRequests = []
            queue.forEach((cb) => cb.reject(err instanceof Error ? err : new Error('Token refresh failed')))
            redirectToLogin()
          })
      }

      // Enqueue current request — it will be retried after refresh succeeds
      // or rejected if refresh fails.
      return new Promise((resolve, reject) => {
        pendingRequests.push({
          resolve: (newToken: string) => {
            if (response.config.headers) {
              response.config.headers.Authorization = `Bearer ${newToken}`
            }
            resolve(service(response.config))
          },
          reject: (error: Error) => {
            reject(error)
          },
        })
      })
    }

    // Unauthorized, redirect to login
    if (data.code === 40100) {
      redirectToLogin()
      return Promise.reject(new Error(data.message || 'Unauthorized'))
    }

    // Forbidden
    if (data.code === 40300) {
      return Promise.reject(new Error(data.message || 'Forbidden'))
    }

    // Other errors
    return Promise.reject(new Error(data.message || `Error ${data.code}`))
  },
  (error) => {
    // Handle HTTP-level errors (e.g. 401, 403, 500).
    // The backend returns error envelopes with HTTP status codes (not always
    // 200), so unauthorized requests arrive here rather than in the success
    // handler above.
    const status = error?.response?.status
    const body = error?.response?.data

    // 401 Unauthorized — token missing, invalid, or expired
    if (status === 401) {
      const code = body?.code
      // 40101 = token expired, attempt refresh first
      if (code === 40101) {
        if (!isRefreshing) {
          isRefreshing = true
          return new Promise((resolve, reject) => {
            refreshToken()
              .then((newToken) => {
                isRefreshing = false
                // Retry queued requests
                const queue = pendingRequests
                pendingRequests = []
                queue.forEach((cb) => cb.resolve(newToken))
                // Retry the original request
                if (error.config?.headers) {
                  error.config.headers.Authorization = `Bearer ${newToken}`
                }
                resolve(service(error.config))
              })
              .catch((err) => {
                isRefreshing = false
                // Reject all queued requests
                const queue = pendingRequests
                pendingRequests = []
                queue.forEach((cb) =>
                  cb.reject(err instanceof Error ? err : new Error('Token refresh failed')),
                )
                redirectToLogin()
                reject(new Error('Session expired'))
              })
          })
        }
        // A refresh is already in progress — enqueue this request
        return new Promise((resolve, reject) => {
          pendingRequests.push({
            resolve: (newToken: string) => {
              if (error.config?.headers) {
                error.config.headers.Authorization = `Bearer ${newToken}`
              }
              resolve(service(error.config))
            },
            reject: (err: Error) => {
              reject(err)
            },
          })
        })
      }
      // Any other 401 (invalid token, missing header) — go to login
      redirectToLogin()
      return Promise.reject(new Error(body?.message || 'Unauthorized'))
    }

    // 403 Forbidden — reject without redirecting
    if (status === 403) {
      return Promise.reject(new Error(body?.message || 'Forbidden'))
    }

    // Extract message from response body if available
    const message = body?.message || error?.message || 'Request failed'
    return Promise.reject(new Error(message))
  },
)

/**
 * Generic request method
 */
export function request<T = unknown>(config: AxiosRequestConfig): Promise<T> {
  return service(config) as Promise<T>
}

export default service
