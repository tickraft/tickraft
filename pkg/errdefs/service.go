// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package errdefs

import (
	"errors"
	"net/http"
)

// ServiceError is a service-layer error that carries an HTTP status and a
// business code, satisfying ErrorCoder so it can be passed directly to
// api.Fail for automatic response mapping.
type ServiceError struct {
	httpStatus int
	code       int
	message    string
}

// NewServiceError constructs a service-level error that implements
// ErrorCoder. The returned error is suitable for service methods that
// need to propagate HTTP-status-aware failures to the transport layer.
func NewServiceError(httpStatus, code int, message string) error {
	return &ServiceError{httpStatus: httpStatus, code: code, message: message}
}

// Error returns the human-readable error message.
func (e *ServiceError) Error() string { return e.message }

// HTTPStatus returns the HTTP status code associated with the error.
func (e *ServiceError) HTTPStatus() int { return e.httpStatus }

// Code returns the application error code associated with the error.
func (e *ServiceError) Code() int { return e.code }

// Sentinel service errors. Each implements ErrorCoder so the response
// layer can map them to the correct HTTP status and business code
// automatically. These are shared across the domain service packages.
var (
	ErrTaskNotFound      = NewServiceError(http.StatusNotFound, CodeNotFound, "task not found")
	ErrRuleNotFound      = NewServiceError(http.StatusNotFound, CodeNotFound, "alert rule not found")
	ErrRecordNotFound    = NewServiceError(http.StatusNotFound, CodeNotFound, "alert record not found")
	ErrExecutionNotFound = NewServiceError(http.StatusNotFound, CodeNotFound, "execution not found")
	ErrChannelNotFound   = NewServiceError(http.StatusNotFound, CodeNotFound,
		"notification channel not found")
	ErrRemediationRuleNotFound = NewServiceError(http.StatusNotFound, CodeNotFound,
		"remediation rule not found")
	ErrInvalidRequest = NewServiceError(http.StatusBadRequest, CodeBadRequest, "invalid request")
	// ErrOldPasswordMismatch reports a wrong current password on a
	// change-password call. It is deliberately a 400-class error, not 401:
	// the session itself is valid, and 401 would make clients treat it as
	// session expiry and drop the user to the login page.
	ErrOldPasswordMismatch = NewServiceError(http.StatusBadRequest, CodeOldPassword, "old password mismatch")
)

// InnermostMessage walks the wrap chain and returns the most informative
// message found along it, discarding sentinel wrapper prefixes (e.g. a
// store's "get rule" prefix) in favor of the longer underlying diagnostic
// such as an expression compile error.
func InnermostMessage(err error) string {
	msg := err.Error()
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return msg
		}
		err = unwrapped
		if detail := err.Error(); len(detail) > len(msg) {
			msg = detail
		}
	}
}
