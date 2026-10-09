// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Search, Refresh, Plus, User } from '@element-plus/icons-vue'
import { DataTable, ConfirmDialog, formatDate, usePermission } from '@tickraft/core'
import {
  getContacts,
  createContact,
  updateContact,
  deleteContact,
} from '../../../../api/contact'
import type { Contact, ContactSaveParams } from '../../../../api/contact'

const { t } = useI18n()
const { canDelete } = usePermission()

const loading = ref(false)
const tableData = ref<Contact[]>([])
const page = ref(1)
const size = ref(10)
const total = ref(0)
const searchQuery = ref('')

const columns = computed(() => [
  { prop: 'name', label: t('prism.contact.name'), minWidth: 180, slot: 'name', align: 'left' as const },
  { prop: 'email', label: t('prism.contact.email'), minWidth: 220, slot: 'email', align: 'left' as const },
  { prop: 'phone', label: t('prism.contact.phone'), minWidth: 160, slot: 'phone', align: 'left' as const },
  { prop: 'remark', label: t('prism.contact.remark'), minWidth: 180, slot: 'remark' },
  { prop: 'createdAt', label: t('prism.contact.createdAt'), width: 170, slot: 'createdAt', align: 'center' as const },
])

// Create / edit dialog (one payload shape: full replace)
const dialogVisible = ref(false)
const dialogLoading = ref(false)
const editingId = ref<number | null>(null)
const form = reactive({
  name: '',
  email: '',
  phone: '',
  remark: '',
})

// Delete confirmation
const deleteVisible = ref(false)
const deleteLoading = ref(false)
const selectedContact = ref<Contact | null>(null)

async function loadData(): Promise<void> {
  loading.value = true
  try {
    const res = await getContacts({
      page: page.value,
      size: size.value,
      keyword: searchQuery.value.trim() || undefined,
    })
    tableData.value = res.items || []
    total.value = res.total || 0
  } catch {
    tableData.value = []
    total.value = 0
    ElMessage.error(t('prism.contact.loadFailed'))
  } finally {
    loading.value = false
  }
}

const dialogTitle = computed(() =>
  editingId.value === null
    ? t('prism.contact.createTitle')
    : t('prism.contact.editTitle'))

function handleCreate(): void {
  editingId.value = null
  form.name = ''
  form.email = ''
  form.phone = ''
  form.remark = ''
  dialogVisible.value = true
}

function handleEdit(row: Contact): void {
  editingId.value = row.id
  form.name = row.name
  form.email = row.email
  form.phone = row.phone
  form.remark = row.remark
  dialogVisible.value = true
}

/** Client-side mirror of the backend validation contract: name required,
 *  at least one of email/phone, email must look like a plain address. */
function validateForm(): string | null {
  const name = form.name.trim()
  if (!name) return t('prism.contact.nameRequired')
  const email = form.email.trim()
  const phone = form.phone.trim()
  if (!email && !phone) return t('prism.contact.channelRequired')
  if (email && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) return t('prism.contact.emailInvalid')
  return null
}

async function handleDialogConfirm(): Promise<void> {
  const problem = validateForm()
  if (problem) {
    ElMessage.warning(problem)
    return
  }
  const params: ContactSaveParams = {
    name: form.name.trim(),
    email: form.email.trim() || undefined,
    phone: form.phone.trim() || undefined,
    remark: form.remark.trim() || undefined,
  }
  dialogLoading.value = true
  try {
    if (editingId.value === null) {
      await createContact(params)
      ElMessage.success(t('prism.contact.createSuccess'))
    } else {
      await updateContact(editingId.value, params)
      ElMessage.success(t('prism.contact.updateSuccess'))
    }
    dialogVisible.value = false
    await loadData()
  } catch (err) {
    // The quota ceiling rejection deserves its own copy; other failures
    // keep the localized backend message (see apiErrorMessages).
    const message = err instanceof Error ? err.message : ''
    if (message.includes('quota exceeded')) {
      ElMessage.error(t('prism.contact.quotaExceeded'))
    } else {
      ElMessage.error(message || t('prism.contact.saveFailed'))
    }
  } finally {
    dialogLoading.value = false
  }
}

function handleDelete(row: Contact): void {
  selectedContact.value = row
  deleteVisible.value = true
}

async function handleDeleteConfirm(): Promise<void> {
  if (!selectedContact.value) return
  deleteLoading.value = true
  try {
    await deleteContact(selectedContact.value.id)
    deleteVisible.value = false
    ElMessage.success(t('prism.contact.deleteSuccess'))
    // Step back a page when the last row of the last page was removed.
    if (tableData.value.length === 1 && page.value > 1) {
      page.value -= 1
    }
    await loadData()
  } catch {
    ElMessage.error(t('prism.contact.deleteFailed'))
  } finally {
    deleteLoading.value = false
  }
}

function handlePageChange(payload: { page: number; size: number }): void {
  page.value = payload.page
  size.value = payload.size
  void loadData()
}

/** Debounced server-side keyword search. */
let searchTimer: ReturnType<typeof setTimeout> | null = null
watch(searchQuery, () => {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    page.value = 1
    void loadData()
  }, 300)
})

onMounted(() => {
  void loadData()
})
</script>

<template>
  <div class="tk-contact tk-page-container">
    <!-- Toolbar -->
    <div class="tk-contact__toolbar">
      <div class="tk-contact__toolbar-title">
        <h2 class="tk-contact__toolbar-heading">
          {{ t('prism.contact.list.title') }}
        </h2>
        <span class="tk-contact__toolbar-sub">
          {{ t('prism.contact.listCount', { count: total }) }}
        </span>
      </div>
      <el-input
        v-model="searchQuery"
        :placeholder="t('prism.contact.searchPlaceholder')"
        :prefix-icon="Search"
        class="tk-contact__search"
        clearable
      />
      <el-button
        :icon="Refresh"
        @click="loadData"
      >
        {{ t('common.app.refresh') }}
      </el-button>
      <el-button
        type="primary"
        :icon="Plus"
        @click="handleCreate"
      >
        {{ t('prism.contact.create') }}
      </el-button>
    </div>

    <!-- Table card -->
    <div class="tk-contact__table-card">
      <DataTable
        table-id="contact-list"
        :data="tableData"
        :columns="columns"
        :loading="loading"
        :total="total"
        :page="page"
        :size="size"
        @page-change="handlePageChange"
      >
        <template #name="{ row }">
          <div class="tk-contact-name">
            <el-icon class="tk-contact-name__icon">
              <User />
            </el-icon>
            <span class="tk-contact-name__title">{{ (row as Contact).name }}</span>
          </div>
        </template>
        <template #email="{ row }">
          <span
            v-if="(row as Contact).email"
            class="tk-contact-cell"
          >{{ (row as Contact).email }}</span>
          <span
            v-else
            class="tk-contact-cell--muted"
          >—</span>
        </template>
        <template #phone="{ row }">
          <span
            v-if="(row as Contact).phone"
            class="tk-contact-cell"
          >{{ (row as Contact).phone }}</span>
          <span
            v-else
            class="tk-contact-cell--muted"
          >—</span>
        </template>
        <template #remark="{ row }">
          <span
            v-if="(row as Contact).remark"
            class="tk-contact-cell--muted"
          >{{ (row as Contact).remark }}</span>
          <span
            v-else
            class="tk-contact-cell--muted"
          >—</span>
        </template>
        <template #createdAt="{ row }">
          <span class="tk-contact-time">{{ formatDate((row as Contact).createdAt) }}</span>
        </template>
        <template #action-column>
          <el-table-column
            :label="t('common.app.action')"
            width="130"
            fixed="right"
            align="center"
            :resizable="false"
          >
            <template #default="{ row }">
              <el-button
                link
                type="primary"
                size="small"
                @click="handleEdit(row as Contact)"
              >
                {{ t('common.app.edit') }}
              </el-button>
              <el-button
                v-if="canDelete('*')"
                link
                type="danger"
                size="small"
                @click="handleDelete(row as Contact)"
              >
                {{ t('common.app.delete') }}
              </el-button>
            </template>
          </el-table-column>
        </template>
      </DataTable>
    </div>

    <!-- Create / edit dialog -->
    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="520px"
    >
      <el-form
        :model="form"
        label-position="top"
      >
        <el-form-item
          :label="t('prism.contact.name')"
          required
        >
          <el-input
            v-model="form.name"
            :placeholder="t('prism.contact.namePlaceholder')"
            maxlength="255"
          />
        </el-form-item>
        <el-form-item :label="t('prism.contact.email')">
          <el-input
            v-model="form.email"
            :placeholder="t('prism.contact.emailPlaceholder')"
            maxlength="255"
          />
        </el-form-item>
        <el-form-item :label="t('prism.contact.phone')">
          <el-input
            v-model="form.phone"
            :placeholder="t('prism.contact.phonePlaceholder')"
            maxlength="64"
          />
        </el-form-item>
        <el-form-item :label="t('prism.contact.remark')">
          <el-input
            v-model="form.remark"
            :placeholder="t('prism.contact.remarkPlaceholder')"
            maxlength="255"
          />
        </el-form-item>
      </el-form>
      <p class="tk-contact__dialog-hint">
        {{ t('prism.contact.dialogHint') }}
      </p>
      <template #footer>
        <el-button @click="dialogVisible = false">
          {{ t('common.app.cancel') }}
        </el-button>
        <el-button
          type="primary"
          :loading="dialogLoading"
          @click="handleDialogConfirm"
        >
          {{ t('common.app.confirm') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- Delete confirmation -->
    <ConfirmDialog
      v-model="deleteVisible"
      type="danger"
      :title="t('prism.contact.deleteTitle')"
      :confirm-text="t('common.app.confirm')"
      :loading="deleteLoading"
      @confirm="handleDeleteConfirm"
    >
      <p class="tk-contact__delete-desc">
        {{ t('prism.contact.deleteConfirmDesc', { name: selectedContact?.name }) }}
      </p>
    </ConfirmDialog>
  </div>
</template>

<style scoped lang="scss">
.tk-contact {
  display: flex;
  flex-direction: column;
  gap: var(--tk-spacing-xl);

  // ---- Toolbar ----
  &__toolbar {
    display: flex;
    flex-wrap: wrap;
    gap: var(--tk-spacing-sm);
    align-items: center;
  }

  &__toolbar-title {
    display: flex;
    gap: var(--tk-spacing-sm);
    align-items: baseline;
    margin-right: auto;
  }

  &__toolbar-heading {
    margin: 0;
    font-size: var(--tk-font-size-xl);
    font-weight: var(--tk-font-weight-bold);
    color: var(--tk-text-primary);
    letter-spacing: -0.02em;
  }

  &__toolbar-sub {
    font-size: var(--tk-font-size-sm);
    color: var(--tk-text-secondary);
  }

  &__search {
    width: 240px;
  }

  // ---- Table card ----
  &__table-card {
    overflow: hidden;
    background-color: var(--tk-bg-surface);
    border: var(--tk-border-default);
    border-radius: var(--tk-border-radius-lg);
  }

  &__dialog-hint {
    margin: 0;
    font-size: var(--tk-font-size-sm);
    line-height: var(--tk-line-height-normal);
    color: var(--tk-text-secondary);
  }

  &__delete-desc {
    margin: 0;
    line-height: var(--tk-line-height-normal);
    color: var(--tk-text-regular);
  }
}

// ---- Name cell ----
.tk-contact-name {
  display: inline-flex;
  gap: var(--tk-spacing-xs);
  align-items: center;

  &__icon {
    color: var(--tk-text-secondary);
  }

  &__title {
    font-weight: var(--tk-font-weight-semibold);
    color: var(--tk-text-primary);
  }
}

// ---- Plain cells ----
.tk-contact-cell {
  color: var(--tk-text-regular);
  word-break: break-all;

  &--muted {
    color: var(--tk-text-secondary);
    opacity: 0.7;
  }
}

.tk-contact-time {
  font-family: var(--tk-font-family-mono);
  font-size: var(--tk-font-size-xs);
  color: var(--tk-text-secondary);
}

// ---- Responsive ----
@media (width <= 768px) {
  .tk-contact__search {
    width: 100%;
  }
}
</style>
