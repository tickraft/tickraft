// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { MockMethod } from './types'

/** Base time (ISO format), aligned with storyboard mock-data.js */
const baseTime = '2026-06-30T14:00:00+08:00'

/** Format ISO time (simplified, aligned with storyboard static time strings) */
function ts(hoursAgo: number): string {
  const d = new Date(baseTime)
  d.setHours(d.getHours() - hoursAgo)
  const pad = (n: number): string => (n < 10 ? `0${n}` : `${n}`)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

/**
 * Asset dataset (16 items, covering 6 asset types and 4 statuses, aligned with
 * storyboard mock-data.js). Uses the backend pkg/asset.Asset model with
 * JSON-encoded metadata holding endpoint / port / labels / description.
 */
export const mockAssets = [
  { id: 1, tenant_id: 0, asset_type: 'host', asset_key: '10.0.1.11', name: 'prod-web-01', status: 'normal', metadata: '{"endpoint":"10.0.1.11","port":22,"labels":["prod","web","us-east-1"],"description":"Primary production web node"}', last_active_at: ts(0), created_at: '2026-06-01 10:00:00', updated_at: '2026-06-15 09:30:00' },
  { id: 2, tenant_id: 0, asset_type: 'host', asset_key: '10.0.1.12', name: 'prod-web-02', status: 'normal', metadata: '{"endpoint":"10.0.1.12","port":22,"labels":["prod","web","us-east-1"],"description":"Secondary production web node"}', last_active_at: ts(0), created_at: '2026-06-01 10:00:00', updated_at: '2026-06-22 11:45:00' },
  { id: 3, tenant_id: 0, asset_type: 'host', asset_key: '10.0.1.13', name: 'prod-api-03', status: 'abnormal', metadata: '{"endpoint":"10.0.1.13","port":22,"labels":["prod","api"],"description":"API server with high load"}', last_active_at: ts(0), created_at: '2026-06-01 10:00:00', updated_at: '2026-06-28 11:00:00' },
  { id: 4, tenant_id: 0, asset_type: 'service', asset_key: 'pay-callback-api', name: 'Payment Callback API', status: 'normal', metadata: '{"endpoint":"10.0.4.41","port":8080,"labels":["prod","api","payment"],"description":"Payment callback service"}', last_active_at: ts(0), created_at: '2026-06-02 11:00:00', updated_at: '2026-06-24 16:00:00' },
  { id: 5, tenant_id: 0, asset_type: 'host', asset_key: '10.0.2.21', name: 'prod-db-02', status: 'normal', metadata: '{"endpoint":"10.0.2.21","port":3306,"labels":["prod","db","mysql"],"description":"Primary MySQL instance"}', last_active_at: ts(0), created_at: '2026-06-02 11:00:00', updated_at: '2026-06-29 10:00:00' },
  { id: 6, tenant_id: 0, asset_type: 'host', asset_key: '10.0.2.22', name: 'prod-db-03', status: 'normal', metadata: '{"endpoint":"10.0.2.22","port":5432,"labels":["prod","db","postgres","replica"],"description":"Postgres read replica"}', last_active_at: ts(0), created_at: '2026-06-04 10:00:00', updated_at: '2026-06-21 14:30:00' },
  { id: 7, tenant_id: 0, asset_type: 'service', asset_key: 'redis-cache', name: 'prod-cache-01', status: 'abnormal', metadata: '{"endpoint":"10.0.3.31","port":6379,"labels":["prod","cache","redis"],"description":"Redis cache cluster node"}', last_active_at: ts(0), created_at: '2026-06-05 14:00:00', updated_at: '2026-06-29 18:30:00' },
  { id: 8, tenant_id: 0, asset_type: 'service', asset_key: 'kafka-broker', name: 'prod-kafka-01', status: 'normal', metadata: '{"endpoint":"10.0.4.41","port":9092,"labels":["prod","kafka","mq"],"description":"Kafka message broker"}', last_active_at: ts(0), created_at: '2026-06-08 10:00:00', updated_at: '2026-06-25 11:00:00' },
  { id: 9, tenant_id: 0, asset_type: 'service', asset_key: 'elasticsearch', name: 'prod-es-01', status: 'offline', metadata: '{"endpoint":"10.0.5.51","port":9200,"labels":["prod","es","search"],"description":"Elasticsearch cluster node (down)"}', last_active_at: ts(29), created_at: '2026-06-10 09:00:00', updated_at: '2026-06-29 10:00:00' },
  { id: 10, tenant_id: 0, asset_type: 'website', asset_key: 'https://cdn.tickraft.io', name: 'cdn-edge-01', status: 'normal', metadata: '{"endpoint":"https://cdn.tickraft.io","labels":["prod","cdn","edge"],"description":"CDN edge endpoint"}', last_active_at: ts(0), created_at: '2026-06-03 09:00:00', updated_at: '2026-06-20 16:30:00' },
  { id: 11, tenant_id: 0, asset_type: 'website', asset_key: 'https://www.tickraft.io', name: 'www.tickraft.io', status: 'normal', metadata: '{"endpoint":"https://www.tickraft.io","labels":["prod","marketing"],"description":"Public marketing site"}', last_active_at: ts(0), created_at: '2026-06-01 10:00:00', updated_at: '2026-06-28 16:00:00' },
  { id: 12, tenant_id: 0, asset_type: 'device', asset_key: '10.0.0.1', name: 'Intranet Gateway', status: 'normal', metadata: '{"endpoint":"10.0.0.1","port":161,"labels":["edge","snmp"],"description":"Edge router (SNMP-managed)"}', last_active_at: ts(0), created_at: '2026-06-06 11:00:00', updated_at: '2026-06-23 09:00:00' },
  { id: 13, tenant_id: 0, asset_type: 'service', asset_key: 'config-center', name: 'Config Center', status: 'unknown', metadata: '{"endpoint":"10.0.6.61","port":2379,"labels":["prod","etcd"],"description":"etcd config center"}', last_active_at: ts(23), created_at: '2026-05-29 14:00:00', updated_at: '2026-06-27 15:30:00' },
  { id: 14, tenant_id: 0, asset_type: 'host', asset_key: '10.0.7.71', name: 'Monitoring Host', status: 'normal', metadata: '{"endpoint":"10.0.7.71","port":22,"labels":["monitor"],"description":"Monitoring server"}', last_active_at: ts(0), created_at: '2026-06-12 14:00:00', updated_at: '2026-06-29 15:00:00' },
  { id: 15, tenant_id: 0, asset_type: 'device', asset_key: '10.0.9.91', name: 'Backup Storage', status: 'normal', metadata: '{"endpoint":"10.0.9.91","port":2049,"labels":["backup","nfs"],"description":"NFS backup storage"}', last_active_at: ts(1), created_at: '2026-05-25 09:00:00', updated_at: '2026-06-27 15:00:00' },
  { id: 16, tenant_id: 0, asset_type: 'host', asset_key: '10.0.1.14', name: 'prod-web-03', status: 'offline', metadata: '{"endpoint":"10.0.1.14","port":22,"labels":["prod","web"],"description":"Decommissioned web node"}', last_active_at: ts(16), created_at: '2026-06-15 10:00:00', updated_at: '2026-06-29 22:30:00' },
]

/** Mutable in-memory copy so create / update / delete are reflected during a dev session. */
let store = [...mockAssets]

function extractId(url: string): number {
  const match = url.match(/\/assets\/(\d+)/)
  return match ? Number(match[1]) : 0
}

/** Valid asset status values (aligned with ASSET_STATUSES in ../api/asset.ts). */
const ASSET_STATUSES: readonly string[] = ['normal', 'abnormal', 'offline', 'unknown']

export default [
  // List assets with pagination + filtering
  {
    url: '/api/v1/assets',
    method: 'get',
    response: ({ query }: { query: { page?: string; size?: string; asset_type?: string; status?: string; keyword?: string } }) => {
      const page = Number(query?.page) || 1
      const size = Number(query?.page_size) || 20
      let filtered = [...store]
      if (query?.asset_type) {
        filtered = filtered.filter((r) => r.asset_type === query.asset_type)
      }
      if (query?.status) {
        filtered = filtered.filter((r) => r.status === query.status)
      }
      if (query?.keyword) {
        const kw = query.keyword.toLowerCase()
        filtered = filtered.filter((r) => r.name.toLowerCase().includes(kw) || r.asset_key.toLowerCase().includes(kw))
      }
      const total = filtered.length
      const start = (page - 1) * size
      const items = filtered.slice(start, start + size)
      return {
        code: 0,
        message: 'success',
        data: { items, total, page, page_size: size },
      }
    },
  },
  // Get a single asset by ID
  {
    url: '/api/v1/assets/:id',
    method: 'get',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const asset = store.find((r) => r.id === id)
      if (!asset) {
        return { code: 404, message: 'asset not found', data: null }
      }
      return { code: 0, message: 'success', data: asset }
    },
  },
  // Create a new asset
  {
    url: '/api/v1/assets',
    method: 'post',
    response: ({ body }: { body: Record<string, unknown> }) => {
      const name = String(body?.name ?? '')
      const assetKey = String(body?.asset_key ?? '')
      const assetType = String(body?.asset_type ?? '')
      if (!name || !assetKey || !assetType) {
        return { code: 400, message: 'name, asset_key and asset_type are required', data: null }
      }
      if (store.some((r) => r.asset_key === assetKey)) {
        return { code: 409, message: 'asset key already exists', data: null }
      }
      const now = ts(0)
      const id = Math.max(0, ...store.map((r) => r.id)) + 1
      const asset = {
        id,
        tenant_id: 0,
        asset_type: assetType,
        asset_key: assetKey,
        name,
        status: String(body?.status ?? 'unknown'),
        metadata: body?.metadata ? String(body.metadata) : undefined,
        last_active_at: now,
        created_at: now,
        updated_at: now,
      }
      store = [asset, ...store]
      return { code: 0, message: 'success', data: asset }
    },
  },
  // Update an existing asset
  {
    url: '/api/v1/assets/:id',
    method: 'put',
    response: ({ url, body }: { url: string; body: Record<string, unknown> }) => {
      const id = extractId(url)
      const idx = store.findIndex((r) => r.id === id)
      if (idx === -1) {
        return { code: 404, message: 'asset not found', data: null }
      }
      const existing = store[idx]
      const updated = {
        ...existing,
        name: body?.name !== undefined ? String(body.name) : existing.name,
        asset_type: body?.asset_type !== undefined ? String(body.asset_type) : existing.asset_type,
        asset_key: body?.asset_key !== undefined ? String(body.asset_key) : existing.asset_key,
        status: body?.status !== undefined ? String(body.status) : existing.status,
        metadata: body?.metadata !== undefined ? (body.metadata ? String(body.metadata) : undefined) : existing.metadata,
        updated_at: ts(0),
      }
      store[idx] = updated
      return { code: 0, message: 'success', data: updated }
    },
  },
  // Update an asset's status (validated against the known status set)
  {
    url: '/api/v1/assets/:id/status',
    method: 'put',
    response: ({ url, body }: { url: string; body: Record<string, unknown> }) => {
      const id = extractId(url)
      const idx = store.findIndex((r) => r.id === id)
      if (idx === -1) {
        return { code: 404, message: 'asset not found', data: null }
      }
      const status = String(body?.status ?? '')
      if (!ASSET_STATUSES.includes(status)) {
        return { code: 400, message: `invalid status, must be one of: ${ASSET_STATUSES.join(', ')}`, data: null }
      }
      store[idx] = { ...store[idx], status, updated_at: ts(0) }
      return { code: 0, message: 'success', data: null }
    },
  },
  // Probe an asset and return its current status as a probe result
  {
    url: '/api/v1/assets/:id/probe',
    method: 'post',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const asset = store.find((r) => r.id === id)
      if (!asset) {
        return { code: 404, message: 'asset not found', data: null }
      }
      return {
        code: 0,
        message: 'success',
        data: { asset_id: asset.id, status: asset.status },
      }
    },
  },
  // Delete an asset
  {
    url: '/api/v1/assets/:id',
    method: 'delete',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const idx = store.findIndex((r) => r.id === id)
      if (idx === -1) {
        return { code: 404, message: 'asset not found', data: null }
      }
      store.splice(idx, 1)
      return { code: 0, message: 'success', data: null }
    },
  },
] as MockMethod[]
