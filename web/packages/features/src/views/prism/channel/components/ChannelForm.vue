// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { AccessibleDrawer, isValidUrl } from '@tickraft/core'
import {
  createChannel,
  updateChannel,
} from '../../../../api/prism'
import type { ChannelPayload, NotificationChannel, WebhookConfig, EmailConfig } from '../../../../api/prism'

/** A single header row in the dynamic headers editor */
interface HeaderRow {
  key: string
  value: string
}

/** Component props */
const props = defineProps<{
  /** Controls drawer visibility (v-model) */
  modelValue: boolean
  /** The channel being edited; null means create mode */
  channel: NotificationChannel | null
}>()

/** Component emits */
const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'saved'): void
}>()

const { t } = useI18n()

const formRef = ref<FormInstance>()
const saving = ref(false)

/** Whether in edit mode */
const isEdit = computed(() => !!props.channel)

/** Drawer title */
const drawerTitle = computed(() =>
  isEdit.value ? t('prism.channel.form.titleEdit') : t('prism.channel.form.titleCreate'),
)

/** Form data */
const form = ref({
  name: '',
  type: 'webhook' as string,
  enabled: true,
  // Webhook-specific fields
  webhookUrl: '',
  webhookMethod: 'POST' as 'POST' | 'PUT',
  webhookTimeout: 10,
  headers: [] as HeaderRow[],
  // Email-specific fields
  emailHost: '',
  emailPort: 587,
  emailUsername: '',
  emailPassword: '',
  emailFrom: '',
  emailTo: '',  // comma-separated string in UI, split to array on submit
  emailTlsMode: 'starttls' as string,
  emailAuthType: 'plain' as string,
  emailHtmlMode: false,
})

/** Channel type options (supports webhook and email) */
const typeOptions = computed(() => [
  { value: 'webhook', label: t('prism.channel.type.webhook') },
  { value: 'email', label: t('prism.channel.type.email') },
])

/** HTTP method options */
const methodOptions = computed(() => [
  { value: 'POST', label: 'POST' },
  { value: 'PUT', label: 'PUT' },
])

/** TLS mode options for email channel */
const tlsModeOptions = computed(() => [
  { value: 'none', label: 'None' },
  { value: 'implicit', label: 'Implicit TLS (465)' },
  { value: 'starttls', label: 'STARTTLS (587)' },
])

/** Auth type options for email channel */
const authTypeOptions = computed(() => [
  { value: 'plain', label: 'PLAIN' },
  { value: 'login', label: 'LOGIN' },
  { value: 'cram-md5', label: 'CRAM-MD5' },
])

/** Whether the current channel type is webhook */
const isWebhook = computed(() => form.value.type === 'webhook')

/** Whether the current channel type is email */
const isEmail = computed(() => form.value.type === 'email')

/** Form validation rules */
const rules = computed<FormRules>(() => ({
  name: [
    { required: true, message: t('prism.channel.form.namePlaceholder'), trigger: 'blur' },
    { max: 255, message: t('prism.channel.form.namePlaceholder'), trigger: 'blur' },
  ],
  type: [{ required: true, message: t('prism.channel.form.typePlaceholder'), trigger: 'change' }],
  webhookUrl: [
    {
      required: true,
      validator: (_rule, value: string, callback) => {
        if (!isWebhook.value) return callback()
        if (!value) {
          callback(new Error(t('prism.channel.form.webhookUrlPlaceholder')))
        } else if (!isValidUrl(value) || !/^https?:\/\//.test(value)) {
          callback(new Error(t('prism.channel.form.invalidUrl')))
        } else {
          callback()
        }
      },
      trigger: 'blur',
    },
  ],
  webhookTimeout: [
    {
      required: true,
      validator: (_rule, value: number, callback) => {
        if (!isWebhook.value) return callback()
        if (!value) {
          callback(new Error(t('prism.channel.form.webhookTimeoutPlaceholder')))
        } else if (value < 1 || value > 60) {
          callback(new Error(t('prism.channel.form.webhookTimeoutPlaceholder')))
        } else {
          callback()
        }
      },
      trigger: 'blur',
    },
  ],
  emailHost: [
    {
      required: true,
      validator: (_rule, value: string, callback) => {
        if (!isEmail.value) return callback()
        if (!value || !value.trim()) {
          callback(new Error(t('prism.channel.form.emailHostPlaceholder')))
        } else {
          callback()
        }
      },
      trigger: 'blur',
    },
  ],
  emailPort: [
    {
      required: true,
      validator: (_rule, value: number, callback) => {
        if (!isEmail.value) return callback()
        if (!value || value < 1 || value > 65535) {
          callback(new Error(t('prism.channel.form.emailPort')))
        } else {
          callback()
        }
      },
      trigger: 'blur',
    },
  ],
  emailUsername: [
    {
      required: true,
      validator: (_rule, value: string, callback) => {
        if (!isEmail.value) return callback()
        if (!value || !value.trim()) {
          callback(new Error(t('prism.channel.form.emailUsernamePlaceholder')))
        } else {
          callback()
        }
      },
      trigger: 'blur',
    },
  ],
  emailPassword: [
    {
      required: true,
      validator: (_rule, value: string, callback) => {
        if (!isEmail.value) return callback()
        if (!value) {
          callback(new Error(t('prism.channel.form.emailPasswordPlaceholder')))
        } else {
          callback()
        }
      },
      trigger: 'blur',
    },
  ],
  emailFrom: [
    {
      required: true,
      validator: (_rule, value: string, callback) => {
        if (!isEmail.value) return callback()
        if (!value || !value.trim()) {
          callback(new Error(t('prism.channel.form.emailFromPlaceholder')))
        } else {
          callback()
        }
      },
      trigger: 'blur',
    },
  ],
  emailTo: [
    {
      required: true,
      validator: (_rule, value: string, callback) => {
        if (!isEmail.value) return callback()
        if (!value || !value.trim()) {
          callback(new Error(t('prism.channel.form.emailToPlaceholder')))
        } else {
          callback()
        }
      },
      trigger: 'blur',
    },
  ],
}))

/** Parse a timeout duration string (e.g. "10s") into seconds */
function parseTimeoutSeconds(timeoutStr: string): number {
  const match = timeoutStr.match(/^(\d+)s$/)
  if (match) return Number(match[1])
  const parsed = parseInt(timeoutStr, 10)
  return Number.isNaN(parsed) ? 10 : parsed
}

/** Load channel data into the form when editing */
function loadChannel(channel: NotificationChannel): void {
  form.value.name = channel.name
  form.value.type = channel.type
  form.value.enabled = channel.enabled

  try {
    if (channel.type === 'email') {
      // Parse email config from the JSON config string
      const cfg = JSON.parse(channel.config) as Partial<EmailConfig>
      form.value.emailHost = cfg.host ?? ''
      form.value.emailPort = cfg.port ?? 587
      form.value.emailUsername = cfg.username ?? ''
      form.value.emailPassword = cfg.password ?? ''
      form.value.emailFrom = cfg.from ?? ''
      form.value.emailTo = (cfg.to ?? []).join(', ')
      form.value.emailTlsMode = cfg.tls_mode ?? 'starttls'
      form.value.emailAuthType = cfg.auth_type ?? 'plain'
      form.value.emailHtmlMode = cfg.html_mode ?? false
    } else {
      // Parse webhook config from the JSON config string
      const cfg = JSON.parse(channel.config) as Partial<WebhookConfig>
      form.value.webhookUrl = cfg.url ?? ''
      form.value.webhookMethod = (cfg.method as 'POST' | 'PUT') ?? 'POST'
      form.value.webhookTimeout = parseTimeoutSeconds(cfg.timeout ?? '10s')
      const headerEntries = Object.entries(cfg.headers ?? {})
      form.value.headers = headerEntries.map(([key, value]) => ({ key, value: String(value) }))
    }
  } catch {
    // If config is not valid JSON, start with empty fields
    resetFormFields()
  }
}

/** Reset form fields to default values (shared by resetForm and loadChannel error fallback) */
function resetFormFields(): void {
  form.value = {
    name: '',
    type: 'webhook',
    enabled: true,
    webhookUrl: '',
    webhookMethod: 'POST',
    webhookTimeout: 10,
    headers: [],
    emailHost: '',
    emailPort: 587,
    emailUsername: '',
    emailPassword: '',
    emailFrom: '',
    emailTo: '',
    emailTlsMode: 'starttls',
    emailAuthType: 'plain',
    emailHtmlMode: false,
  }
}

/** Reset form to default create-mode state */
function resetForm(): void {
  resetFormFields()
  formRef.value?.resetFields()
}

/** Watch drawer visibility to load/reset form data */
watch(
  () => props.modelValue,
  (visible) => {
    if (visible) {
      if (props.channel) {
        loadChannel(props.channel)
      } else {
        resetForm()
      }
    }
  },
)

/** Add a new header row */
function addHeader(): void {
  form.value.headers.push({ key: '', value: '' })
}

/** Remove a header row by index */
function removeHeader(index: number): void {
  form.value.headers.splice(index, 1)
}

/** Build the channel config JSON string from form data based on channel type */
function buildConfigJson(): string {
  if (isEmail.value) {
    const config: EmailConfig = {
      host: form.value.emailHost.trim(),
      port: form.value.emailPort,
      username: form.value.emailUsername.trim(),
      password: form.value.emailPassword,
      from: form.value.emailFrom.trim(),
      to: form.value.emailTo.split(',').map(s => s.trim()).filter(Boolean),
      tls_mode: form.value.emailTlsMode,
      auth_type: form.value.emailAuthType,
      html_mode: form.value.emailHtmlMode,
    }
    return JSON.stringify(config)
  }

  // Webhook config (default)
  const headers: Record<string, string> = {}
  for (const row of form.value.headers) {
    const key = row.key.trim()
    if (key) {
      headers[key] = row.value
    }
  }
  const config: WebhookConfig = {
    url: form.value.webhookUrl.trim(),
    method: form.value.webhookMethod,
    timeout: `${form.value.webhookTimeout}s`,
    headers,
  }
  return JSON.stringify(config)
}

/** Validate the form (callback wrapped as a Promise) */
function validateForm(): Promise<boolean> {
  return new Promise((resolve) => {
    formRef.value?.validate((valid: boolean) => resolve(valid))
  })
}

/** Build the submission payload */
function buildPayload(): ChannelPayload {
  return {
    name: form.value.name.trim(),
    type: form.value.type,
    config: buildConfigJson(),
    enabled: form.value.enabled,
  }
}

/** Submit and save */
async function handleSubmit(): Promise<void> {
  const valid = await validateForm()
  if (!valid) {
    ElMessage.warning(t('prism.channel.form.validateError'))
    return
  }
  saving.value = true
  try {
    const payload = buildPayload()
    if (isEdit.value && props.channel) {
      await updateChannel(props.channel.id, payload)
      ElMessage.success(t('prism.channel.form.updatedToast'))
    } else {
      await createChannel(payload)
      ElMessage.success(t('prism.channel.form.createdToast'))
    }
    emit('update:modelValue', false)
    emit('saved')
  } finally {
    saving.value = false
  }
}

/** Cancel and close drawer */
function handleCancel(): void {
  emit('update:modelValue', false)
}
</script>

<template>
  <AccessibleDrawer
    :model-value="modelValue"
    :title="drawerTitle"
    size="520px"
    :close-on-click-modal="false"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="120px"
    >
      <!-- Section: Basic Info -->
      <section class="tk-channel-form__section">
        <h3 class="tk-channel-form__section-title">
          {{ t('prism.channel.form.sectionBasic') }}
        </h3>
        <el-form-item
          :label="t('prism.channel.form.name')"
          prop="name"
        >
          <el-input
            v-model="form.name"
            :placeholder="t('prism.channel.form.namePlaceholder')"
            maxlength="255"
            show-word-limit
          />
        </el-form-item>
        <el-form-item
          :label="t('prism.channel.form.type')"
          prop="type"
        >
          <el-select
            v-model="form.type"
            :placeholder="t('prism.channel.form.typePlaceholder')"
            class="tk-channel-form__block"
            disabled
          >
            <el-option
              v-for="opt in typeOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('prism.channel.form.enabled')">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </section>

      <!-- Section: Webhook Configuration -->
      <section v-if="isWebhook" class="tk-channel-form__section">
        <h3 class="tk-channel-form__section-title">
          {{ t('prism.channel.form.sectionWebhook') }}
        </h3>
        <el-form-item
          :label="t('prism.channel.form.webhookUrl')"
          prop="webhookUrl"
        >
          <el-input
            v-model="form.webhookUrl"
            :placeholder="t('prism.channel.form.webhookUrlPlaceholder')"
          />
        </el-form-item>
        <el-form-item :label="t('prism.channel.form.webhookMethod')">
          <el-select
            v-model="form.webhookMethod"
            class="tk-channel-form__block"
          >
            <el-option
              v-for="opt in methodOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item
          :label="t('prism.channel.form.webhookTimeout')"
          prop="webhookTimeout"
        >
          <el-input-number
            v-model="form.webhookTimeout"
            :min="1"
            :max="60"
            :step="1"
            :placeholder="t('prism.channel.form.webhookTimeoutPlaceholder')"
            class="tk-channel-form__block"
          />
          <div class="tk-channel-form__help">
            {{ t('prism.channel.form.webhookTimeoutHelp') }}
          </div>
        </el-form-item>

        <!-- Dynamic headers editor -->
        <el-form-item :label="t('prism.channel.form.webhookHeaders')">
          <div class="tk-channel-form__headers">
            <div
              v-for="(header, index) in form.headers"
              :key="index"
              class="tk-channel-form__header-row"
            >
              <el-input
                v-model="header.key"
                :placeholder="t('prism.channel.form.webhookHeaderKey')"
                class="tk-channel-form__header-key"
              />
              <el-input
                v-model="header.value"
                :placeholder="t('prism.channel.form.webhookHeaderValue')"
                class="tk-channel-form__header-value"
              />
              <el-button
                link
                type="danger"
                @click="removeHeader(index)"
              >
                {{ t('prism.channel.list.delete') }}
              </el-button>
            </div>
            <el-button
              link
              type="primary"
              @click="addHeader"
            >
              + {{ t('prism.channel.form.webhookAddHeader') }}
            </el-button>
          </div>
        </el-form-item>
      </section>

      <!-- Section: Email Configuration -->
      <section v-if="isEmail" class="tk-channel-form__section">
        <h3 class="tk-channel-form__section-title">
          {{ t('prism.channel.form.sectionEmail') }}
        </h3>
        <el-form-item :label="t('prism.channel.form.emailHost')" prop="emailHost">
          <el-input v-model="form.emailHost" :placeholder="t('prism.channel.form.emailHostPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('prism.channel.form.emailPort')" prop="emailPort">
          <el-input-number v-model="form.emailPort" :min="1" :max="65535" class="tk-channel-form__block" />
        </el-form-item>
        <el-form-item :label="t('prism.channel.form.emailUsername')" prop="emailUsername">
          <el-input v-model="form.emailUsername" :placeholder="t('prism.channel.form.emailUsernamePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('prism.channel.form.emailPassword')" prop="emailPassword">
          <el-input v-model="form.emailPassword" type="password" show-password :placeholder="t('prism.channel.form.emailPasswordPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('prism.channel.form.emailFrom')" prop="emailFrom">
          <el-input v-model="form.emailFrom" :placeholder="t('prism.channel.form.emailFromPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('prism.channel.form.emailTo')" prop="emailTo">
          <el-input v-model="form.emailTo" type="textarea" :rows="2" :placeholder="t('prism.channel.form.emailToPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('prism.channel.form.emailTlsMode')">
          <el-select v-model="form.emailTlsMode" class="tk-channel-form__block">
            <el-option v-for="opt in tlsModeOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('prism.channel.form.emailAuthType')">
          <el-select v-model="form.emailAuthType" class="tk-channel-form__block">
            <el-option v-for="opt in authTypeOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('prism.channel.form.emailHtmlMode')">
          <el-switch v-model="form.emailHtmlMode" />
        </el-form-item>
      </section>
    </el-form>

    <!-- Footer actions -->
    <template #footer>
      <div class="tk-channel-form__footer">
        <el-button @click="handleCancel">
          {{ t('prism.channel.form.cancel') }}
        </el-button>
        <el-button
          type="primary"
          :loading="saving"
          @click="handleSubmit"
        >
          {{ t('prism.channel.form.save') }}
        </el-button>
      </div>
    </template>
  </AccessibleDrawer>
</template>

<style scoped lang="scss">
.tk-channel-form {
  &__section {
    margin-bottom: var(--tk-spacing-lg);
  }

  &__section-title {
    padding-bottom: var(--tk-spacing-sm);
    margin: 0 0 var(--tk-spacing-md);
    font-size: var(--tk-font-size-md);
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
    border-bottom: 1px solid var(--tk-border-color-light);
  }

  &__block {
    width: 100%;
  }

  &__help {
    margin-top: 4px;
    font-size: var(--tk-font-size-sm);
    line-height: 1.5;
    color: var(--tk-text-secondary);
  }

  &__headers {
    width: 100%;
  }

  &__header-row {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: center;
    margin-bottom: var(--tk-spacing-sm);
  }

  &__header-key {
    flex: 0 0 35%;
  }

  &__header-value {
    flex: 1;
  }

  &__footer {
    display: flex;
    gap: var(--tk-spacing-sm);
    justify-content: flex-end;
  }
}
</style>
