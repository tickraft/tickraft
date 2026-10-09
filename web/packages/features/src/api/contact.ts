// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import { request, PageData } from '@tickraft/core'

/**
 * Notification-only contact directory (backend pkg/contact).
 *
 * Contacts carry no credentials and cannot log in: they only feed
 * notification addressing (email / phone). The directory is quota-governed
 * (quota.TypeContact); the backend rejects the third creation with 409
 * "quota exceeded" while the CE ceiling of 2 is active.
 */
export interface Contact {
  id: number
  tenantId: number
  name: string
  email: string
  phone: string
  remark: string
  createdAt: string
  updatedAt: string
}

/** Contact create/update payload (full replace on update). */
export interface ContactSaveParams {
  name: string
  /** At least one of email and phone must be present. */
  email?: string
  phone?: string
  remark?: string
}

/**
 * Get the contact list (paginated, aligned with backend ListContacts →
 * PageData). keyword is a server-side substring filter on name/email/phone.
 */
export function getContacts(params: { page: number; size: number; keyword?: string }): Promise<PageData<Contact>> {
  return request<PageData<Contact>>({
    url: '/contacts',
    method: 'get',
    params,
  })
}

/**
 * Create a contact. Rejects with a localized "quota exceeded" error once
 * the directory has reached the enforced ceiling.
 */
export function createContact(params: ContactSaveParams): Promise<Contact> {
  return request<Contact>({
    url: '/contacts',
    method: 'post',
    data: params,
  })
}

/**
 * Update a contact (full replace).
 */
export function updateContact(id: number, params: ContactSaveParams): Promise<Contact> {
  return request<Contact>({
    url: `/contacts/${id}`,
    method: 'put',
    data: params,
  })
}

/**
 * Delete a contact.
 */
export function deleteContact(id: number): Promise<void> {
  return request<void>({
    url: `/contacts/${id}`,
    method: 'delete',
  })
}
