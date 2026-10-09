// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package pagination provides shared page-request and cursor types for
// list endpoints, plus keyset (cursor-based) pagination helpers that
// avoid the O(N) cost of OFFSET on deep pages.
//
// # Why keyset pagination?
//
// OFFSET-based pagination scans and discards rows on the database side:
// requesting page 1000 of size 20 forces the engine to walk 20000 rows
// before returning the 20 the caller wants. As the page number grows the
// cost grows linearly, degrading hot list endpoints.
//
// Keyset pagination replaces OFFSET with an index range scan: the query
// remembers the last-seen value of the ordered column and resumes with a
// `WHERE column < last_value ORDER BY column DESC LIMIT size` (for
// descending order). This is an index range scan that returns in
// constant time regardless of how deep the cursor is.
//
// # Usage
//
// Build a [PageRequest] with an opaque cursor (empty for the first
// page) and a page size, then call [Apply] to attach the keyset
// predicate to a GORM query. After fetching the page, build the
// next-page cursor with [NextCursor] from the last row's key value.
//
//	c := pagination.Cursor{Column: "id", Desc: true}
//	req := pagination.PageRequest{Cursor: token, Size: 20}
//	q := pagination.Apply(dbc.WithContext(ctx).Model(&Asset{}), req, c)
//	var rows []*Asset
//	if err := q.Find(&rows).Error; err != nil { ... }
//	next, _ := pagination.NextCursor(c, rows, func(a *Asset) string {
//	    return strconv.FormatInt(a.ID, 10)
//	})
//
// The cursor token is opaque (base64url-encoded JSON) and safe to expose
// to clients.
package pagination
