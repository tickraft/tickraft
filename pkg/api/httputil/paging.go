// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httputil

import (
	"math"
	"net/http"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/pagination"
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

// ParsePaging extracts the page and size query parameters. Missing or
// empty parameters fall back to the defaults (page 1, size 20). Explicitly
// provided but invalid values — non-numeric, page < 1, size < 1 or size
// above pagination.MaxSize — are rejected with a 400 response and ok=false
// so the caller can return early.
func ParsePaging(arc *app.RequestContext) (page, size int, ok bool) {
	page, ok = queryInt(arc, "page", 1, 1, maxPage)
	if !ok {
		return 0, 0, false
	}
	size, ok = queryInt(arc, "size", pagination.DefaultSize, 1, pagination.MaxSize)
	if !ok {
		return 0, 0, false
	}
	return page, size, true
}

// ParsePageRequest extracts pagination in one shot for handlers that
// support both keyset and offset modes: when the cursor parameter is
// present (even empty) the request runs in keyset mode with that token;
// otherwise it runs in offset mode with page/size as in [ParsePaging].
// On invalid input a 400 response is written and ok=false is returned.
func ParsePageRequest(arc *app.RequestContext) (pagination.PageRequest, bool) {
	if _, has := arc.GetQuery("cursor"); has {
		req := pagination.PageRequest{Cursor: arc.Query("cursor")}
		size, ok := queryInt(arc, "size", pagination.DefaultSize, 1, pagination.MaxSize)
		if !ok {
			return pagination.PageRequest{}, false
		}
		req.Size = size
		return req, true
	}
	page, size, ok := ParsePaging(arc)
	if !ok {
		return pagination.PageRequest{}, false
	}
	return pagination.PageRequest{Page: page, Size: size}, true
}

// maxPage is the upper bound accepted for the page parameter; it only
// guards against integer overflow in the offset computation.
const maxPage = math.MaxInt32

// queryInt parses an optional integer query parameter. An absent or empty
// value yields def. A present but non-numeric value, or one outside
// [lo, hi], writes a 400 response and returns ok=false.
func queryInt(arc *app.RequestContext, name string, def, lo, hi int) (int, bool) {
	raw := arc.Query(name)
	if raw == "" {
		return def, true
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < lo || v > hi {
		FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "invalid "+name)
		return 0, false
	}
	return v, true
}

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
