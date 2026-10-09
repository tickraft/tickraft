// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package user implements user identity model, API key, and the user
// store port.
//
// # Positioning
//
// pkg/user is the single home for user-domain value objects and the
// Store / APIKeyStore ports previously scattered across pkg/model
// and pkg/store. The package was split out of pkg/auth so that cross-
// domain callers (asset, task, audit, tenant) can depend on the user
// identity without pulling in authentication logic — addressing the
// "user lives in auth, so importing user means importing authorization"
// confusion documented in the architecture spec
//
// # Current members
//
//   - User — system user for single-process authentication
//   - APIKey — programmatic access key
//   - Store — port for user persistence (GORM-backed impl in store.go)
//   - APIKeyStore — port for API key persistence (GORM-backed impl in store.go)
//
// The User struct contains only fields required by the single-tenant
// runtime. Extended concerns (TenantID, MFASecret, MFAEnabled,
// LastLoginAt, etc.) MUST NOT be defined here; the extended User model
// is defined in callers/user via struct embedding.
//
// # Hard constraints
//
//   - This package MUST NOT import pkg/auth or any authentication logic.
//     Authentication (Authenticator, JWT, Password, TokenBlacklist)
//     lives in pkg/auth.
//   - The Store and APIKeyStore interfaces are ports; their GORM-backed
//     implementations live in this package (store.go).
package user
