// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package api

import "testing"

// TestIsAPIPath verifies that isAPIPath correctly classifies API and
// non-API paths. API paths start with /api/ or are exactly /health or
// /healthz; everything else (including the SPA root / and asset paths) is
// treated as a non-API path handled by the SPA fallback.
func TestIsAPIPath(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		expect bool
	}{
		{"api prefix", "/api/foo", true},
		{"api v1 tasks", "/api/v1/tasks", true},
		{"health exact", "/health", true},
		{"healthz exact", "/healthz", true},
		{"root path", "/", false},
		{"dashboard", "/dashboard", false},
		{"assets js", "/assets/main.js", false},
		{"favicon", "/favicon.ico", false},
		{"health subpath", "/health/sub", false},
		{"api without trailing slash", "/api", false},
		{"empty path", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isAPIPath(c.path)
			if got != c.expect {
				t.Errorf("isAPIPath(%q) = %v, want %v", c.path, got, c.expect)
			}
		})
	}
}

// TestContentTypeFor verifies that contentTypeFor returns the correct MIME
// type for common frontend asset extensions, falling back to
// application/octet-stream for unknown types. The text/* and application/json
// variants include the charset=utf-8 suffix, matching the implementation.
func TestContentTypeFor(t *testing.T) {
	cases := []struct {
		name   string
		file   string
		expect string
	}{
		{"html", "index.html", "text/html; charset=utf-8"},
		{"htm", "page.htm", "text/html; charset=utf-8"},
		{"css", "main.css", "text/css; charset=utf-8"},
		{"js", "app.js", "application/javascript; charset=utf-8"},
		{"mjs", "module.mjs", "application/javascript; charset=utf-8"},
		{"json", "data.json", "application/json; charset=utf-8"},
		{"svg", "logo.svg", "image/svg+xml"},
		{"png", "icon.png", "image/png"},
		{"jpg", "photo.jpg", "image/jpeg"},
		{"jpeg", "photo.jpeg", "image/jpeg"},
		{"gif", "anim.gif", "image/gif"},
		{"ico", "favicon.ico", "image/x-icon"},
		{"woff", "font.woff", "font/woff"},
		{"woff2", "font.woff2", "font/woff2"},
		{"ttf", "font.ttf", "font/ttf"},
		{"eot", "font.eot", "application/vnd.ms-fontobject"},
		{"map", "app.js.map", "application/json; charset=utf-8"},
		{"unknown ext", "file.xyz", "application/octet-stream"},
		{"no extension", "README", "application/octet-stream"},
		{"empty", "", "application/octet-stream"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := contentTypeFor(c.file)
			if got != c.expect {
				t.Errorf("contentTypeFor(%q) = %q, want %q", c.file, got, c.expect)
			}
		})
	}
}
