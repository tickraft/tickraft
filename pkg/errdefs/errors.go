// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package errdefs

import "errors"

// Sentinel errors shared across two or more business modules.
//
// Domain packages that expose domain-specific variants SHOULD wrap these
// sentinels with fmt.Errorf("domain: %w", errdefs.ErrXxx) so that
// errors.Is(err, errdefs.ErrXxx) returns true uniformly.
//
// Sentinels here are intentionally transport-agnostic: they do not
// implement ErrorCoder. Transport layers map them to HTTP status codes
// via the mapError helper (see pkg/api/httputil/errors.go).
var (
	// ErrInvalidArgument indicates that a caller-supplied argument is
	// missing, malformed, or fails validation. Domain variants include
	// user.ErrInvalidUsername, executor.ErrInvalidOperation,
	// scheduler.ErrInvalidCronExpr, telemetry.ErrInvalidConfig, etc.
	ErrInvalidArgument = errors.New("invalid argument")

	// ErrUnauthorized indicates that authentication is missing or has
	// failed. Domain variants include auth.ErrUnauthorized,
	// jwt.ErrTokenInvalid, apikey.ErrAPIKeyInvalid, etc.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrForbidden indicates that the authenticated principal is not
	// permitted to perform the requested action. Domain variants include
	// auth.ErrForbidden, apikey.ErrAPIKeyRevoked, etc.
	ErrForbidden = errors.New("forbidden")

	// ErrNotFound indicates that the requested resource does not exist.
	// Domain variants include task.ErrTaskNotFound,
	// telemetry.ErrAssetNotFound, etc.
	ErrNotFound = errors.New("not found")

	// ErrConflict indicates that the request conflicts with the current
	// state of the resource (e.g. duplicate key, already exists, state
	// transition not allowed). Domain variants include
	// auth.ErrUserExists, task.ErrTaskAlreadyPaused, task.ErrTaskRunning,
	// etc.
	ErrConflict = errors.New("conflict")

	// ErrTooManyRequests indicates that the caller has been rate-limited.
	// Domain variants include auth.ErrTooManyRequests.
	ErrTooManyRequests = errors.New("too many requests")

	// ErrInternal indicates an unexpected internal error. It is the
	// fallback category for errors that do not match any other sentinel.
	ErrInternal = errors.New("internal error")

	// ErrBusNotConfigured indicates that a component was started without
	// an event bus injected. Previously defined separately in pkg/prism/alert
	// and pkg/executor with identical semantics; centralized here to
	// eliminate the duplication.
	ErrBusNotConfigured = errors.New("event bus not configured")
)
