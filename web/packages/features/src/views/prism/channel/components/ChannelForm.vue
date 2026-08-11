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
import type { ChannelPayload, NotificationChannel, WebhookConfig } from '../../../../api/prism'

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
})

/** Channel type options (CE supports only webhook) */
const typeOptions = computed(() => [
  { value: 'webhook', label: t('prism.channel.type.webhook') },
])

/** HTTP method options */
const methodOptions = computed(() => [
  { value: 'POST', label: 'POST' },
  { value: 'PUT', label: 'PUT' },
])

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
    { required: true, message: t('prism.channel.form.webhookTimeoutPlaceholder'), trigger: 'blur' },
    { type: 'number', min: 1, max: 60, message: t('prism.channel.form.webhookTimeoutPlaceholder'), trigger: 'blur' },
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

  // Parse webhook config from the JSON config string
  try {
    const cfg = JSON.parse(channel.config) as Partial<WebhookConfig>
    form.value.webhookUrl = cfg.url ?? ''
    form.value.webhookMethod = (cfg.method as 'POST' | 'PUT') ?? 'POST'
    form.value.webhookTimeout = parseTimeoutSeconds(cfg.timeout ?? '10s')
    const headerEntries = Object.entries(cfg.headers ?? {})
    form.value.headers = headerEntries.map(([key, value]) => ({ key, value: String(value) }))
  } catch {
    // If config is not valid JSON, start with empty webhook fields
    form.value.webhookUrl = ''
    form.value.webhookMethod = 'POST'
    form.value.webhookTimeout = 10
    form.value.headers = []
  }
}

/** Reset form to default create-mode state */
function resetForm(): void {
  form.value = {
    name: '',
    type: 'webhook',
    enabled: true,
    webhookUrl: '',
    webhookMethod: 'POST',
    webhookTimeout: 10,
    headers: [],
  }
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

/** Build the webhook config JSON string from form data */
function buildConfigJson(): string {
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
      <section class="tk-channel-form__section">
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
