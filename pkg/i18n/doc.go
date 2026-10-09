// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package i18n provides the internationalization primitives shared by the
// kernel and callers.
//
// The package exposes four core abstractions:
//
//   - Locale: a BCP 47 language tag with language, region and script components
//     plus a text-direction classifier (LTR/RTL).
//   - Bundle: an immutable store of message keys for a single locale.
//   - Translator: a per-locale message resolver with fallback to the default
//     locale when a key is missing.
//   - Registry: a concurrent-safe registry of Translators that implements the
//     fallback chain (exact match -> language-only -> default locale "zh-Hans").
//
// Asset files are loaded by the Loader (see loader.go) from an fs.FS and
// may be hot-reloaded at runtime via fsnotify. The package intentionally has
// no dependency on any extension code so the runtime can compile
// and run standalone.
package i18n
