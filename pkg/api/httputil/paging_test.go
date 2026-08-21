// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httputil

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
)

func newArcWithQuery(query string) *app.RequestContext {
	arc := &app.RequestContext{}
	arc.Request.SetRequestURI("/list?" + query)
	return arc
}

func TestParsePaging(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		page    int
		size    int
		wantOK  bool
		wantErr bool
	}{
		{name: "defaults", query: "", page: 1, size: 20, wantOK: true},
		{name: "explicit", query: "page=3&size=50", page: 3, size: 50, wantOK: true},
		{name: "size at max", query: "size=100", page: 1, size: 100, wantOK: true},
		{name: "page below min", query: "page=0", wantErr: true},
		{name: "size below min", query: "size=0", wantErr: true},
		{name: "size above max", query: "size=101", wantErr: true},
		{name: "size non-numeric", query: "size=abc", wantErr: true},
		{name: "page non-numeric", query: "page=x", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			arc := newArcWithQuery(tc.query)
			page, size, ok := ParsePaging(arc)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if tc.wantErr {
				if code := arc.Response.StatusCode(); code != http.StatusBadRequest {
					t.Fatalf("expected 400 response, got %d", code)
				}
				return
			}
			if page != tc.page || size != tc.size {
				t.Fatalf("got page=%d size=%d, want page=%d size=%d", page, size, tc.page, tc.size)
			}
		})
	}
}

func TestParsePageRequest(t *testing.T) {
	arc := newArcWithQuery("page=2&size=30")
	req, ok := ParsePageRequest(arc)
	if !ok || req.Page != 2 || req.Size != 30 || req.Cursor != "" || req.IsKeyset() {
		t.Fatalf("offset mode: got %+v ok=%v", req, ok)
	}

	arc = newArcWithQuery("cursor=abc&size=15")
	req, ok = ParsePageRequest(arc)
	if !ok || req.Cursor != "abc" || req.Size != 15 || !req.IsKeyset() {
		t.Fatalf("keyset mode: got %+v ok=%v", req, ok)
	}

	arc = newArcWithQuery("cursor=&size=15")
	req, ok = ParsePageRequest(arc)
	if !ok || !req.IsKeyset() {
		t.Fatalf("empty cursor should select keyset mode, got %+v ok=%v", req, ok)
	}

	arc = newArcWithQuery("cursor=abc&size=0")
	if _, ok = ParsePageRequest(arc); ok {
		t.Fatal("invalid size in keyset mode should fail")
	}
}

func TestPageDataJSONTags(t *testing.T) {
	data, err := json.Marshal(PageData{Items: []int{1}, Total: 2, Page: 3, Size: 4})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"items":[1],"total":2,"page":3,"size":4}`
	if string(data) != want {
		t.Fatalf("got %s, want %s", data, want)
	}

	data, err = json.Marshal(CursorPageData{Items: []int{1}, Total: 2, NextCursor: "c", Size: 4})
	if err != nil {
		t.Fatal(err)
	}
	want = `{"items":[1],"total":2,"next_cursor":"c","size":4}`
	if string(data) != want {
		t.Fatalf("got %s, want %s", data, want)
	}
}
