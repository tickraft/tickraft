// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httputil

import (
	"errors"
	"net/http"

	"github.com/tickraft/tickraft/pkg/errdefs"
)

// mapError maps an error to (httpStatus, code, msg).
// Priority: 1) ErrorCoder interface, 2) errdefs sentinel errors,
// 3) fallback to 500.
//
// Domain packages that expose domain-specific variants of the shared
// sentinels SHOULD wrap them with fmt.Errorf("domain: %w",
// errdefs.ErrXxx) so that errors.Is(err, errdefs.ErrXxx) returns true
// uniformly and this mapper recognizes the variant without needing a
// per-domain case.
func mapError(err error) (httpStatus, code int, msg string) {
	if err == nil {
		return http.StatusInternalServerError, errdefs.CodeInternal, "internal error"
	}

	// 1. Check ErrorCoder interface.
	var ec errdefs.ErrorCoder
	if errors.As(err, &ec) {
		return ec.HTTPStatus(), ec.Code(), ec.Error()
	}

	// 2. Map errdefs sentinel errors. Domain packages that wrap these
	// sentinels (e.g. auth.ErrUnauthorized = fmt.Errorf("auth: %w",
	// errdefs.ErrUnauthorized)) are recognized here automatically.
	switch {
	case errors.Is(err, errdefs.ErrUnauthorized):
		return http.StatusUnauthorized, errdefs.CodeUnauthorized, err.Error()
	case errors.Is(err, errdefs.ErrForbidden):
		return http.StatusForbidden, errdefs.CodeForbidden, err.Error()
	case errors.Is(err, errdefs.ErrNotFound):
		return http.StatusNotFound, errdefs.CodeNotFound, err.Error()
	case errors.Is(err, errdefs.ErrInvalidArgument):
		return http.StatusBadRequest, errdefs.CodeBadRequest, err.Error()
	case errors.Is(err, errdefs.ErrConflict):
		return http.StatusConflict, errdefs.CodeConflict, err.Error()
	case errors.Is(err, errdefs.ErrTooManyRequests):
		return http.StatusTooManyRequests, errdefs.CodeTooManyRequests, err.Error()
	}

	// 3. Fallback: internal error, do not expose details.
	return http.StatusInternalServerError, errdefs.CodeInternal, "internal error"
}
