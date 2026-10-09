// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

package executor

import (
	"testing"
)

// TestExpandDispatchVars pins the dispatch variable substitution: the
// {{task_ref}} placeholder replaced everywhere it occurs (the run handle is
// renderable in both modes) and configs without placeholders passing through
// untouched.
func TestExpandDispatchVars(t *testing.T) {
	cases := []struct {
		config  string
		taskRef string
		want    string
	}{
		{``, "r-9", ``},
		{`{"url":"https://example.local/hook"}`, "r-9", `{"url":"https://example.local/hook"}`},
		{`{{task_ref}}`, "r-9", `r-9`},
		{`{{task_ref}}/{{task_ref}}`, "r-9", `r-9/r-9`},
		{`{"url":"https://example.local/hook?ref={{task_ref}}"}`, "r-9",
			`{"url":"https://example.local/hook?ref=r-9"}`},
		{`{"cmd":"report --ref {{task_ref}}"}`, "r-9", `{"cmd":"report --ref r-9"}`},
		// A Mode B run still carries a run handle, so the same placeholder
		// renders there too.
		{`{"ref":"{{task_ref}}"}`, "mode-b-run", `{"ref":"mode-b-run"}`},
		// Unrelated mustache-style keys are not touched.
		{`{"body":"{{output}}"}`, "r-9", `{"body":"{{output}}"}`},
	}
	for _, tc := range cases {
		if got := expandDispatchVars(tc.config, tc.taskRef); got != tc.want {
			t.Errorf("expandDispatchVars(%q, ref=%q) = %q, want %q", tc.config, tc.taskRef, got, tc.want)
		}
	}
}
