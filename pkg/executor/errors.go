// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package executor

import "errors"

// This file defines the sentinel errors for the executor package.
var (
	// ErrCapabilityNotSupported is returned when the executor does not
	// support the requested operation.
	ErrCapabilityNotSupported = errors.New("executor does not support the requested operation")
	// ErrInvalidOperation is returned when parsing an unknown operation
	// string.
	ErrInvalidOperation = errors.New("executor: invalid operation")
)
