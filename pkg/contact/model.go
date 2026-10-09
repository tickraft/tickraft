// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package contact

import "time"

// Contact is the GORM persistence model for a notification-only contact
// (sys_contact). A contact carries no credentials and has no login path;
// it exists so alert notifications can be addressed to a person by email
// or phone without creating a user account.
type Contact struct {
	// ID is the unique identifier of the contact.
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// TenantID is the tenant that owns the contact. The open-source build
	// is single-tenant and always stores 0; multi-tenant editions scope
	// every query by the tenant resolved from the request context.
	TenantID int64 `gorm:"index;default:0" json:"tenant_id"`
	// Name is the human-readable name of the contact.
	Name string `gorm:"size:255" json:"name"`
	// Email is the contact's email address. At least one of Email and
	// Phone must be set.
	Email string `gorm:"size:255" json:"email"`
	// Phone is the contact's phone number (E.164 or national form). At
	// least one of Email and Phone must be set.
	Phone string `gorm:"size:64" json:"phone"`
	// Remark is an optional free-form note.
	Remark string `gorm:"size:255" json:"remark"`
	// CreatedAt is the timestamp when the contact was created.
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	// UpdatedAt is the timestamp when the contact was last updated.
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName overrides the default GORM table name for Contact.
func (Contact) TableName() string { return "sys_contact" }
