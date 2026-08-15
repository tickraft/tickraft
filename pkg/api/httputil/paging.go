// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httputil

import (
	"net/http"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"go.uber.org/zap"
)

// Field length limits shared by API handlers. They mirror the varchar
// constraints enforced by the GORM models so that over-length input is
// rejected with a 400 Bad Request at the API layer instead of surfacing as a
// 500 from the database.
const (
	// MaxNameLength is the maximum allowed length for human-facing name fields
	// (task name, alert rule name, asset name, telemetry task name). It matches
	// the varchar(255) constraint enforced by the GORM models.
	MaxNameLength = 255

	// MaxDescriptionLength is the maximum allowed length for human-facing
	// description fields. It matches the varchar(1024) constraint.
	MaxDescriptionLength = 1024
)

// Paging defaults and caps.
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// ParseID extracts the :id path parameter as an int64. On failure it writes a
// 400 response and returns ok=false so the caller can return early.
func ParseID(arc *app.RequestContext) (int64, bool) {
	id, err := strconv.ParseInt(arc.Param("id"), 10, 64)
	if err != nil {
		FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "invalid id parameter")
		return 0, false
	}
	return id, true
}

// ParsePaging extracts the page and page_size query parameters with sensible
// defaults and an enforced upper bound. page defaults to 1 and page_size to 20
// when missing or non-positive. page_size is clamped to maxPageSize (100); when
// a client requests more than the cap, the value is reduced and a warning is
// logged so the truncation is observable by operators.
func ParsePaging(arc *app.RequestContext) (int, int) {
	page, _ := strconv.Atoi(arc.Query("page"))
	if page <= 0 {
		page = 1
	}
	size, _ := strconv.Atoi(arc.Query("page_size"))
	if size <= 0 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		zap.L().Warn("page size exceeds max, clamped",
			zap.Int("requested", size),
			zap.Int("max", maxPageSize),
		)
		size = maxPageSize
	}
	return page, size
}

// ClampPaging normalizes page/size parameters for list endpoints.
// page starts at 1; size defaults to 20 when non-positive and is capped at 100.
func ClampPaging(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}
	return page, size
}

// PageWindow returns the [start, end) index window for the given page/size
// over a collection of total items. start may be >= total to signal an empty
// result; callers must guard against that before slicing.
func PageWindow(page, size, total int) (int, int) {
	start := (page - 1) * size
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	return start, end
}
