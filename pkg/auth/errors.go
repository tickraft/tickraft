// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth

import (
	"fmt"

	"github.com/tickraft/tickraft/pkg/errdefs"
)

// Sentinel errors for authentication and authorization.
//
// Each sentinel wraps the corresponding errdefs sentinel so that the
// transport-layer error mapper (pkg/api/httputil.mapError) recognizes
// domain variants uniformly via errors.Is. Domain packages that further
// wrap these sentinels (e.g. fmt.Errorf("oauth: %w", ErrUnauthorized))
// are also recognized automatically.
var (
	// ErrUnauthorized is returned when authentication fails.
	ErrUnauthorized = fmt.Errorf("auth: %w", errdefs.ErrUnauthorized)
	// ErrForbidden is returned when access is denied.
	ErrForbidden = fmt.Errorf("auth: %w", errdefs.ErrForbidden)
	// ErrUserExists is returned when attempting to register an existing user.
	ErrUserExists = fmt.Errorf("auth: %w", errdefs.ErrConflict)
	// ErrOldPassword is returned when change-password verification of the
	// current password fails. 400-class by design so clients show an inline
	// error instead of treating the valid session as expired.
	ErrOldPassword = fmt.Errorf("auth: %w", errdefs.ErrOldPasswordMismatch)
)

// HTTP error code constants live in github.com/tickraft/tickraft/pkg/errdefs.
// Use errdefs.CodeUnauthorized, errdefs.CodeForbidden, etc.
//
// Token and API key sentinel errors are defined in their canonical
// sub-packages: github.com/tickraft/tickraft/pkg/auth/jwt and
// github.com/tickraft/tickraft/pkg/auth/apikey respectively. Callers
// should import those packages directly.
