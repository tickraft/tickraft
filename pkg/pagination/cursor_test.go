// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package pagination

import (
	"strconv"
	"testing"
)

func TestCursorEncodeDecodeRoundTrip(t *testing.T) {
	c := Cursor{Column: "id", Value: "42", Direction: Desc}
	tok, err := c.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if tok == "" {
		t.Fatal("expected non-empty token for non-empty value")
	}
	got, err := DecodeCursor(tok)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Column != c.Column || got.Value != c.Value || got.Direction != c.Direction {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, c)
	}
}

func TestCursorEncodeEmptyValue(t *testing.T) {
	c := Cursor{Column: "id", Direction: Desc}
	tok, err := c.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if tok != "" {
		t.Fatalf("expected empty token for empty value, got %q", tok)
	}
}

func TestDecodeCursorEmptyToken(t *testing.T) {
	c, err := DecodeCursor("")
	if err != nil {
		t.Fatalf("decode empty: %v", err)
	}
	if c != (Cursor{}) {
		t.Fatalf("expected zero cursor, got %+v", c)
	}
}

func TestDecodeCursorMalformed(t *testing.T) {
	if _, err := DecodeCursor("not-base64!!!"); err == nil {
		t.Fatal("expected error for malformed token")
	}
}

func TestClampSize(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, DefaultSize},
		{-5, DefaultSize},
		{1, 1},
		{50, 50},
		{100, 100},
		{101, MaxSize},
		{1000, MaxSize},
	}
	for _, tc := range cases {
		if got := ClampSize(tc.in); got != tc.want {
			t.Errorf("ClampSize(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestPageRequestIsKeyset(t *testing.T) {
	if !(PageRequest{Cursor: "abc"}.IsKeyset()) {
		t.Fatal("cursor present should be keyset")
	}
	if !(PageRequest{}.IsKeyset()) {
		t.Fatal("empty request should be keyset (first page)")
	}
	if (PageRequest{Page: 3}.IsKeyset()) {
		t.Fatal("page>0 without cursor should be offset mode")
	}
}

func TestNextCursor_EmptyRows(t *testing.T) {
	cur := Cursor{Column: "id", Direction: Desc}
	tok, err := NextCursor[int64](cur, nil, func(v int64) string { return strconv.FormatInt(v, 10) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "" {
		t.Fatalf("expected empty token for empty rows, got %q", tok)
	}
}

func TestNextCursor_FullPage(t *testing.T) {
	cur := Cursor{Column: "id", Direction: Desc}
	rows := []int64{100, 99, 98}
	tok, err := NextCursor(cur, rows, func(v int64) string { return strconv.FormatInt(v, 10) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok == "" {
		t.Fatal("expected non-empty token for full page")
	}
	decoded, err := DecodeCursor(tok)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Value != "98" {
		t.Fatalf("expected last value 98, got %q", decoded.Value)
	}
	if decoded.Direction != Desc {
		t.Fatalf("expected Desc, got %v", decoded.Direction)
	}
}

func TestNextCursorForSize_ShortPage(t *testing.T) {
	cur := Cursor{Column: "id", Direction: Asc}
	rows := []int64{1, 2}
	tok, err := NextCursorForSize(cur, rows, 3, func(v int64) string { return strconv.FormatInt(v, 10) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "" {
		t.Fatalf("expected empty token for short page, got %q", tok)
	}
}

func TestInt64ValueRoundTrip(t *testing.T) {
	v := int64(-12345)
	s := Int64Value(v)
	got, err := ParseInt64Value(s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got != v {
		t.Fatalf("round trip: got %d want %d", got, v)
	}
}
