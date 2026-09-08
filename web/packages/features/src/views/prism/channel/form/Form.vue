// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Notification channel form page
 *
 * - Channel type card picker (kernel built-in types: webhook/email plus
 *   the IM adapters dingtalk/feishu/wecom/slack/discord/telegram/teams)
 * - Extension types (e.g. "sms") are hidden unless passed via the
 *   extraTypes prop, letting deployments gate registry-injected types
 * - Dynamic configuration form; fields mirror each backend Config struct
 * - Stored config JSON uses snake_case keys; the API layer snakeizes the
 *   camelCase keys emitted here on the wire
 * - Sensitive fields come back masked ("****xxxx") on edit: leaving the
 *   mask untouched keeps the stored secret (server-side merge)
 * - Test send section (shows response_code / latency)
 * - Type cannot be changed in edit mode
 */
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { usePermission } from '@tickraft/core'
import type { ChannelType, TestResult } from '../../../../api/channel'
import { getChannel, createChannel, updateChannel, testChannel } from '../../../../api/channel'

/**
 * Extra channel types offered beyond the kernel built-ins. Deployments
 * pass the subset their license/registry actually provides (e.g. an
 * extension passes ["sms"] for its registry-injected type).
 */
const props = withDefaults(defineProps<{ extraTypes?: ChannelType[] }>(), { extraTypes: () => [] })

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const { hasFeature } = usePermission()

/**
 * L1 inbound-transport sections (dingtalk stream-mode credentials, wecom
 * callback credentials). The sections are capability-gated: deployments
 * without the private_alert_l1 feature never show them, and the stored
 * keys stay untouched when hidden.
 */
const l1InteractEnabled = computed(() => hasFeature.value('private_alert_l1'))

/**
 * L3 intranet-localization fields (private-edition IM OpenAPI base URLs,
 * custom SMS gateway). Capability-gated like the L1 sections: deployments
 * without the private_alert_l3 feature never show the fields, and the
 * stored keys stay untouched when hidden.
 */
const l3Enabled = computed(() => hasFeature.value('private_alert_l3'))

const channelId = computed<number | null>(() => {
  const id = route.params.id
  return id ? Number(id) : null
})
const isEdit = computed(() => channelId.value !== null)

const formRef = ref<FormInstance>()
const loading = ref(false)
const submitting = ref(false)
const testing = ref(false)
const testResult = ref<TestResult | null>(null)

interface HeaderRow {
  key: string
  value: string
}

interface ChannelFormData {
  name: string
  type: ChannelType
  enabled: boolean
  // Common
  frontendBaseUrl: string
  // Network options (feishu/discord/telegram only)
  proxyUrl: string
  region: string
  // Webhook (generic HTTP endpoint)
  url: string
  timeout: string
  headerRows: HeaderRow[]
  // Email (SMTP)
  host: string
  port: number
  smtpUsername: string
  smtpPassword: string
  fromAddr: string
  toAddrs: string
  tlsMode: string
  authType: string
  htmlMode: boolean
  // Webhook-based types
  webhookUrl: string
  secret: string
  messageType: string
  keyword: string
  channel: string
  username: string
  avatarUrl: string
  // WeCom
  mode: string
  robotWebhookUrl: string
  corpId: string
  agentId: number
  toUser: string
  callbackToken: string
  encodingAesKey: string
  // Telegram
  botToken: string
  chatId: string
  apiBase: string
  // Feishu app / WeCom app / DingTalk stream: private-edition OpenAPI base
  // URL (base_url on the wire)
  baseUrl: string
  // Feishu app mode / DingTalk stream mode application credentials
  appId: string
  appSecret: string
  appKey: string
  // SMS
  provider: string
  apiKey: string
  apiSecret: string
  signName: string
  templateCode: string
  smsRegion: string
  recipients: string
  // SMS custom gateway
  endpoint: string
  method: string
  bodyTemplate: string
}

const formData = reactive<ChannelFormData>({
  name: '', type: 'webhook', enabled: true,
  frontendBaseUrl: '',
  proxyUrl: '', region: '',
  url: '', timeout: '10s', headerRows: [],
  host: '', port: 25, smtpUsername: '', smtpPassword: '',
  fromAddr: '', toAddrs: '', tlsMode: 'none', authType: 'plain', htmlMode: false,
  webhookUrl: '', secret: '', messageType: '', keyword: '',
  channel: '', username: '', avatarUrl: '',
  mode: 'robot', robotWebhookUrl: '', corpId: '', agentId: 0, toUser: '',
  callbackToken: '', encodingAesKey: '',
  botToken: '', chatId: '', apiBase: '',
  baseUrl: '',
  appId: '', appSecret: '', appKey: '',
  provider: 'aliyun', apiKey: '', apiSecret: '', signName: '', templateCode: '', smsRegion: '',
  recipients: '',
  endpoint: '', method: 'POST', bodyTemplate: '',
})

const ALL_TYPE_OPTIONS: Array<{ value: ChannelType; icon: string }> = [
  { value: 'webhook', icon: '🔗' },
  { value: 'email', icon: '📧' },
  { value: 'dingtalk', icon: '📌' },
  { value: 'feishu', icon: '🐦' },
  { value: 'wecom', icon: '💚' },
  { value: 'slack', icon: '#' },
  { value: 'discord', icon: '🎮' },
  { value: 'telegram', icon: '✈' },
  { value: 'teams', icon: '👥' },
  { value: 'sms', icon: '💬' },
]

/** Types registered by extensions rather than the kernel built-ins */
const EXTENSION_TYPES: Set<ChannelType> = new Set(['sms'])

const typeOptions = computed(() =>
  ALL_TYPE_OPTIONS.filter((opt) => !EXTENSION_TYPES.has(opt.value) || props.extraTypes.includes(opt.value)),
)

/** Types whose Config has a plain webhook_url field */
const webhookTypes: Set<ChannelType> = new Set(['dingtalk', 'feishu', 'slack', 'discord', 'teams'])
/** Types whose Config has proxy_url + region (network policy) fields */
const proxyRegionTypes: Set<ChannelType> = new Set(['feishu', 'discord', 'telegram'])
/** Types that carry the frontend_base_url link-back field */
const linkBackTypes: Set<ChannelType> = new Set([
  'dingtalk', 'feishu', 'wecom', 'slack', 'discord', 'telegram', 'teams', 'sms',
])
/** Message type options per type: feishu text/interactive, dingtalk/wecom text/markdown */
const messageTypeOptions: Partial<Record<ChannelType, Array<{ label: string; value: string }>>> = {
  feishu: [
    { label: 'prism.channel.form.messageTypeText', value: 'text' },
    { label: 'prism.channel.form.messageTypeInteractive', value: 'interactive' },
  ],
  dingtalk: [
    { label: 'prism.channel.form.messageTypeText', value: 'text' },
    { label: 'prism.channel.form.messageTypeMarkdown', value: 'markdown' },
  ],
  wecom: [
    { label: 'prism.channel.form.messageTypeText', value: 'text' },
    { label: 'prism.channel.form.messageTypeMarkdown', value: 'markdown' },
  ],
}
/** SMS provider → whether template_code / region apply (twilio and the custom gateway use neither) */
const smsUsesTemplate = computed(() => formData.provider !== 'twilio' && formData.provider !== 'custom')

/**
 * Body-template example shown as the custom-gateway placeholder. Kept out
 * of i18n on purpose: the literal ${...} placeholders would collide with
 * vue-i18n's interpolation syntax inside message texts.
 */
const BODY_TEMPLATE_PLACEHOLDER = '{"to":"${phone}","text":"${content}"}'

/** A masked secret placeholder ("****xxxx") returned on edit reads */
function isMasked(value: string): boolean {
  return value.startsWith('****')
}

function isHttpUrl(value: string): boolean {
  if (!value || isMasked(value)) return true
  try {
    const u = new URL(value)
    return u.protocol === 'http:' || u.protocol === 'https:'
  } catch { return false }
}

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

function isEmailList(value: string): boolean {
  return parseEmails(value).every((addr) => EMAIL_RE.test(addr))
}

function requiredRule(key: string): Array<Record<string, unknown>> {
  return [{ required: true, message: t(key), trigger: 'blur' }]
}

const formRules = computed<FormRules>(() => {
  const rules: FormRules = {
    name: requiredRule('prism.channel.form.nameRequired'),
  }
  if (formData.type === 'webhook') {
    rules.url = [
      ...requiredRule('prism.channel.form.webhookUrlRequired'),
      { validator: (_r, v, cb) => isHttpUrl(v) ? cb() : cb(new Error('Invalid URL')), trigger: 'blur' },
    ]
  }
  if (formData.type === 'email') {
    rules.host = requiredRule('prism.channel.form.hostRequired')
    rules.port = requiredRule('prism.channel.form.emailPort')
    rules.fromAddr = [
      ...requiredRule('prism.channel.form.fromRequired'),
      { validator: (_r, v, cb) => EMAIL_RE.test(v) ? cb() : cb(new Error(t('prism.channel.form.invalidEmail'))), trigger: 'blur' },
    ]
    rules.toAddrs = [
      ...requiredRule('prism.channel.form.toRequired'),
      { validator: (_r, v, cb) => isEmailList(v) ? cb() : cb(new Error(t('prism.channel.form.invalidEmail'))), trigger: 'blur' },
    ]
  }
  // feishu app mode delivers through the application API and carries no
  // webhook_url; the required rule applies to the webhook modes only.
  const needsWebhookUrl =
    webhookTypes.has(formData.type) && !(formData.type === 'feishu' && formData.mode === 'app')
  if (needsWebhookUrl) {
    rules.webhookUrl = [
      ...requiredRule('prism.channel.form.webhookUrlRequired'),
      { validator: (_r, v, cb) => isHttpUrl(v) ? cb() : cb(new Error('Invalid URL')), trigger: 'blur' },
    ]
  }
  if (formData.type === 'feishu' && formData.mode === 'app') {
    rules.appId = requiredRule('prism.channel.form.appIdRequired')
    rules.appSecret = requiredRule('prism.channel.form.appSecretRequired')
    rules.chatId = requiredRule('prism.channel.form.chatIdRequired')
  }
  if (formData.type === 'wecom') {
    if (formData.mode === 'robot') {
      rules.robotWebhookUrl = [
        ...requiredRule('prism.channel.form.webhookUrlRequired'),
        { validator: (_r, v, cb) => isHttpUrl(v) ? cb() : cb(new Error('Invalid URL')), trigger: 'blur' },
      ]
    } else {
      rules.corpId = requiredRule('prism.channel.form.corpIdRequired')
      rules.agentId = requiredRule('prism.channel.form.agentIdRequired')
      rules.toUser = requiredRule('prism.channel.form.toUserRequired')
      rules.secret = requiredRule('prism.channel.form.secretRequired')
    }
  }
  if (formData.type === 'telegram') {
    rules.botToken = requiredRule('prism.channel.form.botTokenRequired')
    rules.chatId = requiredRule('prism.channel.form.chatIdRequired')
  }
  if (formData.type === 'sms') {
    if (formData.provider === 'custom') {
      // The custom gateway authenticates through its header map; the
      // endpoint and body template carry the delivery contract instead.
      rules.endpoint = [
        ...requiredRule('prism.channel.form.endpointRequired'),
        { validator: (_r, v, cb) => isHttpUrl(v) ? cb() : cb(new Error('Invalid URL')), trigger: 'blur' },
      ]
      rules.bodyTemplate = requiredRule('prism.channel.form.bodyTemplateRequired')
    } else {
      rules.apiKey = requiredRule('prism.channel.form.apiKeyRequired')
      rules.apiSecret = requiredRule('prism.channel.form.apiSecretRequired')
    }
    rules.recipients = requiredRule('prism.channel.form.recipientsRequired')
  }
  return rules
})

function handleTypeChange(type: ChannelType): void {
  if (isEdit.value) return
  formData.type = type
  // The mode field is shared by the two mode-switching types; reset it to
  // the per-type default so switching types never leaks a stale value.
  formData.mode = type === 'feishu' ? 'webhook' : 'robot'
  formRef.value?.clearValidate()
}

function addHeaderRow(): void {
  formData.headerRows.push({ key: '', value: '' })
}

function removeHeaderRow(index: number): void {
  formData.headerRows.splice(index, 1)
}

/** Custom HTTP headers for webhook channels: drop rows with empty keys */
function buildHeaders(): Record<string, string> | undefined {
  const entries = formData.headerRows
    .map((row) => ({ key: row.key.trim(), value: row.value }))
    .filter((row) => row.key.length > 0)
  if (entries.length === 0) return undefined
  const headers: Record<string, string> = {}
  for (const entry of entries) headers[entry.key] = entry.value
  return headers
}

function parseRecipients(raw: string): string[] {
  return raw
    .split(/[\n,，;；]/)
    .map((s) => s.trim())
    .filter((s) => s.length > 0)
}

function parseEmails(raw: string): string[] {
  return parseRecipients(raw)
}

/**
 * Build the config object with snake_case wire keys matching the backend
 * channel Config json tags. The request layer only converts top-level body
 * keys, so nested config keys must already be wire-format here. Omitted
 * optional fields are left out.
 */
function buildConfig(): Record<string, unknown> {
  if (formData.type === 'webhook') {
    const cfg: Record<string, unknown> = { url: formData.url }
    if (formData.timeout) cfg.timeout = formData.timeout
    const headers = buildHeaders()
    if (headers) cfg.headers = headers
    return cfg
  }
  if (formData.type === 'email') {
    const cfg: Record<string, unknown> = {
      host: formData.host,
      port: formData.port,
      from: formData.fromAddr,
      to: parseEmails(formData.toAddrs),
      tls_mode: formData.tlsMode,
      auth_type: formData.authType,
      html_mode: formData.htmlMode,
    }
    // Credentials are a pair: send both only when a username is set.
    if (formData.smtpUsername) {
      cfg.username = formData.smtpUsername
      cfg.password = formData.smtpPassword
    }
    return cfg
  }
  const cfg: Record<string, unknown> = { frontend_base_url: formData.frontendBaseUrl }
  if (proxyRegionTypes.has(formData.type)) {
    cfg.proxy_url = formData.proxyUrl
    if (formData.region) cfg.region = formData.region
  }
  if (webhookTypes.has(formData.type) && !(formData.type === 'feishu' && formData.mode === 'app')) {
    cfg.webhook_url = formData.webhookUrl
  }
  switch (formData.type) {
    case 'feishu':
      cfg.mode = formData.mode
      if (formData.mode === 'app') {
        cfg.app_id = formData.appId
        cfg.app_secret = formData.appSecret
        cfg.chat_id = formData.chatId
        if (formData.baseUrl) cfg.base_url = formData.baseUrl
      } else {
        cfg.secret = formData.secret
      }
      if (formData.messageType) cfg.message_type = formData.messageType
      break
    case 'dingtalk':
      cfg.secret = formData.secret
      cfg.keyword = formData.keyword
      // Stream-mode credentials are optional: present only when configured,
      // so an edit that leaves them masked re-submits the mask untouched.
      if (formData.appKey) cfg.app_key = formData.appKey
      if (formData.appSecret) cfg.app_secret = formData.appSecret
      if (formData.baseUrl) cfg.base_url = formData.baseUrl
      if (formData.messageType) cfg.message_type = formData.messageType
      break
    case 'slack':
      cfg.channel = formData.channel
      break
    case 'discord':
      cfg.username = formData.username
      cfg.avatar_url = formData.avatarUrl
      break
    case 'teams':
      break
    case 'wecom':
      cfg.mode = formData.mode
      cfg.message_type = formData.messageType || 'text'
      if (formData.mode === 'robot') {
        cfg.robot_webhook_url = formData.robotWebhookUrl
      } else {
        cfg.corp_id = formData.corpId
        cfg.agent_id = formData.agentId
        cfg.secret = formData.secret
        cfg.to_user = formData.toUser
        if (formData.baseUrl) cfg.base_url = formData.baseUrl
        // Callback credentials are optional: present only when configured,
        // so an edit that leaves them masked re-submits the mask untouched.
        if (formData.callbackToken) cfg.callback_token = formData.callbackToken
        if (formData.encodingAesKey) cfg.encoding_aes_key = formData.encodingAesKey
      }
      break
    case 'telegram':
      cfg.bot_token = formData.botToken
      cfg.chat_id = formData.chatId
      if (formData.apiBase) cfg.api_base = formData.apiBase
      break
    case 'sms':
      cfg.provider = formData.provider
      if (formData.provider === 'custom') {
        cfg.endpoint = formData.endpoint
        cfg.body_template = formData.bodyTemplate
        if (formData.method && formData.method !== 'POST') cfg.method = formData.method
        const headers = buildHeaders()
        if (headers) cfg.headers = headers
      } else {
        cfg.api_key = formData.apiKey
        cfg.api_secret = formData.apiSecret
        cfg.sign_name = formData.signName
        if (smsUsesTemplate.value) {
          cfg.template_code = formData.templateCode
          cfg.region = formData.smsRegion
        }
      }
      cfg.to = parseRecipients(formData.recipients)
      break
  }
  return cfg
}

function readStr(val: unknown, def = ''): string {
  return typeof val === 'string' ? val : def
}

function readNum(val: unknown, def = 0): number {
  return typeof val === 'number' ? val : def
}

function readBool(val: unknown, def = false): boolean {
  return typeof val === 'boolean' ? val : def
}

/** Keep the masked placeholder so an untouched edit preserves the stored secret */
function readSecret(val: unknown): string {
  if (typeof val !== 'string') return ''
  return val
}

async function handleSubmit(): Promise<void> {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  submitting.value = true
  try {
    const payload = { name: formData.name, type: formData.type, config: buildConfig(), enabled: formData.enabled }
    if (isEdit.value && channelId.value) {
      await updateChannel(channelId.value, payload)
      ElMessage.success(t('prism.channel.form.updated'))
    } else {
      await createChannel(payload)
      ElMessage.success(t('prism.channel.form.created'))
    }
    router.push('/prism/channel/list')
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('common.app.failed'))
  } finally {
    submitting.value = false
  }
}

async function handleTest(): Promise<void> {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  testing.value = true
  testResult.value = null
  try {
    const result = await testChannel({ type: formData.type, config: buildConfig() })
    testResult.value = result
    if (result.status === 'success') {
      ElMessage.success(t('prism.channel.list.testSuccess'))
    } else {
      const detail = result.error ? `: ${result.error}` : ''
      ElMessage.error(`${t('prism.channel.list.testFailed')}${detail}`)
    }
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('prism.channel.list.testFailed'))
  } finally {
    testing.value = false
  }
}

function handleCancel(): void {
  router.push('/prism/channel/list')
}

function loadWebhookConfig(c: Record<string, unknown>): void {
  formData.url = readSecret(c.url)
  formData.timeout = readStr(c.timeout, '10s')
  formData.headerRows = []
  if (c.headers && typeof c.headers === 'object') {
    for (const [key, value] of Object.entries(c.headers as Record<string, unknown>)) {
      formData.headerRows.push({ key, value: readStr(value) })
    }
  }
}

function loadEmailConfig(c: Record<string, unknown>): void {
  formData.host = readStr(c.host)
  formData.port = readNum(c.port, 25)
  formData.smtpUsername = readStr(c.username)
  formData.smtpPassword = readSecret(c.password)
  formData.fromAddr = readStr(c.from)
  if (Array.isArray(c.to)) formData.toAddrs = c.to.filter((v): v is string => typeof v === 'string').join('\n')
  formData.tlsMode = readStr(c.tls_mode, 'none')
  formData.authType = readStr(c.auth_type, 'plain')
  formData.htmlMode = readBool(c.html_mode)
}

onMounted(async () => {
  if (!isEdit.value || !channelId.value) return
  loading.value = true
  try {
    const cfg = await getChannel(channelId.value)
    formData.name = cfg.name
    formData.type = cfg.type
    formData.enabled = cfg.enabled
    const c = JSON.parse(cfg.config) as Record<string, unknown>
    if (cfg.type === 'webhook') {
      loadWebhookConfig(c)
    } else if (cfg.type === 'email') {
      loadEmailConfig(c)
    } else {
      formData.frontendBaseUrl = readStr(c.frontend_base_url)
      if (proxyRegionTypes.has(cfg.type)) {
        formData.proxyUrl = readStr(c.proxy_url)
        formData.region = readStr(c.region)
      }
      formData.webhookUrl = readSecret(c.webhook_url)
      if (cfg.type === 'feishu') {
        // Explicit mode wins; channels saved before the mode field infer
        // it the same way the backend does (app_id present → app mode).
        formData.mode = readStr(c.mode) || (c.app_id ? 'app' : 'webhook')
        if (formData.mode === 'app') {
          formData.appId = readStr(c.app_id)
          formData.appSecret = readSecret(c.app_secret)
          formData.chatId = readStr(c.chat_id)
          formData.baseUrl = readStr(c.base_url)
        } else {
          formData.secret = readSecret(c.secret)
        }
        formData.messageType = readStr(c.message_type)
      }
      if (cfg.type === 'dingtalk') {
        formData.secret = readSecret(c.secret)
        formData.keyword = readStr(c.keyword)
        formData.appKey = readStr(c.app_key)
        formData.appSecret = readSecret(c.app_secret)
        formData.baseUrl = readStr(c.base_url)
        formData.messageType = readStr(c.message_type)
      }
      if (cfg.type === 'slack') formData.channel = readStr(c.channel)
      if (cfg.type === 'discord') {
        formData.username = readStr(c.username)
        formData.avatarUrl = readStr(c.avatar_url)
      }
      if (cfg.type === 'wecom') {
        formData.mode = readStr(c.mode, 'robot')
        formData.robotWebhookUrl = readSecret(c.robot_webhook_url)
        formData.corpId = readStr(c.corp_id)
        formData.agentId = readNum(c.agent_id)
        formData.secret = readSecret(c.secret)
        formData.toUser = readStr(c.to_user)
        formData.baseUrl = readStr(c.base_url)
        formData.callbackToken = readSecret(c.callback_token)
        formData.encodingAesKey = readSecret(c.encoding_aes_key)
        formData.messageType = readStr(c.message_type, 'text')
      }
      if (cfg.type === 'telegram') {
        formData.botToken = readSecret(c.bot_token)
        formData.chatId = readStr(c.chat_id)
        formData.apiBase = readStr(c.api_base)
      }
      if (cfg.type === 'sms') {
        formData.provider = readStr(c.provider, 'aliyun')
        formData.apiKey = readSecret(c.api_key)
        formData.apiSecret = readSecret(c.api_secret)
        formData.signName = readStr(c.sign_name)
        formData.templateCode = readStr(c.template_code)
        formData.smsRegion = readStr(c.region)
        formData.endpoint = readStr(c.endpoint)
        formData.method = readStr(c.method, 'POST')
        formData.bodyTemplate = readStr(c.body_template)
        formData.headerRows = []
        if (c.headers && typeof c.headers === 'object') {
          for (const [key, value] of Object.entries(c.headers as Record<string, unknown>)) {
            formData.headerRows.push({ key, value: readStr(value) })
          }
        }
        if (Array.isArray(c.to)) formData.recipients = c.to.filter((v): v is string => typeof v === 'string').join('\n')
      }
    }
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('common.app.failed'))
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="tk-channel-form tk-page-container">
    <div class="tk-page-header">
      <h2 class="tk-page-title">
        {{ isEdit ? $t('prism.channel.form.edit') : $t('prism.channel.form.create') }}
      </h2>
    </div>
    <div
      v-loading="loading"
      class="tk-channel-form__content"
    >
      <!-- Channel type selection -->
      <div class="tk-channel-form-section">
        <div class="tk-channel-form-section__title">
          {{ $t('prism.channel.form.type') }}
          <span
            v-if="isEdit"
            class="tk-channel-form-edit-hint"
          >{{ $t('prism.channel.form.typeEditLocked') }}</span>
        </div>
        <div
          class="tk-channel-type-picker"
          role="group"
          :aria-label="$t('prism.channel.form.type')"
        >
          <div
            v-for="opt in typeOptions"
            :key="opt.value"
            class="tk-channel-type-card"
            :class="{
              'tk-channel-type-card--active': formData.type === opt.value,
              'tk-channel-type-card--locked': isEdit,
            }"
            @click="handleTypeChange(opt.value)"
          >
            <div class="tk-channel-type-card__icon">
              {{ opt.icon }}
            </div>
            <div class="tk-channel-type-card__label">
              {{ $t(`prism.channel.form.${opt.value}`) }}
            </div>
            <div class="tk-channel-type-card__code">
              {{ opt.value }}
            </div>
          </div>
        </div>
      </div>

      <el-form
        ref="formRef"
        :model="formData"
        :rules="formRules"
        label-position="top"
        role="form"
        :aria-label="$t(isEdit ? 'prism.channel.form.edit' : 'prism.channel.form.create')"
        class="tk-channel-form__form"
      >
        <!-- Basic info -->
        <div class="tk-channel-form-section">
          <div class="tk-channel-form-section__title">
            {{ $t('prism.channel.form.configParams') }}
          </div>
          <el-form-item
            :label="$t('prism.channel.form.name')"
            prop="name"
          >
            <el-input
              v-model="formData.name"
              :placeholder="$t('prism.channel.form.namePlaceholder')"
              maxlength="32"
              :aria-label="$t('prism.channel.form.name')"
              aria-required="true"
            />
          </el-form-item>

          <!-- Webhook: generic HTTP endpoint -->
          <template v-if="formData.type === 'webhook'">
            <el-divider content-position="left">
              {{ $t('prism.channel.form.webhook') }}
            </el-divider>
            <el-form-item
              :label="$t('prism.channel.form.webhookUrl')"
              prop="url"
            >
              <el-input
                v-model="formData.url"
                placeholder="https://..."
                aria-required="true"
              />
            </el-form-item>
            <el-form-item
              :label="$t('prism.channel.form.webhookTimeout')"
              prop="timeout"
            >
              <el-input
                v-model="formData.timeout"
                :placeholder="$t('prism.channel.form.webhookTimeoutPlaceholder')"
              />
            </el-form-item>
            <el-form-item :label="$t('prism.channel.form.webhookHeaders')">
              <div class="tk-channel-headers">
                <div
                  v-for="(row, index) in formData.headerRows"
                  :key="index"
                  class="tk-channel-headers__row"
                >
                  <el-input
                    v-model="row.key"
                    :placeholder="$t('prism.channel.form.webhookHeaderKey')"
                  />
                  <el-input
                    v-model="row.value"
                    :placeholder="$t('prism.channel.form.webhookHeaderValue')"
                  />
                  <el-button
                    type="danger"
                    text
                    :aria-label="$t('prism.channel.form.webhookRemoveHeader')"
                    @click="removeHeaderRow(index)"
                  >
                    {{ $t('common.app.delete') }}
                  </el-button>
                </div>
                <el-button @click="addHeaderRow">
                  {{ $t('prism.channel.form.webhookAddHeader') }}
                </el-button>
              </div>
            </el-form-item>
          </template>

          <!-- Email: SMTP -->
          <template v-if="formData.type === 'email'">
            <el-divider content-position="left">
              {{ $t('prism.channel.form.email') }}
            </el-divider>
            <el-form-item
              :label="$t('prism.channel.form.emailHost')"
              prop="host"
            >
              <el-input
                v-model="formData.host"
                :placeholder="$t('prism.channel.form.emailHostPlaceholder')"
                aria-required="true"
              />
            </el-form-item>
            <el-form-item
              :label="$t('prism.channel.form.emailPort')"
              prop="port"
            >
              <el-input-number
                v-model="formData.port"
                :min="1"
                :max="65535"
              />
            </el-form-item>
            <el-form-item :label="$t('prism.channel.form.emailUsername')">
              <el-input
                v-model="formData.smtpUsername"
                :placeholder="$t('prism.channel.form.emailUsernamePlaceholder')"
                autocomplete="off"
              />
            </el-form-item>
            <el-form-item :label="$t('prism.channel.form.emailPassword')">
              <el-input
                v-model="formData.smtpPassword"
                show-password
                autocomplete="new-password"
              />
            </el-form-item>
            <el-form-item
              :label="$t('prism.channel.form.emailFrom')"
              prop="fromAddr"
            >
              <el-input
                v-model="formData.fromAddr"
                :placeholder="$t('prism.channel.form.emailFromPlaceholder')"
                aria-required="true"
              />
            </el-form-item>
            <el-form-item
              :label="$t('prism.channel.form.emailTo')"
              prop="toAddrs"
            >
              <el-input
                v-model="formData.toAddrs"
                type="textarea"
                :rows="3"
                :placeholder="$t('prism.channel.form.emailToPlaceholder')"
                aria-required="true"
              />
            </el-form-item>
            <el-form-item :label="$t('prism.channel.form.emailTlsMode')">
              <el-radio-group v-model="formData.tlsMode">
                <el-radio value="none">
                  {{ $t('prism.channel.form.emailTlsNone') }}
                </el-radio>
                <el-radio value="implicit">
                  {{ $t('prism.channel.form.emailTlsImplicit') }}
                </el-radio>
                <el-radio value="starttls">
                  {{ $t('prism.channel.form.emailTlsStartTLS') }}
                </el-radio>
              </el-radio-group>
            </el-form-item>
            <el-form-item :label="$t('prism.channel.form.emailAuthType')">
              <el-select v-model="formData.authType">
                <el-option
                  label="PLAIN"
                  value="plain"
                />
                <el-option
                  label="LOGIN"
                  value="login"
                />
                <el-option
                  label="CRAM-MD5"
                  value="cram-md5"
                />
              </el-select>
            </el-form-item>
            <el-form-item :label="$t('prism.channel.form.emailHtmlMode')">
              <el-switch v-model="formData.htmlMode" />
            </el-form-item>
          </template>

          <!-- Simple webhook types: dingtalk / feishu / slack / discord / teams -->
          <template v-if="webhookTypes.has(formData.type)">
            <el-divider content-position="left">
              {{ $t(`prism.channel.form.${formData.type}`) }}
            </el-divider>
            <el-form-item
              v-if="formData.type === 'feishu'"
              :label="$t('prism.channel.form.feishuMode')"
            >
              <el-radio-group v-model="formData.mode">
                <el-radio value="webhook">
                  {{ $t('prism.channel.form.feishuModeWebhook') }}
                </el-radio>
                <el-radio value="app">
                  {{ $t('prism.channel.form.feishuModeApp') }}
                </el-radio>
              </el-radio-group>
            </el-form-item>
            <div
              v-if="formData.type === 'feishu' && formData.mode === 'app'"
              class="tk-form-item__help"
              style="margin-bottom: var(--tk-spacing-md);"
            >
              {{ $t('prism.channel.form.feishuAppHint') }}
            </div>
            <el-form-item
              v-if="formData.type !== 'feishu' || formData.mode === 'webhook'"
              :label="$t('prism.channel.form.webhookUrl')"
              prop="webhookUrl"
            >
              <el-input
                v-model="formData.webhookUrl"
                placeholder="https://..."
                aria-required="true"
              />
            </el-form-item>
            <template v-if="(formData.type === 'feishu' && formData.mode === 'webhook') || formData.type === 'dingtalk'">
              <el-form-item
                :label="$t('prism.channel.form.secret')"
                prop="secret"
              >
                <el-input
                  v-model="formData.secret"
                  show-password
                />
              </el-form-item>
            </template>
            <template v-if="formData.type === 'feishu' && formData.mode === 'app'">
              <el-form-item
                :label="$t('prism.channel.form.appId')"
                prop="appId"
              >
                <el-input
                  v-model="formData.appId"
                  aria-required="true"
                />
              </el-form-item>
              <el-form-item
                :label="$t('prism.channel.form.appSecret')"
                prop="appSecret"
              >
                <el-input
                  v-model="formData.appSecret"
                  show-password
                  aria-required="true"
                />
              </el-form-item>
              <el-form-item
                :label="$t('prism.channel.form.chatId')"
                prop="chatId"
              >
                <el-input
                  v-model="formData.chatId"
                  placeholder="oc_..."
                  aria-required="true"
                />
              </el-form-item>
              <el-form-item
                v-if="l3Enabled"
                :label="$t('prism.channel.form.imBaseUrl')"
              >
                <el-input
                  v-model="formData.baseUrl"
                  :placeholder="$t('prism.channel.form.imBaseUrlPlaceholder')"
                />
              </el-form-item>
            </template>
            <template v-if="formData.type === 'dingtalk'">
              <el-form-item
                :label="$t('prism.channel.form.keyword')"
                prop="keyword"
              >
                <el-input v-model="formData.keyword" />
              </el-form-item>
            </template>
            <template v-if="formData.type === 'dingtalk' && l1InteractEnabled">
              <el-divider content-position="left">
                {{ $t('prism.channel.form.dingtalkStream') }}
              </el-divider>
              <div
                class="tk-form-item__help"
                style="margin-bottom: var(--tk-spacing-md);"
              >
                {{ $t('prism.channel.form.dingtalkStreamHint') }}
              </div>
              <el-form-item :label="$t('prism.channel.form.appKey')">
                <el-input
                  v-model="formData.appKey"
                  show-password
                />
              </el-form-item>
              <el-form-item :label="$t('prism.channel.form.appSecret')">
                <el-input
                  v-model="formData.appSecret"
                  show-password
                />
              </el-form-item>
              <el-form-item
                v-if="l3Enabled"
                :label="$t('prism.channel.form.imBaseUrl')"
              >
                <el-input
                  v-model="formData.baseUrl"
                  :placeholder="$t('prism.channel.form.imBaseUrlPlaceholder')"
                />
              </el-form-item>
            </template>
            <template v-if="formData.type === 'slack'">
              <el-form-item
                :label="$t('prism.channel.form.channel')"
                prop="channel"
              >
                <el-input
                  v-model="formData.channel"
                  placeholder="#alerts"
                />
              </el-form-item>
            </template>
            <template v-if="formData.type === 'discord'">
              <el-form-item
                :label="$t('prism.channel.form.username')"
                prop="username"
              >
                <el-input v-model="formData.username" />
              </el-form-item>
              <el-form-item
                :label="$t('prism.channel.form.avatarUrl')"
                prop="avatarUrl"
              >
                <el-input
                  v-model="formData.avatarUrl"
                  placeholder="https://..."
                />
              </el-form-item>
            </template>
          </template>

          <!-- WeCom: robot webhook or application message -->
          <template v-if="formData.type === 'wecom'">
            <el-divider content-position="left">
              {{ $t('prism.channel.form.wecom') }}
            </el-divider>
            <el-form-item :label="$t('prism.channel.form.wecomMode')">
              <el-radio-group v-model="formData.mode">
                <el-radio value="robot">
                  {{ $t('prism.channel.form.wecomModeRobot') }}
                </el-radio>
                <el-radio value="app">
                  {{ $t('prism.channel.form.wecomModeApp') }}
                </el-radio>
              </el-radio-group>
            </el-form-item>
            <el-form-item
              v-if="formData.mode === 'robot'"
              :label="$t('prism.channel.form.robotWebhookUrl')"
              prop="robotWebhookUrl"
            >
              <el-input
                v-model="formData.robotWebhookUrl"
                placeholder="https://..."
                aria-required="true"
              />
            </el-form-item>
            <template v-else>
              <el-form-item
                :label="$t('prism.channel.form.corpId')"
                prop="corpId"
              >
                <el-input v-model="formData.corpId" />
              </el-form-item>
              <el-form-item
                :label="$t('prism.channel.form.agentId')"
                prop="agentId"
              >
                <el-input-number
                  v-model="formData.agentId"
                  :min="1"
                />
              </el-form-item>
              <el-form-item
                :label="$t('prism.channel.form.secret')"
                prop="secret"
              >
                <el-input
                  v-model="formData.secret"
                  show-password
                />
              </el-form-item>
              <el-form-item
                :label="$t('prism.channel.form.toUser')"
                prop="toUser"
              >
                <el-input
                  v-model="formData.toUser"
                  placeholder="@all"
                />
              </el-form-item>
              <el-form-item
                v-if="l3Enabled"
                :label="$t('prism.channel.form.imBaseUrl')"
              >
                <el-input
                  v-model="formData.baseUrl"
                  :placeholder="$t('prism.channel.form.imBaseUrlPlaceholder')"
                />
              </el-form-item>
              <template v-if="l1InteractEnabled">
                <el-divider content-position="left">
                  {{ $t('prism.channel.form.wecomCallback') }}
                </el-divider>
                <div
                  class="tk-form-item__help"
                  style="margin-bottom: var(--tk-spacing-md);"
                >
                  {{ $t('prism.channel.form.wecomCallbackHint') }}
                </div>
                <el-form-item :label="$t('prism.channel.form.callbackToken')">
                  <el-input
                    v-model="formData.callbackToken"
                    show-password
                  />
                </el-form-item>
                <el-form-item :label="$t('prism.channel.form.encodingAesKey')">
                  <el-input
                    v-model="formData.encodingAesKey"
                    show-password
                  />
                </el-form-item>
              </template>
            </template>
            <el-form-item :label="$t('prism.channel.form.messageType')">
              <el-select v-model="formData.messageType">
                <el-option
                  v-for="opt in messageTypeOptions.wecom"
                  :key="opt.value"
                  :label="$t(opt.label)"
                  :value="opt.value"
                />
              </el-select>
            </el-form-item>
          </template>

          <!-- SMS -->
          <template v-if="formData.type === 'sms'">
            <el-divider content-position="left">
              {{ $t('prism.channel.form.sms') }}
            </el-divider>
            <el-form-item :label="$t('prism.channel.form.provider')">
              <el-select v-model="formData.provider">
                <el-option
                  :label="$t('prism.channel.form.providerAliyun')"
                  value="aliyun"
                />
                <el-option
                  :label="$t('prism.channel.form.providerTencent')"
                  value="tencent"
                />
                <el-option
                  :label="$t('prism.channel.form.providerTwilio')"
                  value="twilio"
                />
                <el-option
                  v-if="l3Enabled"
                  :label="$t('prism.channel.form.providerCustom')"
                  value="custom"
                />
              </el-select>
            </el-form-item>
            <template v-if="formData.provider === 'custom'">
              <div
                class="tk-form-item__help"
                style="margin-bottom: var(--tk-spacing-md);"
              >
                {{ $t('prism.channel.form.customGatewayHint') }}
              </div>
              <el-form-item
                :label="$t('prism.channel.form.endpoint')"
                prop="endpoint"
              >
                <el-input
                  v-model="formData.endpoint"
                  placeholder="http://sms-gw.intranet.example/send"
                  aria-required="true"
                />
              </el-form-item>
              <el-form-item :label="$t('prism.channel.form.httpMethod')">
                <el-select v-model="formData.method">
                  <el-option
                    label="POST"
                    value="POST"
                  />
                  <el-option
                    label="PUT"
                    value="PUT"
                  />
                </el-select>
              </el-form-item>
              <el-form-item
                :label="$t('prism.channel.form.bodyTemplate')"
                prop="bodyTemplate"
              >
                <el-input
                  v-model="formData.bodyTemplate"
                  type="textarea"
                  :rows="4"
                  :placeholder="BODY_TEMPLATE_PLACEHOLDER"
                  aria-required="true"
                />
              </el-form-item>
              <el-form-item :label="$t('prism.channel.form.webhookHeaders')">
                <div class="tk-channel-headers">
                  <div
                    v-for="(row, index) in formData.headerRows"
                    :key="index"
                    class="tk-channel-headers__row"
                  >
                    <el-input
                      v-model="row.key"
                      :placeholder="$t('prism.channel.form.webhookHeaderKey')"
                    />
                    <el-input
                      v-model="row.value"
                      :placeholder="$t('prism.channel.form.webhookHeaderValue')"
                    />
                    <el-button
                      type="danger"
                      text
                      :aria-label="$t('prism.channel.form.webhookRemoveHeader')"
                      @click="removeHeaderRow(index)"
                    >
                      {{ $t('common.app.delete') }}
                    </el-button>
                  </div>
                  <el-button @click="addHeaderRow">
                    {{ $t('prism.channel.form.webhookAddHeader') }}
                  </el-button>
                </div>
              </el-form-item>
            </template>
            <template v-else>
              <el-form-item
                :label="$t('prism.channel.form.apiKey')"
                prop="apiKey"
              >
                <el-input
                  v-model="formData.apiKey"
                  show-password
                  aria-required="true"
                />
              </el-form-item>
              <el-form-item
                :label="$t('prism.channel.form.apiSecret')"
                prop="apiSecret"
              >
                <el-input
                  v-model="formData.apiSecret"
                  show-password
                  aria-required="true"
                />
              </el-form-item>
              <el-form-item
                :label="$t('prism.channel.form.signName')"
                prop="signName"
              >
                <el-input v-model="formData.signName" />
              </el-form-item>
            </template>
            <el-form-item
              v-if="smsUsesTemplate"
              :label="$t('prism.channel.form.templateCode')"
              prop="templateCode"
            >
              <el-input v-model="formData.templateCode" />
            </el-form-item>
            <el-form-item
              v-if="smsUsesTemplate"
              :label="$t('prism.channel.form.smsRegion')"
              prop="smsRegion"
            >
              <el-input
                v-model="formData.smsRegion"
                :placeholder="$t('prism.channel.form.smsRegionPlaceholder')"
              />
            </el-form-item>
            <el-form-item
              :label="$t('prism.channel.form.recipients')"
              prop="recipients"
            >
              <el-input
                v-model="formData.recipients"
                type="textarea"
                :rows="3"
                :placeholder="$t('prism.channel.form.recipientsPlaceholder')"
                aria-required="true"
              />
            </el-form-item>
          </template>

          <!-- Telegram -->
          <template v-if="formData.type === 'telegram'">
            <el-divider content-position="left">
              {{ $t('prism.channel.form.telegram') }}
            </el-divider>
            <el-form-item
              :label="$t('prism.channel.form.botToken')"
              prop="botToken"
            >
              <el-input
                v-model="formData.botToken"
                show-password
                aria-required="true"
              />
            </el-form-item>
            <el-form-item
              :label="$t('prism.channel.form.chatId')"
              prop="chatId"
            >
              <el-input
                v-model="formData.chatId"
                aria-required="true"
              />
            </el-form-item>
            <el-form-item
              :label="$t('prism.channel.form.apiBase')"
              prop="apiBase"
            >
              <el-input
                v-model="formData.apiBase"
                placeholder="https://api.telegram.org"
              />
            </el-form-item>
          </template>

          <!-- Message type for feishu/dingtalk (wecom renders its own above) -->
          <el-form-item
            v-if="formData.type === 'feishu' || formData.type === 'dingtalk'"
            :label="$t('prism.channel.form.messageType')"
          >
            <el-select v-model="formData.messageType">
              <el-option
                v-for="opt in messageTypeOptions[formData.type]"
                :key="opt.value"
                :label="$t(opt.label)"
                :value="opt.value"
              />
            </el-select>
          </el-form-item>

          <!-- Common configuration -->
          <el-divider
            v-if="linkBackTypes.has(formData.type)"
            content-position="left"
          >
            {{ $t('prism.channel.form.config') }}
          </el-divider>
          <el-form-item
            v-if="linkBackTypes.has(formData.type)"
            :label="$t('prism.channel.form.frontendBaseUrl')"
            prop="frontendBaseUrl"
          >
            <el-input
              v-model="formData.frontendBaseUrl"
              placeholder="https://..."
            />
          </el-form-item>
          <template v-if="proxyRegionTypes.has(formData.type)">
            <el-form-item
              :label="$t('prism.channel.form.proxyUrl')"
              prop="proxyUrl"
            >
              <el-input
                v-model="formData.proxyUrl"
                placeholder="https://..."
              />
            </el-form-item>
            <el-form-item
              :label="$t('prism.channel.form.region')"
              prop="region"
            >
              <el-select v-model="formData.region">
                <el-option
                  :label="$t('prism.channel.form.regionDefault')"
                  value=""
                />
                <el-option
                  :label="$t('prism.channel.form.regionCN')"
                  value="cn"
                />
                <el-option
                  :label="$t('prism.channel.form.regionGlobal')"
                  value="global"
                />
              </el-select>
            </el-form-item>
          </template>

          <el-form-item :label="$t('prism.channel.form.enabled')">
            <el-switch v-model="formData.enabled" />
          </el-form-item>
        </div>

        <!-- Test send section -->
        <div class="tk-channel-form-test-section">
          <div class="tk-channel-form-section__title">
            {{ $t('prism.channel.form.testSection') }}
          </div>
          <div
            class="tk-form-item__help"
            style="margin-bottom: var(--tk-spacing-md);"
          >
            {{ $t('prism.channel.form.testHint') }}
          </div>
          <el-button
            :loading="testing"
            @click="handleTest"
          >
            {{ $t('prism.channel.form.test') }}
          </el-button>
          <div
            v-if="testResult"
            class="tk-channel-test-result"
            :class="{ 'tk-channel-test-result--failed': testResult.status === 'failed' }"
          >
            <div class="tk-channel-test-result__meta">
              <span>{{ testResult.status === 'success' ? '✓ ' + $t('prism.channel.list.success') : '✗ ' + $t('prism.channel.list.failed') }}</span>
              <span v-if="testResult.latency">{{ $t('prism.channel.list.latency') }}: {{ testResult.latency }}ms</span>
              <span v-if="testResult.responseCode">{{ $t('prism.channel.delivery.responseCode') }}: {{ testResult.responseCode }}</span>
            </div>
            <div
              v-if="testResult.error"
              style="margin-top:4px;color:var(--tk-danger-color-text);"
            >
              {{ testResult.error }}
            </div>
          </div>
        </div>

        <!-- Action buttons -->
        <div class="tk-channel-form-actions">
          <el-button @click="handleCancel">
            {{ $t('prism.channel.form.cancel') }}
          </el-button>
          <el-button
            type="primary"
            :loading="submitting"
            @click="handleSubmit"
          >
            {{ $t('prism.channel.form.save') }}
          </el-button>
        </div>
      </el-form>
    </div>
  </div>
</template>

<style scoped lang="scss">
.tk-channel-form__content {
  max-width: 800px;
}

.tk-channel-form-section {
  padding: var(--tk-spacing-md) var(--tk-spacing-lg);
  margin-bottom: var(--tk-spacing-md);
  background-color: var(--tk-bg-color);
  border: 1px solid var(--tk-border-color-lighter);
  border-radius: var(--tk-border-radius-md);
}

.tk-channel-form-section__title {
  display: flex;
  gap: var(--tk-spacing-sm);
  align-items: center;
  padding-bottom: var(--tk-spacing-xs);
  margin-bottom: var(--tk-spacing-md);
  font-size: var(--tk-font-size-base);
  font-weight: var(--tk-font-weight-semibold);
  color: var(--tk-text-primary);
  border-bottom: 1px solid var(--tk-border-color-lighter);
}

.tk-channel-form-edit-hint {
  font-size: var(--tk-font-size-xs);
  color: var(--tk-warning-color-text);
}

.tk-channel-type-picker {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
  gap: var(--tk-spacing-sm);
}

.tk-channel-type-card {
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-xs);
  align-items: center;
  padding: var(--tk-spacing-md);
  cursor: pointer;
  background-color: var(--tk-bg-color);
  border: 2px solid var(--tk-border-color-light);
  border-radius: var(--tk-border-radius-md);
  transition: all var(--tk-animation-duration-base) var(--tk-ease-in-out);

  &:hover { border-color: var(--tk-primary-color-light-5); }

  &--active {
    background-color: var(--tk-primary-color-light-9);
    border-color: var(--tk-primary-color);
  }

  &--locked {
    pointer-events: none;
    cursor: not-allowed;
    opacity: 0.55;
  }
}

.tk-channel-type-card__icon { font-size: 24px; line-height: 1; }
.tk-channel-type-card__label { font-size: var(--tk-font-size-sm); color: var(--tk-text-regular); }
.tk-channel-type-card__code { font-family: var(--tk-font-family-mono); font-size: 10px; color: var(--tk-text-secondary); }

.tk-channel-headers__row {
  display: flex;
  gap: var(--tk-spacing-sm);
  align-items: center;
  width: 100%;
  margin-bottom: var(--tk-spacing-sm);
}

.tk-channel-form-test-section {
  padding: var(--tk-spacing-md) var(--tk-spacing-lg);
  margin-bottom: var(--tk-spacing-md);
  background-color: var(--tk-bg-color);
  border: 1px solid var(--tk-border-color-lighter);
  border-radius: var(--tk-border-radius-md);
}

.tk-channel-test-result {
  padding: var(--tk-spacing-sm) var(--tk-spacing-md);
  margin-top: var(--tk-spacing-md);
  font-family: var(--tk-font-family-mono);
  font-size: var(--tk-font-size-xs);
  color: var(--tk-text-regular);
  word-break: break-all;
  background-color: var(--tk-success-color-light-9);
  border-left: 3px solid var(--tk-success-color);
  border-radius: var(--tk-border-radius-base);

  &--failed {
    background-color: var(--tk-danger-color-light-9);
    border-left-color: var(--tk-danger-color);
  }
}

.tk-channel-test-result__meta {
  display: flex;
  gap: var(--tk-spacing-sm);
  margin-bottom: 4px;
  font-weight: var(--tk-font-weight-semibold);
}

.tk-channel-form-actions {
  display: flex;
  gap: var(--tk-spacing-sm);
  justify-content: flex-end;
  padding-top: var(--tk-spacing-md);
}

:deep(.el-select),
:deep(.el-input-number) {
  width: 100%;
}
</style>
