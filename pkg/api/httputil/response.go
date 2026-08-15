// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httputil

import (
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
)

// Response is the unified API response structure.
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// PageData is the data structure for paginated responses.
type PageData struct {
	Items    any   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// CursorPageData is the data structure for cursor-based (keyset) paginated
// responses. Unlike PageData it carries an opaque NextCursor token instead
// of a 1-based page number, which avoids the O(N) cost of OFFSET on deep
// pages. Clients pass NextCursor as the ?cursor= query parameter to fetch
// the next page; an empty NextCursor means no more rows.
type CursorPageData struct {
	Items      any    `json:"items"`
	Total      int64  `json:"total"`
	NextCursor string `json:"next_cursor"`
	PageSize   int    `json:"page_size"`
}

// Success writes a successful response with code=0.
func Success(c *app.RequestContext, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: "ok",
		Data:    data,
	})
}

// Fail writes an error response by auto-mapping the error to HTTP status and code.
// It checks: 1) ErrorCoder interface, 2) known sentinel errors, 3) fallback to 500.
func Fail(c *app.RequestContext, err error) {
	httpStatus, code, msg := mapError(err)
	c.JSON(httpStatus, Response{
		Code:    code,
		Message: msg,
		Data:    nil,
	})
}

// FailWithCode writes an error response with explicit HTTP status, code, and message.
func FailWithCode(c *app.RequestContext, httpStatus int, code int, msg string) {
	c.JSON(httpStatus, Response{
		Code:    code,
		Message: msg,
		Data:    nil,
	})
}

// FailWithData writes an error response with additional data.
func FailWithData(c *app.RequestContext, httpStatus int, code int, msg string, data interface{}) {
	c.JSON(httpStatus, Response{
		Code:    code,
		Message: msg,
		Data:    data,
	})
}

// SuccessPage writes a successful paginated response.
func SuccessPage(c *app.RequestContext, items interface{}, total int64, page int, size int) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: "ok",
		Data: PageData{
			Items:    items,
			Total:    total,
			Page:     page,
			PageSize: size,
		},
	})
}

// SuccessPageCursor writes a successful cursor-based (keyset) paginated
// response. nextCursor is the opaque token clients pass as ?cursor= to
// fetch the next page; pass an empty string when there are no more rows.
func SuccessPageCursor(c *app.RequestContext, items interface{}, total int64, nextCursor string, size int) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: "ok",
		Data: CursorPageData{
			Items:      items,
			Total:      total,
			NextCursor: nextCursor,
			PageSize:   size,
		},
	})
}
