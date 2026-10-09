// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package errdefs

// ErrorCoder is implemented by errors that carry an HTTP status code and
// an application error code. Transport layers (e.g. pkg/api/httputil) use
// this interface to auto-map an error to a structured failure response
// without inspecting concrete error types.
//
// Implementations are typically domain-specific ServiceError values defined
// in handler packages; the interface itself lives here so that the error
// vocabulary (sentinels + codes + ErrorCoder) is centralized in a single
// kernel package with no business-module dependencies.
type ErrorCoder interface {
	error
	// HTTPStatus returns the HTTP status code for this error.
	HTTPStatus() int
	// Code returns the application error code that appears in the
	// standard response envelope's `code` field.
	Code() int
}
