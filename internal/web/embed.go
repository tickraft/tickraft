// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package web

import (
	"embed"
	"io/fs"
)

// distFS holds the embedded frontend build output. The //go:embed directive
// requires the dist directory to exist at compile time; a .gitkeep placeholder
// ensures the directory is tracked by git even when empty.
//
//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded frontend filesystem rooted at "dist".
// The returned fs.FS serves files directly from the compiled binary with no
// disk dependency, making it suitable for single-binary deployments.
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
