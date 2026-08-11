// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
/**
 * Asset create / edit form page.
 *
 * Single form component reused for both create and edit modes, distinguished
 * by the presence of an `:id` route param. The form works with flat fields
 * (name, assetType, assetKey, endpoint, port, labels, description); the API
 * layer packs endpoint / port / labels / description into the metadata JSON
 * field before sending to the backend.
 */
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import {
  ASSET_TYPES,
  createAsset,
  getAsset,
  updateAsset,
  parseMetadata,
} from '../../../api/asset'
import type { AssetFormData, AssetType } from '../../../types/asset'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const formRef = ref<FormInstance>()
const submitting = ref(false)
const loading = ref(false)

/** Edit mode when an id is present in the route path */
const editId = computed(() => {
  const raw = route.params.id
  return raw ? Number(Array.isArray(raw) ? raw[0] : raw) : 0
})
const isEdit = computed(() => editId.value > 0)

/** Page title key based on mode */
const titleKey = computed(() => (isEdit.value ? 'asset.create.editTitle' : 'asset.create.title'))
const subtitleKey = computed(() => (isEdit.value ? 'asset.create.editSubtitle' : 'asset.create.subtitle'))

const form = reactive<AssetFormData>({
  name: '',
  assetType: 'host',
  assetKey: '',
  endpoint: '',
  port: undefined,
  labels: [],
  description: '',
})

/** Tag input buffer for labels */
const labelInput = ref('')

const rules = computed<FormRules<AssetFormData>>(() => ({
  name: [{ required: true, message: t('asset.create.nameRequired'), trigger: 'blur' }],
  assetKey: [{ required: true, message: t('asset.create.endpointRequired'), trigger: 'blur' }],
}))

const typeOptions = computed(() =>
  ASSET_TYPES.map((value) => ({ label: t(`asset.type.${value}`), value })),
)

/** Add a label from the input buffer */
function addLabel(): void {
  const value = labelInput.value.trim()
  if (!value) return
  if (!form.labels.includes(value)) {
    form.labels.push(value)
  }
  labelInput.value = ''
}

/** Remove a label by value */
function removeLabel(label: string): void {
  const idx = form.labels.indexOf(label)
  if (idx !== -1) form.labels.splice(idx, 1)
}

async function loadAsset(): Promise<void> {
  if (!isEdit.value || !editId.value) return
  loading.value = true
  try {
    const record = await getAsset(editId.value)
    form.name = record.name
    form.assetType = record.assetType
    form.assetKey = record.assetKey
    const m = parseMetadata(record.metadata)
    form.endpoint = m.endpoint ?? ''
    form.port = m.port
    form.labels = [...(m.labels ?? [])]
    form.description = m.description ?? ''
  } catch {
    ElMessage.error(t('asset.create.notFound'))
    router.replace('/asset/list')
  } finally {
    loading.value = false
  }
}

async function handleSubmit(): Promise<void> {
  if (!formRef.value) return
  try {
    await formRef.value.validate()
  } catch {
    return
  }
  submitting.value = true
  try {
    const payload: AssetFormData = {
      ...form,
      assetType: form.assetType as AssetType,
      port: form.port || undefined,
    }
    if (isEdit.value && editId.value) {
      await updateAsset(editId.value, payload)
      ElMessage.success(t('asset.create.updateSuccess'))
    } else {
      await createAsset(payload)
      ElMessage.success(t('asset.create.submitSuccess'))
    }
    router.replace('/asset/list')
  } catch {
    // Errors are handled centrally by the interceptor
  } finally {
    submitting.value = false
  }
}

function handleCancel(): void {
  router.back()
}

onMounted(() => {
  void loadAsset()
})
</script>

<template>
  <div
    v-loading="loading"
    class="tk-page-container"
  >
    <!-- Page header -->
    <div class="tk-asset-form-header">
      <h1 class="tk-asset-form-header__title">
        {{ t(titleKey) }}
      </h1>
      <p class="tk-asset-form-header__subtitle">
        {{ t(subtitleKey) }}
      </p>
    </div>

    <!-- Form -->
    <div class="tk-asset-form-card">
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-position="top"
        class="tk-asset-form"
        @submit.prevent
      >
        <div class="tk-asset-form__grid">
          <el-form-item
            :label="t('asset.create.name')"
            prop="name"
          >
            <el-input
              v-model="form.name"
              :placeholder="t('asset.create.namePlaceholder')"
              clearable
            />
          </el-form-item>

          <el-form-item
            :label="t('asset.create.type')"
            prop="assetType"
          >
            <el-select
              v-model="form.assetType"
              :placeholder="t('asset.create.typePlaceholder')"
              class="tk-asset-form__select"
            >
              <el-option
                v-for="opt in typeOptions"
                :key="opt.value"
                :label="opt.label"
                :value="opt.value"
              />
            </el-select>
          </el-form-item>

          <el-form-item
            :label="t('asset.create.assetKey')"
            prop="assetKey"
          >
            <el-input
              v-model="form.assetKey"
              :placeholder="t('asset.create.assetKeyPlaceholder')"
              clearable
            />
          </el-form-item>

          <el-form-item
            :label="t('asset.create.port')"
            prop="port"
          >
            <el-input-number
              v-model="form.port"
              :min="1"
              :max="65535"
              :placeholder="t('asset.create.portPlaceholder')"
              controls-position="right"
              class="tk-asset-form__select"
            />
          </el-form-item>
        </div>

        <el-form-item
          :label="t('asset.create.labels')"
          prop="labels"
        >
          <div class="tk-asset-form__labels">
            <el-tag
              v-for="label in form.labels"
              :key="label"
              closable
              size="small"
              effect="plain"
              class="tk-asset-form__label"
              @close="removeLabel(label)"
            >
              {{ label }}
            </el-tag>
            <el-input
              v-model="labelInput"
              class="tk-asset-form__label-input"
              :placeholder="t('asset.create.labelsPlaceholder')"
              size="small"
              @keyup.enter="addLabel"
              @blur="addLabel"
            />
          </div>
        </el-form-item>

        <el-form-item
          :label="t('asset.create.description')"
          prop="description"
        >
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="3"
            :placeholder="t('asset.create.descriptionPlaceholder')"
            maxlength="200"
            show-word-limit
          />
        </el-form-item>

        <div class="tk-asset-form__actions">
          <el-button @click="handleCancel">
            {{ t('asset.create.cancel') }}
          </el-button>
          <el-button
            type="primary"
            :loading="submitting"
            @click="handleSubmit"
          >
            {{ t('asset.create.submit') }}
          </el-button>
        </div>
      </el-form>
    </div>
  </div>
</template>

<style scoped lang="scss">
.tk-asset-form-header {
  margin-bottom: var(--tk-spacing-md);

  &__title {
    margin: 0;
    font-size: var(--tk-font-size-xl);
    font-weight: var(--tk-font-weight-semibold);
    line-height: 1.1;
    color: var(--tk-text-primary);
  }

  &__subtitle {
    margin-top: 6px;
    font-size: var(--tk-font-size-sm);
    line-height: 1.5;
    color: var(--tk-text-secondary);
  }
}

.tk-asset-form-card {
  padding: var(--tk-spacing-lg);
  background-color: var(--tk-bg-color);
  border-radius: var(--tk-border-radius-md);
  box-shadow: var(--tk-shadow-sm);
}

.tk-asset-form {
  max-width: 880px;

  &__grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 0 var(--tk-spacing-lg);
  }

  &__select {
    width: 100%;
  }

  &__labels {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
    width: 100%;
    padding: 6px 8px;
    background-color: var(--tk-bg-color);
    border: 1px solid var(--tk-border-color);
    border-radius: var(--tk-border-radius-base);
  }

  &__label {
    flex-shrink: 0;
  }

  &__label-input {
    flex: 1;
    min-width: 140px;

    :deep(.el-input__wrapper) {
      background-color: transparent;
      box-shadow: none !important;
    }
  }

  &__actions {
    display: flex;
    gap: var(--tk-spacing-sm);
    justify-content: flex-end;
    margin-top: var(--tk-spacing-md);
  }
}

@media (max-width: 767px) {
  .tk-asset-form__grid {
    grid-template-columns: 1fr;
  }
}
</style>
