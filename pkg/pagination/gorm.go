// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package pagination

import (
	"fmt"

	"gorm.io/gorm"
)

// Apply attaches the keyset (or offset) predicate to a GORM query based
// on req and cur, and returns the scoped query ready for .Find.
//
// Behavior:
//
//   - Keyset mode (req.IsKeyset()): when cur.Value is non-empty, a
//     range predicate is added so the query resumes after the cursor:
//     `column < value` for [Desc] order, `column > value` for [Asc]
//     order. The ORDER BY clause is set to `column DESC` or `column ASC`
//     and Limit is set to req.Size. No OFFSET is emitted, so the query
//     is an index range scan whose cost is independent of depth.
//   - Offset mode (!req.IsKeyset()): ORDER BY column + LIMIT + OFFSET
//     are applied using the 1-based Page number from the request.
//
// For composite orderings (cur.Column2 set), the predicate is the
// standard tuple comparison:
//
//	(column < v1) OR (column = v1 AND column2 < v2)   [Desc]
//	(column > v1) OR (column = v1 AND column2 > v2)   [Asc]
//
// which uses an index range scan on the composite index.
//
// cur.Column selects the ordered column; cur.Direction selects the
// order. When the cursor has no Value (first page) only the ORDER BY +
// LIMIT are applied. cur.Column must be non-empty; otherwise Apply
// returns an error to avoid an unbounded scan.
//
// The caller is responsible for adding any tenant or filter predicates
// to the query before calling Apply, e.g.:
//
//	q := pagination.Apply(
//	    dbc.WithContext(ctx).Model(&Asset{}).Where("tenant_id = ?", tenantID),
//	    req, pagination.Cursor{Column: "id", Direction: pagination.Desc})
func Apply(dbc *gorm.DB, req PageRequest, cur Cursor) (*gorm.DB, error) {
	if dbc == nil {
		return nil, fmt.Errorf("pagination: nil database handle")
	}
	if cur.Column == "" {
		return nil, fmt.Errorf("pagination: cursor column is required")
	}

	size := ClampSize(req.Size)
	dir := "DESC"
	op := "<"
	if cur.Direction == Asc {
		dir = "ASC"
		op = ">"
	}
	orderClause := cur.Column + " " + dir
	if cur.Column2 != "" {
		orderClause += ", " + cur.Column2 + " " + dir
	}

	if !req.IsKeyset() {
		// Offset mode (default).
		page := req.Page
		page = max(page, 1)
		return dbc.Order(orderClause).Limit(size).Offset((page - 1) * size), nil
	}

	// Keyset mode: range scan resuming after the cursor value.
	if req.Cursor != "" {
		decoded, err := DecodeCursor(req.Cursor)
		if err != nil {
			return nil, err
		}
		if decoded.Column != cur.Column || decoded.Direction != cur.Direction || decoded.Column2 != cur.Column2 {
			return nil, fmt.Errorf("%w: column/direction mismatch (expected %s/%s %v)",
				ErrInvalidCursor, cur.Column, cur.Column2, cur.Direction)
		}
		if decoded.Value != "" {
			dbc = applyKeysetPredicate(dbc, cur, decoded, op)
		}
	}

	return dbc.Order(orderClause).Limit(size), nil
}

// applyKeysetPredicate adds the range predicate that resumes after the
// decoded cursor value. For a single-column keyset this is a simple
// comparison; for a composite keyset it is the tuple comparison
// described in [Apply].
func applyKeysetPredicate(dbc *gorm.DB, cur, decoded Cursor, op string) *gorm.DB {
	if cur.Column2 == "" || decoded.Value2 == "" {
		return dbc.Where(fmt.Sprintf("%s %s ?", cur.Column, op), decoded.Value)
	}
	// Composite keyset: (col op v1) OR (col = v1 AND col2 op v2).
	return dbc.Where(
		fmt.Sprintf("(%s %s ?) OR (%s = ? AND %s %s ?)",
			cur.Column, op, cur.Column, cur.Column2, op),
		decoded.Value, decoded.Value, decoded.Value2,
	)
}

// NextCursor builds the opaque next-page cursor from the last row of the
// current page. keyOf extracts the ordered column value from a row as a
// string (use [Int64Value] for int64 IDs).
//
// If rows is empty or has fewer than req.Size entries, NextCursor
// returns an empty string (no next page). Otherwise it encodes the last
// row's key value into cur and returns the token. The caller should
// populate PageResult.NextCursor with the returned token.
//
// cur.Column and cur.Direction must be set; they are copied into the
// encoded cursor so [Apply] can validate them on the next request.
func NextCursor[T any](cur Cursor, rows []T, keyOf func(T) string) (string, error) {
	if keyOf == nil {
		return "", fmt.Errorf("pagination: keyOf is nil")
	}
	// No next page when the page was not full.
	if len(rows) == 0 {
		return "", nil
	}
	next := cur
	next.Value = keyOf(rows[len(rows)-1])
	return next.Encode()
}

// NextCursorForSize builds the next-page cursor only when the page was
// full (len(rows) == size), which is the standard signal that more rows
// may exist. Use this when the caller does not want to rely on a
// separate count.
func NextCursorForSize[T any](cur Cursor, rows []T, size int, keyOf func(T) string) (string, error) {
	if len(rows) < size {
		return "", nil
	}
	return NextCursor(cur, rows, keyOf)
}

// NextCursor2 is the composite-key variant of [NextCursor]: keyOf
// returns both the primary and tie-breaker column values for a row. Use
// it when cur.Column2 is set. The same full-page signal as NextCursor
// applies (empty rows → no next page).
func NextCursor2[T any](cur Cursor, rows []T, keyOf func(T) (string, string)) (string, error) {
	if keyOf == nil {
		return "", fmt.Errorf("pagination: keyOf is nil")
	}
	if len(rows) == 0 {
		return "", nil
	}
	next := cur
	next.Value, next.Value2 = keyOf(rows[len(rows)-1])
	return next.Encode()
}

// NextCursor2ForSize is the composite-key variant of
// [NextCursorForSize].
func NextCursor2ForSize[T any](cur Cursor, rows []T, size int, keyOf func(T) (string, string)) (string, error) {
	if len(rows) < size {
		return "", nil
	}
	return NextCursor2(cur, rows, keyOf)
}
