// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package handler

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNoPkgAuthImport enforces the architectural constraint that the handler
// package must NOT directly import pkg/auth or pkg/auth/jwt. The handler
// package is the route layer; it receives middleware and
// services as opaque interfaces from the caller (internal/api/router/router.go) so
// that no auth implementation details leak into it. If this test fails,
// somebody has added a direct import of pkg/auth or pkg/auth/jwt to a
// non-test file under pkg/api/handler and the abstraction has been broken.
//
// We check DIRECT imports only (go list -f '{{.Imports}}' .), not transitive
// dependencies: the handler package inevitably pulls pkg/auth transitively
// through pkg/api -> internal/api, and that chain is owned by the
// internal api package, not the handler package.
func TestNoPkgAuthImport(t *testing.T) {
	out, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Imports "\n"}}`, ".").Output()
	if err != nil {
		t.Skipf("go list failed (skipping): %v", err)
	}
	forbidden := []string{
		"github.com/tickraft/tickraft/pkg/auth",
		"github.com/tickraft/tickraft/pkg/auth/jwt",
	}
	imports := strings.Split(string(out), "\n")
	for _, imp := range imports {
		imp = strings.TrimSpace(imp)
		for _, f := range forbidden {
			if imp == f {
				t.Fatalf("handler package must not directly import %s "+
					"(handler must remain auth-implementation-agnostic)", f)
			}
		}
	}
}
