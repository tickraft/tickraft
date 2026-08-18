// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth

import (
	"time"

	"github.com/tickraft/tickraft/pkg/auth/jwt"
)

// LoginResult is the value returned by a successful login. It embeds the
// issued *jwt.TokenPair and carries policy flags that the caller (handler
// or frontend) must honor, such as MustChangePassword which forces a
// password change before the user may continue.
type LoginResult struct {
	*jwt.TokenPair
	// MustChangePassword is true when the user must change their password
	// before being allowed to perform further actions.
	MustChangePassword bool `json:"must_change_password"`
	// MFARequired is true when the user has MFA enabled. In this case
	// TokenPair is nil and MFATicket contains a short-lived ticket that
	// must be exchanged via MFALogin together with a TOTP code.
	MFARequired bool   `json:"mfa_required"`
	MFATicket   string `json:"mfa_ticket,omitempty"`
}

// Info represents the metadata of an API key without the raw secret.
type Info struct {
	// ID is the unique identifier of the API key record.
	ID int64
	// Name is the human-readable label of the API key.
	Name string
	// KeyPrefix is the non-secret prefix of the key used for identification.
	KeyPrefix string
	// KeyHash is the hashed representation of the full key for verification.
	KeyHash string
	// Status indicates whether the key is active (1) or revoked (0).
	Status int
	// CreatedAt is the timestamp when the key was created.
	CreatedAt time.Time
	// ExpiredAt is the optional timestamp when the key expires; nil means no expiry.
	ExpiredAt *time.Time
}

// Role constants matching user.User.Role and jwt.Claims.Role field values.
//
// These mirror the pkg/types.Role typed constants (the canonical
// definition of the role values) but are intentionally untyped so they
// can be used directly in map[int]/map[int64] indexes and assigned to
// int and int64 fields (e.g. user.User.Role, user.Store.Create's role
// parameter) without conversion. Untyped constants cannot reference the
// typed pkg/types values, so this set is defined independently and must
// be kept in sync with pkg/types. New code that wants compile-time type
// safety should use pkg/types.Role instead.
const (
	// RoleVisitor represents a read-only user role (viewer).
	RoleVisitor = 0
	// RoleDeveloper represents a user with read/write access to tasks, devices, and alerts.
	RoleDeveloper = 1
	// RoleAdmin represents a user with full access to all resources.
	RoleAdmin = 2
)

// Action constants for permission checks.
const (
	// ActionRead represents the read permission action.
	ActionRead = "read"
	// ActionWrite represents the write permission action.
	ActionWrite = "write"
	// ActionDelete represents the delete permission action.
	ActionDelete = "delete"
)

// Token type constants used to distinguish between access and refresh tokens.
const (
	// TokenTypeAccess identifies an access token.
	TokenTypeAccess = "access"
	// TokenTypeRefresh identifies a refresh token.
	TokenTypeRefresh = "refresh"
)
