// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package contact manages the notification-only contact directory: the
// people alert notifications are addressed to. A contact holds no
// credentials and can never log in; it is a directory entry (name plus
// email/phone) consumed when composing notification recipients.
//
// The directory is quota-governed: creating a contact is rejected once the
// active quota provider's contact ceiling is reached (quota.TypeContact).
// The open-source build caps the directory at two seats; multi-tenant
// editions raise the ceiling through their own provider.
package contact
