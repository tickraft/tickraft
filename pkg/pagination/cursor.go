// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package pagination

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"

	"github.com/bytedance/sonic"
)

// Direction selects the ordering of the keyset column.
type Direction int

const (
	// Asc ascending order: the next page resumes after the largest
	// value seen so far (column > last_value).
	Asc Direction = iota
	// Desc descending order: the next page resumes after the smallest
	// value seen so far (column < last_value).
	Desc
)

// Cursor describes a keyset position. It is the in-memory representation
// of an opaque cursor token exchanged with callers.
//
// Column is the name of the ordered column as it appears in the SQL
// query (e.g. "id", "created_at"). Value is the last-seen value of that
// column, serialized as a string so the same type serves int64 IDs,
// timestamps and string keys. Direction selects ascending or descending
// order.
//
// For composite orderings such as `created_at DESC, id DESC` (used to
// break ties when many rows share a timestamp), set Column2 to the
// tie-breaker column and Value2 to its last-seen value. When Column2 is
// empty the keyset is single-column. The tie-breaker must use the same
// Direction as the primary column.
type Cursor struct {
	Column    string    `json:"column"`
	Value     string    `json:"value,omitempty"`
	Direction Direction `json:"direction"`
	// Column2 is the optional tie-breaker column for composite orderings.
	Column2 string `json:"column2,omitempty"`
	// Value2 is the last-seen value of Column2.
	Value2 string `json:"value2,omitempty"`
}

// Encode serializes the cursor into an opaque, URL-safe token. The
// encoding is base64url(JSON(Cursor)). An empty cursor (no Value) is
// represented by an empty string to signal "first page".
func (c Cursor) Encode() (string, error) {
	if c.Value == "" && c.Value2 == "" {
		return "", nil
	}
	data, err := sonic.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("pagination: encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// DecodeCursor parses an opaque cursor token produced by [Cursor.Encode].
// An empty token decodes to a zero-value Cursor (first page). A malformed
// token returns an error so callers can reject bad input rather than
// silently returning the first page.
func DecodeCursor(token string) (Cursor, error) {
	if token == "" {
		return Cursor{}, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return Cursor{}, fmt.Errorf("pagination: decode cursor: %w", err)
	}
	var c Cursor
	if err = sonic.Unmarshal(data, &c); err != nil {
		return Cursor{}, fmt.Errorf("pagination: decode cursor: %w", err)
	}
	return c, nil
}

// PageRequest describes a page to fetch. It supports two modes:
//
//   - Keyset mode (preferred for deep pagination): set Cursor to the
//     opaque next-page token returned by the previous request. Leave
//     Page zero.
//   - Offset mode (default): leave Cursor empty and set Page to a 1-based
//     page number. The first request in keyset mode also uses an empty
//     Cursor with Page zero.
//
// Size is the maximum number of rows to return. Callers should clamp it
// to an upper bound before constructing the request.
type PageRequest struct {
	// Cursor is the opaque next-page token. Empty means the first page.
	Cursor string
	// Page is the 1-based offset page number, used only when Cursor is
	// empty and the caller wants offset semantics. When both Cursor and
	// Page are zero the first page is returned in keyset mode.
	Page int
	// Size is the maximum number of rows to return.
	Size int
}

// IsKeyset reports whether the request is in keyset mode. A request is
// keyset when a non-empty Cursor token is present, or when Page is zero
// (first page of a keyset stream).
func (r PageRequest) IsKeyset() bool {
	return r.Cursor != "" || r.Page <= 0
}

// PageResult holds a page of items plus the cursor for the next page.
// NextCursor is empty when the page was the last one (fewer than Size
// rows returned) or when keyset pagination is not in use.
type PageResult[T any] struct {
	// Items is the page contents.
	Items []T
	// Total is the total row count, when known. Callers that cannot
	// cheaply compute a total (e.g. keyset-only endpoints) may leave it
	// zero.
	Total int64
	// NextCursor is the opaque token to pass as PageRequest.Cursor for
	// the next page. Empty when there is no next page.
	NextCursor string
}

// ErrInvalidCursor is returned when a cursor token cannot be decoded or
// does not match the expected column.
var ErrInvalidCursor = errors.New("pagination: invalid cursor")

// MaxSize is the default upper bound on page size. Callers may clamp to
// a tighter bound; this constant exists so clamping is consistent
// across endpoints.
const MaxSize = 100

// DefaultSize is the page size used when PageRequest.Size is not
// positive.
const DefaultSize = 20

// ClampSize clamps size to the [1, MaxSize] range, defaulting to
// DefaultSize when non-positive.
func ClampSize(size int) int {
	if size <= 0 {
		return DefaultSize
	}
	if size > MaxSize {
		return MaxSize
	}
	return size
}

// Int64Value converts an int64 key value to the string form stored in a
// [Cursor]. It is provided as a convenience for the common ID-keyset
// case.
func Int64Value(v int64) string {
	return strconv.FormatInt(v, 10)
}

// ParseInt64Value parses a cursor value back into an int64. It is the
// inverse of [Int64Value].
func ParseInt64Value(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
