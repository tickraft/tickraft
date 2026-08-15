// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
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
// # Internal Interfaces
//
// The Authenticator, Authorizer, and Registrar interfaces and
// their constructors (NewAuthenticator, NewAuthorizer, NewRegistrar)
// live in internal/auth. They are internal abstractions not consumed
// by the extended repository. The Policy interface and DefaultPolicy
// constructor are exported from this package (pkg/auth) so they can
// be consumed by both the internal composition root and downstream
// editions.
//
// # Subpackages
//
//   - apikey: API key generation, SHA-256 hashing, and validation.
//   - jwt: dual access/refresh token pair generation, validation, and
//     in-memory blacklist.
//   - password: bcrypt password hashing and verification.
//   - region: HMAC-signed region cookie with key rotation support.
//   - totp: RFC 6238 TOTP generation and validation (stdlib only).
//
// # Security Considerations
//
// Passwords are hashed with bcrypt and never stored in plaintext. API keys
// are hashed with SHA-256 and compared in constant time. JWT secrets must be
// at least 32 bytes. Login rate limiting locks accounts after 5 failed
// attempts for 15 minutes. TOTP validation uses constant-time comparison
// and allows ±1 time step for clock drift.
package auth
