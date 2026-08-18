// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package builtin embeds the built-in alert template TOML files so the
// default template library ships inside the binary without external
// file dependencies.
package builtin

import "embed"

// templatesFS holds the built-in alert template TOML files. The files are
// embedded at compile time so the binary has no external file dependencies
// for the default template library.
//
//go:embed templates/*.toml
var templatesFS embed.FS

// EmbeddedFS returns the embedded filesystem rooted at the builtin package.
// Callers walk the filesystem to discover and parse template files.
func EmbeddedFS() embed.FS {
	return templatesFS
}
