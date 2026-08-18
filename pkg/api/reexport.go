// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package api

import (
	"github.com/tickraft/tickraft/pkg/api/httputil"
)

// --- Re-exports from httputil ---

// Context helpers
var (
	GetRequestID    = httputil.GetRequestID
	SetRequestID    = httputil.SetRequestID
	SetUserClaims   = httputil.SetUserClaims
	GetUserClaims   = httputil.GetUserClaims
	SetRegion       = httputil.SetRegion
	GetRegion       = httputil.GetRegion
	SetAPIKeyID     = httputil.SetAPIKeyID
	GetAPIKeyID     = httputil.GetAPIKeyID
	SetUser         = httputil.SetUser
	GetUser         = httputil.GetUser
	BindAndValidate = httputil.BindAndValidate
	GetClientIP     = httputil.GetClientIP
)

// Response types

type (
	// Response aliases httputil.Response, the unified API response envelope.
	Response = httputil.Response
	// PageData aliases httputil.PageData, the offset-based pagination
	// payload of a response.
	PageData = httputil.PageData
	// CursorPageData aliases httputil.CursorPageData, the cursor-based
	// (keyset) pagination payload of a response.
	CursorPageData = httputil.CursorPageData
)

// Response functions
var (
	Success           = httputil.Success
	Fail              = httputil.Fail
	FailWithCode      = httputil.FailWithCode
	FailWithData      = httputil.FailWithData
	SuccessPage       = httputil.SuccessPage
	SuccessPageCursor = httputil.SuccessPageCursor
)
