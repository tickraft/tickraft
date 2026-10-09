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

// TestCustomFieldCount covers the metered unit of the custom-field quota:
// preset metadata keys are free, every other key counts as one custom
// field, and values of any JSON type count (unlike TagsFromMetadata,
// which only decodes string values).
func TestCustomFieldCount(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"empty blob counts zero", "", 0},
		{"malformed blob counts zero", `not-json`, 0},
		{"non-object blob counts zero", `["rack"]`, 0},
		{"null object counts zero", `null`, 0},
		{"preset-only blob counts zero",
			`{"business_line":"core","project":"x","owner":"ops","priority":"p1","environment":"prod"}`, 0},
		{"custom keys count", `{"rack":"r1","zone":"z1"}`, 2},
		{"mixed preset and custom counts customs only",
			`{"environment":"prod","rack":"r1","comment":"c"}`, 2},
		{"non-string values count", `{"replicas":3,"ha":true}`, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CustomFieldCount(tc.raw); got != tc.want {
				t.Errorf("CustomFieldCount(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}
