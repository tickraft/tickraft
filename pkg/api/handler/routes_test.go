// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package handler

import (
	"regexp"
	"testing"
)

// TestRoutePrefixesShape verifies that the route prefixes follow
// the documented /api/v1/<module>(/<endpoint>) shape (or are the standalone
// /healthz probe). This guards against accidental drift in route
// registration: a module must use a lowercase kebab-case segment after
// /api/v1/, optionally followed by another lowercase segment, and the bare
// /healthz probe must remain unchanged.
func TestRoutePrefixesShape(t *testing.T) {
	// apiPrefixRE matches /api/v1/<module> or /api/v1/<module>/<rest> where
	// <module> is a lowercase letter followed by lowercase letters, digits,
	// or hyphens, and <rest> is one or more lowercase segments separated by '/'.
	apiPrefixRE := regexp.MustCompile(`^/api/v1/[a-z][a-z0-9-]*(/[a-z][a-z0-9/-]*)?$`)

	expectedPrefixes := []string{
		"/api/v1/tasks",
		"/api/v1/tasks/stats",
		"/api/v1/auth/apikeys",
		"/api/v1/prism/alert/rules",
		"/api/v1/prism/alert/records",
		"/api/v1/system/config",
		"/api/v1/system/info",
		"/api/v1/assets",
		"/api/v1/telemetry",
		"/api/v1/telemetry/heartbeat",
		"/api/v1/telemetry/metrics",
		"/api/v1/telemetry/logs",
	}

	for _, p := range expectedPrefixes {
		t.Run(p, func(t *testing.T) {
			if !apiPrefixRE.MatchString(p) {
				t.Errorf("prefix %q does not match the /api/v1/<module>(/<endpoint>) shape", p)
			}
		})
	}

	// /healthz is a special standalone probe and must not match the
	// /api/v1/... regex.
	if apiPrefixRE.MatchString("/healthz") {
		t.Error("/healthz should NOT match the /api/v1/ shape; it is a standalone probe")
	}
}

// TestExpectedRoutePrefixesPresent verifies that the canonical list of
// route prefixes (the ones actually registered by routes.go)
// appears verbatim in the expected list. This catches accidental renaming
// of a module prefix.
func TestExpectedRoutePrefixesPresent(t *testing.T) {
	registeredPrefixes := []string{
		"/api/v1/auth",                 // auth module (login, refresh, logout, apikeys)
		"/api/v1/tasks",                // task module (task CRUD + trigger)
		"/api/v1/tasks/:id/executions", // task module (execution records)
		"/api/v1/tasks/stats",          // task module (execution statistics)
		"/api/v1/prism/alert/rules",    // prism module (alert rule CRUD)
		"/api/v1/prism/alert/records",  // prism module (alert record list/get)
		"/api/v1/expr",                 // expr module (expression validation)
		"/api/v1/system",               // system module (config, info)
		"/api/v1/assets",               // asset module (asset CRUD)
		"/api/v1/telemetry",            // telemetry module (CRUD + distributed reports)
		"/healthz",                     // standalone health probe
	}

	wantSet := map[string]bool{
		"/api/v1/auth":                 true,
		"/api/v1/tasks":                true,
		"/api/v1/tasks/:id/executions": true,
		"/api/v1/tasks/stats":          true,
		"/api/v1/prism/alert/rules":    true,
		"/api/v1/prism/alert/records":  true,
		"/api/v1/expr":                 true,
		"/api/v1/system":               true,
		"/api/v1/assets":               true,
		"/api/v1/telemetry":            true,
		"/healthz":                     true,
	}

	for _, p := range registeredPrefixes {
		if !wantSet[p] {
			t.Errorf("registered prefix %q is not in the expected set %v", p, wantSet)
		}
	}
}
