// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import (
	"embed"
	"io/fs"
)

// resourceFS holds the embedded resource files for the i18n bundle. callers
// merge additional resources on top of these at startup via Loader.LoadToRegistry.
//
//go:embed resource/*.toml
var resourceFS embed.FS

// EmbeddedFS returns the embedded resource filesystem. The returned fs.FS
// is rooted at the "resource" directory and contains the
// locale packs (en-US.toml, zh-Hans.toml) registered under their standard BCP 47 tags.
// callers merge additional locale packs (zh-Hant, en-GB, ar, ja,
// de, fr, es, ru, ko) on top of these at startup via Loader.LoadToRegistry.
//
// Callers should pass the returned fs.FS to Loader.LoadToRegistry to
// populate a Registry at startup:
//
//	loader := i18n.NewLoader(logger)
//	registry := i18n.NewRegistry(logger)
//	if err := loader.LoadToRegistry(i18n.EmbeddedFS(), registry); err != nil {
//	    return fmt.Errorf("load i18n resources: %w", err)
//	}
func EmbeddedFS() fs.FS {
	sub, err := fs.Sub(resourceFS, "resource")
	if err != nil {
		// fs.Sub on an embedded filesystem with a valid path never
		// returns an error. Panicking here would violate the "no panic
		// in business logic" rule, so fall back to the root filesystem
		// which is always valid.
		return resourceFS
	}
	return sub
}
