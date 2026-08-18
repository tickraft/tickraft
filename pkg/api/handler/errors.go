// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package handler

import (
	"net/http"

	"github.com/tickraft/tickraft/pkg/errdefs"
)

// ServiceError is a handler-layer error that carries an HTTP status and a
// business code, satisfying errdefs.ErrorCoder so it can be passed directly to
// api.Fail for automatic response mapping.
type ServiceError struct {
	httpStatus int
	code       int
	message    string
}

// NewServiceError constructs a service-level error that implements
// errdefs.ErrorCoder. The returned error is suitable for service methods that
// need to propagate HTTP-status-aware failures to the handler layer.
func NewServiceError(httpStatus, code int, message string) error {
	return &ServiceError{httpStatus: httpStatus, code: code, message: message}
}

// Error returns the human-readable error message.
func (e *ServiceError) Error() string { return e.message }

// HTTPStatus returns the HTTP status code associated with the error.
func (e *ServiceError) HTTPStatus() int { return e.httpStatus }

// Code returns the application error code associated with the error.
func (e *ServiceError) Code() int { return e.code }

// Sentinel service errors. Each implements errdefs.ErrorCoder so the response
// layer can map them to the correct HTTP status and business code
// automatically. These are shared across all service implementations in both
// pkg/ sub-packages and internal/api/service/ sub-packages.
var (
	ErrTaskNotFound      = NewServiceError(http.StatusNotFound, errdefs.CodeNotFound, "task not found")
	ErrRuleNotFound      = NewServiceError(http.StatusNotFound, errdefs.CodeNotFound, "alert rule not found")
	ErrRecordNotFound    = NewServiceError(http.StatusNotFound, errdefs.CodeNotFound, "alert record not found")
	ErrExecutionNotFound = NewServiceError(http.StatusNotFound, errdefs.CodeNotFound, "execution not found")
	ErrChannelNotFound   = NewServiceError(http.StatusNotFound, errdefs.CodeNotFound,
		"notification channel not found")
	ErrRemediationRuleNotFound = NewServiceError(http.StatusNotFound, errdefs.CodeNotFound,
		"remediation rule not found")
	ErrInvalidRequest = NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest, "invalid request")
)
