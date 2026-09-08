// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

/**
 * Notification channel page composables
 *
 * Extracts data loading, search, pagination, CRUD, and test business logic
 * out of .vue files so List.vue / Delivery.vue focus on UI rendering,
 * satisfying the single-file <=500 lines constraint.
 */
import { computed, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import {
  getChannels,
  deleteChannel,
  testChannel,
  toggleChannel,
  getDeliveries,
  retryDelivery,
  type ChannelConfig,
  type DeliveryRecord,
  type TestResult,
  type ChannelType,
} from '../../../api/channel'

/** Channel type icon mapping */
const CHANNEL_ICONS: Record<string, string> = {
  webhook: '🔗', email: '📧',
  sms: '💬', dingtalk: '📌', feishu: '🐦',
  wecom: '💚', slack: '#', discord: '🎮', telegram: '✈', teams: '👥',
}

/** Channel type display order (card view grouping order) */
const TYPE_ORDER: ChannelType[] = [
  'webhook', 'email', 'dingtalk', 'feishu', 'wecom',
  'slack', 'discord', 'telegram', 'teams', 'sms',
]

/**
 * Notification channel list data management: load, toggle, delete, test
 */
export function useChannels() {
  const { t } = useI18n()
  const channels = ref<ChannelConfig[]>([])
  const loading = ref(false)
  const testingId = ref<number | null>(null)
  const lastTestResult = ref<TestResult | null>(null)
  const lastTestChannel = ref<string>('')

  async function loadChannels(): Promise<void> {
    loading.value = true
    try {
      const res = await getChannels()
      channels.value = res.items ?? []
    } catch (e) {
      ElMessage.error((e as Error).message)
    } finally {
      loading.value = false
    }
  }

  async function handleToggle(ch: ChannelConfig): Promise<boolean> {
    const action = ch.enabled ? t('prism.channel.list.toggleDisable') : t('prism.channel.list.toggleEnable')
    try {
      await ElMessageBox.confirm(
        t('prism.channel.list.toggleConfirmContent', { action, name: ch.name }),
        t('prism.channel.list.toggleConfirmTitle'),
        { confirmButtonText: t('common.app.confirm'), cancelButtonText: t('common.app.cancel'), type: 'warning' },
      )
    } catch {
      return false
    }
    try {
      await toggleChannel(ch.id, !ch.enabled)
      ElMessage.success(t('prism.channel.list.resultSuccess', { action }))
      await loadChannels()
      return true
    } catch (e) {
      ElMessage.error((e as Error).message)
      return false
    }
  }

  async function handleDelete(ch: ChannelConfig): Promise<boolean> {
    try {
      await ElMessageBox.confirm(
        t('prism.channel.list.deleteConfirmWarn', { name: ch.name }),
        t('common.app.confirm'),
        { confirmButtonText: t('common.app.confirm'), cancelButtonText: t('common.app.cancel'), type: 'warning' },
      )
    } catch {
      return false
    }
    try {
      await deleteChannel(ch.id)
      ElMessage.success(t('prism.channel.list.resultDeleted'))
      await loadChannels()
      return true
    } catch (e) {
      ElMessage.error((e as Error).message)
      return false
    }
  }

  async function handleTest(ch: ChannelConfig): Promise<void> {
    testingId.value = ch.id
    lastTestResult.value = null
    lastTestChannel.value = ch.name
    try {
      const result = await testChannel({ id: ch.id })
      lastTestResult.value = result
      if (result.status === 'success') {
        ElMessage.success(t('prism.channel.list.testSuccess'))
      } else {
        const detail = result.error ? `: ${result.error}` : ''
        ElMessage.error(`${t('prism.channel.list.testFailed')}${detail}`)
      }
      await loadChannels()
    } catch (e) {
      ElMessage.error((e as Error).message)
    } finally {
      testingId.value = null
    }
  }

  /** Group channels by type (for card view) */
  const groupedChannels = computed(() => {
    const groups = new Map<ChannelType, ChannelConfig[]>()
    for (const ch of channels.value) {
      const list = groups.get(ch.type) ?? []
      list.push(ch)
      groups.set(ch.type, list)
    }
    return TYPE_ORDER
      .filter((type) => groups.has(type))
      .map((type) => ({ type, items: groups.get(type)!, icon: CHANNEL_ICONS[type] ?? '📬' }))
  })

  function getIcon(type: string): string {
    return CHANNEL_ICONS[type] ?? '📬'
  }

  return {
    channels, loading, testingId, lastTestResult, lastTestChannel,
    groupedChannels, getIcon,
    loadChannels, handleToggle, handleDelete, handleTest,
  }
}

/**
 * Delivery record data management: load, search, paginate, retry
 */
export function useDeliveries() {
  const { t } = useI18n()
  const records = ref<DeliveryRecord[]>([])
  const total = ref(0)
  const loading = ref(false)
  const page = ref(1)
  const size = ref(20)
  const retryingId = ref<number | null>(null)
  const filters = reactive({
    channelId: 0,
    status: '',
    alertTitle: '',
    startTime: '',
    endTime: '',
  })

  async function loadDeliveries(channelId: number): Promise<void> {
    loading.value = true
    try {
      const result = await getDeliveries(channelId, {
        page: page.value,
        size: size.value,
        status: filters.status || undefined,
        alertTitle: filters.alertTitle || undefined,
        startTime: filters.startTime || undefined,
        endTime: filters.endTime || undefined,
      })
      records.value = result.items
      total.value = result.total
    } catch (e) {
      ElMessage.error((e as Error).message)
    } finally {
      loading.value = false
    }
  }

  function handleSearch(): void {
    page.value = 1
    void loadDeliveries(filters.channelId)
  }

  function handleReset(): void {
    Object.assign(filters, { channelId: 0, status: '', alertTitle: '', startTime: '', endTime: '' })
    page.value = 1
    void loadDeliveries(0)
  }

  function handlePageChange(payload: { page: number; size: number }): void {
    size.value = payload.size
    page.value = payload.page
    void loadDeliveries(filters.channelId)
  }

  async function handleRetry(record: DeliveryRecord): Promise<boolean> {
    if (record.status !== 'failed') {
      ElMessage.warning(t('prism.channel.delivery.retryOnlyFailed'))
      return false
    }
    try {
      await ElMessageBox.confirm(
        t('prism.channel.delivery.retryConfirmContent'),
        t('prism.channel.delivery.retryConfirmTitle'),
        { confirmButtonText: t('prism.channel.delivery.retryConfirmText'), cancelButtonText: t('common.app.cancel'), type: 'warning' },
      )
    } catch {
      return false
    }
    retryingId.value = record.id
    try {
      // The retry is synchronous: the response is the updated record whose
      // status reflects the outcome of the replay attempt.
      const updated = await retryDelivery(record.id)
      if (updated.status === 'success') {
        ElMessage.success(t('prism.channel.delivery.retrySucceeded'))
      } else {
        const detail = updated.error ? `: ${updated.error}` : ''
        ElMessage.error(`${t('prism.channel.delivery.retryStillFailed')}${detail}`)
      }
      await loadDeliveries(filters.channelId)
      return true
    } catch (e) {
      ElMessage.error((e as Error).message)
      return false
    } finally {
      retryingId.value = null
    }
  }

  /** Export the currently loaded delivery records as a CSV file. */
  function handleExport(): void {
    const header = [
      'id', 'channel_name', 'channel_type', 'alert_title', 'event_id',
      'status', 'response_code', 'duration_ms', 'error', 'sent_at',
    ]
    const esc = (v: unknown): string => {
      const s = String(v ?? '')
      return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
    }
    const lines = [header.join(',')]
    for (const r of records.value) {
      lines.push([
        r.id, r.channelName, r.channelType, r.alertTitle, r.eventId,
        r.status, r.responseCode, r.durationMs, r.error, r.sentAt,
      ].map(esc).join(','))
    }
    const blob = new Blob(['\ufeff' + lines.join('\n')], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `deliveries-${new Date().toISOString().slice(0, 10)}.csv`
    anchor.click()
    URL.revokeObjectURL(url)
  }

  return {
    records, total, loading, page, size, retryingId, filters,
    loadDeliveries, handleSearch, handleReset, handlePageChange, handleRetry, handleExport,
  }
}

/* ============================================================
 * Delivery detail helpers (used by Delivery.vue drawer display)
 * ============================================================ */

/** Get channel type icon */
export function getChannelIcon(type: string): string {
  return CHANNEL_ICONS[type] ?? '📬'
}
