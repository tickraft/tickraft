// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package errdefs

// Error codes — general (4 digits, prefix matches HTTP status).
//
// These codes are the stable API contract returned in the `code` field of
// the standard response envelope. Renumbering existing codes is a breaking
// change; add new codes at the end of their segment.
const (
	CodeSuccess          = 0
	CodeBadRequest       = 40000
	CodeMissingParam     = 40001
	CodeInvalidFormat    = 40002
	CodeOldPassword      = 40003
	CodeUnauthorized     = 40100
	CodeTokenExpired     = 40101
	CodeAssetKeyMissing  = 40102
	CodeForbidden        = 40300
	CodeAssetKeyInvalid  = 40301
	CodeNotFound         = 40400
	CodeMethodNotAllowed = 40500
	CodeConflict         = 40900
	CodeTooManyRequests  = 42900
	CodeInternal         = 50000
)

// Error code segments (5 digits, first 2 = module number).
//
// Modules allocate their own 1xxx sub-range starting from the segment
// minimum declared here. For example, the task module owns 10xxx and may
// use 10001, 10002, etc.
const (
	CodeTaskMin   = 10000 // Task module: 10xxx
	CodeDeviceMin = 11000 // Device module: 11xxx
	CodeAlertMin  = 12000 // Alert module: 12xxx
)
