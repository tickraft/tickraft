// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httputil

import (
	"strings"
	"testing"

	"github.com/tickraft/tickraft/pkg/types"
)

// TestResponseStatus pins the judgment matrix shared by the http and webhook
// executors. Both directions of the expect_status override are covered: an
// exact non-2xx match is normal and a mismatching 2xx is abnormal.
func TestResponseStatus(t *testing.T) {
	cases := []struct {
		name   string
		expect int
		code   int
		want   types.AssetStatus
	}{
		{"default 200 is normal", 0, 200, types.AssetStatusNormal},
		{"default 204 is normal", 0, 204, types.AssetStatusNormal},
		{"default 299 is normal", 0, 299, types.AssetStatusNormal},
		{"default 199 is abnormal", 0, 199, types.AssetStatusAbnormal},
		{"default 300 is abnormal", 0, 300, types.AssetStatusAbnormal},
		{"default 503 is abnormal", 0, 503, types.AssetStatusAbnormal},
		{"expect exact non-2xx match is normal", 503, 503, types.AssetStatusNormal},
		{"expect mismatching 2xx is abnormal", 200, 204, types.AssetStatusAbnormal},
		{"expect mismatching non-2xx is abnormal", 201, 200, types.AssetStatusAbnormal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResponseStatus(tc.expect, tc.code); got != tc.want {
				t.Errorf("ResponseStatus(%d, %d): got %q, want %q", tc.expect, tc.code, got, tc.want)
			}
		})
	}
}

// TestReadBodyTruncates verifies the body cap: content beyond the limit is
// dropped instead of buffered.
func TestReadBodyTruncates(t *testing.T) {
	r := strings.NewReader(strings.Repeat("x", ProbeBodyLimit+100))
	if got := ReadBody(r, ProbeBodyLimit); len(got) != ProbeBodyLimit {
		t.Errorf("ReadBody length: got %d, want %d", len(got), ProbeBodyLimit)
	}
	empty := strings.NewReader("")
	if got := ReadBody(empty, ProbeBodyLimit); len(got) != 0 {
		t.Errorf("ReadBody empty: got %d bytes, want 0", len(got))
	}
}
