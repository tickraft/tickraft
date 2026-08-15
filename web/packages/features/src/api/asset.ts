// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { request } from '@tickraft/core'
import type { PageData } from '@tickraft/core'
import type {
  Asset,
  AssetFormData,
  AssetListQuery,
  AssetMetadata,
  AssetStatus,
  AssetType,
} from '../types/asset'

/** All asset type keys (for select options / filtering). */
export const ASSET_TYPES: AssetType[] = [
  'host',
  'service',
  'website',
  'device',
  'port',
  'task',
]

/** All asset status keys. */
export const ASSET_STATUSES: AssetStatus[] = ['normal', 'abnormal', 'offline', 'unknown']

/**
 * Parse the JSON-encoded metadata field into a typed object.
 * Returns an empty object when metadata is absent or invalid.
 */
export function parseMetadata(metadata?: string): AssetMetadata {
  if (!metadata) return {}
  try {
    return JSON.parse(metadata) as AssetMetadata
  } catch {
    return {}
  }
}

/**
 * Serialize a typed metadata object into a JSON string.
 * Returns undefined when the object is empty so the backend can omit the column.
 */
export function stringifyMetadata(meta: AssetMetadata): string | undefined {
  const hasValue = Object.values(meta).some(
    (v) => v !== undefined && v !== null && (Array.isArray(v) ? v.length > 0 : true),
  )
  if (!hasValue) return undefined
  return JSON.stringify(meta)
}

/**
 * Convert a flat form payload into the backend Asset shape, packing
 * endpoint / port / labels / description into the metadata JSON field.
 */
function formToPayload(form: AssetFormData): Partial<Asset> {
  const meta: AssetMetadata = {
    endpoint: form.endpoint || undefined,
    port: form.port || undefined,
    labels: form.labels.length ? form.labels : undefined,
    description: form.description || undefined,
  }
  return {
    name: form.name,
    assetType: form.assetType,
    assetKey: form.assetKey,
    metadata: stringifyMetadata(meta),
  }
}

/**
 * Get asset list (paginated).
 *
 * Server-side pagination (page/pageSize, snake-cased to page_size by the
 * request interceptor) and keyword/assetType/status filtering are applied
 * by the backend.
 */
export function getAssets(params: AssetListQuery): Promise<PageData<Asset>> {
  return request<PageData<Asset>>({
    url: '/assets',
    method: 'get',
    params,
  })
}

/** Get a single asset by ID. */
export function getAsset(id: number): Promise<Asset> {
  return request<Asset>({
    url: `/assets/${id}`,
    method: 'get',
  })
}

/** Create a new asset from form data. */
export function createAsset(form: AssetFormData): Promise<Asset> {
  return request<Asset>({
    url: '/assets',
    method: 'post',
    data: formToPayload(form),
  })
}

/** Update an existing asset from form data. */
export function updateAsset(id: number, form: AssetFormData): Promise<Asset> {
  return request<Asset>({
    url: `/assets/${id}`,
    method: 'put',
    data: formToPayload(form),
  })
}

/** Delete an asset by ID. */
export function deleteAsset(id: number): Promise<void> {
  return request<void>({
    url: `/assets/${id}`,
    method: 'delete',
  })
}

/**
 * Update an asset's status.
 *
 * Aligned with backend PUT /assets/:id/status which validates the status
 * and delegates to store.UpdateStatus. Returns void on success.
 */
export function updateAssetStatus(id: number, status: AssetStatus): Promise<void> {
  return request<void>({
    url: `/assets/${id}/status`,
    method: 'put',
    data: { status },
  })
}

/** Probe result (aligned with backend probeResult). */
export interface ProbeResult {
  assetId: number
  status: AssetStatus
}

/**
 * Probe an asset to determine its current status.
 *
 * Aligned with backend POST /assets/:id/probe which loads the asset and
 * returns its current status as a probe result.
 */
export function probeAsset(id: number): Promise<ProbeResult> {
  return request<ProbeResult>({
    url: `/assets/${id}/probe`,
    method: 'post',
  })
}
