// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package template implements the alert template system for the tickraft
// alerting pipeline.
//
// A Template is a self-contained alert rendering definition that bundles:
//
//   - An ID (e.g. "cpu_high") for lookup.
//   - A Name and Description for display in management UIs.
//   - An AlertType ("metric", "log", "generic") that constrains which
//     [github.com/tickraft/tickraft/pkg/prism/alert.Event] values the
//     template can render.
//   - A Variables list declaring the substitution keys the template body
//     expects (e.g. "metric_name", "current_value").
//   - Translations: a per-locale map of style-scoped template strings. Each
//     locale provides a "title.<style>" and "description.<style>" template
//     for the concise/detailed/technical styles.
//   - Styles: the style variants the template supports.
//   - ChannelHints: optional hints for which channels (email/sms/im) the
//     template is optimized for.
//
// A Library is a registry of Templates. The kernel ships a
// built-in library (see NewBuiltinLibrary); callers may extend
// it with tenant-specific custom templates via the Store SPI.
//
// A Renderer turns an [alert.Event] into an
// [i18n.FormattedMessage] by looking up a template in a Library, selecting
// the translation for the requested locale (with fallback), and substituting
// variables using text/template. The default Renderer (NewRenderer) is the
// canonical implementation; extended channels may wrap it to add
// channel-specific fields.
//
// All public types are safe for concurrent use after construction.
package template
