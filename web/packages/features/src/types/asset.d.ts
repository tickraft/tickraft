// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// AssetStatus is a common status enum owned by @tickraft/core; re-exported here to keep business type aggregation complete.
export type { AssetStatus } from '@tickraft/core'
import type { AssetStatus, PageParams } from '@tickraft/core'

/**
 * Asset type enum (aligned with backend asset.AssetType).
 *
 * CE supports six asset types; the commercial edition extends with additional
 * sub-types via metadata.
 */
export type AssetType = 'task' | 'device' | 'host' | 'port' | 'website' | 'service'

/**
 * Asset model (aligned with backend pkg/asset.Asset).
 *
 * An Asset is the unified entity that can be scheduled (by Task) and/or
 * observed (by Telemetry). The lightweight CE asset management UI reads and
 * writes this model directly via /api/v1/assets.
 */
export interface Asset {
  /** Asset ID */
  id: number
  /** Tenant ID (always 0 in single-tenant CE) */
  tenantId: number
  /** Asset type */
  assetType: AssetType
  /** Tenant-unique identifier (e.g. IP, URL, host:port) */
  assetKey: string
  /** Human-readable asset name */
  name: string
  /** Asset status */
  status: AssetStatus
  /** Optional JSON-encoded extension data (see AssetMetadata) */
  metadata?: string
  /** Last active time */
  lastActiveAt: string
  /** Created time */
  createdAt: string
  /** Updated time */
  updatedAt: string
}

/**
 * AssetMetadata is the typed view over the JSON-encoded Asset.metadata field.
 *
 * CE recognizes a small set of advisory keys; the commercial edition extends
 * this with additional classification fields. Stored as JSON in the backend
 * `metadata` column; parsed/serialized by the frontend API layer.
 */
export interface AssetMetadata {
  /** Endpoint address (IP or hostname) */
  endpoint?: string
  /** Port number (optional) */
  port?: number
  /** Free-form labels for grouping / filtering */
  labels?: string[]
  /** Human-readable description */
  description?: string
}

/**
 * Asset list query parameters.
 */
export interface AssetListQuery extends PageParams {
  keyword?: string
  assetType?: AssetType
  status?: AssetStatus
}

/**
 * Asset form data (create / edit). The frontend form works with flat fields;
 * the API layer packs endpoint / port / labels / description into the metadata
 * JSON before sending to the backend.
 */
export interface AssetFormData {
  name: string
  assetType: AssetType
  assetKey: string
  endpoint?: string
  port?: number
  labels: string[]
  description?: string
}

/** All asset type keys (for select options / filtering). Declared in api/asset.ts. */
export declare const ASSET_TYPES: AssetType[]

/** All asset status keys. Declared in api/asset.ts. */
export declare const ASSET_STATUSES: AssetStatus[]
