// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package types

// Role is the cross-domain user role for authentication and authorization.
//
// Previously this concept was expressed as bare untyped int constants
// (`RoleVisitor`, `RoleDeveloper`, `RoleAdmin`) in pkg/auth/types.go,
// matched against `model.User.Role int` and `jwt.Claims.Role int`.
// Centralizing the type here gives the compiler a way to detect
// mismatched role values at the call site and documents the role
// semantics in a single location.
//
// Migration note: pkg/auth keeps its own untyped int constants
// (auth.RoleVisitor, auth.RoleDeveloper, auth.RoleAdmin) mirroring these
// values. They intentionally remain separate definitions rather than
// references, because untyped constants cannot be derived from these
// typed ones, and downstream call sites pass them directly to both int
// fields (user.User.Role, jwt.Claims.Role) and int64 parameters
// (user.Store.Create). This package is the canonical definition of the
// role values; the auth constants must be kept in sync with it.
type Role int

const (
	// RoleVisitor represents a read-only user role (viewer).
	RoleVisitor Role = 0
	// RoleDeveloper represents a user with read/write access to tasks,
	// devices, and alerts.
	RoleDeveloper Role = 1
	// RoleAdmin represents a user with full access to all resources.
	RoleAdmin Role = 2
)
