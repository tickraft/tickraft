// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package contact

import "context"

// Service defines the operations for managing the notification-only
// contact directory.
//
// The concrete implementation is injected via the WithContactService
// RouteOption. The default ContactService persists to sys_contact
// directly; multi-tenant editions inject their own tenant-scoped
// implementation with the same wire contract.
type Service interface {
	// ListContacts returns a page of contacts matching the filter together
	// with the total count (offset pagination).
	ListContacts(ctx context.Context, filter ListFilter) ([]Contact, int64, error)
	// GetContact returns a single contact.
	GetContact(ctx context.Context, id int64) (*Contact, error)
	// CreateContact validates and creates a new contact. It fails with
	// ErrQuotaExceeded once the directory has reached the enforced seat
	// ceiling (quota.TypeContact).
	CreateContact(ctx context.Context, req *CreateRequest) (*Contact, error)
	// UpdateContact applies a full update to the contact identified by id.
	UpdateContact(ctx context.Context, id int64, req *UpdateRequest) (*Contact, error)
	// DeleteContact removes the contact identified by id.
	DeleteContact(ctx context.Context, id int64) error
	// CountContacts returns the number of contacts in the directory.
	CountContacts(ctx context.Context) (int64, error)
}

// ListFilter narrows a contact listing.
type ListFilter struct {
	// Keyword matches a substring of the name, email, or phone
	// (case-insensitive).
	Keyword string
	// Page is the 1-based page number.
	Page int
	// Size is the page size.
	Size int
}

// CreateRequest is the payload of a contact creation.
type CreateRequest struct {
	// Name is the human-readable name. Required, max 255 characters.
	Name string `json:"name"`
	// Email is the contact's email address. At least one of Email and
	// Phone must be present; when present it must be a valid address.
	Email string `json:"email"`
	// Phone is the contact's phone number. At least one of Email and Phone
	// must be present.
	Phone string `json:"phone"`
	// Remark is an optional free-form note (max 255 characters).
	Remark string `json:"remark"`
}

// UpdateRequest is the payload of a contact update.
type UpdateRequest = CreateRequest
