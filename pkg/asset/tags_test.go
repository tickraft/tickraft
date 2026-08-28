// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package asset

import "testing"

// TestTagsFromMetadata covers the best-effort decode of an asset's JSON
// metadata blob into the tag map rule expressions consume.
func TestTagsFromMetadata(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want map[string]string
	}{
		{"empty blob yields empty map", "", map[string]string{}},
		{"valid blob decodes", `{"env":"prod"}`, map[string]string{"env": "prod"}},
		{"malformed blob yields empty map", `not-json`, map[string]string{}},
		{"non-object blob yields empty map", `["env"]`, map[string]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TagsFromMetadata(tc.raw)
			if len(got) != len(tc.want) {
				t.Fatalf("TagsFromMetadata(%q) = %v, want %v", tc.raw, got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("TagsFromMetadata(%q)[%q] = %q, want %q", tc.raw, k, got[k], v)
				}
			}
		})
	}
}
