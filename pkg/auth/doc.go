// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package auth provides authentication, authorization, and registration
// abstractions for API access control in tickraft.
//
// The package provides a complete single-user, single-tenant implementation
// suitable for the standalone runtime: bcrypt password hashing, JWT token
// management, API key generation, and login rate limiting.
//
// # Construction
//
// The primary constructor is NewService, which accepts the required JWT
// manager and stores:
//
//	svc := auth.NewService(jwtMgr, userStore, apiKeyStore, blacklist)
//
// NewService starts the background login-fail cleanup goroutine.
//
// # Interfaces
//
// The Policy interface and DefaultPolicy constructor are exported from
// this package so both the internal composition root and downstream
// editions can consume them. Authentication flows through Service.Login;
// route authorization runs in the RequirePermission middleware.
package auth
