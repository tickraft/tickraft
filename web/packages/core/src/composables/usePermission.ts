// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { computed } from 'vue'
import type { ComputedRef } from 'vue'
import { useUserStore } from '../stores/user'

/**
 * Feature flag constants.
 *
 * Aligned with `03_tickraft_frontend.md` chapter 7 version capability boundary:
 * - `basic_*` are open-source built-in capabilities; the open-source repo
 *   returns true by default.
 * - Others are paid-tier capabilities; the open-source repo returns false by
 *   default, and extension injects a provider to perform real checks.
 *
 * Permission fallback rule: frontend feature flags are display-layer only;
 * all permission and authorization checks are enforced by the backend.
 */
export const FeatureConstants = {
  // ── Open-source · Auth module ──
  BASIC_AUTH: 'basic_auth',
  // ── Open-source · Dashboard ──
  BASIC_DASHBOARD: 'basic_dashboard',
  // ── Open-source · Task engine ──
  BASIC_TASK: 'basic_task',
  BASIC_HTTP_EXECUTOR: 'basic_http_executor',
  BASIC_TCP_EXECUTOR: 'basic_tcp_executor',
  BASIC_LOCAL_EXECUTOR: 'basic_local_executor',
  BASIC_EXECUTION_LOG: 'basic_execution_log',
  // ── Open-source · Telemetry engine ──
  BASIC_TELEMETRY: 'basic_telemetry',
  BASIC_WEBHOOK_LISTENER: 'basic_webhook_listener',
  BASIC_ICMP_PROBER: 'basic_icmp_prober',
  BASIC_TCP_PROBER: 'basic_tcp_prober',
  BASIC_HTTP_PROBER: 'basic_http_prober',
  // ── Open-source · Alert center ──
  BASIC_ALERT: 'basic_alert',
  BASIC_ALERT_RULE: 'basic_alert_rule',
  // ── Open-source · System settings ──
  BASIC_SYSTEM: 'basic_system',
  BASIC_API_KEY: 'basic_api_key',
  // ── Personal tier ──
  SSH_EXECUTOR: 'ssh_executor',
  MYSQL_EXECUTOR: 'mysql_executor',
  REDIS_EXECUTOR: 'redis_executor',
  DNS_PROBER: 'dns_prober',
  REDIS_PROBER: 'redis_prober',
  SSL_CERT_PROBER: 'ssl_cert_prober',
  // ── Team tier ──
  USER_MANAGEMENT: 'user_management',
  ROLE_PERMISSION: 'role_permission',
  AUDIT_LOG: 'audit_log',
  SYSLOG_LISTENER: 'syslog_listener',
  SNMP_LISTENER: 'snmp_listener',
  MQTT_LISTENER: 'mqtt_listener',
  METRIC_DASHBOARD: 'metric_dashboard',
  MULTI_NOTIFICATION: 'multi_notification',
  // ── Enterprise tier ──
  LOG_SEARCH: 'log_search',
  DISTRIBUTED_CLUSTER: 'distributed_cluster',
  SSO: 'sso',
  CUSTOM_ROLE: 'custom_role',
  DATA_PERMISSION: 'data_permission',
} as const

/** Feature flag identifier type */
export type FeatureKey = keyof typeof FeatureConstants

/** Paid-tier feature flag provider (function form, simplified injection) */
export type FeatureProvider = (feature: string) => boolean

/**
 * Paid-tier permission provider interface (object form, supports fetching
 * the list of enabled features).
 *
 * Extension calls {@link installPermissionProvider} at app startup to inject
 * this interface; its implementation returns the real feature flag state
 * based on license / authorization info.
 */
export interface PermissionProvider {
  /** Check whether a feature flag is enabled */
  hasFeature: (feature: string) => boolean
  /** Get the list of currently enabled feature flags */
  getEnabledFeatures: () => string[]
}

/** Open-source default-enabled feature flags (all `basic_` prefixed) */
const OPEN_SOURCE_FEATURES: ReadonlySet<string> = new Set<string>(
  Object.values(FeatureConstants).filter((key) => key.startsWith('basic_')),
)

/** Injected object-form permission provider (module-level singleton, app-wide) */
let permissionProvider: PermissionProvider | null = null

/** Injected function-form feature flag provider (module-level singleton) */
let featureProvider: FeatureProvider | null = null

/**
 * Register a paid-tier feature flag provider (function form).
 *
 * Called by extension at startup to inject paid-tier permission logic. Once
 * registered, {@link usePermission} delegates to this provider first; when
 * not registered, falls back to the open-source default behavior.
 *
 * Note: frontend feature flags are display-layer only; final authorization
 * is enforced by the backend.
 * @param provider - feature flag predicate
 */
export function registerFeatureProvider(provider: FeatureProvider): void {
  featureProvider = provider
}

/**
 * Inject a paid-tier permission provider (object form, supports fetching the
 * enabled list).
 *
 * Takes precedence over {@link registerFeatureProvider}; once injected, the
 * open-source default behavior is overridden.
 * @param provider - permission provider implementation
 */
export function installPermissionProvider(provider: PermissionProvider): void {
  permissionProvider = provider
}

/**
 * Clear injected permission providers (test only; resets to open-source
 * default behavior).
 */
export function resetPermissionProvider(): void {
  permissionProvider = null
  featureProvider = null
}

/** Return type of usePermission */
export interface UsePermissionReturn {
  /**
   * Check whether the user has the specified feature flag(s) (multiple
   * arguments supported; returns true if any is enabled).
   *
   * Returned as a ComputedRef. Usage:
   * - `hasFeature.value('basic_task')`
   * - `hasFeature.value('ssh_executor', 'mysql_executor')`
   */
  hasFeature: ComputedRef<(...features: string[]) => boolean>
  /** Check whether the user has any of the given feature flags */
  hasAnyFeature: (...features: string[]) => boolean
  /** Check whether the user has all of the given feature flags */
  hasAllFeatures: (...features: string[]) => boolean
  /** Currently enabled feature flag identifiers */
  features: ComputedRef<string[]>
  /** Whether the user has the admin role */
  isAdmin: ComputedRef<boolean>
  /**
   * Check whether the current role may delete the given resource type
   * ('task' | 'device' | 'alert' | '*'). Mirrors the backend RBAC policy.
   */
  canDelete: ComputedRef<(resource: string) => boolean>
  /** Inject a paid-tier permission provider */
  installPermissionProvider: typeof installPermissionProvider
}

/**
 * Permission and feature flag composable.
 *
 * Single responsibility: provides feature flag query primitives only.
 * Default behavior: open-source flags (`basic_` prefix) return true, paid-tier
 * flags return false; extension can inject full permission logic via
 * {@link installPermissionProvider} to override the default.
 *
 * Frontend is display-layer only; all permission and authorization checks
 * are enforced by the backend.
 * @returns permission predicate methods and feature flag list
 */
export function usePermission(): UsePermissionReturn {
  const userStore = useUserStore()

  /**
   * Resolve whether a single feature flag is enabled.
   *
   * Priority: object-form provider > function-form provider > open-source
   * default > userStore.features.
   */
  function resolveFeature(feature: string): boolean {
    if (permissionProvider) {
      return permissionProvider.hasFeature(feature)
    }
    if (featureProvider) {
      return featureProvider(feature)
    }
    if (OPEN_SOURCE_FEATURES.has(feature)) {
      return true
    }
    return userStore.features[feature] === true
  }

  /**
   * Check whether the user has the specified feature flag(s) (multiple
   * arguments supported; returns true if any is enabled).
   *
   * Returned as a ComputedRef so it reacts to userStore.features changes.
   */
  const hasFeature = computed<(...features: string[]) => boolean>(() => {
    return (...features: string[]): boolean => {
      if (features.length === 0) {
        return false
      }
      return features.some((feature) => resolveFeature(feature))
    }
  })

  /**
   * Check whether the user has any of the given feature flags.
   * @param features - feature flag identifiers
   * @returns true if any is satisfied
   */
  function hasAnyFeature(...features: string[]): boolean {
    return features.some((feature) => resolveFeature(feature))
  }

  /**
   * Check whether the user has all of the given feature flags.
   * @param features - feature flag identifiers
   * @returns true only when all are satisfied
   */
  function hasAllFeatures(...features: string[]): boolean {
    return features.length > 0 && features.every((feature) => resolveFeature(feature))
  }

  /** Currently enabled feature flag identifiers (based on the resolve logic, including open-source defaults) */
  const features: ComputedRef<string[]> = computed(() => {
    if (permissionProvider) {
      return permissionProvider.getEnabledFeatures()
    }
    const known: string[] = Object.values(FeatureConstants).filter((feature) =>
      resolveFeature(feature),
    )
    const extraKeys = Object.keys(userStore.features).filter(
      (key) => !known.includes(key) && userStore.features[key] === true,
    )
    return [...known, ...extraKeys]
  })

  /** Whether the user has the admin role */
  const isAdmin: ComputedRef<boolean> = computed(() => userStore.role === 'admin')

  /**
   * Check whether the current role may delete the given resource type.
   *
   * Mirrors the backend RBAC policy (admin deletes everything; developer
   * deletes only tasks; viewer never deletes). Display-layer only — the
   * backend enforces the final decision.
   * @param resource - resource type: 'task' | 'device' | 'alert' | '*'
   */
  const canDelete: ComputedRef<(resource: string) => boolean> = computed(() => {
    return (resource: string): boolean => {
      if (userStore.role === 'admin') return true
      if (userStore.role === 'developer') return resource === 'task'
      return false
    }
  })

  return {
    hasFeature,
    hasAnyFeature,
    hasAllFeatures,
    features,
    isAdmin,
    canDelete,
    installPermissionProvider,
  }
}
